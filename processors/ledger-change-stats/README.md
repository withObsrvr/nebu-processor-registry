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
| `ledgerEntriesState` | State snapshots — **not** mutations |
| `totalChanges` | Every change seen, state included |
| `feeRelatedChanges`, `feeRefundRelatedChanges`, `txRelatedChanges`, `operationRelatedChanges`, `upgradeRelatedChanges`, `unknownReasonChanges` | Changes by reason; these sum to `totalChanges` |
| `entryTypes[]` | Per-entry-type `{created, updated, deleted, restored, state, total}`. Only types that saw a change appear, ordered by the XDR enum |
| `evictedKeys` | Entries evicted at close because their lifetime ran out |
| `evictedByType[]` | Eviction counts by entry type |
| `evictionAvailable` | **Check this before reading `evictedKeys`.** False when the ledger's meta version predates eviction reporting; a zero count would otherwise read as "nothing was evicted" |

Ordering of both per-type lists follows the XDR enum rather than map iteration, so identical input produces identical bytes across runs.

## Verification status

Counts and the entry-type split are verified against live mainnet (ledgers 60200000–60200060).

**Restores and evictions were zero across that whole range**, so those two paths are not confirmed against real data here — the range contains no `RestoreFootprint` operations at all, which is why. Both branches are covered by unit tests over constructed changes instead. Treat the live behaviour of `ledgerEntriesRestored` and `evictedKeys` as untested until someone runs this over a range known to contain archival activity.

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
