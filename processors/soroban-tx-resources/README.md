# soroban-tx-resources

Extract per-transaction Soroban resource footprints, decoded result codes, fees, and envelope sizes from Stellar ledgers.

One event per transaction, in apply order, failed transactions included.

## Why this exists

Every other origin answers "what happened". This one answers "what was reserved, and what went wrong".

The distinction that motivates it: **the protocol charges a ledger's capacity against the entries DECLARED in each transaction's readWrite footprint, not against the entries observed to change.** `contract-state` can tell you what changed; only the declared footprint can tell you whether a ledger was write-bound. Measured over mainnet ledgers 60200000–60200008, the observed-change proxy undercounts declared writes by roughly 20–70%:

| ledger | declared write entries | contract-state + ttl-tracker proxy |
|---|---|---|
| 60200000 | 325 | 112 |
| 60200002 | 345 | 246 |
| 60200008 | 352 | 286 |

Four facts here are reachable from no other nebu origin:

| Fact | Field | Nearest alternative |
|---|---|---|
| Footprint entry counts | `readOnlyEntries`, `readWriteEntries` | none — `contract-invocation` carries no resource footprint |
| Envelope size | `envelopeSizeBytes` | none |
| Decoded result codes | `resultCode`, `operationResultCodes` | `contract-invocation.successful` (bool only) |
| Contract error number | `contractErrorCode` | `diagnosticEvents` carries `fn_return` topics, not error codes |

## Installation

```bash
nebu install soroban-tx-resources
```

## Usage

```bash
# Bounded range
soroban-tx-resources --start-ledger 60200000 --end-ledger 60200100 -q

# Streaming
soroban-tx-resources --start-ledger 60200000

# To JSONL
soroban-tx-resources --start-ledger 60200000 --end-ledger 60200100 -q \
  | json-file-sink --out resources.jsonl
```

### Per-ledger capacity numerators

The aggregate this processor was built to make possible:

```bash
nebu-sql -c "
SELECT
  CAST(json_extract_string(meta, '\$.ledgerSequence') AS BIGINT) AS ledger_sequence,
  COUNT(*)                                              AS transactions,
  COUNT(*) FILTER (WHERE isSoroban = 'true')            AS soroban_transactions,
  SUM(CAST(readWriteEntries AS BIGINT))                 AS write_entries,
  SUM(CAST(readOnlyEntries  AS BIGINT))                 AS read_entries,
  SUM(CAST(instructions     AS BIGINT))                 AS instructions,
  SUM(CAST(envelopeSizeBytes AS BIGINT))                AS envelope_bytes
FROM nebu('soroban-tx-resources', start = 60200000, stop = 60200010)
GROUP BY 1 ORDER BY 1
"
```

Divide each by the matching `ConfigSettingContractLedgerCostV0` /
`ConfigSettingContractComputeV0` limit in force at that ledger to get
utilisation. Those caps change by protocol upgrade, which is exactly why this
processor emits numerators only and leaves the division downstream.

### Failure grouping

```bash
soroban-tx-resources --start-ledger 60200000 --end-ledger 60200020 -q \
  | jq -c 'select(.successful==false)
           | {resultCode, op: .operationResultCodes[0],
              err: .contractErrorType, code: .contractErrorCode}' \
  | sort | uniq -c | sort -rn
```

## Fields

| Field | Description |
|---|---|
| `successful` | Whether the transaction applied |
| `resultCode` | Horizon-style code, e.g. `tx_SUCCESS`, `tx_BAD_SEQ`, `tx_FEE_BUMP_INNER_FAILED` |
| `operationResultCodes` | Per-operation codes in operation order, e.g. `op_INVOKE_HOST_FUNCTION_TRAPPED`, `op_PAYMENT_NO_TRUST` |
| `sourceAccount` | Transaction source (fee account for a fee bump) |
| `operationCount` | Operations in the transaction |
| `isSoroban` / `isFeeBump` | Shape flags |
| `contractErrorPresent` | A diagnostic event carried an `ScError` |
| `contractErrorType` | `contract`, `wasm_vm`, `budget`, `storage`, `auth`, … |
| `contractErrorCode` | The contract's own error number — the `#4` in `#4 HealthFactorTooLow`. Meaningful only when `contractErrorType` is `contract` |
| `feeChargedStroops` | Fee actually charged. In a contested ledger this is the clearing price, identical across all included transactions |
| `maxFeeStroops` | The bid. A transaction charged far below its bid is evidence the market did not clear at the bid |
| `resourceFeeStroops` | Declared Soroban resource fee; zero for classic |
| `envelopeSizeBytes` | Length of the marshaled `TransactionEnvelope` |
| `instructions`, `diskReadBytes`, `writeBytes` | Declared Soroban resources |
| `readOnlyEntries`, `readWriteEntries` | Footprint entry counts — the capacity numerators |
| `meta.transactionIndex` | Apply order. Stellar shuffles this deterministically from the previous ledger hash, so it is **not** fee order |

## Result code decoding

Operation results resolve generically: every inner arm carries a `Code` whose
generated `String()` follows the same naming convention, so one reflective
lookup handles Soroban and classic alike and will not silently miss an
operation type added by a future protocol. Unrecognised values keep their
numeric identity rather than being folded into an existing bucket — a
mislabelled failure code is worse than an obviously unknown one.

## Known limits

- **Non-contract `ScError`s report their type but not their `ScErrorCode`.** A
  `storage` or `budget` error emits `contractErrorType` with
  `contractErrorCode` zero. Only contract-defined errors carry a number.
- **First error wins.** A contract that errors typically unwinds, and later
  diagnostic events describe the unwinding rather than the cause.
- **Declared, not consumed.** These are footprint declarations. A transaction
  may declare more than it uses; the protocol charges the declaration.
- **Eviction and restoration are not here.** Those live on `LedgerCloseMeta`
  (`EvictedLedgerKeys()`), not on any transaction, so they belong in a
  ledger-scoped processor.

## Configuration

| Flag | Description | Default |
|---|---|---|
| `--start-ledger` | Starting ledger sequence | — |
| `--end-ledger` | Ending ledger sequence (0 for unbounded) | 0 |
| `--rpc-url` | Stellar RPC endpoint | `https://archive-rpc.lightsail.network` |
| `--network` | Network passphrase | Mainnet |
| `-q, --quiet` | Suppress startup banner | false |

The default is a public third-party archive. For Obsrvr's own endpoints use
`--rpc-url https://gateway.withobsrvr.com/rpc/{mainnet,testnet}/` with
`NEBU_RPC_AUTH="Api-Key <key>"`; they serve archive depth transparently, so the
same URL works for both live and historical ranges.

## License

MIT
