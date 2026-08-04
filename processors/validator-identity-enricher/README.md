# Validator Identity Enricher

`validator-identity-enricher` is a Nebu transform that joins
`validator-analytics` events to stable validator identity metadata. It emits a
new `nebu.validator_analytics_enriched.v1` record while preserving every
upstream ledger fact.

Identity is deliberately separate from the deterministic origin processor:

```text
ledger source -> validator-analytics -> validator-identity-enricher -> sink
```

The default adapter resolves the validator public key through Radar. An
offline snapshot adapter supports reproducible historical backfills and local
development without an HTTP dependency.

## Build and test

```bash
go test ./...
go build -o validator-identity-enricher ./cmd/validator-identity-enricher
./validator-identity-enricher --describe-json | jq .
```

The processor uses `github.com/stellar/go-stellar-sdk v0.7.1` for Stellar
public-key and network validation and `github.com/withObsrvr/nebu v0.6.13` for
the standard transform CLI.

## Live Radar enrichment

For a validator analytics stream, network selection defaults to `auto` and is
derived from each record's `networkPassphrase`:

```bash
validator-analytics \
  --network mainnet \
  --start-ledger 60200000 \
  --end-ledger 60200100 \
  -q | \
validator-identity-enricher -q | jq .
```

Radar defaults:

- mainnet: `https://radar.withobsrvr.com/api`
- testnet: `https://radar.withobsrvr.com/testnet-api`

Use `--radar-url` for a local server, proxy, or controlled integration test.
Use `--identity-at 2026-07-01T00:00:00Z` to send Radar's optional fixed `at`
query on every lookup. A fixed time keeps the cache key coherent; this
processor does not perform a different HTTP request for every ledger close
time.

## Deterministic snapshot enrichment

Radar's bulk endpoint can be pinned and replayed:

```bash
curl -sS https://radar.withobsrvr.com/api/v1/node \
  -o radar-mainnet-nodes.json

validator-analytics \
  --network mainnet \
  --start-ledger 60200000 \
  --end-ledger 60200100 \
  -q | \
validator-identity-enricher \
  --source snapshot \
  --snapshot radar-mainnet-nodes.json \
  --network mainnet \
  -q | jq .
```

The snapshot adapter accepts either Radar's raw node array or this canonical
envelope:

```json
{
  "network": "mainnet",
  "generatedAt": "2026-07-20T12:00:00Z",
  "nodes": [
    {
      "publicKey": "GCGB2S2KGYARPVIA37HYZXVRM2YZUEXA6S33ZU5BUDC6THSB62LZSTYH",
      "name": "SDF 1",
      "alias": "sdf1",
      "homeDomain": "www.stellar.org",
      "organizationId": "266107f8966d45eedce41fee2581326d",
      "dateUpdated": "2026-07-15T21:06:29.862Z"
    }
  ]
}
```

## Output

All input fields remain unchanged except:

- `_schema` becomes `nebu.validator_analytics_enriched.v1`;
- `_nebu_version` records the Nebu library version used by this transform;
- `validatorIdentity` is added.

Example identity:

```json
{
  "status": "resolved",
  "publicKey": "GCGB2S2KGYARPVIA37HYZXVRM2YZUEXA6S33ZU5BUDC6THSB62LZSTYH",
  "name": "SDF 1",
  "alias": "sdf1",
  "homeDomain": "www.stellar.org",
  "organizationId": "266107f8966d45eedce41fee2581326d",
  "source": "radar",
  "sourceUpdatedAt": "2026-07-15T21:06:29.862Z",
  "resolvedAt": "2026-07-20T15:04:05Z",
  "temporalBasis": "current"
}
```

`status` has three stable values:

- `resolved`: identity fields were found;
- `not_found`: Radar returned 404 or the key was absent from the snapshot;
- `unavailable`: attribution, network selection, the snapshot, or Radar could
  not provide an answer.

`reason` supplies a machine-readable explanation for non-resolved states. A
non-resolved identity never filters or drops the ledger record.

Only stable identity fields are copied. Radar health, lag, overload,
centrality, and validator availability metrics change independently of ledger
facts and should use a separate temporal dataset.

## Reliability and performance

- The default per-identity timeout is 1 second, including one transient retry.
- Resolved identities and 404s are cached for the processor lifetime.
- Unavailable results are cached for 30 seconds to avoid hammering Radar during
  an outage, then retried.
- The LRU cache is bounded to 4,096 distinct `(network, validator)` keys.
- Warnings are deduplicated by validator and reason and written to stderr.
- JSONL data remains the only stdout output.

Relevant flags:

| Flag | Default | Purpose |
|---|---:|---|
| `--source` | `radar` | Select `radar` or `snapshot` |
| `--network` | `auto` | Select `auto`, `mainnet`, or `testnet` |
| `--snapshot` | | Snapshot file for offline enrichment |
| `--radar-url` | | Override both network-specific Radar bases |
| `--identity-at` | | Fixed RFC3339 historical identity time |
| `--timeout` | `1s` | Total lookup deadline including retries |
| `--retries` | `1` | Transient Radar retries |
| `--retry-delay` | `50ms` | Delay between retries |
| `--cache-size` | `4096` | Maximum cached keys |
| `--failure-cache-ttl` | `30s` | Unavailable-result suppression window |

## Developer guidance

Use the Radar adapter for recent exploratory streams and live processing. Use a
pinned snapshot for tests, offline work, and replayable backfills. In the
production Lake path, persist validator identities as a temporal dimension and
join them into the bounded recent-ledger serving projection; Prism should not
call Radar once per displayed ledger row.
