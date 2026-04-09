---
name: nebu-processor-builder
description: Interactively scaffold production-ready Nebu processors (origin/transform/sink) with proper Go module structure, CLI helpers, and optional registry entries
---

# Nebu Processor Builder

Interactively scaffold production-ready Nebu processors with proper structure and patterns.

## Description

Guides you through creating origin, transform, or sink processors for Nebu. Generates:
- Proper Go module structure (no replace directives)
- Main entry point using official CLI helpers
- Processor business logic skeleton with TODOs
- README with usage examples
- Optional registry entry with metadata

This skill ensures new processors follow current best practices and compile immediately.

## When to Use

- Creating a new Nebu processor from scratch
- Want guided setup instead of copy-pasting
- Need to follow latest conventions
- Creating a processor for the public registry

## Prerequisites

- Must be in the nebu repository (`/home/tillman/Documents/nebu`)
- Go 1.25+ installed
- Familiarity with Nebu processor types

## Workflow

### Step 1: Understand User Intent

Ask these questions in order:

**Q1: What type of processor?**
```
What type of processor are you building?
  - origin: Extract data from Stellar ledgers
  - transform: Filter or modify event streams
  - sink: Write events to external systems

Your choice:
```

**Q2: What does it do?**
```
What does your {type} processor do? (one-line description)

Example: "Filters token transfers to only USDC over $10,000"
```

**Q3: What should we call it?**
```
Processor name? (kebab-case, e.g., "large-usdc-filter")

Rules:
- Lowercase letters, numbers, hyphens only
- Start with letter, end with letter/number
- 3-50 characters
```

**Q4: Create registry entry?**
```
Should I create a registry entry in nebu-processor-registry? (y/n)

Registry entries help others discover and use your processor.
```

### Step 2: Validate Inputs

Before generating anything, verify:

1. **Check current directory:**
   ```bash
   pwd | grep -q "/nebu$" || error "Not in nebu repository"
   ```

2. **Check processor name:**
   - Matches pattern: `^[a-z][a-z0-9-]*[a-z0-9]$`
   - Not already in `examples/processors/`
   - Length 3-50 characters

3. **Confirm with user:**
   Show summary and ask for confirmation:
   ```
   Ready to generate:
   - Type: {type}
   - Name: {name}
   - Description: {description}
   - Registry entry: {yes/no}

   Proceed? (y/n)
   ```

If any validation fails, explain the issue and ask again.

### Step 3: Read Reference Patterns

Before generating code, read the appropriate instruction file:

**For origin processors:**
- Read `instructions/origin-processors.md`
- Reference processor: `examples/processors/token-transfer` or `contract-events`

**For transform processors:**
- Read `instructions/transform-processors.md`
- Reference processor: `examples/processors/amount-filter`

**For sink processors:**
- Read `instructions/sink-processors.md`
- Reference processor: `examples/processors/json-file-sink` or `nats-sink`

### Step 4: Generate Directory Structure

Create the processor directory:

```bash
mkdir -p examples/processors/{name}/cmd/{name}
```

Files to create:
1. `examples/processors/{name}/cmd/{name}/main.go`
2. `examples/processors/{name}/go.mod`
3. `examples/processors/{name}/processor.go` (for complex processors)
4. `examples/processors/{name}/README.md`

### Step 5: Generate main.go

**Pattern to follow:** Copy structure from reference processor, adapt name and description.

**Key requirements:**
- Package comment explaining what it does
- Import `github.com/withObsrvr/nebu/pkg/processor/cli`
- Version constant: `var version = "0.1.0"`
- Use appropriate CLI helper:
  - Origin: `cli.RunProtoOriginCLI()` or `cli.RunGenericOriginCLI()`
  - Transform: `cli.RunTransformCLI()`
  - Sink: `cli.RunSinkCLI()`
- Add TODO comments where business logic goes
- Include `addFlags()` function for custom flags

**Template structure:** See `templates/origin-template.go`, `templates/transform-template.go`, or `templates/sink-template.go` for the canonical per-type scaffold. The high-level shape for each type:

- **Origin**: use `cli.RunProtoOriginCLI[T]` for proto events or `cli.RunGenericOriginCLI[T]` for arbitrary Go structs. Implement `ProcessLedger(ctx, ledger)` as a *void* method and emit events via a `*processor.Emitter[T]`. Per-ledger failures are reported via `processor.ReportWarning(ctx, name, err)` (streams-never-throw).
- **Transform**: use `cli.RunTransformCLI`. The transform function signature is `func(event map[string]interface{}) map[string]interface{}` — return `nil` to filter, return the event to pass through. **There is no error return.** Log recoverable issues to stderr and return `nil` to skip.
- **Sink**: use `cli.RunSinkCLI`. The sink function signature is `func(event map[string]interface{}) error`. Returning an error logs the failure as a warning and continues to the next event (streams-never-throw). For truly fatal conditions (dropped DB connection, revoked credentials), call `os.Exit` or `panic` directly — `RunSinkCLI` does not plumb a reporter into `SinkFunc`, so `processor.ReportFatal` is not reachable from a sink.

Every config struct (`OriginConfig`, `TransformConfig`, `SinkConfig`) supports a `SchemaID` field — set it to a canonical event identifier like `"nebu.my_processor.v1"`. It's surfaced in the `--describe-json` envelope that every helper wires up automatically.

### Step 6: Generate go.mod

**Critical: NO replace directives!**

```go
module github.com/withObsrvr/nebu/examples/processors/{name}

go 1.25.4

require (
    github.com/withObsrvr/nebu v0.6.1
)

// Add type-specific dependencies:
// - Origin: github.com/stellar/go-stellar-sdk v0.5.0
// - Origin (proto): google.golang.org/protobuf v1.36.11
// - Sink (postgres): github.com/lib/pq v1.10.9
// - Sink (nats): github.com/nats-io/nats.go v1.47.0
```

Always pin a concrete `github.com/withObsrvr/nebu` version (e.g., `v0.6.1`) in the `require` block — do not write `latest`, which is not a valid Go module version. Check [the nebu releases page](https://github.com/withObsrvr/nebu/releases) for the current tag.

### Step 7: Generate README.md

Structure:
```markdown
# {name}

{user's description}

## Installation

\`\`\`bash
nebu install {name}
\`\`\`

## Usage

### Basic Usage

\`\`\`bash
# {Type-appropriate example}
\`\`\`

### Configuration

{List flags and options}

## Examples

{3-5 real-world examples}

## How It Works

{Expanded explanation}

## Dependencies

{List dependencies}

## License

MIT
```

### Step 8: Update go.work

If `/home/tillman/Documents/nebu/go.work` exists:
- Add new processor module to `use` block
- Keep alphabetically sorted

```go
use (
    .
    ./examples/processors/amount-filter
    ./examples/processors/{name}  // ← Add here
    ./examples/processors/token-transfer
    ...
)
```

### Step 9: Generate Registry Entry (if requested)

If user said yes to registry entry:

Create `/home/tillman/Documents/nebu-processor-registry/processors/{name}/description.yml`:

```yaml
processor:
  name: {name}
  type: {origin|transform|sink}
  description: {user's description}
  version: 1.0.0
  language: Go
  license: MIT
  maintainers:
    - withObsrvr

repo:
  github: withObsrvr/nebu
  ref: main

docs:
  quick_start: |
    # Install
    nebu install {name}

    # Basic usage
    {type-appropriate example}

  examples: |
    {3-5 usage examples}

  extended_description: |
    {detailed explanation}
```

### Step 10: Summarize & Guide Next Steps

Tell the user:

```
✓ Created examples/processors/{name}/
  ├── cmd/{name}/main.go ({using CLI helper})
  ├── go.mod (module: github.com/withObsrvr/nebu/examples/processors/{name})
  ├── README.md (usage examples included)
  └── {other files}

{if go.work updated}
✓ Updated go.work (added {name} module)

{if registry entry}
✓ Created nebu-processor-registry/processors/{name}/description.yml

Next steps:
1. Implement business logic (see TODOs in main.go around line X)
2. Test build: cd examples/processors/{name} && go build ./cmd/{name}
3. Test run: {type-specific test command}
4. Reference: See examples/processors/{reference} for similar patterns

The processor will:
- {Bullet list of what it does based on user's description}

Ready to implement! Let me know if you need help with the logic.
```

## Error Handling

### Processor name already exists
```
✗ Processor '{name}' already exists in examples/processors/
Please choose a different name.
```

### Not in nebu repository
```
✗ Not in nebu repository.
Current directory: {pwd}
Expected: /home/tillman/Documents/nebu

Please cd to the nebu repository and try again.
```

### Invalid processor name
```
✗ Invalid processor name: '{name}'

Processor names must:
- Use kebab-case (lowercase, hyphens only)
- Start with a letter
- End with a letter or number
- Be 3-50 characters

Examples: "my-filter", "usdc-tracker", "pg-sink"
```

## Type-Specific Notes

### Origin Processors

**When to use:**
- Extracting data from Stellar ledgers
- Reading blockchain events
- Processing transaction data

**Key patterns:**
- Implement `ProcessLedger(ctx context.Context, ledger xdr.LedgerCloseMeta)` — the method is **void** (streams-never-throw). Report per-ledger failures via `processor.ReportWarning(ctx, name, err)` and `return`. Report unrecoverable failures via `processor.ReportFatal(ctx, name, err)` and `return`.
- Use `Emitter[T]` for typed event output
- Prefer protobuf-based output (`RunProtoOriginCLI[T]`) — you get automatic JSON Schema generation in `--describe-json` for free
- Set `SchemaID` on `OriginConfig` (e.g., `"nebu.my_processor.v1"`)
- Optionally wire `Hooks []runtime.Hooks` on `OriginConfig` for progress bars, metrics, or checkpointing — see `docs/HOOKS.md` in the nebu repo

**Reference:** `token-transfer`, `contract-events`

### Transform Processors

**When to use:**
- Filtering event streams
- Modifying event data
- Enriching events with additional info
- Deduplicating streams

**Key patterns:**
- Read JSON from stdin, write to stdout
- Function signature: `func(event map[string]interface{}) map[string]interface{}` — **no error return**
- Return `nil` to filter out
- Return the event (modified or unchanged) to pass through
- There is no way to halt the pipeline from inside a transform. Log recoverable issues to stderr and `return nil` to skip the bad event.
- Set `SchemaID` on `TransformConfig`; optionally set `InputType`/`OutputType` to a zero-value proto.Message for richer `--describe-json` schemas.
- Can be stateless or stateful

**Reference:** `amount-filter`, `dedup`, `usdc-filter`

### Sink Processors

**When to use:**
- Writing to databases
- Publishing to message queues
- Sending to external APIs
- File output

**Key patterns:**
- Read JSON from stdin
- Function signature: `func(event map[string]interface{}) error`. Returning an error logs the failure as a warning and **continues to the next event** (streams-never-throw).
- Handle connection management (lazy initialization on first event, not in `main`)
- Implement batching for performance
- Flush on shutdown
- For truly fatal conditions (dropped DB connection that can't be re-established, revoked credentials), call `os.Exit` or `panic` directly. **`processor.ReportFatal` is not reachable from a sink** — `RunSinkCLI` does not plumb a reporter into `SinkFunc`.
- Set `SchemaID` on `SinkConfig` to declare the canonical event shape you expect (generic sinks that accept any JSON shape can leave this empty).

**Reference:** `json-file-sink`, `postgres-sink`, `nats-sink`

## Validation Checklist

Before completing, verify:

- [ ] Code compiles: `go build ./cmd/{name}` succeeds
- [ ] Module path correct in go.mod
- [ ] NO replace directives in go.mod
- [ ] Uses appropriate CLI helper (not custom cobra)
- [ ] Version set to "0.1.0"
- [ ] TODOs mark where business logic goes
- [ ] README has clear examples
- [ ] If registry entry: valid YAML syntax

## Tips for Success

1. **Start simple** - Get it compiling first, add features later
2. **Follow references** - Copy patterns from similar processors
3. **Use CLI helpers** - Don't reinvent argument parsing
4. **No replace directives** - Critical for `go install` to work
5. **Add helpful TODOs** - Guide future implementation
6. **Test immediately** - Verify build works before moving on

## Common Pitfalls to Avoid

❌ Custom cobra setup instead of CLI helpers
❌ Replace directives in go.mod
❌ Implementing full logic (just skeleton)
❌ Forgetting to update go.work
❌ Missing package comments
❌ Hardcoded paths or assumptions

## Resources

- Nebu docs: https://github.com/withObsrvr/nebu
- Processor registry: https://github.com/withObsrvr/nebu-processor-registry
- Instructions: See `instructions/` directory in this skill
- Examples: All processors in `examples/processors/`

## Skill Metadata

- **Version:** 1.0.0
- **Author:** OBSRVR
- **License:** MIT
- **Repository:** github.com/withObsrvr/nebu-processor-registry
