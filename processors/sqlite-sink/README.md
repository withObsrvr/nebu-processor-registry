# sqlite-sink

Write nebu events to a local SQLite database. See
[`description.yml`](description.yml) for usage, flags, and storage layout.

This is the **reference non-Go processor**: it implements the
language-agnostic nebu processor contract
([docs/PROCESSOR_CONTRACT.md](https://github.com/withObsrvr/nebu/blob/main/docs/PROCESSOR_CONTRACT.md)
in the nebu repo) in TypeScript with zero dependencies. If you want to
write a processor in something other than Go, start by reading
[`src/main.ts`](src/main.ts) — every contract rule it satisfies is marked
with a `Contract:` comment.

## Development

Requires [Bun](https://bun.sh) (SQLite is built in via `bun:sqlite`).

```bash
# Run from source
printf '{"_schema":"nebu.test.v1","meta":{"ledgerSequence":1},"x":1}\n' | \
  bun run src/main.ts --db /tmp/test.db

# Compile a native binary for the current platform
bun run build

# Cross-compile all release platforms into dist/
bun run build:all
```

## Releasing

Releases are cut by tagging: `git tag sqlite-sink-v<version> && git push --tags`.
The `release-sqlite-sink` workflow compiles all platforms, generates
`checksums.txt` (sha256), smoke-tests the Linux binary against the
contract, and publishes a GitHub release. `nebu install sqlite-sink`
downloads the matching artifact and verifies its checksum.

Keep the version in three places in sync: `src/main.ts` (`VERSION`),
`package.json`, and `description.yml` (`processor.version`, which the
install block's `{version}` resolves from).
