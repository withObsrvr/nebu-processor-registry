# nebu-processor-registry Setup Status

## Completed Setup

The nebu community processor registry is now ready for use.

### Created Files

#### Documentation
- ✅ `README.md` - Main registry documentation with submission workflow
- ✅ `CONTRIBUTING.md` - Detailed contribution guidelines and best practices
- ✅ `SETUP.md` - Technical setup and automation documentation
- ✅ `LICENSE` - MIT License for registry metadata

#### Automation
- ✅ `.github/workflows/validate-pr.yml` - GitHub Actions workflow for automated PR validation
- ✅ `scripts/validate-processor.sh` - Validates description.yml files
- ✅ `scripts/build-processor.sh` - Clones and builds processors to verify they work
- ✅ `scripts/generate-list.sh` - Auto-generates PROCESSORS.md from registry

#### Templates and Examples
- ✅ `processors/.template/description.yml` - Template for new processor submissions
- ✅ `processors/token-transfer/description.yml` - Example processor entry

#### Configuration
- ✅ `.gitignore` - Excludes generated files (PROCESSORS.md, build artifacts, etc.)

### Directory Structure

```
nebu-processor-registry/
├── README.md                    # User-facing documentation
├── CONTRIBUTING.md              # Submission guidelines
├── SETUP.md                     # Technical documentation
├── LICENSE                      # MIT License
├── .gitignore                   # Git configuration
├── processors/
│   ├── .template/
│   │   └── description.yml      # Submission template
│   └── token-transfer/
│       └── description.yml      # Example processor
├── scripts/
│   ├── validate-processor.sh   # YAML validation
│   ├── build-processor.sh      # Build testing
│   └── generate-list.sh        # List generation
└── .github/
    └── workflows/
        └── validate-pr.yml      # CI/CD automation
```

### Tested Features

#### Validation Script
```bash
$ bash scripts/validate-processor.sh processors/token-transfer
✓ description.yml exists
✓ Valid YAML syntax
✓ Required field .processor.name present
✓ Required field .processor.type present
✓ Required field .processor.description present
✓ Required field .processor.version present
✓ Required field .processor.language present
✓ Required field .processor.license present
✓ Required field .repo.github present
✓ Required field .repo.ref present
✓ Valid processor type: origin
✓ Valid GitHub repository: withObsrvr/nebu
✓ Maintainers specified: 1
✓ Documentation section .docs.quick_start present
✓ Documentation section .docs.examples present
✅ Processor validation passed: processors/token-transfer
```

#### List Generation
```bash
$ bash scripts/generate-list.sh
Generating processor list...
✅ Generated processor list: PROCESSORS.md (1 processors)
```

### How It Works

#### For Users
```bash
# List all available processors
nebu list

# Install a community processor
nebu install <processor-name>

# Use in pipelines
<origin> | <transform> | <sink>
```

#### For Contributors
```bash
# 1. Create processor in own repository
# 2. Tag a release (e.g., v1.0.0)

# 3. Fork registry and add description.yml
git clone https://github.com/<you>/nebu-processor-registry
cd nebu-processor-registry
cp processors/.template/description.yml processors/my-processor/description.yml

# 4. Fill in processor details
# 5. Validate locally
bash scripts/validate-processor.sh processors/my-processor

# 6. Submit PR
git add processors/my-processor/description.yml
git commit -m "Add my-processor to registry"
git push origin add-my-processor
# Open PR on GitHub

# 7. GitHub Actions automatically validates
# 8. Maintainer reviews and merges
```

### GitHub Actions Workflow

When a PR is submitted that modifies `processors/**`:

1. ✅ Sets up Go environment
2. ✅ Installs yq for YAML validation
3. ✅ Detects changed processor directories
4. ✅ Validates each processor's description.yml
5. ✅ Clones and builds processors from GitHub repos
6. ✅ Generates updated PROCESSORS.md
7. ✅ Comments on PR if validation fails

### Validation Checks

Each processor submission is validated for:

- ✅ Valid YAML syntax
- ✅ Required fields (name, type, description, version, license, repo, maintainers)
- ✅ Processor type is origin, transform, or sink
- ✅ Version follows semver (1.0.0)
- ✅ GitHub repo format (username/repository)
- ✅ At least one maintainer
- ⚠️  Documentation sections (recommended)
- ✅ Processor builds successfully
- ✅ Processor executes without errors

### Trust Model

**Official Processors**
- Maintained by OBSRVR team
- Located in withObsrvr/nebu repository
- High trust level

**Community Processors**
- Maintained by community authors
- Located in contributor repositories
- ⚠️ NOT vetted for security
- ⚠️ Review code before production use
- ⚠️ Check processor reputation

### Next Steps

1. **Push to GitHub**: Initialize git repository and push to GitHub
   ```bash
   cd /home/tillman/Documents/nebu-processor-registry
   git init
   git add .
   git commit -m "Initial registry setup"
   git remote add origin https://github.com/withObsrvr/nebu-processor-registry
   git push -u origin main
   ```

2. **Configure GitHub Actions**: Ensure workflow has necessary permissions

3. **Announce to Community**: Share registry with Stellar community

4. **Add More Processors**: Migrate examples from withObsrvr/nebu:
   - usdc-filter
   - amount-filter
   - dedup
   - time-window
   - json-file-sink
   - duckdb-sink

5. **Future Enhancements**:
   - Processor popularity metrics
   - Automated security scanning
   - Performance benchmarks
   - Official processor badges
   - Multi-language support

### Pattern Followed

This registry follows the **DuckDB Community Extensions** pattern:

- ✅ Metadata-only (processors live in their own repos)
- ✅ Automated validation with GitHub Actions
- ✅ Template-based submission
- ✅ description.yml metadata format
- ✅ Clear trust model (official vs community)
- ✅ Comprehensive documentation

### Resources

- **Registry Repo**: https://github.com/withObsrvr/nebu-processor-registry
- **nebu Core**: https://github.com/withObsrvr/nebu
- **Example Processors**: https://github.com/withObsrvr/nebu/tree/main/examples/processors
- **DuckDB Pattern**: https://github.com/duckdb/community-extensions

---

**Status**: ✅ Ready for use
**Created**: 2025-12-12
**Last Updated**: 2025-12-12
