# Swap Candidate JSON Schema

This document defines the JSON output schema for the swap-candidate transform processor.

## Current Version: v1

**Schema identifier**: `nebu.swap_candidate.v1`
**Status**: Stable

---

## Schema Structure

```json
{
  "_schema": "nebu.swap_candidate.v1",
  "_nebu_version": "1.0.0",
  "ledger_sequence": 60200000,
  "tx_hash": "abc123...",
  "timestamp_unix": 1707123456,
  "pivot_address": "GABC...",
  "in_successful_tx": true,
  "legs": [
    {
      "from": "GABC...",
      "to": "CPOOL...",
      "asset": {"code": "USDC", "issuer": "GA5Z..."},
      "amount": "1000000000",
      "contract_address": "CA..."
    },
    {
      "from": "CPOOL...",
      "to": "GABC...",
      "asset": {"code": "XLM"},
      "amount": "500000000",
      "contract_address": "CA..."
    }
  ],
  "hop_count": 1,
  "contract_addresses": ["CA..."]
}
```

### Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `_schema` | string | Yes | Schema identifier: `"nebu.swap_candidate.v1"` |
| `_nebu_version` | string | Yes | Processor version |
| `ledger_sequence` | number | Yes | Stellar ledger sequence number |
| `tx_hash` | string | Yes | Transaction hash |
| `timestamp_unix` | number | Yes | Ledger close time (Unix epoch seconds) |
| `pivot_address` | string | Yes | Address that swapped (has both in and out with different assets) |
| `in_successful_tx` | boolean | Yes | Whether the transaction succeeded |
| `legs` | array | Yes | Array of transfer legs (see below) |
| `hop_count` | number | Yes | Number of intermediate hops (legs - 1) |
| `contract_addresses` | array | Yes | Unique contract addresses involved |

### Leg Object

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `from` | string | Yes | Source address |
| `to` | string | Yes | Destination address |
| `asset` | object | Yes | Asset info with `code` and optional `issuer` |
| `amount` | string | Yes | Amount in stroops |
| `contract_address` | string | No | Contract address for this leg |

---

## Swap Detection Logic

A swap is detected when:
1. A transaction has 2+ non-fee token transfers
2. Any address in the transaction has both inbound (received) and outbound (sent) transfers
3. The inbound and outbound transfers involve **different** assets

The address meeting these criteria becomes the `pivot_address` — typically the trader or router contract.

### Examples

**Simple swap:** TRADER sends USDC, receives XLM through a pool
- Pivot: TRADER (sends USDC out, receives XLM in)

**Multi-hop:** TRADER → POOL_A (USDC→TOKEN) → POOL_B (TOKEN→XLM) → TRADER
- Pivot: TRADER (sends USDC out, receives XLM in)
- POOL_A and POOL_B also have counter-directional transfers

**Not a swap:** ALICE sends USDC to BOB, BOB sends USDC to CHARLIE
- No pivot found (BOB receives and sends the same asset)

---

## Downstream Processors

Swap candidates are designed to be consumed by:
- `soroswap-detector` — adds `protocol: "soroswap"` if Soroswap contracts are involved
- `swap-normalizer` — normalizes into `nebu.dex_swap.v1` with trader/sold/bought fields
