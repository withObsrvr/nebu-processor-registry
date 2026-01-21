# Building Protobuf-First Processors for Stellar

**The definitive guide to creating type-safe, production-ready nebu processors using Protocol Buffers.**

## Table of Contents

- [Why Proto-First?](#why-proto-first)
- [Prerequisites](#prerequisites)
- [The Proto-First Workflow](#the-proto-first-workflow)
- [Tutorial: Building a Complete Origin Processor](#tutorial-building-a-complete-origin-processor)
- [Modeling Stellar Data with Protobuf](#modeling-stellar-data-with-protobuf)
- [Working with Stellar XDR](#working-with-stellar-xdr)
- [Common Patterns](#common-patterns)
- [Real-World Examples](#real-world-examples)
- [Best Practices](#best-practices)
- [Troubleshooting](#troubleshooting)

---

## Why Proto-First?

Building processors with Protocol Buffers gives you:

### Type Safety
- **Compile-time validation**: Catch schema errors before runtime
- **IDE support**: Autocomplete, type checking, refactoring
- **No runtime type assertions**: Strong typing throughout your code

### Consistency
- **Single source of truth**: Proto files define your schema
- **Automatic JSON conversion**: Free serialization via `protojson`
- **Version compatibility**: Proto3 handles backward compatibility

### Production-Ready
- **Schema versioning**: Track schema changes explicitly
- **Documentation**: Proto comments become API docs
- **Multi-language**: Generate code for Python, Rust, TypeScript
- **gRPC-ready**: Add server in 10 lines for flowctl integration

### Example: Type Safety in Action

**Without protobuf** (map-based):
```go
// Runtime type assertions everywhere
amount, ok := event["transfer"].(map[string]interface{})["amount"].(string)
if !ok {
    // Handle missing field at runtime
}
```

**With protobuf** (type-safe):
```go
// Compile-time type checking
amount := event.Transfer.Amount  // Type: string, never nil if field exists
```

---

## Prerequisites

### Required Tools

1. **Go 1.21+**
   ```bash
   go version
   ```

2. **Protocol Buffers Compiler**
   ```bash
   # On NixOS/using Nix
   nix-shell -p protobuf

   # Or install system-wide
   # macOS: brew install protobuf
   # Ubuntu: apt install protobuf-compiler
   ```

3. **Go protobuf plugin**
   ```bash
   go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
   export PATH="$HOME/go/bin:$PATH"
   ```

4. **nebu repository**
   ```bash
   git clone https://github.com/withObsrvr/nebu
   cd nebu
   ```

### Verify Installation

```bash
protoc --version        # Should show libprotoc 3.x or higher
protoc-gen-go --version # Should show protoc-gen-go version
go version             # Should show go1.21 or higher
```

---

## The Proto-First Workflow

Building a protobuf-first processor follows this workflow:

```
1. Define Schema     →  2. Generate Code    →  3. Implement Logic   →  4. Build CLI
   (.proto file)        (protoc command)       (processor.go)          (main.go)
       ↓                       ↓                      ↓                     ↓
   Event types         Go structs with        Extract from XDR,      RunProtoOriginCLI
   Metadata fields     Marshal/Unmarshal      populate protos        wrapper
```

### Why This Order?

1. **Schema first** forces you to think about your data model upfront
2. **Generated code** gives you type-safe structs immediately
3. **Implementation** is just filling in strongly-typed structs
4. **CLI wrapper** handles all the plumbing (RPC, JSON output, flags)

---

## Tutorial: Building a Complete Origin Processor

We'll build a **contract invocation** processor that extracts function calls from Soroban transactions.

### Step 1: Design Your Schema

Think about what data you want to extract. For contract invocations:
- Function name
- Contract ID
- Arguments
- Success/failure
- State changes
- Metadata (ledger, tx hash, etc.)

### Step 2: Create Proto File

Create `examples/processors/contract-invocation/proto/contract_invocation.proto`:

```protobuf
syntax = "proto3";

package contract_invocation;

option go_package = "github.com/withObsrvr/nebu/examples/processors/contract-invocation/proto";

// ContractInvocation represents a Soroban contract function call
message ContractInvocation {
  // Contract being invoked
  string contract_id = 1;

  // Function name being called
  string function_name = 2;

  // Account that invoked the contract
  string invoking_account = 3;

  // Function arguments (decoded from XDR to JSON-like values)
  repeated ScVal arguments = 4;

  // Whether the invocation succeeded
  bool successful = 5;

  // Ledger entry state changes caused by this invocation
  repeated LedgerEntryChange state_changes = 6;

  // Diagnostic events emitted during execution
  repeated DiagnosticEvent diagnostic_events = 7;

  // Transaction metadata
  Metadata meta = 8;
}

// ScVal represents a Stellar Contract Value (simplified)
message ScVal {
  oneof value {
    string address_value = 1;
    string string_value = 2;
    uint64 u64_value = 3;
    string u128_value = 4;  // As string to avoid overflow
    int64 i64_value = 5;
    string i128_value = 6;
    bool bool_value = 7;
    bytes bytes_value = 8;
    ScVec vec_value = 9;
    ScMap map_value = 10;
  }
}

message ScVec {
  repeated ScVal values = 1;
}

message ScMap {
  repeated ScMapEntry entries = 1;
}

message ScMapEntry {
  ScVal key = 1;
  ScVal value = 2;
}

// LedgerEntryChange represents a state change
message LedgerEntryChange {
  string change_type = 1;  // "created", "updated", "removed"
  string entry_type = 2;   // "account", "contract_data", "ttl"
  bytes entry_xdr = 3;     // Raw XDR of the entry
}

// DiagnosticEvent represents a debug event
message DiagnosticEvent {
  bool in_successful_contract_call = 1;
  repeated ScVal topics = 2;
  ScVal data = 3;
}

// Metadata common to all events
message Metadata {
  uint32 ledger_sequence = 1;
  string tx_hash = 2;
  uint32 transaction_index = 3;
  uint32 operation_index = 4;
  int64 ledger_close_time = 5;
  bool in_successful_tx = 6;
}
```

### Step 3: Generate Go Code

```bash
cd examples/processors/contract-invocation

# Generate Go structs from proto
export PATH="$HOME/go/bin:$PATH"
nix-shell -p protobuf --run \
  "protoc --go_out=. --go_opt=paths=source_relative \
   proto/contract_invocation.proto"

# You now have: proto/contract_invocation.pb.go
```

This creates:
- `ContractInvocation` struct
- `ScVal`, `Metadata`, etc. structs
- Marshal/Unmarshal methods
- Protobuf type system integration

### Step 4: Implement Processor Logic

Create `contract_invocation.go`:

```go
package contract_invocation

import (
	"context"
	"fmt"

	"github.com/stellar/go/xdr"
	"github.com/withObsrvr/nebu/examples/processors/contract-invocation/proto"
	"github.com/withObsrvr/nebu/pkg/processor"
)

// Origin processor that extracts contract invocations
type Origin struct {
	networkPass string
	out         chan *proto.ContractInvocation
}

func NewOrigin(networkPass string) *Origin {
	return &Origin{
		networkPass: networkPass,
		out:         make(chan *proto.ContractInvocation, 128),
	}
}

// ProcessLedger extracts contract invocations from a ledger
func (o *Origin) ProcessLedger(ctx context.Context, ledger xdr.LedgerCloseMeta) error {
	// Extract invocations
	invocations := o.extractInvocations(ledger)

	// Emit each invocation
	for _, inv := range invocations {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case o.out <- inv:
			// Event emitted
		}
	}

	return nil
}

// extractInvocations does the heavy lifting
func (o *Origin) extractInvocations(ledger xdr.LedgerCloseMeta) []*proto.ContractInvocation {
	var invocations []*proto.ContractInvocation

	// Get ledger metadata
	ledgerSeq := ledger.LedgerSequence()
	ledgerCloseTime := int64(ledger.LedgerHeaderHistoryEntry().Header.ScpValue.CloseTime)

	// Iterate through transactions
	for txIdx, tx := range ledger.TransactionsWithMeta() {
		txHash := tx.TransactionHash()
		txSuccess := tx.Result.Successful()

		// Look for InvokeHostFunction operations
		for opIdx, op := range tx.Operations() {
			// Check if operation is InvokeHostFunction
			invokeOp, ok := op.Body.GetInvokeHostFunctionOp()
			if !ok {
				continue
			}

			// Extract invocation details
			invocation := &proto.ContractInvocation{
				ContractId:      extractContractId(invokeOp),
				FunctionName:    extractFunctionName(invokeOp),
				InvokingAccount: extractInvoker(op.SourceAccount),
				Arguments:       extractArguments(invokeOp),
				Successful:      txSuccess,
				StateChanges:    extractStateChanges(tx.Meta, opIdx),
				DiagnosticEvents: extractDiagnosticEvents(tx.Meta, opIdx),
				Meta: &proto.Metadata{
					LedgerSequence:    ledgerSeq,
					TxHash:            txHash.HexString(),
					TransactionIndex:  uint32(txIdx),
					OperationIndex:    uint32(opIdx),
					LedgerCloseTime:   ledgerCloseTime,
					InSuccessfulTx:    txSuccess,
				},
			}

			invocations = append(invocations, invocation)
		}
	}

	return invocations
}

// Helper functions for extraction
func extractContractId(op *xdr.InvokeHostFunctionOp) string {
	// Implementation: parse contract ID from operation
	// ...
	return ""
}

func extractFunctionName(op *xdr.InvokeHostFunctionOp) string {
	// Implementation: decode function name from XDR
	// ...
	return ""
}

func extractInvoker(source xdr.MuxedAccount) string {
	// Implementation: get account address
	// ...
	return ""
}

func extractArguments(op *xdr.InvokeHostFunctionOp) []*proto.ScVal {
	// Implementation: convert XDR ScVals to proto ScVals
	// ...
	return nil
}

func extractStateChanges(meta xdr.TransactionMeta, opIdx int) []*proto.LedgerEntryChange {
	// Implementation: extract state changes from transaction meta
	// ...
	return nil
}

func extractDiagnosticEvents(meta xdr.TransactionMeta, opIdx int) []*proto.DiagnosticEvent {
	// Implementation: extract diagnostic events
	// ...
	return nil
}

// Required interface methods
func (o *Origin) Out() <-chan *proto.ContractInvocation { return o.out }
func (o *Origin) Close()                                 { close(o.out) }
func (o *Origin) Name() string                           { return "contract-invocation" }
func (o *Origin) Type() processor.Type                   { return processor.TypeOrigin }
```

### Step 5: Create CLI Wrapper

Create `cmd/contract-invocation/main.go`:

```go
package main

import (
	"github.com/withObsrvr/nebu/examples/processors/contract-invocation"
	proto "github.com/withObsrvr/nebu/examples/processors/contract-invocation/proto"
	"github.com/withObsrvr/nebu/pkg/processor/cli"
)

const version = "1.0.0"

func main() {
	config := cli.OriginConfig{
		Name:        "contract-invocation",
		Description: "Extract contract invocations from Stellar ledgers",
		Version:     version,
	}

	// RunProtoOriginCLI handles:
	// - Connecting to RPC
	// - Processing ledgers
	// - Converting protos to JSON (protojson)
	// - Writing to stdout
	cli.RunProtoOriginCLI(config, func(networkPass string) cli.ProtoOriginProcessor[*proto.ContractInvocation] {
		return contract_invocation.NewOrigin(networkPass)
	})
}
```

### Step 6: Build and Test

```bash
# Initialize module
go mod init github.com/withObsrvr/nebu/examples/processors/contract-invocation
go mod tidy

# Build
go build -o ../../../bin/contract-invocation ./cmd/contract-invocation

# Test
NEBU_RPC_AUTH="Api-Key YOUR_KEY" \
  ./bin/contract-invocation --start-ledger 60200000 --end-ledger 60200001 | head -1 | jq
```

Expected output:
```json
{
  "_schema": "nebu.contract_invocation.v1",
  "_nebu_version": "1.0.0",
  "contractId": "CA...",
  "functionName": "transfer",
  "invokingAccount": "GA...",
  "arguments": [...],
  "successful": true,
  "stateChanges": [...],
  "diagnosticEvents": [...],
  "meta": {
    "ledgerSequence": 60200000,
    "txHash": "abc123...",
    ...
  }
}
```

---

## Modeling Stellar Data with Protobuf

### Common Patterns

#### 1. Event Types with Oneof

For processors that emit multiple event types (like token-transfer):

```protobuf
message TokenEvent {
  // Metadata common to all event types
  Metadata meta = 1;

  // Exactly one of these will be set
  oneof event {
    Transfer transfer = 10;
    Mint mint = 11;
    Burn burn = 12;
    Fee fee = 13;
  }
}

message Transfer {
  string from = 1;
  string to = 2;
  string amount = 3;
  string asset_code = 4;
}

message Mint {
  string to = 1;
  string amount = 2;
  string asset_code = 3;
}
```

**Why oneof?** Consumers can pattern match on event type:
```go
switch evt := event.Event.(type) {
case *proto.TokenEvent_Transfer:
    handleTransfer(evt.Transfer)
case *proto.TokenEvent_Mint:
    handleMint(evt.Mint)
}
```

#### 2. Amounts as Strings

Stellar uses int64/int128 for amounts. In proto:

```protobuf
message Transfer {
  string amount = 1;  // NOT int64 or uint64
}
```

**Why strings?**
- Avoids overflow (Stellar uses 128-bit amounts)
- Preserves precision for large numbers
- JSON-safe (JavaScript Number is 53-bit)

#### 3. Addresses as Strings

```protobuf
message Transfer {
  string from = 1;  // "GABC..."
  string to = 2;    // "GXYZ..."
  string contract_address = 3;  // "CA..."
}
```

Encode addresses as Stellar StrKey format (human-readable).

#### 4. Metadata Standard

All events should include common metadata:

```protobuf
message Metadata {
  uint32 ledger_sequence = 1;        // Ledger number
  string tx_hash = 2;                 // Transaction hash (hex)
  uint32 transaction_index = 3;      // Position in ledger
  uint32 operation_index = 4;        // Position in transaction
  int64 ledger_close_time = 5;       // Unix timestamp
  bool in_successful_tx = 6;         // Transaction succeeded?
  string contract_address = 7;       // Contract that emitted event (if applicable)
}
```

This enables TOID generation, filtering, and ordering.

#### 5. Repeated vs Optional

```protobuf
// ✓ Use repeated for collections
repeated ScVal arguments = 1;  // 0 or more arguments

// ✓ Use message for optional complex types
message Transfer {
  Fee fee = 1;  // May be nil if no fee
}

// ✗ Don't use repeated for single optional values
repeated string maybe_memo = 1;  // Confusing!
```

---

## Working with Stellar XDR

Stellar ledgers are encoded in XDR (External Data Representation). Your processor needs to decode XDR into protobuf.

### XDR → Protobuf Conversion Patterns

#### Pattern 1: Simple Field Mapping

```go
import "github.com/stellar/go/xdr"

// XDR operation → Proto message
func convertOperation(xdrOp xdr.Operation) *proto.Operation {
	return &proto.Operation{
		SourceAccount: xdrOp.SourceAccount.Address(),
		Type:          xdrOp.Body.Type.String(),
	}
}
```

#### Pattern 2: Enum Conversion

```go
// XDR enum → Proto string
func convertOperationType(xdrType xdr.OperationType) string {
	switch xdrType {
	case xdr.OperationTypePayment:
		return "payment"
	case xdr.OperationTypeCreateAccount:
		return "create_account"
	default:
		return "unknown"
	}
}
```

#### Pattern 3: Amount Conversion

```go
// XDR int64 → Proto string (7 decimal places)
func convertAmount(xdrAmount xdr.Int64) string {
	return strconv.FormatInt(int64(xdrAmount), 10)
}

// XDR Int128Parts → Proto string
func convertI128Amount(parts xdr.Int128Parts) string {
	// Combine high and low parts into string representation
	hi := uint64(parts.Hi)
	lo := uint64(parts.Lo)

	// Simple approach for positive numbers
	if hi == 0 {
		return strconv.FormatUint(lo, 10)
	}

	// For full int128 support, use big.Int
	val := new(big.Int)
	val.SetUint64(hi)
	val.Lsh(val, 64)
	val.Add(val, new(big.Int).SetUint64(lo))
	return val.String()
}
```

#### Pattern 4: Address Conversion

```go
import "github.com/stellar/go/strkey"

// XDR AccountID → Proto string (Stellar address)
func convertAccountID(xdrAccount xdr.AccountId) string {
	address, _ := xdrAccount.GetAddress()
	return address
}

// XDR SCAddress → Proto string
func convertSCAddress(scAddr xdr.ScAddress) string {
	switch scAddr.Type {
	case xdr.ScAddressTypeScAddressTypeAccount:
		return scAddr.AccountId.Address()
	case xdr.ScAddressTypeScAddressTypeContract:
		hash := scAddr.ContractId.HexString()
		encoded, _ := strkey.Encode(strkey.VersionByteContract, hash[:])
		return encoded
	}
	return ""
}
```

#### Pattern 5: ScVal Conversion (Soroban Values)

```go
// XDR ScVal → Proto ScVal
func convertScVal(xdrVal xdr.ScVal) *proto.ScVal {
	protoVal := &proto.ScVal{}

	switch xdrVal.Type {
	case xdr.ScValTypeScvU64:
		val := uint64(*xdrVal.U64)
		protoVal.Value = &proto.ScVal_U64Value{U64Value: val}

	case xdr.ScValTypeScvI64:
		val := int64(*xdrVal.I64)
		protoVal.Value = &proto.ScVal_I64Value{I64Value: val}

	case xdr.ScValTypeScvU128:
		parts := *xdrVal.U128
		protoVal.Value = &proto.ScVal_U128Value{
			U128Value: convertI128Amount(parts),
		}

	case xdr.ScValTypeScvString:
		val := string(*xdrVal.Str)
		protoVal.Value = &proto.ScVal_StringValue{StringValue: val}

	case xdr.ScValTypeScvAddress:
		addr := convertSCAddress(*xdrVal.Address)
		protoVal.Value = &proto.ScVal_AddressValue{AddressValue: addr}

	case xdr.ScValTypeScvVec:
		vec := &proto.ScVec{}
		if xdrVal.Vec != nil {
			for _, item := range *xdrVal.Vec {
				vec.Values = append(vec.Values, convertScVal(item))
			}
		}
		protoVal.Value = &proto.ScVal_VecValue{VecValue: vec}

	case xdr.ScValTypeScvMap:
		scMap := &proto.ScMap{}
		if xdrVal.Map != nil {
			for _, entry := range *xdrVal.Map {
				scMap.Entries = append(scMap.Entries, &proto.ScMapEntry{
					Key:   convertScVal(entry.Key),
					Value: convertScVal(entry.Val),
				})
			}
		}
		protoVal.Value = &proto.ScVal_MapValue{MapValue: scMap}
	}

	return protoVal
}
```

### Transaction Metadata Extraction

Stellar transactions include metadata about state changes:

```go
func extractStateChanges(txMeta xdr.TransactionMeta, opIdx int) []*proto.LedgerEntryChange {
	var changes []*proto.LedgerEntryChange

	// Get operation-specific meta
	switch meta := txMeta.(type) {
	case *xdr.TransactionMetaV3:
		if len(meta.Operations) > opIdx {
			opMeta := meta.Operations[opIdx]

			// Extract changes
			for _, change := range opMeta.Changes {
				changes = append(changes, convertLedgerEntryChange(change))
			}
		}
	}

	return changes
}

func convertLedgerEntryChange(xdrChange xdr.LedgerEntryChange) *proto.LedgerEntryChange {
	change := &proto.LedgerEntryChange{}

	switch xdrChange.Type {
	case xdr.LedgerEntryChangeTypeLedgerEntryCreated:
		change.ChangeType = "created"
		change.EntryType = xdrChange.Created.Data.Type.String()
		change.EntryXdr, _ = xdrChange.Created.MarshalBinary()

	case xdr.LedgerEntryChangeTypeLedgerEntryUpdated:
		change.ChangeType = "updated"
		change.EntryType = xdrChange.Updated.Data.Type.String()
		change.EntryXdr, _ = xdrChange.Updated.MarshalBinary()

	case xdr.LedgerEntryChangeTypeLedgerEntryRemoved:
		change.ChangeType = "removed"
		// Entry type from key
		change.EntryType = xdrChange.Removed.Type.String()
		change.EntryXdr, _ = xdrChange.Removed.MarshalBinary()
	}

	return change
}
```

---

## Common Patterns

### Pattern: Graceful Error Handling

```go
func (o *Origin) ProcessLedger(ctx context.Context, ledger xdr.LedgerCloseMeta) error {
	// Extract events
	events, err := o.extractEvents(ledger)
	if err != nil {
		// Log error but don't stop processing
		fmt.Fprintf(os.Stderr, "Warning: failed to extract from ledger %d: %v\n",
			ledger.LedgerSequence(), err)
		return nil  // Continue to next ledger
	}

	// Emit events...
	return nil
}
```

### Pattern: Context Cancellation

```go
func (o *Origin) ProcessLedger(ctx context.Context, ledger xdr.LedgerCloseMeta) error {
	events := o.extractEvents(ledger)

	for _, event := range events {
		select {
		case <-ctx.Done():
			return ctx.Err()  // Graceful shutdown
		case o.out <- event:
			// Event sent
		}
	}

	return nil
}
```

### Pattern: Buffered Channel

```go
func NewOrigin(networkPass string) *Origin {
	return &Origin{
		out: make(chan *proto.MyEvent, 128),  // Buffer for performance
	}
}
```

**Why buffer?** Decouples extraction from JSON serialization. Processor can work ahead.

---

## Real-World Examples

Study these official processors for complete patterns:

### 1. token-transfer (Multiple Event Types)
**Location**: `examples/processors/token-transfer/`
**Pattern**: Oneof for transfer/mint/burn/fee events
**Learn**: How to model event variants with oneof

### 2. contract-events (Event Decoding)
**Location**: `examples/processors/contract-events/`
**Pattern**: Decode Soroban contract events
**Learn**: Working with ScVal, event topics

### 3. contract-invocation (Complex Extraction)
**Location**: `examples/processors/contract-invocation/`
**Pattern**: Extract function calls with arguments
**Learn**: Parsing InvokeHostFunction operations, state changes

### Code Organization

All three follow this structure:
```
processor-name/
├── proto/
│   ├── processor_name.proto       # Schema definition
│   └── processor_name.pb.go       # Generated code
├── processor_name.go              # Core logic
├── cmd/processor-name/
│   └── main.go                    # CLI wrapper
├── go.mod                         # Module definition
└── README.md                      # Documentation
```

---

## Best Practices

### 1. Schema Design

**✓ Do:**
- Use clear, descriptive field names
- Document fields with comments
- Version your schema identifier
- Use oneof for event variants
- Include comprehensive metadata

**✗ Don't:**
- Use abbreviations (write `transaction_hash` not `tx_hash` in proto)
- Nest too deeply (flatten where possible)
- Mix concerns (separate metadata from data)

### 2. XDR Conversion

**✓ Do:**
- Handle all XDR enum cases
- Convert amounts to strings (avoid overflow)
- Use Stellar SDK helpers (`.Address()`, `.GetAddress()`)
- Preserve raw XDR when needed (state changes)

**✗ Don't:**
- Assume XDR fields are always present (check nil)
- Ignore error returns from XDR methods
- Use int64 for amounts > 2^63

### 3. Error Handling

**✓ Do:**
- Log warnings to stderr
- Continue processing on non-fatal errors
- Return errors for critical failures
- Respect context cancellation

**✗ Don't:**
- Write logs to stdout (breaks JSON)
- Silently skip errors
- Panic on unexpected data

### 4. Performance

**✓ Do:**
- Use buffered channels (128-512 capacity)
- Reuse allocations where possible
- Process ledgers in parallel (runtime handles this)
- Profile with real data

**✗ Don't:**
- Block in ProcessLedger
- Allocate inside tight loops
- Do network I/O per event

### 5. Testing

**✓ Do:**
- Test with real ledger data
- Test empty ledgers
- Test failed transactions
- Validate JSON output

**✗ Don't:**
- Test only with synthetic data
- Skip edge cases
- Ignore schema validation

---

## Troubleshooting

### Problem: protoc: command not found

**Solution:**
```bash
# Install protobuf compiler
nix-shell -p protobuf
# Or use system package manager
```

### Problem: protoc-gen-go: program not found

**Solution:**
```bash
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
export PATH="$HOME/go/bin:$PATH"
```

### Problem: Generated code has import errors

**Solution:**
Check your `go_package` option matches your module path:
```protobuf
option go_package = "github.com/YOUR_USERNAME/YOUR_REPO/proto";
```

### Problem: JSON output is missing fields

**Cause:** Protobuf omits zero values by default.

**Solution:** Use proto3 `optional` for explicit presence:
```protobuf
message Event {
  optional string memo = 1;  // Included even if empty
}
```

### Problem: Amount overflow in JSON

**Solution:** Use string for amounts, not int64:
```protobuf
message Transfer {
  string amount = 1;  // NOT int64
}
```

### Problem: XDR parsing errors

**Cause:** Stellar XDR structures are complex and deeply nested.

**Solution:**
- Use Stellar Go SDK helpers
- Check for nil before dereferencing
- Study working examples (token-transfer)

---

## Next Steps

### Learn and Build

1. **Study the official processors**:
   - Start with `token-transfer` (simpler)
   - Then `contract-events` (event decoding)
   - Finally `contract-invocation` (complex)

2. **Build your own processor**:
   - Choose a Stellar data type you want to extract
   - Design the protobuf schema
   - Implement the extraction logic
   - Test with real ledger data

3. **Submit to the registry**:
   - Follow [CONTRIBUTING.md](CONTRIBUTING.md)
   - Create a description.yml
   - Open a pull request

### Graduate to Production

Once your nebu processor is stable and well-tested, you can migrate it to **flowctl** for production deployment with:

- **Control plane orchestration** (health checks, restarts, scaling)
- **Multi-component pipelines** (source -> processor -> sink chains)
- **Production monitoring** (metrics, dashboards)
- **Containerized deployment** (Docker, Kubernetes, Nomad)

**Your proto definitions and core extraction logic stay the same!** Only the wrapper changes from `RunProtoOriginCLI` to `stellar.Run()`.

Read the migration guide: **[GRADUATING_TO_FLOWCTL.md](docs/GRADUATING_TO_FLOWCTL.md)**

### Shared Proto Definitions

For processors that will be used across the ecosystem, consider contributing your proto definitions to the shared repository:

- **flow-proto**: https://github.com/withObsrvr/flow-proto

This enables other developers to consume your event types in their pipelines.

### Join the Community

- Share your processor
- Help others build processors
- Suggest improvements to the guide

---

## Resources

- **Stellar Go SDK**: https://github.com/stellar/go
- **Protocol Buffers Guide**: https://protobuf.dev/
- **nebu Core Docs**: https://withobsrvr.github.io/nebu/
- **XDR Spec**: https://github.com/stellar/stellar-xdr
- **Soroban Docs**: https://soroban.stellar.org/

---

## Getting Help

- **Registry Issues**: https://github.com/withObsrvr/nebu-processor-registry/issues
- **Processor Development**: https://github.com/withObsrvr/nebu/discussions
- **Stellar Questions**: https://discord.gg/stellar

Happy building! 🚀
