# nebu Processor Registry Setup

This document describes the structure and automation of the nebu community processor registry.

## Repository Structure

```
nebu-processor-registry/
├── README.md                    # Main documentation
├── CONTRIBUTING.md              # Submission guidelines
├── LICENSE                      # MIT License
├── SETUP.md                     # This file (setup documentation)
├── .gitignore                   # Ignore generated files
│
├── processors/                  # Processor metadata directory
│   ├── .template/
│   │   └── description.yml      # Template for new submissions
│   └── token-transfer/
│       └── description.yml      # Example processor
│
├── scripts/                     # Validation and automation scripts
│   ├── validate-processor.sh   # Validate description.yml
│   ├── build-processor.sh      # Build and test processors
│   └── generate-list.sh        # Generate PROCESSORS.md
│
└── .github/
    └── workflows/
        └── validate-pr.yml      # Automated PR validation

# Generated files (not in git)
PROCESSORS.md                    # Auto-generated processor list
```

## Automation

### GitHub Actions Workflow

**Trigger**: Pull requests that modify `processors/**`

**Steps**:
1. Checkout code and setup Go environment
2. Install `yq` for YAML validation
3. Detect changed processor directories
4. Validate each processor's `description.yml`
5. Build processors from their GitHub repositories
6. Generate updated processor list
7. Comment on PR if validation fails

**File**: `.github/workflows/validate-pr.yml`

### Validation Scripts

#### `scripts/validate-processor.sh`

Validates processor metadata:

- ✅ `description.yml` exists and is valid YAML
- ✅ Required fields present (name, type, description, version, license, repo, maintainers)
- ✅ Processor type is one of: origin, transform, sink
- ✅ Version follows semver format
- ✅ GitHub repo in `username/repository` format
- ✅ At least one maintainer specified
- ⚠️  Documentation sections recommended

**Usage**:
```bash
bash scripts/validate-processor.sh processors/token-transfer
```

#### `scripts/build-processor.sh`

Tests that processor builds successfully:

- Clones processor repository
- Checks out specified ref (tag/branch/commit)
- Builds processor based on language (Go/Python/Rust)
- Tests basic execution (--help flag)

**Usage**:
```bash
bash scripts/build-processor.sh processors/token-transfer
```

#### `scripts/generate-list.sh`

Generates `PROCESSORS.md` from all processors:

- Scans `processors/*/description.yml` files
- Groups by type (origin, transform, sink)
- Extracts metadata (name, version, language, license, repo)
- Generates installation and usage examples

**Usage**:
```bash
bash scripts/generate-list.sh
# Creates PROCESSORS.md
```

## Local Development

### Setup

```bash
# Clone the registry
git clone https://github.com/withObsrvr/nebu-processor-registry
cd nebu-processor-registry

# Install yq (for validation)
wget -qO /tmp/yq https://github.com/mikefarah/yq/releases/latest/download/yq_linux_amd64
chmod +x /tmp/yq
sudo mv /tmp/yq /usr/local/bin/yq

# Make scripts executable
chmod +x scripts/*.sh
```

### Testing a Submission

```bash
# Validate a processor
bash scripts/validate-processor.sh processors/my-processor

# Build and test it
bash scripts/build-processor.sh processors/my-processor

# Generate processor list
bash scripts/generate-list.sh
cat PROCESSORS.md
```

## Submission Workflow

### For Contributors

1. Create processor in own repository
2. Tag a release (e.g., `v1.0.0`)
3. Fork this registry repository
4. Copy template: `cp processors/.template/description.yml processors/my-processor/description.yml`
5. Fill in processor details
6. Test locally: `bash scripts/validate-processor.sh processors/my-processor`
7. Submit pull request
8. Wait for automated validation
9. Address any validation failures
10. Merge approved

### For Maintainers

1. Review `description.yml` for completeness
2. Check automated validation passed
3. Optionally test processor manually
4. Merge PR
5. GitHub Actions regenerates `PROCESSORS.md`

## Processor Requirements

### All Processors

- ✅ Implements nebu processor interface
- ✅ Builds as standalone CLI binary
- ✅ Outputs newline-delimited JSON
- ✅ Includes `_schema` and `_nebu_version` in output
- ✅ Supports `-q/--quiet` flag
- ✅ Has README with examples
- ✅ Includes tests
- ✅ Uses semantic versioning

### Origin Processors

- ✅ Accepts `--start-ledger` and `--end-ledger` flags
- ✅ Connects to RPC endpoint
- ✅ Supports `--rpc-url` and `--network` flags
- ✅ Can run standalone or pipe to transforms/sinks

### Transform Processors

- ✅ Reads JSON from stdin
- ✅ Writes JSON to stdout
- ✅ Preserves schema versioning fields
- ✅ Handles EOF gracefully

### Sink Processors

- ✅ Reads JSON from stdin
- ✅ Writes to external system (DB, file, API)
- ✅ Handles errors gracefully
- ✅ Supports batch writes (recommended)

## Schema Versioning

All processors should include schema metadata:

```json
{
  "_schema": "nebu.processor_name.v1",
  "_nebu_version": "1.0.0",
  "type": "event_type",
  ...
}
```

This enables:
- Forward compatibility (consumers ignore unknown fields)
- Version detection (handle multiple schema versions)
- Documentation (schema identifier → docs)

## Trust Model

### Official Processors

- Located in `withObsrvr/nebu/examples/processors`
- Maintained by OBSRVR team
- Marked with `official: true` flag (future)

### Community Processors

- Located in contributor repositories
- Maintained by community members
- **NOT vetted for security**
- Users should review code before production use
- Check reputation (stars, forks, activity)

## Governance

- Community-driven registry
- Processors maintained by authors, not nebu core team
- Validation ensures basic quality standards
- Security and correctness are contributor responsibility

## Future Enhancements

Potential additions:

- [ ] Processor popularity metrics (downloads, stars)
- [ ] Dependency scanning and vulnerability alerts
- [ ] Automated benchmarking
- [ ] Official processor badge
- [ ] Deprecation workflow
- [ ] Multi-language support (Python, Rust, etc.)
- [ ] Processor testing framework
- [ ] Community voting on processors

## Resources

- **nebu Documentation**: https://github.com/withObsrvr/nebu
- **Building Processors**: https://github.com/withObsrvr/nebu/blob/main/docs/BUILDING_PROCESSORS.md
- **Processor Interface**: https://github.com/withObsrvr/nebu/tree/main/pkg/processor
- **Discussions**: https://github.com/withObsrvr/nebu-processor-registry/discussions

## Support

- Registry issues → This repository
- Processor issues → Processor repository
- General questions → GitHub Discussions
- nebu core issues → withObsrvr/nebu
