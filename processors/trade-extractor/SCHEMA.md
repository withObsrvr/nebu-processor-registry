# DEX Trade JSON Schema

This document defines the JSON output schema for the trade-extractor origin processor.

## Current Version: v1

**Schema identifier**: `nebu.dex_trade.v1`
**Status**: Stable

---

## Schema Structure

```json
{
  "_schema": "nebu.dex_trade.v1",
  "_nebu_version": "0.1.0",
  "ledger_sequence": 61696585,
  "timestamp_unix": 1773771107,
  "tx_hash": "1394685a...",
  "operation_index": 0,
  "trade_type": "orderbook",
  "seller": "GDXE...",
  "buyer": "GDSQ...",
  "sold_asset": {"code": "USDC", "issuer": "GA5Z..."},
  "sold_amount": "1409992",
  "bought_asset": {"code": "XLM"},
  "bought_amount": "8000000",
  "offer_id": 1828461794,
  "pool_id": "",
  "in_successful_tx": true
}
```

### Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `_schema` | string | Yes | `"nebu.dex_trade.v1"` |
| `_nebu_version` | string | Yes | Processor version |
| `ledger_sequence` | number | Yes | Stellar ledger sequence |
| `timestamp_unix` | number | Yes | Ledger close time (Unix seconds) |
| `tx_hash` | string | Yes | Transaction hash |
| `operation_index` | number | Yes | Operation index within the transaction |
| `trade_type` | string | Yes | `"orderbook"`, `"path_payment"`, or `"liquidity_pool"` |
| `seller` | string | Yes | Seller address (empty for liquidity pool trades) |
| `buyer` | string | Yes | Buyer/initiator address |
| `sold_asset` | object | Yes | Asset sold (`code`, optional `issuer`) |
| `sold_amount` | string | Yes | Amount sold in stroops |
| `bought_asset` | object | Yes | Asset bought (`code`, optional `issuer`) |
| `bought_amount` | string | Yes | Amount bought in stroops |
| `offer_id` | number | No | Offer ID for orderbook/path_payment trades |
| `pool_id` | string | No | Liquidity pool ID (hex) for pool trades |
| `in_successful_tx` | boolean | Yes | Whether the transaction succeeded |

---

## Trade Types

### Orderbook
From `ManageSellOffer`, `ManageBuyOffer`, `CreatePassiveSellOffer` operations. Each `ClaimAtom` in the result represents one filled offer.

### Path Payment
From `PathPaymentStrictReceive`, `PathPaymentStrictSend` operations. Each hop in the path generates one or more `ClaimAtom` entries.

### Liquidity Pool
From AMM pool interactions. The `pool_id` identifies which liquidity pool was involved. The `seller` field is empty since there's no counterparty account.
