# nft-events

Extract NFT-like contract events and contract calls from Stellar ledgers.

## What this scaffold does

This processor is a pragmatic starting point for NFT indexing in nebu.
It currently inspects:

- Soroban contract events
- `InvokeContract` calls
- contract-data state changes

and emits normalized NFT-like facts when it sees common NFT signals such as:

- `transfer`
- `mint`
- `burn`
- `approve`
- `approve_for_all`
- `owner_of`
- `token_uri`

## Current behavior

The current version is heuristic-first, but it now applies stronger classification rules across event shapes, invocation method names, and contract-state key/value patterns.

It emits:

- `contractId`
- `tokenId` (best effort)
- `action`
- `from` / `to`
- `owner` / `approved` / `operator`
- `functionName`
- `collectionName`
- `symbol`
- `stateKey` / `stateValue`
- `durability`
- `tokenExists` / `burned`
- `standard`
- `implementation`
- `classificationSource`
- `confidence`
- raw decoded topics / args
- transaction metadata

## Intended use

Use this as the extraction layer for a broader NFT indexing stack.
Typical downstream responsibilities are:

- materializing current ownership
- collection/token detail views
- external metadata resolution
- provenance feeds
- holder analytics
- gateway/API serving

## Install

```bash
nebu install nft-events
```

## Usage

```bash
# bounded range
nft-events --start-ledger 60200000 --end-ledger 60200100

# stdin mode
nebu fetch 60200000 60200100 | nft-events
```

## Example filters

```bash
# all candidate NFT activity
nft-events --start-ledger 60200000 --end-ledger 60200100 | jq

# likely SEP-50 activity
nft-events --start-ledger 60200000 --end-ledger 60200100 | \
  jq 'select(.standard == "sep_50")'

# approvals only
nft-events --start-ledger 60200000 --end-ledger 60200100 | \
  jq 'select(.action == "approve" or .action == "approve_for_all")'
```

## Example output

```json
{
  "_schema": "nebu.nft_events.v1",
  "_nebu_version": "0.1.0",
  "meta": {
    "ledgerSequence": 60200000,
    "closedAtUnix": 1765158311,
    "txHash": "abc123...",
    "transactionIndex": 3,
    "operationIndex": 0,
    "eventIndex": 1,
    "inSuccessfulTx": true
  },
  "contractId": "CA...",
  "tokenId": "42",
  "action": "transfer",
  "standard": "sep_50",
  "implementation": "unknown",
  "classificationSource": "heuristic_framework",
  "confidence": 0.9,
  "from": "GA...",
  "to": "GB...",
  "functionName": "transfer",
  "methodsDetected": ["transfer"],
  "eventTypesDetected": ["transfer"],
  "rawTopics": ["transfer", "GA...", "GB...", "42"],
  "rawData": "",
  "sourceKind": "contract_event"
}
```

## Limitations

Current scaffold limitations:

- token id extraction is best effort
- classification is heuristic, not authoritative
- contract state inspection is heuristic and best effort
- no external metadata fetching
- no ownership materialization
- no OpenZeppelin code-hash matching yet

## Next recommended improvements

1. add code-hash / wasm-hash based OpenZeppelin detection
2. add TTL/health extraction
3. emit explicit collection/token metadata events
4. improve owner/token-id extraction from nested state keys
5. add tests with known NFT ledgers/contracts
