# Graduating to Flowctl

**When your nebu processor is ready for production, this guide helps you migrate to flowctl for orchestration, monitoring, and deployment.**

## Table of Contents

- [When to Graduate](#when-to-graduate)
- [Architecture Comparison](#architecture-comparison)
- [Migration Overview](#migration-overview)
- [Step-by-Step Migration](#step-by-step-migration)
- [Code Transformation](#code-transformation)
- [Proto Reusability](#proto-reusability)
- [Configuration Mapping](#configuration-mapping)
- [Testing Your Migration](#testing-your-migration)
- [Dual-Mode Processors](#dual-mode-processors)

---

## When to Graduate

Use this checklist to determine if your processor is ready for flowctl:

### Ready for Graduation

- [ ] Processor logic is stable and well-tested with nebu
- [ ] You need **multi-component pipelines** (source -> processor -> sink)
- [ ] You require **control plane orchestration** (health checks, restarts)
- [ ] You need **production monitoring** (metrics, dashboards)
- [ ] You're deploying to **containerized environments** (Docker, Kubernetes, Nomad)
- [ ] You need **event routing** between multiple processors
- [ ] You require **service discovery** for dynamic scaling

### Stay with Nebu

- Rapid prototyping and exploration
- Single-processor data extraction
- Ad-hoc analysis with Unix pipes
- Local development and testing

> **Tip:** Many developers use nebu for prototyping and graduate to flowctl for production. Both tools share the same proto-first philosophy.

---

## Architecture Comparison

### Nebu: Unix Pipe Model

```
┌─────────────────────────────────────────────────────────────┐
│                     nebu Architecture                       │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  nebu fetch       processor       filter        jq          │
│  ┌─────────┐     ┌─────────┐    ┌─────────┐   ┌─────────┐  │
│  │ Ledger  │────▶│ Extract │───▶│ Filter  │──▶│ Format  │  │
│  │ Fetcher │ XDR │ Events  │JSON│  Data   │   │ Output  │  │
│  └─────────┘     └─────────┘    └─────────┘   └─────────┘  │
│       │               │              │             │        │
│       └───────────────┴──────────────┴─────────────┘        │
│                    Unix Pipes (stdin/stdout)                │
│                                                             │
└─────────────────────────────────────────────────────────────┘

Characteristics:
- Simple process-per-stage model
- Text-based communication (JSON via protojson)
- No orchestration or health checks
- Ideal for rapid prototyping
```

### Flowctl: gRPC Orchestration Model

```
┌─────────────────────────────────────────────────────────────────────┐
│                      flowctl Architecture                           │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│                      ┌───────────────────┐                          │
│                      │   Control Plane   │                          │
│                      │   (flowctl run)   │                          │
│                      └─────────┬─────────┘                          │
│                                │                                    │
│            ┌───────────────────┼───────────────────┐                │
│            │ Registration      │ Health     │ Discovery             │
│            ▼                   ▼ Checks     ▼                       │
│     ┌─────────────┐     ┌─────────────┐     ┌─────────────┐         │
│     │   Source    │     │  Processor  │     │    Sink     │         │
│     │ (gRPC svc)  │────▶│ (gRPC svc)  │────▶│ (gRPC svc)  │         │
│     └─────────────┘     └─────────────┘     └─────────────┘         │
│            │                   │                   │                │
│            └───────────────────┴───────────────────┘                │
│                  Protobuf Events (efficient binary)                 │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘

Characteristics:
- gRPC-based service communication
- Binary protobuf encoding (more efficient)
- Health checks, heartbeats, restart policies
- Control plane orchestration
- Ideal for production deployments
```

### Key Differences

| Aspect | Nebu | Flowctl |
|--------|------|---------|
| Communication | Unix pipes (JSON text) | gRPC (binary protobuf) |
| Orchestration | None (manual process management) | Control plane with health checks |
| Configuration | CLI flags + env vars | Pipeline YAML + env vars |
| Deployment | Single process per stage | Multi-service orchestration |
| Monitoring | Manual (logs to stderr) | Built-in metrics, dashboards |
| Error handling | Process exit codes | Retry policies, backpressure |
| Proto serialization | `protojson` (text) | `proto.Marshal` (binary) |

---

## Migration Overview

The migration transforms your nebu processor from a CLI tool to a gRPC service:

```
Nebu Pattern                          Flowctl Pattern
═══════════════                       ═══════════════

┌──────────────────────┐              ┌──────────────────────┐
│  RunProtoOriginCLI   │              │    stellar.Run()     │
│  ────────────────    │              │    ────────────      │
│  - CLI flag parsing  │   ──────▶    │  - gRPC server       │
│  - Ledger iteration  │              │  - Control plane     │
│  - JSON output       │              │    registration      │
└──────────────────────┘              │  - Health checks     │
                                      └──────────────────────┘

┌──────────────────────┐              ┌──────────────────────┐
│  ProcessLedger()     │              │ EventsFromLedger()   │
│  ────────────────    │              │ ───────────────────  │
│  - XDR decoding      │   ──────▶    │  - Same XDR logic    │
│  - Event extraction  │              │  - Same extraction   │
│  - Proto population  │              │  - Same proto types  │
└──────────────────────┘              └──────────────────────┘

Your extraction logic stays the SAME!
Only the wrapper changes.
```

---

## Step-by-Step Migration

### Step 1: Review Your Nebu Processor

Identify the key components:

```go
// Typical nebu processor structure

// 1. Proto definitions (KEEP THESE)
type TokenEvent struct { ... }  // From your .proto file

// 2. Processor struct (ADAPT THIS)
type Origin struct {
    networkPass string
    out         chan *proto.TokenEvent
}

// 3. Core extraction logic (KEEP THIS)
func (o *Origin) ProcessLedger(ctx context.Context, ledger xdr.LedgerCloseMeta) error {
    events := o.extractEvents(ledger)  // Your extraction logic
    for _, event := range events {
        o.out <- event
    }
    return nil
}

// 4. CLI wrapper (REPLACE THIS)
func main() {
    cli.RunProtoOriginCLI(config, func(networkPass string) cli.ProtoOriginProcessor[*proto.TokenEvent] {
        return NewOrigin(networkPass)
    })
}
```

### Step 2: Refactor Core Logic

Extract your processing logic into a reusable function that matches the flowctl-sdk pattern:

```go
// Before: Nebu Origin processor
func (o *Origin) ProcessLedger(ctx context.Context, ledger xdr.LedgerCloseMeta) error {
    events := o.extractEvents(ledger)
    for _, event := range events {
        select {
        case <-ctx.Done():
            return ctx.Err()
        case o.out <- event:
        }
    }
    return nil
}

// After: Flowctl-compatible function
func EventsFromLedger(networkPassphrase string, ledger xdr.LedgerCloseMeta) (*proto.TokenEventBatch, error) {
    events := extractEvents(networkPassphrase, ledger)  // Same extraction logic
    return &proto.TokenEventBatch{Events: events}, nil
}
```

### Step 3: Create Flowctl Main

Replace the CLI wrapper with the flowctl-sdk:

```go
package main

import (
    "github.com/withObsrvr/flowctl-sdk/pkg/stellar"
    "github.com/stellar/go/xdr"
    proto "your-processor/proto"
)

func main() {
    stellar.Run(stellar.ProcessorConfig{
        ProcessorName: "Token Transfer Processor",
        OutputType:    "stellar.token.transfer.v1",
        ProcessLedger: func(networkPassphrase string, ledger xdr.LedgerCloseMeta) (proto.Message, error) {
            // Call your extraction logic
            return EventsFromLedger(networkPassphrase, ledger)
        },
    })
}
```

### Step 4: Create Pipeline Configuration

Create a pipeline YAML that orchestrates your processor:

```yaml
apiVersion: flowctl/v1
kind: Pipeline
metadata:
  name: token-transfer-pipeline

spec:
  driver: docker  # or "process" for local dev

  sources:
    - id: stellar-source
      image: "ghcr.io/withobsrvr/stellar-ledger-source:latest"
      env:
        NETWORK_PASSPHRASE: "Public Global Stellar Network ; September 2015"
        START_LEDGER: "60000000"

  processors:
    - id: token-transfer
      image: "ghcr.io/yourorg/token-transfer-processor:latest"
      inputs: ["stellar-source"]
      env:
        NETWORK_PASSPHRASE: "Public Global Stellar Network ; September 2015"

  sinks:
    - id: postgres-sink
      image: "ghcr.io/withobsrvr/postgres-consumer:latest"
      inputs: ["token-transfer"]
      env:
        POSTGRES_HOST: "localhost"
        POSTGRES_DB: "stellar_events"
```

---

## Code Transformation

### Before: Complete Nebu Processor

```go
// cmd/token-transfer/main.go (nebu)
package main

import (
    "context"

    "github.com/stellar/go/xdr"
    "github.com/withObsrvr/nebu/pkg/processor/cli"
    proto "token-transfer/proto"
)

const version = "1.0.0"

type Origin struct {
    networkPass string
    out         chan *proto.TokenEvent
}

func NewOrigin(networkPass string) *Origin {
    return &Origin{
        networkPass: networkPass,
        out:         make(chan *proto.TokenEvent, 128),
    }
}

func (o *Origin) ProcessLedger(ctx context.Context, ledger xdr.LedgerCloseMeta) error {
    events := extractTokenEvents(o.networkPass, ledger)
    for _, event := range events {
        select {
        case <-ctx.Done():
            return ctx.Err()
        case o.out <- event:
        }
    }
    return nil
}

func (o *Origin) Out() <-chan *proto.TokenEvent { return o.out }
func (o *Origin) Close()                         { close(o.out) }
func (o *Origin) Name() string                   { return "token-transfer" }

// extractTokenEvents contains your core business logic
func extractTokenEvents(networkPass string, ledger xdr.LedgerCloseMeta) []*proto.TokenEvent {
    // ... extraction logic ...
    return events
}

func main() {
    config := cli.OriginConfig{
        Name:        "token-transfer",
        Description: "Extract token transfer events",
        Version:     version,
    }

    cli.RunProtoOriginCLI(config, func(networkPass string) cli.ProtoOriginProcessor[*proto.TokenEvent] {
        return NewOrigin(networkPass)
    })
}
```

### After: Complete Flowctl Processor

```go
// cmd/token-transfer/main.go (flowctl)
package main

import (
    "github.com/stellar/go/xdr"
    "github.com/withObsrvr/flowctl-sdk/pkg/stellar"
    "google.golang.org/protobuf/proto"

    tokenproto "token-transfer/proto"
)

func main() {
    stellar.Run(stellar.ProcessorConfig{
        ProcessorName: "Token Transfer Processor",
        OutputType:    "stellar.token.transfer.v1",
        ProcessLedger: EventsFromLedger,
    })
}

// EventsFromLedger extracts token events from a ledger
// This is the SAME logic from your nebu processor, just restructured
func EventsFromLedger(networkPassphrase string, ledger xdr.LedgerCloseMeta) (proto.Message, error) {
    events := extractTokenEvents(networkPassphrase, ledger)

    if len(events) == 0 {
        return nil, nil  // No events to emit
    }

    return &tokenproto.TokenEventBatch{
        Events: events,
    }, nil
}

// extractTokenEvents - YOUR CORE LOGIC STAYS EXACTLY THE SAME
func extractTokenEvents(networkPass string, ledger xdr.LedgerCloseMeta) []*tokenproto.TokenEvent {
    // ... same extraction logic as nebu version ...
    return events
}
```

### Key Changes Summary

| Component | Nebu | Flowctl |
|-----------|------|---------|
| Entry point | `cli.RunProtoOriginCLI()` | `stellar.Run()` |
| Processor type | Interface with channel | Function returning `proto.Message` |
| Output | `chan *proto.Event` | Return `proto.Message` |
| Configuration | CLI flags | Environment variables + YAML |
| Core logic | **Unchanged** | **Unchanged** |

---

## Proto Reusability

Your proto definitions work in both environments without modification:

### Shared Proto Location

Store protos in a shared repository for reuse:

```
flow-proto/
├── proto/
│   └── stellar/
│       └── v1/
│           ├── token_transfer.proto    # Your event types
│           └── contract_events.proto   # Other event types
└── go/
    └── gen/
        └── stellar/
            └── v1/
                └── *.pb.go             # Generated Go code
```

### Using Shared Protos

Both nebu and flowctl processors import from the same source:

```go
// Works in both nebu and flowctl processors
import tokenproto "github.com/withObsrvr/flow-proto/go/gen/stellar/v1"

// Your event types are identical
event := &tokenproto.TokenTransferEvent{
    From:   "GA...",
    To:     "GB...",
    Amount: "1000000",
    // ...
}
```

### Proto Guidelines

1. **Use flow-proto for shared types**: Common event types should live in the flow-proto repository
2. **Keep processor-specific protos local**: If a proto is only used by one processor, keep it in that processor's repo
3. **Version your protos**: Use `v1`, `v2` suffixes for breaking changes

---

## Configuration Mapping

### Environment Variables

| Purpose | Nebu | Flowctl |
|---------|------|---------|
| Network | `NEBU_NETWORK` | `NETWORK_PASSPHRASE` |
| RPC endpoint | `NEBU_RPC_URL` | `STELLAR_RPC_URL` |
| Auth | `NEBU_RPC_AUTH` | `STELLAR_RPC_AUTH` |
| Start ledger | `--start-ledger` flag | `START_LEDGER` |
| End ledger | `--end-ledger` flag | `END_LEDGER` |
| Control plane | N/A | `FLOWCTL_ENDPOINT` |
| Enable flowctl | N/A | `ENABLE_FLOWCTL` |

### Example processor.yaml (flowctl)

```yaml
# processor.yaml - Flowctl processor configuration
processor:
  name: "Token Transfer Processor"
  description: "Extracts token transfer events from Stellar ledgers"
  version: "1.0.0"
  input: "stellar.ledger.v1"
  output: "stellar.token.transfer.v1"

network:
  passphrase: "Public Global Stellar Network ; September 2015"
  rpc_url: "https://mainnet.sorobanrpc.com"

flowctl:
  enabled: true
  endpoint: "localhost:8080"
  heartbeat_interval: 10000  # ms
```

---

## Testing Your Migration

### 1. Unit Test Your Extraction Logic

```go
func TestEventsFromLedger(t *testing.T) {
    // Load test ledger XDR
    ledger := loadTestLedger(t, "testdata/ledger_60000000.xdr")

    // Call your extraction function
    batch, err := EventsFromLedger(testNetworkPassphrase, ledger)

    require.NoError(t, err)
    require.NotNil(t, batch)

    // Verify events
    events := batch.(*tokenproto.TokenEventBatch).Events
    assert.GreaterOrEqual(t, len(events), 1)
}
```

### 2. Run Locally with Flowctl

```bash
# Build your processor
go build -o bin/token-transfer ./cmd/token-transfer

# Create a local pipeline config
cat > local-pipeline.yaml <<EOF
apiVersion: flowctl/v1
kind: Pipeline
metadata:
  name: test-pipeline

spec:
  driver: process

  sources:
    - id: stellar-source
      command: ["stellar-ledger-source"]
      env:
        NETWORK_PASSPHRASE: "Test SDF Network ; September 2015"
        START_LEDGER: "1000"
        END_LEDGER: "1010"

  processors:
    - id: token-transfer
      command: ["./bin/token-transfer"]
      inputs: ["stellar-source"]
      env:
        NETWORK_PASSPHRASE: "Test SDF Network ; September 2015"
EOF

# Run the pipeline
flowctl run local-pipeline.yaml
```

### 3. Compare Output

Run both versions and compare:

```bash
# Nebu output
nebu fetch --start-ledger 60000000 --end-ledger 60000001 | \
  ./bin/token-transfer-nebu | head -5 > nebu-output.json

# Flowctl output (capture from processor logs or sink)
# Compare event structure and counts
```

---

## Dual-Mode Processors

For maximum flexibility, you can create processors that work in both nebu and flowctl:

```go
package main

import (
    "os"

    "github.com/withObsrvr/flowctl-sdk/pkg/stellar"
    "github.com/withObsrvr/nebu/pkg/processor/cli"
)

func main() {
    // Detect environment
    if os.Getenv("FLOWCTL_ENDPOINT") != "" || os.Getenv("ENABLE_FLOWCTL") == "true" {
        // Run as flowctl processor
        runFlowctlMode()
    } else {
        // Run as nebu processor
        runNebuMode()
    }
}

func runFlowctlMode() {
    stellar.Run(stellar.ProcessorConfig{
        ProcessorName: "Token Transfer Processor",
        OutputType:    "stellar.token.transfer.v1",
        ProcessLedger: EventsFromLedger,
    })
}

func runNebuMode() {
    cli.RunProtoOriginCLI(cli.OriginConfig{
        Name:        "token-transfer",
        Description: "Extract token transfer events",
        Version:     "1.0.0",
    }, func(networkPass string) cli.ProtoOriginProcessor[*proto.TokenEvent] {
        return NewOrigin(networkPass)
    })
}
```

See [flowctl-sdk/examples/dual-mode-template](https://github.com/withObsrvr/flowctl-sdk/tree/main/examples/dual-mode-template) for a complete example.

---

## Next Steps

1. **Read the flowctl-sdk quickstart**: [QUICKSTART.md](https://github.com/withObsrvr/flowctl-sdk/blob/main/docs/QUICKSTART.md)
2. **Study example processors**: [flowctl-sdk/examples](https://github.com/withObsrvr/flowctl-sdk/tree/main/examples)
3. **Review the Stellar pattern**: [token_transfer.go](https://github.com/withObsrvr/flowctl-sdk/blob/main/pkg/stellar/helpers/token_transfer.go)
4. **Deploy with flowctl**: [flowctl documentation](https://github.com/withObsrvr/flowctl)

---

## Resources

- **flowctl-sdk**: https://github.com/withObsrvr/flowctl-sdk
- **flowctl**: https://github.com/withObsrvr/flowctl
- **flow-proto**: https://github.com/withObsrvr/flow-proto
- **nebu**: https://github.com/withObsrvr/nebu

---

## Getting Help

- **Migration questions**: https://github.com/withObsrvr/flowctl-sdk/discussions
- **Bug reports**: https://github.com/withObsrvr/flowctl-sdk/issues
- **Stellar questions**: https://discord.gg/stellar
