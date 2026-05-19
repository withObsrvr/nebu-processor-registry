# Soroswap Pool Transform

`soroswap-pool-transform` reads `contract-events` JSONL from stdin and emits normalized Soroswap pool discovery records (`nebu.soroswap_pool_discovery.v1`). It is a pure transform: it does **not** query Soroban RPC or `getEvents`.

## Historical backfill

```bash
nebu fetch \
  --network pubnet \
  --mode archive \
  --start-ledger 50000000 \
  --end-ledger 51000000 \
  --datastore-type S3 \
  --bucket-path aws-public-blockchain/v1.1/stellar/ledgers/pubnet \
  | contract-events \
  | soroswap-pool-transform --network pubnet \
  > soroswap_pools.jsonl
```

Use `--factory` to override the known factory allowlist (when any `--factory` is supplied, it fully replaces the default for the network):

```bash
cat contract_events.jsonl \
  | soroswap-pool-transform \
      --network testnet \
      --factory CDP3HMUH6SMS3S7NPGNDJLULCOXXEPSHY4JKUKMBNQMATHDHWXRRJTBY
```

Inspect the schema for registry and SQL tooling:

```bash
soroswap-pool-transform --describe-json
```

Optional SQL exploration once installed:

```sql
SELECT *
FROM nebu('soroswap-pool-transform', '--network', 'testnet');
```

## Output example

```json
{
  "_schema": "nebu.soroswap_pool_discovery.v1",
  "_nebu_version": "1.0.0",
  "network": "testnet",
  "protocol": "soroswap",
  "factory_contract_id": "CDP3...JTBY",
  "pool_contract_id": "CDVA...FKDB",
  "token_a_contract_id": "CB3T...OV2F",
  "token_b_contract_id": "CDLZ...CYSC",
  "token_pair_key": "CB3T...OV2F:CDLZ...CYSC",
  "ledger_sequence": 2606504,
  "ledger_closed_at": "2026-05-17T18:14:58Z",
  "transaction_hash": "42a7...673",
  "operation_index": 0,
  "event_index": 3,
  "factory_event_name": "pair_created",
  "source_contract_id": "CDP3...JTBY",
  "discovery_method": "contract-events",
  "raw_event": { "type": "CONTRACT", "contractId": "CDP3...JTBY", "eventType": "pair_created", "topicDecoded": [{"symbolValue":"pair_created"}], "dataDecoded": { "token_a": "CB3T...OV2F", "token_b": "CDLZ...CYSC", "pair": "CDVA...FKDB" } }
}
```

`raw_event` is included by default for audit/replay evidence (preserved verbatim from upstream). Use `--omit-raw` to drop it entirely from output. Both snake_case and camelCase field names are accepted on input, so the processor works with `contract-events` (which emits camelCase via protojson) and with manually-fed snake_case test fixtures alike.

## Flags

- `--network <pubnet|testnet>`: used when input rows omit `network`; also selects the built-in factory defaults. Aliases like `mainnet`, `futurenet`, and the full Stellar network passphrases (e.g., `"Public Global Stellar Network ; September 2015"`) are also accepted on input/CLI and normalized; only `pubnet` and `testnet` have built-in factory defaults — other networks must pass `--factory`.
- `--factory <contract_id>`: repeatable factory allowlist. Required if `--network` is omitted or has no known factory.
- `--event-name <symbol>`: repeatable accepted pool creation event names. Defaults: `new_pair`, `pair_created`, `create_pair`.
- `--include-raw` / `--omit-raw`: include or omit the original event in output (`--include-raw` is the default; `--omit-raw` overrides it).
- `--strict`: fail on malformed JSON or undecodable matching factory events.
- `--stats`: print read/match/emit/error counts to stderr.
- `--verbose`: print per-error diagnostics to stderr (parse errors, decode errors).
- `-q`, `--quiet`: suppress all non-error stderr output (overrides `--verbose` and `--stats`).
- `--describe-json`: print the standard nebu describe envelope.

## Proto-first contract

This processor uses JSONL for Unix/Nebu composability, but the output is shaped as the JSON projection of a future typed pool-discovery message. Registry metadata uses `_schema` and `_nebu_version` for compatibility with other nebu processors. It intentionally separates pool discovery (`nebu.soroswap_pool_discovery.v1`) from pool operation events such as deposits, withdrawals, and swaps.

## Development

```bash
go test ./...
go build -o soroswap-pool-transform ./cmd/soroswap-pool-transform
```
