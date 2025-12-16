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

```bash
# List all available processors
nebu list

# Install a community processor
nebu install <processor-name>

# Use in pipelines
<origin-processor> | <transform-processor> | <sink-processor>
```

## Submitting Your Processor

We welcome contributions! To submit your processor to the community registry:

### 1. Create Your Processor

Your processor should:
- Live in its own GitHub repository
- Follow the [nebu processor interface](https://github.com/withObsrvr/nebu/tree/main/pkg/processor)
- Include a `manifest.yaml` with metadata
- Have a `cmd/main.go` for standalone CLI usage
- Include a README with usage examples
- Have tests

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

# Optional: Schema versioning
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
- ✅ Include proper documentation
- ✅ Have valid `description.yml` metadata
- ✅ Include tests

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

- [nebu Documentation](https://withobsrvr.github.io/nebu/) - Official docs and getting started
- [nebu GitHub](https://github.com/withObsrvr/nebu) - Source code and examples
- [Processor Interface Reference](https://github.com/withObsrvr/nebu/tree/main/pkg/processor) - Go interfaces
- [Community Discussions](https://github.com/withObsrvr/nebu-processor-registry/discussions) - Ask questions

## Support

- **Registry Issues**: Open an issue in this repository
- **Processor Issues**: Open an issue in the processor's repository
- **General Questions**: Use [GitHub Discussions](https://github.com/withObsrvr/nebu-processor-registry/discussions)
