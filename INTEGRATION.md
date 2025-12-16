# nebu Community Registry Integration Strategy

This document discusses how the nebu community processor registry integrates with the nebu CLI and outlines potential future enhancements.

## Current State

### nebu CLI (Official Processors)

nebu currently manages processors through a local `registry.yaml` file in the nebu repository:

```bash
# List processors from local registry.yaml
nebu list

# Install processor from local path
nebu install token-transfer
# → Runs: go build ./examples/processors/token-transfer/cmd
# → Installs to: $GOPATH/bin/token-transfer
```

All processors in `registry.yaml` are currently **local** (type: `local`, path: `./examples/processors/...`).

**registry.yaml structure:**
```yaml
processors:
  - name: token-transfer
    type: origin
    description: Stream token transfer events from Stellar ledgers
    location:
      type: local
      path: ./examples/processors/token-transfer
      package: github.com/withObsrvr/nebu/examples/processors/token-transfer
```

### Community Processor Registry (This Repository)

The community registry is a **separate repository** that catalogs community-contributed processors:

- Each processor lives in its **own GitHub repository**
- This registry contains only **metadata** (`description.yml` files)
- Processors are **not built into** or **shipped with** nebu
- GitHub Actions validates submissions (YAML syntax, builds from source)

**description.yml structure:**
```yaml
processor:
  name: awesome-processor
  type: transform
  description: Does something awesome
  version: 1.0.0
  language: Go
  license: MIT
  maintainers:
    - github-username

repo:
  github: user/awesome-processor-repo
  ref: v1.0.0  # Git tag
```

## Current Integration: Manual Discovery

**As of now, there is NO automatic integration** between the community registry and nebu CLI.

Users discover and install community processors **manually**:

### Discovery
```bash
# Browse community registry on GitHub
# https://github.com/withObsrvr/nebu-processor-registry

# Or view generated list
# https://github.com/withObsrvr/nebu-processor-registry/blob/main/PROCESSORS.md
```

### Installation (Manual)
```bash
# Option 1: Clone and build manually
git clone https://github.com/user/awesome-processor
cd awesome-processor
go build -o $GOPATH/bin/awesome-processor ./cmd

# Option 2: Use go install (if supported)
go install github.com/user/awesome-processor/cmd@v1.0.0

# Option 3: Download pre-built binaries (if provided)
# (download from GitHub releases, move to PATH)
```

### Usage
```bash
# Once installed, use like any processor
awesome-processor --start-ledger 60200000 --end-ledger 60200100
token-transfer | awesome-processor | json-file-sink
```

## Why This Approach?

This "manual discovery" approach allows us to:

1. **Validate demand**: See if people actually want to build and share processors
2. **Learn patterns**: Understand what types of processors the community creates
3. **Avoid premature complexity**: Don't build deep CLI integration before we know it's needed
4. **Keep nebu minimal**: Don't add registry-fetching machinery until justified
5. **Test the metadata format**: Ensure `description.yml` captures the right information

## Future Integration Options

Once the community registry proves valuable, we can enhance integration:

### Option 1: Manual Sync with Git Support

**Concept**: Approved community processors get manually added to nebu's `registry.yaml` with git locations.

**Changes to registry.yaml:**
```yaml
processors:
  # Official processors (local)
  - name: token-transfer
    type: origin
    location:
      type: local
      path: ./examples/processors/token-transfer

  # Community processors (git)
  - name: awesome-processor
    type: transform
    location:
      type: git
      url: https://github.com/user/awesome-processor
      ref: v1.0.0
    maintainer:
      name: GitHub User
      url: https://github.com/user
```

**Changes to nebu CLI:**

Extend `nebu install` to support git repositories:

```bash
# Install official processor (local)
nebu install token-transfer
# → go build ./examples/processors/token-transfer/cmd

# Install community processor (git)
nebu install awesome-processor
# → git clone --depth 1 --branch v1.0.0 https://github.com/user/awesome-processor /tmp/...
# → cd /tmp/awesome-processor && go build -o $GOPATH/bin/awesome-processor ./cmd
```

**Workflow:**
1. User submits processor to community registry
2. Community registry validates and merges PR
3. After review period, **nebu maintainers manually add** to nebu's `registry.yaml`
4. Processor appears in `nebu list` and works with `nebu install`

**Pros:**
- ✅ Single user workflow: `nebu list` and `nebu install` for all processors
- ✅ Quality control: Manual approval gate before appearing in `nebu list`
- ✅ Simple implementation: Just add git clone logic to install command
- ✅ No network calls during `nebu list` (reads local file)

**Cons:**
- ❌ Manual work to sync community registry → nebu registry.yaml
- ❌ nebu repo needs updates for every community processor
- ❌ Delay between community approval and nebu availability

---

### Option 2: Remote Registry Fetch (DuckDB-like)

**Concept**: nebu dynamically fetches community registry from GitHub.

**Changes to nebu CLI:**

```bash
# List official processors (local registry.yaml)
nebu list

# List community processors (fetch from GitHub)
nebu list --community
nebu list --all

# Install official processor
nebu install token-transfer

# Install community processor
nebu install --community awesome-processor
# OR
nebu install community/awesome-processor
```

**Implementation:**
- `nebu list --community` fetches `https://github.com/withObsrvr/nebu-processor-registry`
- Parses all `processors/*/description.yml` files
- Displays in table format
- `nebu install --community <name>` clones from `repo.github` in description.yml

**Workflow:**
1. User submits processor to community registry
2. Community registry validates and merges PR
3. **Immediately available** via `nebu list --community`
4. No nebu update required

**Pros:**
- ✅ Truly decentralized: nebu doesn't need updates for new processors
- ✅ Instant availability: Merged PRs immediately discoverable
- ✅ Clear separation: `--community` flag distinguishes trust levels
- ✅ Scales to thousands of processors

**Cons:**
- ❌ Network dependency: `nebu list --community` requires internet
- ❌ More complex implementation: GitHub API or git clone for discovery
- ❌ Caching needed: Don't fetch registry on every list command
- ❌ Requires significant nebu CLI changes

---

### Option 3: Hybrid Approach

**Concept**: Official processors in local registry, curated community processors in remote registry.

```bash
# Official processors (shipped with nebu)
nebu list

# Curated community processors (vetted, stable, popular)
nebu list --curated

# All community processors (fetch from GitHub)
nebu list --community
```

**registry.yaml tiers:**
```yaml
# Local registry.yaml
official_processors:
  - token-transfer
  - json-file-sink

# Also in registry.yaml, but points to remote
curated_community_processors:
  - awesome-processor  # Manually promoted after vetting
  - popular-processor

# Not in registry.yaml, discovered dynamically
community_processors:
  - (fetched from GitHub)
```

**Pros:**
- ✅ Multiple trust levels: official → curated → community
- ✅ Balances control and openness
- ✅ Popular processors can be promoted to curated

**Cons:**
- ❌ Most complex to implement
- ❌ Blurry lines between tiers

---

### Option 4: Keep Completely Separate

**Concept**: Community registry is purely a directory/catalog. No CLI integration.

```bash
# nebu CLI only knows about official processors
nebu list
nebu install token-transfer

# Community processors discovered manually via web
# Browse: https://github.com/withObsrvr/nebu-processor-registry
# Install: git clone && go build (manual)
```

**Pros:**
- ✅ Simplest: No nebu changes needed
- ✅ Clear separation: Official vs community
- ✅ No trust model ambiguity

**Cons:**
- ❌ Poor UX: Community processors are second-class
- ❌ Manual installation friction
- ❌ Community processors won't get adopted

---

## Recommended Phased Approach

### Phase 1: Manual Discovery (Current State) ✅

**Status**: Implemented

**User workflow:**
- Browse community registry on GitHub
- Clone and install manually: `git clone && go build`

**Goal**: Validate demand and learn what processors people want

**Success criteria**:
- 5+ community processor submissions
- Evidence of manual installations (GitHub stars, clones)

---

### Phase 2: Git Support in nebu install (If Phase 1 succeeds)

**Implementation**: Option 1 (Manual Sync with Git Support)

**Changes**:
1. Extend `nebu install` to support git repositories
2. Add `location.type: git` support in registry.yaml
3. Manually add approved community processors to nebu's registry.yaml

**User workflow:**
```bash
nebu list  # Shows both official and curated community processors
nebu install awesome-processor  # Works for both types
```

**Goal**: Make installing community processors as easy as official ones

**Success criteria**:
- 10+ community processors in nebu registry.yaml
- Community processors getting regular updates

---

### Phase 3: Remote Registry (If Phase 2 scales poorly)

**Implementation**: Option 2 (Remote Registry Fetch)

**Changes**:
1. Add `nebu list --community` to fetch from GitHub
2. Add caching layer (don't fetch on every command)
3. Support `nebu install --community <name>`

**User workflow:**
```bash
nebu list  # Official processors
nebu list --community  # All community processors
nebu install --community awesome-processor
```

**Goal**: Scale to 50+ community processors without manual sync

---

## Current Recommendation

**Start with Phase 1** (already implemented). Use the community registry as-is:

1. **Let it exist as a directory/catalog** for discovering processors
2. **Manual installation** via git clone or go install
3. **Monitor adoption**: Are people submitting processors? Installing them?
4. **Collect feedback**: What friction do users experience?
5. **Decide later**: Once we see demand, choose Phase 2 or Phase 3

This approach:
- ✅ Validates the concept before heavy investment
- ✅ Keeps nebu minimal until proven necessary
- ✅ Allows iteration on metadata format based on real usage
- ✅ No premature optimization

## Trust Model

Regardless of integration approach, maintain clear trust levels:

| Level | Location | Maintained By | Vetting | Distribution |
|-------|----------|---------------|---------|--------------|
| **Official** | `withObsrvr/nebu/examples` | OBSRVR team | ✅ Full review | Shipped with nebu |
| **Curated** | Community repos | Community | ⚠️ Basic review | Added to nebu registry.yaml |
| **Community** | Community repos | Community | ⚠️ CI validation only | Self-service via registry |

Users should always be aware which tier they're using.

## Implementation Notes for Future Work

### Extending registry.yaml for Git

Add `type: git` support:

```yaml
processors:
  - name: awesome-processor
    type: transform
    description: Filter for awesome events
    location:
      type: git
      url: https://github.com/user/awesome-processor
      ref: v1.0.0  # Git tag or commit SHA
      subpath: cmd  # Optional: path to main package within repo
    maintainer:
      name: Community Author
      url: https://github.com/user
```

### Extending nebu install

```go
func installProcessor(proc *registry.Processor, installPath string) error {
    switch proc.Location.Type {
    case "local":
        // Existing logic: go build ./examples/...
        return buildLocal(proc, installPath)

    case "git":
        // New logic: clone, build, install
        return buildFromGit(proc, installPath)

    default:
        return fmt.Errorf("unsupported location type: %s", proc.Location.Type)
    }
}

func buildFromGit(proc *registry.Processor, installPath string) error {
    tmpDir := filepath.Join(os.TempDir(), proc.Name)

    // Clone repository
    cmd := exec.Command("git", "clone", "--depth", "1",
        "--branch", proc.Location.Ref,
        proc.Location.URL, tmpDir)
    if err := cmd.Run(); err != nil {
        return fmt.Errorf("failed to clone: %w", err)
    }
    defer os.RemoveAll(tmpDir)

    // Build
    buildPath := filepath.Join(tmpDir, proc.Location.Subpath)
    buildCmd := exec.Command("go", "build", "-o",
        filepath.Join(installPath, proc.Name), ".")
    buildCmd.Dir = buildPath
    return buildCmd.Run()
}
```

## Conversion from description.yml to registry.yaml

If implementing Phase 2, convert community `description.yml` to nebu `registry.yaml` format:

```python
# Example conversion
description_yml = {
    "processor": {
        "name": "awesome-processor",
        "type": "transform",
        "description": "Filter for awesome events",
        ...
    },
    "repo": {
        "github": "user/awesome-processor",
        "ref": "v1.0.0"
    }
}

registry_yaml = {
    "name": description_yml["processor"]["name"],
    "type": description_yml["processor"]["type"],
    "description": description_yml["processor"]["description"],
    "location": {
        "type": "git",
        "url": f"https://github.com/{description_yml['repo']['github']}",
        "ref": description_yml["repo"]["ref"]
    }
}
```

## Questions to Answer Before Phase 2

1. **Demand**: Are people submitting processors to the community registry?
2. **Quality**: Are submissions high quality or requiring heavy moderation?
3. **Support burden**: Are community processors causing support issues?
4. **Discovery**: Are users finding processors easily enough with manual discovery?
5. **Installation friction**: Is manual `git clone && go build` a real blocker?

---

**Status**: Phase 1 - Manual Discovery (Current)

**Next Steps**:
- Monitor community registry adoption
- Collect user feedback
- Revisit integration strategy in 3-6 months
