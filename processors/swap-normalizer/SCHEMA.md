# DEX Swap JSON Schema

This document defines the JSON output schema for the swap-normalizer transform processor.

## Current Version: v1

**Schema identifier**: `nebu.dex_swap.v1`
**Status**: Stable

---

## Schema Structure

```json
{
  "_schema": "nebu.dex_swap.v1",
  "_nebu_version": "1.0.0",
  "ledger_sequence": 60200000,
  "tx_hash": "abc123...",
  "timestamp_unix": 1707123456,
  "trader": "GABC...",
  "sold_asset": {"code": "USDC", "issuer": "GA5Z..."},
  "sold_amount": "1000000000",
  "bought_asset": {"code": "XLM"},
  "bought_amount": "500000000",
  "protocol": "soroswap",
  "router_contract": "CCJUD...",
  "hop_count": 1,
  "in_successful_tx": true
}
```

### Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `_schema` | string | Yes | Schema identifier: `"nebu.dex_swap.v1"` |
| `_nebu_version` | string | Yes | Processor version |
| `ledger_sequence` | number | Yes | Stellar ledger sequence number |
| `tx_hash` | string | Yes | Transaction hash |
| `timestamp_unix` | number | Yes | Ledger close time (Unix epoch seconds) |
| `trader` | string | Yes | Address that performed the swap |
| `sold_asset` | object | Yes | Asset the trader sold (`code`, optional `issuer`) |
| `sold_amount` | string | Yes | Amount sold in stroops |
| `bought_asset` | object | Yes | Asset the trader bought (`code`, optional `issuer`) |
| `bought_amount` | string | Yes | Amount bought in stroops |
| `protocol` | string | Yes | DEX protocol: `"soroswap"`, `"unknown"`, etc. |
| `router_contract` | string | No | Router contract address (if detected) |
| `hop_count` | number | Yes | Number of intermediate hops |
| `in_successful_tx` | boolean | Yes | Whether the transaction succeeded |

---

## Human-Readable Display

This schema powers human-readable swap descriptions for the Prism block explorer:

> "Address GABC...XYZ swapped 100.0 USDC to 50.0 XLM at Soroswap"

Computed from:
- `trader` → address
- `sold_amount` / 10^7 + `sold_asset.code` → "100.0 USDC"
- `bought_amount` / 10^7 + `bought_asset.code` → "50.0 XLM"
- `protocol` → "at Soroswap"

---

## Pipeline

```
token-transfer | swap-candidate | soroswap-detector | swap-normalizer | postgres-sink
```

Each processor is composable:
- **swap-candidate**: detects swap patterns, emits `nebu.swap_candidate.v1`
- **soroswap-detector**: tags Soroswap swaps (chainable with other detectors)
- **swap-normalizer**: normalizes into this `nebu.dex_swap.v1` schema
