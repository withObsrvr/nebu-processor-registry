# ledger-change-stats

Count ledger entry changes by change type, by reason, and by entry type — plus the entries evicted at ledger close.

One event per ledger.

## What this adds over the original

This is a rebuild of the `ledger-change-stats` example from the nebu repo, moved into the registry so it is installable and queryable from `nebu-sql`. Four things were wrong or missing:

**1. Restores were invisible.** The original derived the change type from whether `Pre`/`Post` were nil:

```go
if change.Pre == nil && change.Post != nil {
    return xdr.LedgerEntryChangeTypeLedgerEntryCreated
}
```

A restore presents the same way, so restorations were counted as creations. `ingest.Change` carries `ChangeType` directly, including `LedgerEntryRestored`; this version reads it.

**2. Two change reasons were dropped entirely.** The original switched on three reasons. `LedgerEntryChangeReasonFeeRefund` and `LedgerEntryChangeReasonUpgrade` fell through and counted toward nothing — refunds are not rare, at roughly 107 changes per mainnet ledger in the sampled range. Unknown reasons now get their own counter rather than being silently absorbed.

**3. State entries inflated mutation counts.** `LedgerEntryChangeTypeLedgerEntryState` records an entry's value before a transaction observed it. It is not a mutation. It is now counted separately so `created + updated + deleted + restored` is a true mutation total.

**4. No entry-type split, and no eviction.** Both added — see below.

## Why not just use contract-state

`contract-state` sees contract data only. This walks the full change stream, so it sees classic entries too. On mainnet ledger 60200000:

| entry type | created | updated | deleted | total |
|---|---|---|---|---|
| account | 0 | 1038 | 0 | 1038 |
| trustline | 0 | 382 | 0 | 382 |
| offer | 17 | 151 | 14 | 182 |
| claimable_balance | 0 | 0 | 90 | 90 |
| liquidity_pool | 0 | 72 | 0 | 72 |
| contract_data | 8 | 152 | 22 | 182 |
| ttl | 8 | 0 | 22 | 30 |

`contract-state` would have reported the 182 contract-data changes and none of the other 1,794.

## Installation

```bash
nebu install ledger-change-stats
```

## Usage

```bash
# Bounded range
ledger-change-stats --start-ledger 60200000 --end-ledger 60200100 -q

# Entry-type profile for one ledger
ledger-change-stats --start-ledger 60200000 --end-ledger 60200000 -q | jq '.entryTypes'

# Ledgers where anything was archived or restored
ledger-change-stats --start-ledger 60200000 --end-ledger 60201000 -q \
  | jq -c 'select(.evictedKeys > 0 or .ledgerEntriesRestored > 0)
           | {ledger: .ledgerSequence, archived: .evictedKeys, restored: .ledgerEntriesRestored}'
```

Via `nebu-sql`:

```bash
nebu-sql -c "
SELECT
  ledgerSequence,
  ledgerEntriesCreated  AS created,
  ledgerEntriesUpdated  AS modified,
  ledgerEntriesDeleted  AS deleted,
  ledgerEntriesRestored AS restored,
  evictedKeys           AS archived
FROM nebu('ledger-change-stats', start = 60200000, stop = 60200010)
ORDER BY 1
"
```

## Fields

| Field | Description |
|---|---|
| `ledgerSequence`, `closedAtUnix` | Ledger identity and close time |
| `ledgerEntriesCreated` / `Updated` / `Deleted` / `Restored` | Mutations by change type |
| `ledgerEntriesState` | State snapshots — **not** mutations. **Always zero in practice**; the ingest change reader elides state entries before this processor sees them. Kept so the five change-type buckets sum to `totalChanges`. Do not build on it |
| `totalChanges` | Every change seen, state included |
| `feeRelatedChanges`, `feeRefundRelatedChanges`, `txRelatedChanges`, `operationRelatedChanges`, `upgradeRelatedChanges`, `unknownReasonChanges` | Changes by reason; these sum to `totalChanges` |
| `entryTypes[]` | Per-entry-type `{created, updated, deleted, restored, state, total}`. Only types that saw a change appear, ordered by the XDR enum |
| `evictedKeys` | Entries evicted at close because their lifetime ran out |
| `evictedByType[]` | Eviction counts by entry type |
| `evictionAvailable` | **Check this before reading `evictedKeys`.** False when the ledger's meta version predates eviction reporting; a zero count would otherwise read as "nothing was evicted" |

Ordering of both per-type lists follows the XDR enum rather than map iteration, so identical input produces identical bytes across runs.

## Verification status

Every field is verified against live chain data.

**Counts and entry-type split** — mainnet 60200000–60200060.

**Evictions** — mainnet 61500000–61500099, which sits inside a bulk eviction sweep: exactly 2,000 per ledger, split 1,000 `contract_data` + 1,000 `ttl`. The 1:1 entry-to-TTL pairing is the internal check that the number is real, and the flat 2,000 is the per-ledger eviction cap. Testnet evicts more naturally, 4–56 per ledger on most ledgers.

**Restores** — verified on both networks.

Mainnet, in the 1,200 ledgers immediately following the 61.5M eviction sweep:

| ledger | restored | breakdown |
|---|---|---|
| 61500126 | 22 | `contract_data` 11 + `ttl` 11 |
| 61500233 | 2 | `contract_data` 1 + `ttl` 1 |
| 61500252 | 2 | `contract_data` 1 + `ttl` 1 |
| 61500320 | 2 | `contract_data` 1 + `ttl` 1 |
| 61501156 | 4 | `contract_data` 2 + `ttl` 2 |

Testnet: 3966108, 3966250, 3966310, 3966376, 3950232, 3950254, 3950256. Ledger 3966108 restored 4 — `contract_data` 1 + `contract_code` 1 + `ttl` 2.

In every case the TTL count equals the number of non-TTL entries restored, because restoring an entry restores its TTL alongside it. That pairing is the internal check that the counts are real.

**Restores cluster after evictions.** They were absent from every range sampled away from archival activity, and appeared immediately after the 61.5M sweep — entries evicted to the hot archive being pulled back by contracts that still needed them. Sample near an eviction sweep, not at random.

### Restores do not come from RestoreFootprintOp

Worth knowing before anyone writes a detector for this.

Ledger 3966108 contains **no `RestoreFootprintOp`** — its operations are 15 × `op_INVOKE_HOST_FUNCTION_SUCCESS`, 3 × `op_MANAGE_SELL_OFFER_SUCCESS`, 2 × `op_SET_TRUST_LINE_FLAGS_SUCCESS`. Under [CAP-0062](https://github.com/stellar/stellar-protocol/blob/master/core/cap-0062.md), archived entries in a transaction's readWrite footprint are restored automatically during `InvokeHostFunction`, emitting `LedgerEntryRestored` changes with no dedicated operation.

Scanning operation result codes for `RESTORE` therefore finds nothing even on ledgers that restored entries — confirmed across ~5,400 mainnet and 1,400 testnet ledgers, zero hits, while the change stream shows restores on both networks in the ledgers tabled above. **Detect restores from the change stream, never from operation codes.**

### Reproducing

Mainnet, just after the eviction sweep:

```bash
ledger-change-stats --start-ledger 61500100 --end-ledger 61501299 -q \
  | jq -c 'select(.ledgerEntriesRestored > 0)
           | {ledger: .ledgerSequence, restored: .ledgerEntriesRestored,
              types: [.entryTypes[] | select(.restored > 0)]}'
```

Testnet:

```bash
ledger-change-stats \
  --rpc-url "https://soroban-testnet.stellar.org" \
  --network "Test SDF Network ; September 2015" \
  --start-ledger 3966000 --end-ledger 3966599 -q \
  | jq -c 'select(.ledgerEntriesRestored > 0) | {ledger: .ledgerSequence, restored: .ledgerEntriesRestored}'
```

Restores are sparse — roughly 5 ledgers per 1,200 even in an active region — so a short sample proves nothing. Watch stderr: both public RPCs return HTTP 429 above about two concurrent scanners, and a rate-limited run exits early and looks exactly like a clean run that found nothing.

## Configuration

| Flag | Description | Default |
|---|---|---|
| `--start-ledger` | Starting ledger sequence | — |
| `--end-ledger` | Ending ledger sequence (0 for unbounded) | 0 |
| `--rpc-url` | Stellar RPC endpoint | `https://archive-rpc.lightsail.network` |
| `--network` | Network passphrase | Mainnet |
| `-q, --quiet` | Suppress startup banner | false |

## Related

- `soroban-tx-resources` — per-transaction resource footprints and result codes
- `contract-state` — contract data entry changes with key/value payloads
- `ttl-tracker` — TTL expirations per entry

## License

MIT
