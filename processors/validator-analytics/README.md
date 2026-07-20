# Validator Analytics Processor

`validator-analytics` is a protobuf-first Nebu origin processor that emits one
replayable analytics record for each Stellar ledger.

It is a tracer bullet for richer ledger summaries in Obsrvr Lake and Prism. It
extracts facts directly from `LedgerCloseMeta` without calling the Query API:

- validator G-address and base64 ledger-close signature, when the SCP value is signed;
- ledger sequence, close time, hashes, protocol version, and network passphrase;
- total, successful, and failed transaction counts;
- total and successful operation counts;
- operation composition for account creation, payments, offers/AMMs,
  trustlines, claimable balances, sponsorship, Soroban, and other operations.

The processor does not assign validator names or infer bias. Validator identity
is time-varying registry data, and frequency, streak, and runs-test calculations
belong in downstream SQL over a ledger range.

## Build and test

```bash
go test ./...
go build -o validator-analytics ./cmd/validator-analytics
./validator-analytics --describe-json | jq .
```

## Run

```bash
validator-analytics \
  --start-ledger 60200000 \
  --end-ledger 60200100 \
  -q | jq
```

The standard Nebu origin flags select RPC, archive, or data-store sources and
the network passphrase. The schema identifier is
`nebu.validator_analytics.v1`.

## Output semantics

`operationCount` and `operationCategories` include operations in failed
transactions because those operations were present in the ledger transaction
set. `successfulOperationCount` and `successfulOperationCategories` include
only operations from successful transactions and therefore describe applied
activity.

Future operation types are classified as `other`; they are never assumed to be
Soroban merely because their numeric discriminant is newer.

## Validator identity

`validatorAddress` is the stable join key for validator metadata. On mainnet,
Radar exposes that metadata at:

```text
GET https://radar.withobsrvr.com/api/v1/node/{validatorAddress}
```

The response can include `name`, `alias`, `homeDomain`, and organization and
health fields. This processor deliberately does not perform that lookup: an
external HTTP call would make ledger replay non-deterministic and add a
per-ledger latency dependency. Resolve identity in a cached downstream join and
retain the public key as the canonical evidence field.
