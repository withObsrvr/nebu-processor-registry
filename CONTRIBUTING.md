# Contributing to nebu Processor Registry

Thank you for contributing to the nebu community processor registry! This guide will help you submit your processor.

**New to building processors?** See [BUILDING_PROTO_PROCESSORS.md](BUILDING_PROTO_PROCESSORS.md) for a comprehensive guide to building protobuf-first processors for Stellar.

## Submission Process

### 1. Prepare Your Processor

Your processor must meet these requirements:

#### Repository Structure (Proto-First Origin Processors)

For origin processors, use the **protobuf-first** approach:

```
my-processor/
├── proto/
│   ├── my_processor.proto      # Protobuf schema definition
│   └── my_processor.pb.go      # Generated Go code
├── my_processor.go             # Processor implementation
├── cmd/my-processor/
│   └── main.go                 # CLI wrapper with RunProtoOriginCLI
├── go.mod                      # Go module
├── README.md                   # Documentation
└── *_test.go                   # Tests
```

**See the guide**: [BUILDING_PROTO_PROCESSORS.md](BUILDING_PROTO_PROCESSORS.md) for step-by-step instructions.

#### Repository Structure (Transform/Sink Processors)

For transform and sink processors:

```
my-processor/
├── cmd/my-processor/
│   └── main.go           # Standalone CLI binary (Go)
├── go.mod                # Go module (if Go)
├── README.md             # Documentation
└── *_test.go             # Tests
```

#### Processor Requirements

**All Processors:**
- ✅ Builds as standalone CLI binary
- ✅ Outputs newline-delimited JSON to stdout
- ✅ Includes `_schema` and `_nebu_version` fields in output
- ✅ Supports `-q/--quiet` flag for pipeline usage
- ✅ Implements the `--describe-json` introspection protocol (free when using `RunProtoOriginCLI` / `RunTransformCLI` / `RunSinkCLI`)
- ✅ Declares a `SchemaID` in its CLI config when the output has a stable schema
- ✅ Has comprehensive README with usage examples
- ✅ Includes tests
- ✅ Uses semantic versioning (Git tags)

**Origin Processors (MUST use protobuf-first):**
- ✅ Defines protobuf schema in `.proto` file
- ✅ Uses `RunProtoOriginCLI` wrapper
- ✅ Implements `ProcessLedger(ctx, ledger)` — **no error return** (streams-never-throw; see [BUILDING_PROTO_PROCESSORS.md](BUILDING_PROTO_PROCESSORS.md))
- ✅ Reports per-ledger failures via `processor.ReportWarning(ctx, name, err)`
- ✅ Emits strongly-typed protobuf messages

**Transform Processors:**
- ✅ Reads JSON from stdin, writes JSON to stdout
- ✅ Uses `RunTransformCLI` wrapper
- ✅ Preserves schema versioning fields
- ✅ Optionally declares `InputType`/`OutputType` on the config for richer describe output

**Sink Processors:**
- ✅ Reads JSON from stdin
- ✅ Uses `RunSinkCLI` wrapper
- ✅ Per-event write failures returned from `SinkFunc` are logged as warnings by the helper and the loop continues
- ✅ Truly fatal conditions (dropped DB connection, revoked credentials) should be handled explicitly by the sink — call `processor.ReportFatal` or `os.Exit` as appropriate

### 2. Create description.yml

Fork this repository and create `processors/<your-processor-name>/description.yml`:

```yaml
processor:
  name: my-processor-name
  type: origin  # origin, transform, or sink
  description: One-line description of what your processor does
  version: 1.0.0
  language: Go
  license: MIT  # or Apache-2.0, BSD-3-Clause, etc.
  maintainers:
    - your-github-username

repo:
  github: your-username/your-processor-repo
  ref: v1.0.0  # Git tag (recommended), branch, or commit SHA

# Optional: Protocol buffer definitions
proto:
  source: github.com/your-username/your-processor-repo/proto
  package: your_processor_package

# Optional: Schema versioning
schema:
  version: v1
  identifier: nebu.your_processor.v1
  documentation: https://github.com/your-username/your-processor-repo/blob/main/SCHEMA.md

docs:
  quick_start: |
    # Install the processor
    nebu install my-processor-name

    # Basic usage
    my-processor-name --help

  examples: |
    # Example for origin processor
    my-processor-name --start-ledger 60200000 --end-ledger 60200100

    # Example for transform processor
    token-transfer | my-processor-name --option value | json-file-sink

    # Example for sink processor
    token-transfer | my-processor-name --db connection-string

  extended_description: |
    Detailed description of your processor.

    What problem does it solve?
    How does it work internally?
    What are the performance characteristics?
    What are the dependencies?

    Include any important notes about:
    - Configuration options
    - Resource requirements
    - Limitations
    - Best practices
```

### 3. Submit Pull Request

1. Fork this repository
2. Create a new branch: `git checkout -b add-my-processor`
3. Add your `processors/<processor-name>/description.yml` file
4. Commit your changes: `git commit -m "Add my-processor to registry"`
5. Push to your fork: `git push origin add-my-processor`
6. Open a Pull Request

### 4. Automated Validation

When you submit your PR, GitHub Actions will automatically:

- ✅ Validate your `description.yml` syntax and required fields
- ✅ Clone and build your processor
- ✅ Test that it executes without errors
- ✅ Generate updated processor list

If validation fails, check the workflow logs and fix any issues.

## Best Practices

### Building Protobuf-First Processors

**For origin processors**, always use the protobuf-first approach. See our comprehensive guide:

📖 **[BUILDING_PROTO_PROCESSORS.md](BUILDING_PROTO_PROCESSORS.md)** - Complete tutorial with:
- Why proto-first for Stellar processors
- Step-by-step walkthrough
- XDR → Protobuf conversion patterns
- Real-world examples
- Troubleshooting

### Naming Conventions

- **Origin processors**: Describe what they extract (e.g., `token-transfer`, `contract-events`, `contract-invocation`)
- **Transform processors**: Describe the transformation (e.g., `usdc-filter`, `dedup`, `time-window`)
- **Sink processors**: Describe the destination (e.g., `json-file-sink`, `nats-sink`, `postgres-sink`)

### Versioning

- Use semantic versioning: `MAJOR.MINOR.PATCH`
- Tag releases in your repository (e.g., `v1.0.0`)
- Reference tags in `repo.ref` (not branches) for stability
- Increment versions when making breaking changes

### Documentation

Your processor's README should include:

1. **Overview**: What does it do?
2. **Installation**: `nebu install <processor-name>`
3. **Usage**: Command-line flags and examples
4. **Configuration**: Environment variables, config files
5. **Output Format**: Schema and example output
6. **Performance**: Expected throughput, resource usage
7. **Development**: How to build, test, contribute

### Schema Versioning

Include schema metadata in all output:

```json
{
  "_schema": "nebu.my_processor.v1",
  "_nebu_version": "1.0.0",
  "type": "my_event_type",
  "data": { ... }
}
```

This allows:
- **Forward compatibility**: Old consumers can ignore new fields
- **Version detection**: Consumers can handle multiple schema versions
- **Documentation**: Schema identifier links to documentation

### Testing

Include tests in your repository:

- **Unit tests**: Test core logic
- **Integration tests**: Test against sample ledger data
- **End-to-end tests**: Test full pipeline (origin → transform → sink)

Example test structure:

```bash
# In your processor repository
go test ./...

# Test with sample data
nebu fetch 60200000 60200100 | ./my-processor | jq
```

## Processor Types in Detail

### Origin Processors

Extract structured events from raw Stellar ledger data.

**IMPORTANT:** Origin processors MUST use the **protobuf-first** approach. This ensures:
- Type safety via protobuf schemas
- Consistency across processors
- Automatic JSON conversion via protojson
- Future gRPC support for flowctl integration

**Requirements:**
- Defines events in `.proto file`
- Uses `RunProtoOriginCLI` wrapper
- Implements `ProcessLedger(ctx context.Context, ledger xdr.LedgerCloseMeta)` — void, no error return (streams-never-throw)
- Reports per-ledger errors via `processor.ReportWarning(ctx, name, err)`
- Accepts `--start-ledger` and `--end-ledger` flags
- Connects to RPC endpoint (supports `--rpc-url` flag)
- Outputs newline-delimited JSON events (auto-converted from protobuf)
- Supports streaming (unbounded ranges)
- Supports `--describe-json` (automatic with `RunProtoOriginCLI`)

**Quick Example:**

See [BUILDING_PROTO_PROCESSORS.md](BUILDING_PROTO_PROCESSORS.md) for complete tutorial. Here's the minimal structure:

```protobuf
// proto/my_event.proto
syntax = "proto3";
package my_processor;

message MyEvent {
  string event_type = 1;
  Metadata meta = 2;
}

message Metadata {
  uint32 ledger_sequence = 1;
  string tx_hash = 2;
  // ... standard metadata fields
}
```

```go
// my_processor.go
func (o *Origin) ProcessLedger(ctx context.Context, ledger xdr.LedgerCloseMeta) {
    events, err := o.extractEvents(ledger)
    if err != nil {
        // Per-ledger failures are reported, not returned. The pipeline
        // continues to the next ledger. This is "streams-never-throw".
        processor.ReportWarning(ctx, o.Name(),
            fmt.Errorf("ledger %d: %w", ledger.LedgerSequence(), err))
        return
    }

    for _, event := range events {
        select {
        case <-ctx.Done():
            return
        case o.out <- event:  // Send proto message
        }
    }
}
```

```go
// cmd/my-processor/main.go
func main() {
    config := cli.OriginConfig{
        Name:        "my-processor",
        Description: "...",
        Version:     version,
        SchemaID:    "nebu.my_event.v1", // Populates --describe-json output
    }
    cli.RunProtoOriginCLI(config, func(networkPass string) cli.ProtoOriginProcessor[*proto.MyEvent] {
        return NewOrigin(networkPass)
    })
}
```

**Real Examples:**
- [token-transfer](https://github.com/withObsrvr/nebu/tree/main/examples/processors/token-transfer)
- [contract-events](https://github.com/withObsrvr/nebu/tree/main/examples/processors/contract-events)
- [contract-invocation](https://github.com/withObsrvr/nebu/tree/main/examples/processors/contract-invocation)

### Transform Processors

Filter, aggregate, or transform event streams.

**Requirements:**
- Reads newline-delimited JSON from stdin
- Writes newline-delimited JSON to stdout
- Preserves `_schema` and `_nebu_version` fields
- Uses `RunTransformCLI` wrapper from nebu/pkg/processor/cli

**Example:**
```go
package main

import (
	"github.com/withObsrvr/nebu/pkg/processor/cli"
)

func main() {
	config := cli.TransformConfig{
		Name:        "my-filter",
		Description: "Filter events based on criteria",
		Version:     "1.0.0",
		// Optional: declare the schema you pass through unchanged.
		// Pass-through filters usually set both InputType and
		// OutputType to the same proto message type.
		SchemaID: "nebu.token_transfer.v1",
	}

	cli.RunTransformCLI(config, filterFunc, nil)
}

// Return event to keep, nil to filter out.
func filterFunc(event map[string]interface{}) map[string]interface{} {
	if shouldKeep(event) {
		return event
	}
	return nil
}
```

**Real Examples:**
- [usdc-filter](https://github.com/withObsrvr/nebu/tree/main/examples/processors/usdc-filter)
- [amount-filter](https://github.com/withObsrvr/nebu/tree/main/examples/processors/amount-filter)
- [dedup](https://github.com/withObsrvr/nebu/tree/main/examples/processors/dedup)
- [time-window](https://github.com/withObsrvr/nebu/tree/main/examples/processors/time-window)

### Sink Processors

Write events to external systems (databases, files, APIs).

**Requirements:**
- Reads newline-delimited JSON from stdin
- Uses `RunSinkCLI` wrapper from nebu/pkg/processor/cli
- Handles connection errors gracefully
- Supports batch writes (recommended for performance)

**Example:**
```go
package main

import (
	"github.com/withObsrvr/nebu/pkg/processor/cli"
)

var db *sql.DB  // Lazy initialized

func main() {
	config := cli.SinkConfig{
		Name:        "my-sink",
		Description: "Write events to database",
		Version:     "1.0.0",
	}

	cli.RunSinkCLI(config, sinkFunc, addFlags)
}

// Return an error for per-event failures. The CLI helper logs them
// as warnings and continues to the next event (streams-never-throw).
// For unrecoverable failures (dropped connection that can't be
// re-established), call os.Exit or panic with a clear message.
func sinkFunc(event map[string]interface{}) error {
	// Lazy connect on first event
	if db == nil {
		var err error
		db, err = connectToDB()
		if err != nil {
			return err
		}
	}

	// Write to database
	return db.Insert(event)
}
```

**Real Examples:**
- [json-file-sink](https://github.com/withObsrvr/nebu/tree/main/examples/processors/json-file-sink)
- [nats-sink](https://github.com/withObsrvr/nebu/tree/main/examples/processors/nats-sink)
- [postgres-sink](https://github.com/withObsrvr/nebu/tree/main/examples/processors/postgres-sink)

## Maintenance

### Updating Your Processor

To update your processor in the registry:

1. Tag a new release in your repository: `git tag v1.1.0 && git push --tags`
2. Update `processors/<your-processor>/description.yml` with new `version` and `ref`
3. Submit a PR with your changes

### Deprecating Your Processor

To deprecate your processor:

1. Add `deprecated: true` to your `description.yml`
2. Add `deprecation_notice` with migration instructions
3. Submit a PR

Example:

```yaml
processor:
  name: my-old-processor
  deprecated: true
  deprecation_notice: |
    This processor is deprecated. Please migrate to my-new-processor:
    nebu install my-new-processor
```

## Support

- **Registry Issues**: Open an issue in this repository
- **Processor Issues**: Open an issue in the processor's repository
- **General Questions**: Use [GitHub Discussions](https://github.com/withObsrvr/nebu-processor-registry/discussions)
- **nebu Core**: See [nebu repository](https://github.com/withObsrvr/nebu)

## Code of Conduct

Be respectful, inclusive, and constructive. We're building tools together for the Stellar community.

## License

By contributing to this registry, you agree that your processor metadata (description.yml) is MIT licensed. Your actual processor code retains its own license as specified in your `description.yml`.
