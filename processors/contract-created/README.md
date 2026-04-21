# contract-created

Extract contract creation events from Stellar ledgers.

## What it detects

This processor looks for:

- `InvokeHostFunction` operations
- host function type `create_contract`
- host function type `create_contract_v2`
- created contract instance / contract-data ledger entries
- related TTL changes for the created contract state

## Output

Each emitted event includes raw creation facts plus family discovery fields:

- `contractId`
- `deployer`
- `preimageAddress`
- `saltHex`
- `executableType`
- `wasmHash`
- `createHostFunctionType`
- `constructorInvoked`
- `constructorName`
- `constructorArgs`
- `contractInstanceCreated`
- `initializedState`
- `ttlExtendedToLedger`
- `classificationHint`
- `classificationReasons`
- `familyHint`
- `familyConfidence`
- `familyReasons`
- `familyTags`
- `familyCandidates`

Current family heuristics include:

- `fungible_token_like`
- `multisig_like`
- `smart_account_like`
- `identity_registry_like`
- `nft_like`
- `auth_credential_like`
- `defi_router_like`
- `defi_pool_adapter_like`
- `vault_admin_like`
- `generic_contract`

## Install

```bash
nebu install contract-created
```

## Usage

```bash
# bounded range
contract-created --start-ledger 60200000 --end-ledger 60200100

# stdin mode
nebu fetch 60200000 60200100 | contract-created
```

## Example output

```json
{
  "_schema": "nebu.contract_created.v1",
  "_nebu_version": "0.1.0",
  "meta": {
    "ledgerSequence": 1928929,
    "closedAtUnix": 1775648102,
    "txHash": "21d85b12f9f9cdeae1cc3cd8063402cc1cf7a8d0a7c251740c181e08a56dffcd",
    "transactionIndex": 0,
    "operationIndex": 0,
    "inSuccessfulTx": true
  },
  "contractId": "CATJ...",
  "deployer": "GCIL...",
  "preimageAddress": "GCIL...",
  "saltHex": "...",
  "executableType": "wasm",
  "wasmHash": "706bb37ef21dd159a479f9ef251db714648ed93b1624656128650415787836ec",
  "createHostFunctionType": "HostFunctionTypeHostFunctionTypeCreateContractV2",
  "constructorInvoked": true,
  "constructorName": "__constructor",
  "constructorArgs": [
    "GCIL...",
    "SCF Membership",
    "scf",
    "https://ipfs.io/ipfs/..."
  ],
  "contractInstanceCreated": true,
  "initializedState": [
    {"key": "LedgerKeyContractInstance", "value": "706bb3...", "operation": "create", "durability": "persistent"},
    {"key": "Name", "value": "SCF Membership", "operation": "create", "durability": "persistent"},
    {"key": "Symbol", "value": "scf", "operation": "create", "durability": "persistent"},
    {"key": "NextTokenId", "value": "0", "operation": "create", "durability": "persistent"}
  ],
  "ttlExtendedToLedger": 2049888,
  "classificationHint": "identity_registry_like",
  "classificationReasons": [
    "identity registry ownership/metadata pattern detected"
  ],
  "familyHint": "identity_registry_like",
  "familyConfidence": 0.99,
  "familyReasons": [
    "identity registry ownership/metadata pattern detected"
  ],
  "familyTags": ["registry", "identity", "owner", "agent", "nft_like"],
  "familyCandidates": [
    {"family": "identity_registry_like", "confidence": 0.99, "reasons": ["identity registry ownership/metadata pattern detected"], "tags": ["registry", "identity", "owner", "agent", "nft_like"], "ruleIds": ["family.identity_registry"]},
    {"family": "nft_like", "confidence": 0.99, "reasons": ["constructor shape resembles owner + name + symbol", "NFT-style metadata initialized in contract state"], "tags": ["nft_like", "collection", "metadata"], "ruleIds": ["family.nft.constructor", "family.nft.metadata"]}
  ]
}
```

## Notes

- contract creation is detected from host-function + metadata together
- constructor args are extracted directly from `create_contract_v2` and also from auth trees when needed
- family discovery is heuristic and intentionally explainable via `familyReasons` and `familyCandidates`
- repeated deployments can benefit from in-process WASM family memory during a scan
- see `HEURISTICS.md` for how rules work and how to add new ones
