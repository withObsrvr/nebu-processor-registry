# nebu Community Processor Registry

**Community-contributed processors for querying live Stellar data**

This repository collects processors for [nebu](https://github.com/withObsrvr/nebu) - Unix pipes for blockchain indexing. Build custom processors, share with the community, and compose them into powerful data pipelines.

## About

nebu processors are composable Unix-style programs that process Stellar ledger data through stdin/stdout pipes. This registry allows the community to discover, share, and learn from processors built by others.

**Processor Types:**
- **Origin**: Consume ledgers from Stellar RPC, emit typed events (e.g., token transfers, Soroban events)
- **Transform**: Filter, aggregate, or transform event streams (e.g., USDC-only filter, deduplication)
- **Sink**: Write events to external systems (e.g., databases, files, message queues)

## Using Processors from this Registry

First install the nebu CLI:

```bash
go install github.com/withObsrvr/nebu/cmd/nebu@latest
export PATH="$HOME/go/bin:$PATH"
```

Then you can discover and install processors from this registry through nebu itself:

```bash
# List all available processors (built-in + community)
nebu list

# Install a community processor
nebu install <processor-name>

# Use in pipelines
<origin-processor> | <transform-processor> | <sink-processor>
```

If you cloned this repository locally and want to build a processor directly from source instead of going through `nebu install`, you can do that too:

```bash
cd processors/<processor-name>
go install ./cmd/<processor-name>
```

**Note:** cloning this registry repo alone does not install the `nebu` CLI.

## Claude Code Skills for Processor Development

This repository also hosts Claude Code skills to help you build processors interactively.

### nebu-processor-builder

Interactively scaffold production-ready Nebu processors with proper structure and patterns.

**Installation:**

**Option 1: Project-Level (Recommended)**
```bash
# In your nebu repository
mkdir -p .claude/skills
cd .claude/skills
git clone https://github.com/withObsrvr/nebu-processor-registry.git
ln -s nebu-processor-registry/skills/nebu-processor-builder nebu-processor-builder
```

**Option 2: Personal**
```bash
# In your home directory
mkdir -p ~/.claude/skills
cd ~/.claude/skills
git clone https://github.com/withObsrvr/nebu-processor-registry.git
ln -s nebu-processor-registry/skills/nebu-processor-builder nebu-processor-builder
```

**Usage:**

```
# In Claude Code, invoke the skill
/nebu-processor-builder
```

Claude Code automatically discovers skills at startup from `.claude/skills/` (project) and `~/.claude/skills/` (personal).

The skill will guide you through:
1. Choosing processor type (origin/transform/sink)
2. Describing what it does
3. Naming your processor
4. Generating proper Go module structure
5. Using official CLI helpers
6. Creating registry entry (optional)

**What it generates:**
- ✓ Proper `go.mod` (no replace directives!)
- ✓ Main entry point using CLI helpers
- ✓ Skeleton with TODOs for your logic
- ✓ README with usage examples
- ✓ Registry entry (optional)
- ✓ Code that compiles immediately

**Learn more:** See [`skills/nebu-processor-builder/`](skills/nebu-processor-builder/)

## Submitting Your Processor

We welcome contributions! To submit your processor to the community registry:

### 1. Create Your Processor

Your processor should:
- Live in its own GitHub repository
- Follow the [nebu processor interface](https://github.com/withObsrvr/nebu/tree/main/pkg/processor) — see [nebu's STABILITY.md](https://github.com/withObsrvr/nebu/blob/main/docs/STABILITY.md) for the stable surface
- Use one of the `pkg/processor/cli` helpers (`RunProtoOriginCLI`, `RunTransformCLI`, `RunSinkCLI`) so the `--describe-json` introspection protocol is wired automatically
- Declare a `SchemaID` on its CLI config (e.g., `"nebu.my_processor.v1"`)
- Have a `cmd/main.go` for standalone CLI usage
- Include a README with usage examples
- Have tests — including `./my-processor --describe-json | jq .` as a smoke test

Example structure:
```
my-processor/
├── cmd/main.go           # Standalone CLI binary
├── processor.go          # Processor implementation
├── manifest.yaml         # Processor metadata
├── README.md             # Documentation
├── go.mod
└── *_test.go             # Tests
```

### 2. Create a Pull Request

1. Fork this repository
2. Create a directory: `processors/<your-processor-name>/`
3. Add a `description.yml` file (see template below)
4. Submit a pull request

### 3. Description File Template

Create `processors/<your-processor-name>/description.yml`:

```yaml
processor:
  name: my-awesome-processor
  type: origin  # origin, transform, or sink
  description: Short description of what your processor does
  version: 1.0.0
  language: Go
  license: MIT
  maintainers:
    - github_username

repo:
  github: your-username/nebu-processor-awesome
  ref: v1.0.0  # Git tag, branch, or commit SHA

# Optional: Protocol buffer definitions
proto:
  source: github.com/your-username/nebu-processor-awesome/proto
  package: my_processor

# Optional: Schema versioning. The live JSON Schema is fetched from
# the processor binary at describe time via --describe-json; this
# block is a static pointer used when the binary isn't installed.
schema:
  version: v1
  identifier: nebu.my_processor.v1
  documentation: https://github.com/your-username/nebu-processor-awesome/blob/main/SCHEMA.md

docs:
  quick_start: |
    # Install the processor
    nebu install my-awesome-processor

    # Use it
    my-awesome-processor --start-ledger 60200000 --end-ledger 60200100

  examples: |
    # Origin processor example
    my-awesome-processor --start 60200000 --end 60200100 | jq

    # Transform processor example
    token-transfer | my-awesome-processor --filter xyz | json-file-sink

  extended_description: |
    Detailed description of your processor.
    What problem does it solve?
    How does it work?
    Any performance characteristics?
```

## Validation

All submitted processors are validated to ensure they:
- ✅ Build successfully
- ✅ Follow the nebu processor interface
- ✅ Implement the `--describe-json` protocol (emit a valid [DescribeEnvelope](https://github.com/withObsrvr/nebu/blob/main/pkg/processor/describe.go))
- ✅ Include proper documentation
- ✅ Have valid `description.yml` metadata
- ✅ Include tests

The `--describe-json` check is a simple invocation:

```bash
./my-processor --describe-json | jq -e '.name and .type' > /dev/null
```

## Governance

This is a community-driven registry. Processors are maintained by their respective authors, not by the nebu core team.

**Trust Model:**
- ⚠️ Community processors are NOT vetted for security
- ⚠️ Review code before using in production
- ⚠️ Check processor reputation (stars, forks, activity)
- ✅ Official processors are marked with `official: true`

## Official vs Community Processors

| Type | Location | Maintained By | Trust Level |
|------|----------|---------------|-------------|
| **Official** | `withObsrvr/nebu/examples/processors` | OBSRVR team | ✅ High |
| **Community** | This registry | Community authors | ⚠️ Review code |

## License

This registry (metadata only) is MIT licensed. Individual processors have their own licenses specified in their `description.yml`.

## Resources

- [nebu Documentation](https://withobsrvr.github.io/nebu/) — Official docs and getting started
- [nebu GitHub](https://github.com/withObsrvr/nebu) — Source code and examples
- [Processor Interface Reference](https://github.com/withObsrvr/nebu/tree/main/pkg/processor) — Go interfaces
- [Stability Policy](https://github.com/withObsrvr/nebu/blob/main/docs/STABILITY.md) — What's stable in nebu, including the `--describe-json` protocol
- [Registry Specification](https://github.com/withObsrvr/nebu/blob/main/docs/REGISTRY_SPEC.md) — Formal spec for `registry.yaml` and `description.yml`
- [BUILDING_PROTO_PROCESSORS.md](BUILDING_PROTO_PROCESSORS.md) — Comprehensive proto-first processor tutorial
- [Community Discussions](https://github.com/withObsrvr/nebu-processor-registry/discussions) — Ask questions

## Support

- **Registry Issues**: Open an issue in this repository
- **Processor Issues**: Open an issue in the processor's repository
- **General Questions**: Use [GitHub Discussions](https://github.com/withObsrvr/nebu-processor-registry/discussions)
