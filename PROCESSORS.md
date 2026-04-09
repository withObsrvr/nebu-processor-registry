# Available Processors

This is an auto-generated list of all processors in the nebu community registry.

## Origin Processors

### account-effects

Extract account-level effects (trustlines, signers, offers, data) from Stellar ledgers

- **Version**: 1.0.0
- **Language**: Go
- **License**: MIT
- **Schema**: `nebu.account_effects.v1`
- **Repository**: [github.com/withObsrvr/nebu-processor-registry](https://github.com/withObsrvr/nebu-processor-registry)

```bash
# Install
nebu install account-effects

# Use as origin
account-effects --start-ledger 60200000 --end-ledger 60200100 | jq
```

### contract-events

Extract and decode Soroban contract events from Stellar ledgers

- **Version**: 1.0.0
- **Language**: Go
- **License**: MIT
- **Schema**: `nebu.contract_events.v1`
- **Repository**: [github.com/withObsrvr/nebu-processor-registry](https://github.com/withObsrvr/nebu-processor-registry)

```bash
# Install
nebu install contract-events

# Use as origin
contract-events --start-ledger 60200000 --end-ledger 60200100 | jq
```

### contract-invocation

Extract and decode Soroban contract invocations from Stellar ledgers

- **Version**: 1.0.0
- **Language**: Go
- **License**: MIT
- **Schema**: `nebu.contract_invocation.v1`
- **Repository**: [github.com/withObsrvr/nebu-processor-registry](https://github.com/withObsrvr/nebu-processor-registry)

```bash
# Install
nebu install contract-invocation

# Use as origin
contract-invocation --start-ledger 60200000 --end-ledger 60200100 | jq
```

### contract-state

Extract contract data state changes from Stellar ledgers

- **Version**: 1.0.0
- **Language**: Go
- **License**: MIT
- **Schema**: `nebu.contract_state.v1`
- **Repository**: [github.com/withObsrvr/nebu-processor-registry](https://github.com/withObsrvr/nebu-processor-registry)

```bash
# Install
nebu install contract-state

# Use as origin
contract-state --start-ledger 60200000 --end-ledger 60200100 | jq
```

### liquidity-pool-events

Extract liquidity pool operations (deposit, withdraw, trade) from Stellar ledgers

- **Version**: 1.0.0
- **Language**: Go
- **License**: MIT
- **Schema**: `nebu.liquidity_pool.v1`
- **Repository**: [github.com/withObsrvr/nebu-processor-registry](https://github.com/withObsrvr/nebu-processor-registry)

```bash
# Install
nebu install liquidity-pool-events

# Use as origin
liquidity-pool-events --start-ledger 60200000 --end-ledger 60200100 | jq
```

### soroswap-pool-indexer

Index Soroswap pool creation events from the factory contract via getEvents RPC

- **Version**: 1.0.0
- **Language**: Go
- **License**: MIT
- **Schema**: `nebu.soroswap_pool.v1`
- **Repository**: [github.com/withObsrvr/nebu-processor-registry](https://github.com/withObsrvr/nebu-processor-registry)

```bash
# Install
nebu install soroswap-pool-indexer

# Use as origin
soroswap-pool-indexer --start-ledger 60200000 --end-ledger 60200100 | jq
```

### token-transfer

Extract and emit token transfer events from Stellar ledgers

- **Version**: 1.0.0
- **Language**: Go
- **License**: MIT
- **Schema**: `nebu.token_transfer.v1`
- **Repository**: [github.com/withObsrvr/nebu-processor-registry](https://github.com/withObsrvr/nebu-processor-registry)

```bash
# Install
nebu install token-transfer

# Use as origin
token-transfer --start-ledger 60200000 --end-ledger 60200100 | jq
```

### trade-extractor

Extract classic DEX orderbook and liquidity pool trades from Stellar ledgers

- **Version**: 1.0.0
- **Language**: Go
- **License**: MIT
- **Schema**: `nebu.dex_trade.v1`
- **Repository**: [github.com/withObsrvr/nebu-processor-registry](https://github.com/withObsrvr/nebu-processor-registry)

```bash
# Install
nebu install trade-extractor

# Use as origin
trade-extractor --start-ledger 60200000 --end-ledger 60200100 | jq
```

### ttl-tracker

Extract TTL (time-to-live) changes from Stellar ledgers

- **Version**: 1.0.0
- **Language**: Go
- **License**: MIT
- **Schema**: `nebu.ttl_tracker.v1`
- **Repository**: [github.com/withObsrvr/nebu-processor-registry](https://github.com/withObsrvr/nebu-processor-registry)

```bash
# Install
nebu install ttl-tracker

# Use as origin
ttl-tracker --start-ledger 60200000 --end-ledger 60200100 | jq
```


## Transform Processors

### account-filter

Filter events by Stellar account address

- **Version**: 1.0.0
- **Language**: Go
- **License**: MIT
- **Repository**: [github.com/withObsrvr/nebu-processor-registry](https://github.com/withObsrvr/nebu-processor-registry)

```bash
# Install
nebu install account-filter

# Use in pipeline
token-transfer | account-filter | json-file-sink
```

### aggregator

Aggregate events into time-bucketed summaries with count, sum, min, max

- **Version**: 1.0.0
- **Language**: Go
- **License**: MIT
- **Repository**: [github.com/withObsrvr/nebu-processor-registry](https://github.com/withObsrvr/nebu-processor-registry)

```bash
# Install
nebu install aggregator

# Use in pipeline
token-transfer | aggregator | json-file-sink
```

### amount-filter

Filter token transfer events by amount (min/max thresholds)

- **Version**: 1.0.0
- **Language**: Go
- **License**: MIT
- **Repository**: [github.com/withObsrvr/nebu-processor-registry](https://github.com/withObsrvr/nebu-processor-registry)

```bash
# Install
nebu install amount-filter

# Use in pipeline
token-transfer | amount-filter | json-file-sink
```

### aquarius-detector

Detect Aquarius DEX swaps by matching the router contract in swap candidates

- **Version**: 1.0.0
- **Language**: Go
- **License**: MIT
- **Repository**: [github.com/withObsrvr/nebu-processor-registry](https://github.com/withObsrvr/nebu-processor-registry)

```bash
# Install
nebu install aquarius-detector

# Use in pipeline
token-transfer | aquarius-detector | json-file-sink
```

### asset-filter

Filter token transfer events by asset code and optionally issuer

- **Version**: 1.0.0
- **Language**: Go
- **License**: MIT
- **Repository**: [github.com/withObsrvr/nebu-processor-registry](https://github.com/withObsrvr/nebu-processor-registry)

```bash
# Install
nebu install asset-filter

# Use in pipeline
token-transfer | asset-filter | json-file-sink
```

### contract-filter

Filter events by one or more contract IDs

- **Version**: 1.0.0
- **Language**: Go
- **License**: MIT
- **Repository**: [github.com/withObsrvr/nebu-processor-registry](https://github.com/withObsrvr/nebu-processor-registry)

```bash
# Install
nebu install contract-filter

# Use in pipeline
token-transfer | contract-filter | json-file-sink
```

### dedup

Deduplicate events based on a specified key field

- **Version**: 1.0.0
- **Language**: Go
- **License**: MIT
- **Repository**: [github.com/withObsrvr/nebu-processor-registry](https://github.com/withObsrvr/nebu-processor-registry)

```bash
# Install
nebu install dedup

# Use in pipeline
token-transfer | dedup | json-file-sink
```

### rate-limiter

Limit event throughput to a target rate using token bucket

- **Version**: 1.0.0
- **Language**: Go
- **License**: MIT
- **Repository**: [github.com/withObsrvr/nebu-processor-registry](https://github.com/withObsrvr/nebu-processor-registry)

```bash
# Install
nebu install rate-limiter

# Use in pipeline
token-transfer | rate-limiter | json-file-sink
```

### soroswap-detector

Detect Soroswap DEX swaps by matching contract addresses in swap candidates

- **Version**: 1.0.0
- **Language**: Go
- **License**: MIT
- **Repository**: [github.com/withObsrvr/nebu-processor-registry](https://github.com/withObsrvr/nebu-processor-registry)

```bash
# Install
nebu install soroswap-detector

# Use in pipeline
token-transfer | soroswap-detector | json-file-sink
```

### swap-candidate

Detect swap patterns in token transfer events by buffering transfers per tx_hash

- **Version**: 1.0.0
- **Language**: Go
- **License**: MIT
- **Schema**: `nebu.swap_candidate.v1`
- **Repository**: [github.com/withObsrvr/nebu-processor-registry](https://github.com/withObsrvr/nebu-processor-registry)

```bash
# Install
nebu install swap-candidate

# Use in pipeline
token-transfer | swap-candidate | json-file-sink
```

### swap-formatter

Format dex_swap events into human-readable text with display fields for block explorers

- **Version**: 1.0.0
- **Language**: Go
- **License**: MIT
- **Repository**: [github.com/withObsrvr/nebu-processor-registry](https://github.com/withObsrvr/nebu-processor-registry)

```bash
# Install
nebu install swap-formatter

# Use in pipeline
token-transfer | swap-formatter | json-file-sink
```

### swap-normalizer

Normalize swap candidates into unified dex_swap events

- **Version**: 1.0.0
- **Language**: Go
- **License**: MIT
- **Schema**: `nebu.dex_swap.v1`
- **Repository**: [github.com/withObsrvr/nebu-processor-registry](https://github.com/withObsrvr/nebu-processor-registry)

```bash
# Install
nebu install swap-normalizer

# Use in pipeline
token-transfer | swap-normalizer | json-file-sink
```

### time-window

Filter events by time range using ledger timestamps

- **Version**: 1.0.0
- **Language**: Go
- **License**: MIT
- **Repository**: [github.com/withObsrvr/nebu-processor-registry](https://github.com/withObsrvr/nebu-processor-registry)

```bash
# Install
nebu install time-window

# Use in pipeline
token-transfer | time-window | json-file-sink
```

### usdc-filter

Filter token transfer events for USDC only

- **Version**: 1.0.0
- **Language**: Go
- **License**: MIT
- **Repository**: [github.com/withObsrvr/nebu-processor-registry](https://github.com/withObsrvr/nebu-processor-registry)

```bash
# Install
nebu install usdc-filter

# Use in pipeline
token-transfer | usdc-filter | json-file-sink
```


## Sink Processors

### json-file-sink

Write JSON events to a JSONL file (one event per line)

- **Version**: 1.0.0
- **Language**: Go
- **License**: MIT
- **Repository**: [github.com/withObsrvr/nebu-processor-registry](https://github.com/withObsrvr/nebu-processor-registry)

```bash
# Install
nebu install json-file-sink

# Use as sink
token-transfer | json-file-sink
```

### kafka-sink

Publish JSON events to Kafka topics with partitioning and compression

- **Version**: 1.0.0
- **Language**: Go
- **License**: MIT
- **Repository**: [github.com/withObsrvr/nebu-processor-registry](https://github.com/withObsrvr/nebu-processor-registry)

```bash
# Install
nebu install kafka-sink

# Use as sink
token-transfer | kafka-sink
```

### nats-sink

Publish JSON events to NATS message bus for real-time distribution

- **Version**: 1.0.0
- **Language**: Go
- **License**: MIT
- **Repository**: [github.com/withObsrvr/nebu-processor-registry](https://github.com/withObsrvr/nebu-processor-registry)

```bash
# Install
nebu install nats-sink

# Use as sink
token-transfer | nats-sink
```

### postgres-sink

Store events in PostgreSQL with JSONB schema and automatic TOID generation

- **Version**: 1.0.0
- **Language**: Go
- **License**: MIT
- **Repository**: [github.com/withObsrvr/nebu-processor-registry](https://github.com/withObsrvr/nebu-processor-registry)

```bash
# Install
nebu install postgres-sink

# Use as sink
token-transfer | postgres-sink
```

### s3-sink

Write JSONL events to S3-compatible object storage with time partitioning

- **Version**: 1.0.0
- **Language**: Go
- **License**: MIT
- **Repository**: [github.com/withObsrvr/nebu-processor-registry](https://github.com/withObsrvr/nebu-processor-registry)

```bash
# Install
nebu install s3-sink

# Use as sink
token-transfer | s3-sink
```

### webhook-sink

POST JSON events to an HTTP endpoint with retry and batching

- **Version**: 1.0.0
- **Language**: Go
- **License**: MIT
- **Repository**: [github.com/withObsrvr/nebu-processor-registry](https://github.com/withObsrvr/nebu-processor-registry)

```bash
# Install
nebu install webhook-sink

# Use as sink
token-transfer | webhook-sink
```


---

*Total processors: 29*

*Last updated: 2026-04-09*
