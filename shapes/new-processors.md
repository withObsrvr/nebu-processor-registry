# New Processor Shapes

12 processor shapes derived from gaps between `stellar-ledger-data-indexer` and the existing nebu processor registry.

---

## 1. contract-state (Origin)

**Problem:** Nebu has contract *events* and *invocations* but no way to extract the actual contract key/value state from ledgers. The stellar-ledger-data-indexer does this (`contract_data.go`) but it's monolithic and writes directly to Postgres. Users need composable access to contract state changes.

**Appetite:** 1 week

**Solution:** An origin processor that reads ledger XDR, extracts `LedgerEntryTypeContractData` changes, and emits structured JSON events per contract state entry. Uses the Stellar Go SDK's `contract.TransformContractData` (same as the indexer). Deduplicates multiple changes to the same entry within a ledger.

**Output event:**
```json
{
  "_schema": "nebu.contract_state.v1",
  "contractId": "C...",
  "ledgerKeyHash": "abc123...",
  "durability": "persistent",
  "keySymbol": "Balance",
  "key": "<base64 XDR>",
  "val": "<base64 XDR>",
  "meta": {
    "ledgerSequence": 60200001,
    "timestamp": "2024-12-10T14:30:00Z"
  }
}
```

**Flags:** `--start-ledger`, `--end-ledger`, `--rpc-url`, `--network`, `--decode` (attempt human-readable key/val decoding), `-q`

**Rabbit Holes:**
- Don't try to decode all ScVal types on day 1; base64 XDR is fine as default
- Don't build a diffing engine (full state vs delta) — just emit what's in the ledger changes
- Don't track historical state; this is a stream, not a database

**No-Gos:**
- No built-in Postgres write (that's what postgres-sink is for)
- No TTL data (separate processor)
- No contract code/WASM extraction

**Scope Line:**
```
MUST HAVE:   Extract contract data changes, emit JSON, dedup per ledger
NICE TO HAVE: --decode flag for human-readable ScVal
COULD HAVE:  --contract flag to filter by contract ID at source
```

**Done:** `contract-state --start-ledger 60200000 --end-ledger 60200010 | jq` shows contract state entries with contractId, key, val, durability.

---

## 2. ttl-tracker (Origin)

**Problem:** Contract data entries on Stellar have TTLs (time-to-live) that determine when they expire. The indexer tracks this but there's no composable way to stream TTL changes. Users managing contracts need to know when entries are approaching expiration.

**Appetite:** 3 days

**Solution:** Origin processor that extracts `LedgerEntryTypeTtl` changes from ledger XDR. Uses `contract.TransformTtl` from the Stellar Go SDK. Deduplicates by KeyHash+LedgerSequence.

**Output event:**
```json
{
  "_schema": "nebu.ttl_tracker.v1",
  "keyHash": "abc123...",
  "expirationLedger": 61000000,
  "meta": {
    "ledgerSequence": 60200001,
    "timestamp": "2024-12-10T14:30:00Z"
  }
}
```

**Flags:** `--start-ledger`, `--end-ledger`, `--rpc-url`, `--network`, `-q`

**Rabbit Holes:**
- Don't correlate TTLs with their contract data entries (that's a downstream join)
- Don't calculate "time remaining" — emit raw expiration ledger, let consumers do math

**No-Gos:**
- No alerting logic (combine with a transform for that)
- No contract data in output (use contract-state for that)

**Scope Line:**
```
MUST HAVE:   Extract TTL changes, emit JSON, dedup per ledger
NICE TO HAVE: --expiring-within flag to filter entries expiring within N ledgers
COULD HAVE:  Nothing else — keep it minimal
```

**Done:** `ttl-tracker --start-ledger 60200000 --end-ledger 60200010 | jq` shows TTL entries with keyHash and expirationLedger.

---

## 3. contract-filter (Transform)

**Problem:** Users tracking a specific protocol (DEX, lending pool, NFT marketplace) need to filter any event stream by contract ID. Currently requires jq with long select expressions.

**Appetite:** 3 days

**Solution:** Transform processor that reads JSON events from stdin and passes through only events matching one or more contract IDs. Checks common field paths: `contractId`, `transfer.from`, `transfer.to`, `contract`, `meta.contractId`.

**Flags:** `--contract <ID>` (repeatable), `--contract-file <path>` (one ID per line), `--field <path>` (custom field to check), `-q`

**Rabbit Holes:**
- Don't try to auto-detect every possible contract ID field location
- Don't resolve contract aliases or names

**No-Gos:**
- No contract metadata enrichment
- No regex matching on contract IDs

**Scope Line:**
```
MUST HAVE:   Filter by one or more contract IDs, check standard fields
NICE TO HAVE: --contract-file for large contract lists
COULD HAVE:  --field for custom field path
```

**Done:** `contract-events --start-ledger 60200000 | contract-filter --contract CABC...XYZ | jq` shows only events for that contract.

---

## 4. account-filter (Transform)

**Problem:** "Show me everything involving account X" is the most common ad-hoc query. Currently requires complex jq expressions checking multiple fields (from, to, account, source).

**Appetite:** 3 days

**Solution:** Transform that filters events by one or more Stellar account addresses. Checks `from`, `to`, `account`, `source`, `transfer.from`, `transfer.to`, `fee.account`.

**Flags:** `--account <G.../C...>` (repeatable), `--account-file <path>`, `--role <any|source|destination>` (default: any), `-q`

**Rabbit Holes:**
- Don't resolve account aliases or federation addresses
- Don't deep-search nested arrays for addresses

**No-Gos:**
- No account metadata enrichment (balances, signers, etc.)
- No muxed account ID decomposition

**Scope Line:**
```
MUST HAVE:   Filter by account address(es), check standard fields, role=any
NICE TO HAVE: --role flag for source-only or destination-only filtering
COULD HAVE:  --account-file for watchlists
```

**Done:** `token-transfer --start-ledger 60200000 | account-filter --account GABC...XYZ | jq` shows only events involving that account.

---

## 5. asset-filter (Transform)

**Problem:** `usdc-filter` is hardcoded to USDC. Users need to filter by any asset (AQUA, yXLM, custom tokens). Currently requires custom jq per asset.

**Appetite:** 3 days

**Solution:** Generalized asset filter. Matches by asset code, optionally by issuer. Checks the same fields as usdc-filter but parameterized.

**Flags:** `--code <ASSET_CODE>` (repeatable), `--issuer <G...>` (optional, narrows match), `--native` (match XLM), `-q`

**Rabbit Holes:**
- Don't support regex on asset codes
- Don't resolve asset metadata (domain, TOML, etc.)

**No-Gos:**
- No asset price lookups
- No SAC contract address resolution

**Scope Line:**
```
MUST HAVE:   Filter by asset code, check transfer/mint/burn/clawback fields
NICE TO HAVE: --issuer to distinguish same-code assets from different issuers
COULD HAVE:  --native shorthand for XLM
```

**Done:** `token-transfer --start-ledger 60200000 | asset-filter --code AQUA | jq` shows only AQUA transfers. Effectively makes usdc-filter a special case: `asset-filter --code USDC`.

---

## 6. aggregator (Transform)

**Problem:** Raw event streams are too granular for dashboards and alerting. Users need time-bucketed summaries (transfer count, total volume per minute/hour) without storing every event.

**Appetite:** 1 week

**Solution:** Stateful transform that buckets events by time window and emits summary events. Uses ledger timestamps (not wall clock) for deterministic replay.

**Output event (on window close):**
```json
{
  "_schema": "nebu.aggregator.v1",
  "window": {
    "start": "2024-12-10T14:00:00Z",
    "end": "2024-12-10T14:05:00Z",
    "duration": "5m"
  },
  "stats": {
    "count": 1247,
    "sum": "89234000000",
    "min": "100",
    "max": "50000000000"
  }
}
```

**Flags:** `--window <duration>` (1m, 5m, 1h, etc.), `--sum-field <path>` (field to aggregate, e.g., `transfer.amount`), `--group-by <path>` (optional grouping key), `-q`

**Rabbit Holes:**
- Don't build a full-featured analytics engine
- Don't handle out-of-order events (ledger timestamps are monotonic)
- Don't persist window state to disk

**No-Gos:**
- No percentile/histogram calculations
- No multi-field aggregation (pick one sum field)
- No sliding windows (tumbling only)

**Scope Line:**
```
MUST HAVE:   Tumbling time windows, count + sum, emit on window close
NICE TO HAVE: --group-by for per-asset or per-account summaries
COULD HAVE:  min/max stats
```

**Done:** `token-transfer --start-ledger 60200000 | aggregator --window 5m --sum-field transfer.amount | jq` shows 5-minute volume summaries.

---

## 7. rate-limiter (Transform)

**Problem:** Some downstream sinks have rate limits (webhooks, third-party APIs). Unthrottled pipelines can overwhelm them, causing failures and data loss.

**Appetite:** 2 days

**Solution:** Simple transform that limits event throughput to a target rate. Buffers stdin, emits to stdout at controlled pace using a token bucket algorithm.

**Flags:** `--rate <N>` (events per second), `--burst <N>` (max burst size, default: rate), `-q`

**Rabbit Holes:**
- Don't build backpressure signaling to upstream processors
- Don't implement priority queuing

**No-Gos:**
- No event dropping (buffer and slow down, don't discard)
- No per-key rate limiting

**Scope Line:**
```
MUST HAVE:   Token bucket rate limiting, configurable rate
NICE TO HAVE: --burst for short bursts above steady rate
COULD HAVE:  Nothing else — dead simple
```

**Done:** `token-transfer --start-ledger 60200000 | rate-limiter --rate 100 | webhook-sink --url https://example.com/hook` delivers at most 100 events/sec.

---

## 8. webhook-sink (Sink)

**Problem:** No way to push events to arbitrary HTTP endpoints. This is the most requested integration pattern — webhooks connect nebu to Slack, Discord, PagerDuty, custom apps, and any SaaS with a webhook receiver.

**Appetite:** 1 week

**Solution:** Sink that POSTs each event (or batch of events) as JSON to an HTTP endpoint. Handles retries, authentication, and backoff.

**Flags:** `--url <URL>`, `--method <POST|PUT>` (default: POST), `--header <key:value>` (repeatable), `--auth-header <value>` (or `WEBHOOK_AUTH` env), `--batch-size <N>` (default: 1), `--retry <N>` (default: 3), `--timeout <seconds>` (default: 10), `-q`

**Rabbit Holes:**
- Don't implement webhook signature verification (that's the receiver's job)
- Don't build a dead letter queue
- Don't support non-JSON content types

**No-Gos:**
- No response body processing
- No OAuth2 flow (use static tokens)
- No webhook registration/management

**Scope Line:**
```
MUST HAVE:   POST JSON to URL, configurable headers, retry with backoff
NICE TO HAVE: --batch-size for batched delivery, --auth-header shorthand
COULD HAVE:  --method PUT support
```

**Done:** `token-transfer --start-ledger 60200000 | usdc-filter | webhook-sink --url https://hooks.slack.com/... --header "Content-Type:application/json"` delivers USDC events to Slack.

---

## 9. kafka-sink (Sink)

**Problem:** Many organizations use Kafka as their event backbone. NATS sink exists but doesn't help Kafka shops. Kafka is the de facto standard for enterprise event streaming.

**Appetite:** 1 week

**Solution:** Sink that publishes events to Kafka topics. Supports key extraction for partitioning, configurable serialization, and producer tuning.

**Flags:** `--brokers <host:port,...>`, `--topic <name>`, `--key <field.path>` (partition key extraction), `--compression <none|gzip|snappy|lz4>` (default: snappy), `--batch-size <N>`, `--acks <0|1|all>` (default: all), `-q`

**Environment:** `KAFKA_BROKERS`, `KAFKA_SASL_USER`, `KAFKA_SASL_PASSWORD`, `KAFKA_TLS_CA`

**Rabbit Holes:**
- Don't implement consumer logic (this is a producer/sink only)
- Don't auto-create topics (require pre-existing topics)
- Don't support Avro/Protobuf serialization (JSON only, for now)

**No-Gos:**
- No Kafka Streams processing
- No Schema Registry integration
- No exactly-once semantics (at-least-once is fine)

**Scope Line:**
```
MUST HAVE:   Produce JSON to Kafka topic, configurable brokers, SASL auth
NICE TO HAVE: --key for partition key extraction, --compression
COULD HAVE:  --batch-size for producer batching
```

**Done:** `token-transfer --start-ledger 60200000 | kafka-sink --brokers kafka:9092 --topic stellar-transfers` publishes events to Kafka.

---

## 10. s3-sink (Sink)

**Problem:** Long-term storage of event data needs a cost-effective, queryable data lake. JSONL files in object storage (S3/GCS/MinIO) integrate with Athena, BigQuery, Spark, DuckDB.

**Appetite:** 1 week

**Solution:** Sink that writes buffered JSONL files to S3-compatible object storage, partitioned by time.

**Output path pattern:** `s3://bucket/prefix/year=2024/month=12/day=10/hour=14/events-<uuid>.jsonl.gz`

**Flags:** `--bucket <name>`, `--prefix <path>`, `--partition <hourly|daily>` (default: hourly), `--format <jsonl|jsonl.gz>` (default: jsonl.gz), `--flush-interval <duration>` (default: 5m), `--flush-size <bytes>` (default: 64MB), `--endpoint <url>` (for MinIO/custom S3), `-q`

**Environment:** `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_REGION`, `S3_ENDPOINT`

**Rabbit Holes:**
- Don't implement Parquet output (JSONL.gz is good enough and simpler)
- Don't build compaction or file merging
- Don't implement multipart upload resumption

**No-Gos:**
- No GCS-native API (use S3-compatible endpoint)
- No file listing or read-back capability
- No Hive metastore registration

**Scope Line:**
```
MUST HAVE:   Buffer events, write gzipped JSONL to S3, time-based partitioning
NICE TO HAVE: --flush-interval and --flush-size for tuning
COULD HAVE:  --endpoint for MinIO/custom S3-compatible stores
```

**Done:** `token-transfer --start-ledger 60200000 --follow | s3-sink --bucket my-data-lake --prefix stellar/transfers` writes partitioned JSONL.gz files to S3 every 5 minutes.

---

## 11. liquidity-pool-events (Origin)

**Problem:** The token-transfer processor captures LP deposits/withdrawals as token transfers but loses pool-level context: which pool, reserve amounts, shares minted/burned, price impact. DeFi analytics need this detail.

**Appetite:** 1 week

**Solution:** Origin processor that extracts liquidity pool operations with full pool context. Reads LP-related operations and their effects from ledger XDR.

**Output event:**
```json
{
  "_schema": "nebu.liquidity_pool.v1",
  "type": "deposit",
  "poolId": "abc123...",
  "account": "GABC...XYZ",
  "assets": [
    {"code": "USDC", "issuer": "GA5Z...", "amount": "10000000000"},
    {"code": "XLM", "amount": "50000000000"}
  ],
  "sharesMinted": "7071067811",
  "reserves": {
    "before": [{"amount": "100000000000"}, {"amount": "500000000000"}],
    "after": [{"amount": "110000000000"}, {"amount": "550000000000"}]
  },
  "meta": {
    "ledgerSequence": 60200001,
    "txHash": "abc123...",
    "transactionIndex": 5,
    "operationIndex": 0,
    "timestamp": "2024-12-10T14:30:00Z"
  }
}
```

**Event types:** deposit, withdraw, trade (path payment through pool)

**Flags:** `--start-ledger`, `--end-ledger`, `--rpc-url`, `--network`, `--pool <poolId>` (optional filter), `-q`

**Rabbit Holes:**
- Don't calculate impermanent loss (downstream analytics)
- Don't track cumulative pool state (emit per-operation deltas)
- Don't handle AMM constant product math

**No-Gos:**
- No Soroban DEX support (only native AMM pools)
- No pool creation/deletion events (just operations)
- No price oracle functionality

**Scope Line:**
```
MUST HAVE:   Extract LP deposit/withdraw with pool context, reserves before/after
NICE TO HAVE: Trade events (path payments routed through pools)
COULD HAVE:  --pool filter for single-pool monitoring
```

**Done:** `liquidity-pool-events --start-ledger 60200000 --end-ledger 60200100 | jq 'select(.type == "deposit")'` shows LP deposits with pool reserves.

---

## 12. account-effects (Origin)

**Problem:** Token-transfer captures value movement but misses account-level mutations: trustline changes, signer updates, data entry modifications, offer creation/deletion. These are critical for compliance, account monitoring, and protocol tracking.

**Appetite:** 1 week

**Solution:** Origin processor that extracts account-level effects (non-payment operations) from ledger XDR. Covers the operations that token-transfer intentionally skips.

**Event types:**
- `trustline_created`, `trustline_updated`, `trustline_removed`
- `signer_created`, `signer_updated`, `signer_removed`
- `data_created`, `data_updated`, `data_removed`
- `offer_created`, `offer_updated`, `offer_removed`
- `account_created`, `account_updated`, `account_removed`
- `thresholds_updated`, `flags_updated`, `home_domain_updated`

**Output event:**
```json
{
  "_schema": "nebu.account_effects.v1",
  "type": "trustline_created",
  "account": "GABC...XYZ",
  "details": {
    "asset": {"code": "USDC", "issuer": "GA5Z..."},
    "limit": "922337203685477580",
    "authorized": true
  },
  "meta": {
    "ledgerSequence": 60200001,
    "txHash": "abc123...",
    "transactionIndex": 5,
    "operationIndex": 0,
    "timestamp": "2024-12-10T14:30:00Z"
  }
}
```

**Flags:** `--start-ledger`, `--end-ledger`, `--rpc-url`, `--network`, `--types <comma-separated>` (filter to specific effect types), `-q`

**Rabbit Holes:**
- Don't try to track cumulative account state (just emit changes)
- Don't decode offer book matching (emit offer lifecycle events only)
- Don't duplicate what token-transfer already covers (skip payments)

**No-Gos:**
- No payment/transfer events (use token-transfer)
- No account balance tracking (derived from transfers)
- No sponsorship relationship tracking

**Scope Line:**
```
MUST HAVE:   Trustline, signer, and offer lifecycle events
NICE TO HAVE: Data entry and account flag changes
COULD HAVE:  --types filter for specific effect types
```

**Done:** `account-effects --start-ledger 60200000 --end-ledger 60200010 | jq 'select(.type == "trustline_created")'` shows new trustlines being established.

---

## Summary

| # | Processor | Type | Appetite | Priority Signal |
|---|-----------|------|----------|-----------------|
| 1 | contract-state | Origin | 1 week | Fills biggest gap vs indexer |
| 2 | ttl-tracker | Origin | 3 days | Complements contract-state |
| 3 | contract-filter | Transform | 3 days | Enables per-protocol tracking |
| 4 | account-filter | Transform | 3 days | Most common ad-hoc query |
| 5 | asset-filter | Transform | 3 days | Generalizes usdc-filter |
| 6 | aggregator | Transform | 1 week | Enables dashboards without raw storage |
| 7 | rate-limiter | Transform | 2 days | Protects downstream sinks |
| 8 | webhook-sink | Sink | 1 week | Unlocks all HTTP integrations |
| 9 | kafka-sink | Sink | 1 week | Enterprise event backbone |
| 10 | s3-sink | Sink | 1 week | Data lake / long-term storage |
| 11 | liquidity-pool-events | Origin | 1 week | DeFi analytics |
| 12 | account-effects | Origin | 1 week | Compliance & account monitoring |

**Total appetite: ~9 weeks** (if done serially). Can parallelize origins + transforms + sinks across contributors.

### Suggested Build Order (solo, by dependency)

**Cycle 1 (2 weeks):** contract-state + ttl-tracker + contract-filter
  - contract-state is the highest-value origin; ttl-tracker is small and complementary; contract-filter makes both useful in pipelines

**Cycle 2 (2 weeks):** asset-filter + account-filter + webhook-sink
  - Two quick transforms + the highest-value sink; immediately useful with existing origins

**Cycle 3 (2 weeks):** aggregator + rate-limiter + s3-sink
  - Pipeline infrastructure: summarize, throttle, archive

**Cycle 4 (2 weeks):** liquidity-pool-events + account-effects + kafka-sink
  - Specialized origins for DeFi and compliance; Kafka for enterprise users

**Cool-down between each cycle: 2-3 days**
