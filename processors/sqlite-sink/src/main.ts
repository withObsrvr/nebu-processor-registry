// sqlite-sink — write nebu events to a local SQLite database.
//
// Reference implementation of the language-agnostic nebu processor
// contract (docs/PROCESSOR_CONTRACT.md in the nebu repo) in TypeScript.
// Zero dependencies: SQLite comes from Bun's built-in bun:sqlite, and the
// release artifacts are standalone binaries from `bun build --compile`.
//
// Contract notes, mapped to code:
//   - stdout is the data plane: a sink writes nothing to stdout except
//     the --describe-json envelope. All logging goes to stderr.
//   - "streams never throw": malformed input lines are warned and
//     skipped, never crash the process.
//   - SIGINT/SIGTERM flush the pending batch, close the database, exit 0.
//   - Unrecoverable database errors are fatal: message on stderr, exit 1.

import { Database } from "bun:sqlite";
import { createInterface } from "node:readline";
import process from "node:process";

const NAME = "sqlite-sink";
const VERSION = "0.1.0";

const DESCRIBE_ENVELOPE = {
  name: NAME,
  type: "sink",
  version: VERSION,
  description: "Write nebu events to a local SQLite database",
  schema: {
    input: { type: "object" },
  },
  flags: [
    { name: "db", type: "string", required: true, description: "Path to the SQLite database file (created if missing)" },
    { name: "table", type: "string", required: false, description: "Table to insert into (default: events; created if missing)" },
    { name: "batch-size", type: "int", required: false, description: "Events per insert transaction (default 100)" },
    { name: "flush-interval-ms", type: "int", required: false, description: "Flush a partial batch after this idle time (default 1000)" },
    { name: "quiet", type: "bool", required: false, description: "Suppress the startup banner (-q)" },
  ],
  examples: [
    {
      comment: "Store a ledger range for later querying",
      command: "token-transfer --start-ledger 60200000 --end-ledger 60200100 | sqlite-sink --db transfers.db",
    },
    {
      comment: "Query the stored events with any SQLite client",
      command: "sqlite3 transfers.db \"SELECT json_extract(event, '$.transfer.assetCode') AS asset, COUNT(*) FROM events GROUP BY asset\"",
    },
  ],
};

// Contract: --describe-json must work before any flag validation, print a
// single JSON envelope to stdout, and exit 0.
if (process.argv.includes("--describe-json")) {
  process.stdout.write(JSON.stringify(DESCRIBE_ENVELOPE, null, 2) + "\n");
  process.exit(0);
}

const USAGE = `Usage: ${NAME} --db <path> [options]

Write nebu events from stdin to a SQLite database.

Events land in a table with indexed envelope columns (schema,
ledger_sequence, tx_hash) plus the full event as JSON in the event
column — query it with SQLite's json_extract().

Options:
  --db <path>               SQLite database file (created if missing)
  --table <name>            Table name (default: events)
  --batch-size <n>          Events per insert transaction (default 100)
  --flush-interval-ms <ms>  Flush partial batches after idle time (default 1000)
  -q, --quiet               Suppress the startup banner
  --describe-json           Print the processor envelope and exit
  --help                    Show this help
`;

interface Config {
  db: string;
  table: string;
  batchSize: number;
  flushIntervalMs: number;
  quiet: boolean;
}

function fail(msg: string): never {
  process.stderr.write(`${NAME}: ${msg}\n`);
  process.exit(2);
}

function parseArgs(argv: string[]): Config {
  const cfg: Config = {
    db: "",
    table: "events",
    batchSize: 100,
    flushIntervalMs: 1000,
    quiet: false,
  };

  const intFlag = (flag: string, val: string | undefined, min: number): number => {
    const n = Number(val);
    if (val === undefined || !Number.isInteger(n) || n < min) {
      fail(`${flag} requires an integer >= ${min}`);
    }
    return n;
  };

  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i];
    switch (arg) {
      case "--db":
        cfg.db = argv[++i] ?? fail("--db requires a value");
        break;
      case "--table":
        cfg.table = argv[++i] ?? fail("--table requires a value");
        break;
      case "--batch-size":
        cfg.batchSize = intFlag(arg, argv[++i], 1);
        break;
      case "--flush-interval-ms":
        cfg.flushIntervalMs = intFlag(arg, argv[++i], 1);
        break;
      case "-q":
      case "--quiet":
        cfg.quiet = true;
        break;
      case "--help":
        process.stderr.write(USAGE);
        process.exit(0);
      default:
        fail(`unknown flag ${JSON.stringify(arg)}\n\n${USAGE}`);
    }
  }

  if (!cfg.db) fail(`--db is required\n\n${USAGE}`);
  // The table name is interpolated into DDL/DML; restrict it rather than
  // trying to escape it.
  if (!/^[A-Za-z_][A-Za-z0-9_]*$/.test(cfg.table)) {
    fail(`--table must match [A-Za-z_][A-Za-z0-9_]* (got ${JSON.stringify(cfg.table)})`);
  }
  return cfg;
}

const cfg = parseArgs(process.argv.slice(2));

let db: Database;
try {
  db = new Database(cfg.db, { create: true });
  db.exec("PRAGMA journal_mode = WAL;");
  db.exec("PRAGMA synchronous = NORMAL;");
  db.exec(`
    CREATE TABLE IF NOT EXISTS "${cfg.table}" (
      id              INTEGER PRIMARY KEY,
      schema          TEXT,
      nebu_version    TEXT,
      ledger_sequence INTEGER,
      tx_hash         TEXT,
      event           TEXT NOT NULL
    );
  `);
  db.exec(`CREATE INDEX IF NOT EXISTS "idx_${cfg.table}_ledger" ON "${cfg.table}" (ledger_sequence);`);
  db.exec(`CREATE INDEX IF NOT EXISTS "idx_${cfg.table}_schema" ON "${cfg.table}" (schema);`);
} catch (err) {
  process.stderr.write(`${NAME}: fatal: cannot open ${cfg.db}: ${err instanceof Error ? err.message : err}\n`);
  process.exit(1);
}

const insert = db.prepare(
  `INSERT INTO "${cfg.table}" (schema, nebu_version, ledger_sequence, tx_hash, event) VALUES (?, ?, ?, ?, ?)`,
);

type NebuEvent = {
  _schema?: unknown;
  _nebu_version?: unknown;
  meta?: { ledgerSequence?: unknown; txHash?: unknown };
};

let eventsWritten = 0;
let transactions = 0;
let malformedLines = 0;
const MALFORMED_WARN_LIMIT = 5;

let batch: Array<{ event: NebuEvent; raw: string }> = [];

const insertBatch = db.transaction((rows: typeof batch) => {
  for (const { event, raw } of rows) {
    insert.run(
      typeof event._schema === "string" ? event._schema : null,
      typeof event._nebu_version === "string" ? event._nebu_version : null,
      typeof event.meta?.ledgerSequence === "number" ? event.meta.ledgerSequence : null,
      typeof event.meta?.txHash === "string" ? event.meta.txHash : null,
      raw,
    );
  }
});

// bun:sqlite is synchronous, so a flush cannot race the read loop or the
// timer; no serialization machinery is needed.
function flush(): void {
  if (batch.length === 0) return;
  const rows = batch;
  batch = [];
  try {
    insertBatch(rows);
    eventsWritten += rows.length;
    transactions++;
  } catch (err) {
    // Contract: an unrecoverable error is fatal — nonzero exit with a
    // message on stderr. Continuing would silently drop data.
    process.stderr.write(`${NAME}: fatal: insert failed: ${err instanceof Error ? err.message : err}\n`);
    process.exit(1);
  }
}

function summary(): void {
  const skipped = malformedLines > 0 ? `, ${malformedLines} malformed lines skipped` : "";
  process.stderr.write(
    `${NAME}: wrote ${eventsWritten} events in ${transactions} transactions to ${cfg.db}${skipped}\n`,
  );
}

if (!cfg.quiet) {
  process.stderr.write(`${NAME} v${VERSION} → ${cfg.db} table "${cfg.table}" (batch=${cfg.batchSize})\n`);
}

const rl = createInterface({ input: process.stdin, crlfDelay: Infinity });

// Contract: SIGINT/SIGTERM stop reading; the post-loop path below then
// flushes and closes cleanly.
for (const signal of ["SIGINT", "SIGTERM"] as const) {
  process.on(signal, () => {
    rl.close();
    process.stdin.destroy();
  });
}

const timer = setInterval(flush, cfg.flushIntervalMs);

for await (const line of rl) {
  const trimmed = line.trim();
  if (trimmed === "") continue;
  let event: NebuEvent;
  try {
    event = JSON.parse(trimmed);
  } catch {
    // Contract: streams never throw. Warn (bounded) and keep going.
    malformedLines++;
    if (malformedLines <= MALFORMED_WARN_LIMIT) {
      process.stderr.write(`${NAME}: warning: skipping malformed JSON line\n`);
    }
    continue;
  }
  batch.push({ event, raw: trimmed });
  if (batch.length >= cfg.batchSize) flush();
}

clearInterval(timer);
flush();
db.close();
summary();
process.exit(0);
