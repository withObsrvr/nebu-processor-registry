# Contributing to nebu Processor Registry

Thank you for contributing to the nebu community processor registry! This guide will help you submit your processor.

## Submission Process

### 1. Prepare Your Processor

Your processor must meet these requirements:

#### Repository Structure

Your processor repository should have:

```
my-processor/
├── cmd/main.go           # Standalone CLI binary (Go)
├── processor.go          # Processor implementation
├── go.mod                # Go module (if Go)
├── README.md             # Documentation
└── *_test.go             # Tests
```

#### Processor Requirements

- ✅ Implements nebu processor interface (Origin, Transform, or Sink)
- ✅ Builds as standalone CLI binary
- ✅ Accepts input via stdin (for Transform/Sink) or flags (for Origin)
- ✅ Outputs newline-delimited JSON to stdout
- ✅ Includes `_schema` and `_nebu_version` fields in output
- ✅ Supports `-q/--quiet` flag for pipeline usage
- ✅ Has comprehensive README with usage examples
- ✅ Includes tests
- ✅ Uses semantic versioning (Git tags)

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

### Naming Conventions

- **Origin processors**: Describe what they extract (e.g., `token-transfer`, `soroban-events`)
- **Transform processors**: Describe the transformation (e.g., `usdc-filter`, `dedup`, `time-window`)
- **Sink processors**: Describe the destination (e.g., `json-file-sink`, `duckdb-sink`, `postgres-sink`)

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

**Requirements:**
- Accepts `--start-ledger` and `--end-ledger` flags
- Connects to RPC endpoint (supports `--rpc-url` flag)
- Outputs newline-delimited JSON events
- Supports streaming (unbounded ranges)

**Example:**
```go
type MyOrigin struct{}

func (o *MyOrigin) ProcessLedger(ctx context.Context, ledger xdr.LedgerCloseMeta) error {
    // Extract events from ledger
    for _, tx := range ledger.TransactionsWithoutMeta() {
        // ... process transactions
        event := map[string]interface{}{
            "_schema": "nebu.my_processor.v1",
            "_nebu_version": "1.0.0",
            "type": "my_event",
            // ... event data
        }
        json.NewEncoder(os.Stdout).Encode(event)
    }
    return nil
}
```

### Transform Processors

Filter, aggregate, or transform event streams.

**Requirements:**
- Reads newline-delimited JSON from stdin
- Writes newline-delimited JSON to stdout
- Preserves `_schema` and `_nebu_version` fields
- Supports `--quiet` flag

**Example:**
```go
func main() {
    scanner := bufio.NewScanner(os.Stdin)
    for scanner.Scan() {
        var event map[string]interface{}
        json.Unmarshal(scanner.Bytes(), &event)

        // Transform event
        if shouldKeep(event) {
            transformed := transform(event)
            json.NewEncoder(os.Stdout).Encode(transformed)
        }
    }
}
```

### Sink Processors

Write events to external systems (databases, files, APIs).

**Requirements:**
- Reads newline-delimited JSON from stdin
- Writes to configured destination
- Handles connection errors gracefully
- Supports batch writes (for performance)

**Example:**
```go
func main() {
    db := connectToDB()
    defer db.Close()

    scanner := bufio.NewScanner(os.Stdin)
    for scanner.Scan() {
        var event map[string]interface{}
        json.Unmarshal(scanner.Bytes(), &event)

        // Write to database
        if err := db.Insert(event); err != nil {
            log.Printf("Failed to insert: %v", err)
        }
    }
}
```

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
