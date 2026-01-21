"""Compact output formatter for reduced token usage."""

from typing import Any


def compact_event(event: dict[str, Any]) -> dict[str, Any]:
    """Convert a full event to compact format.

    Compact format includes only essential fields:
    - type: Event type (transfer, mint, burn, etc.)
    - ledger: Ledger sequence number
    - from/to: Account addresses (for transfers)
    - amount: Amount in stroops
    - asset: Asset code
    """
    result: dict[str, Any] = {}

    # Determine event type
    event_type = None
    for t in ["transfer", "mint", "burn", "clawback", "fee"]:
        if t in event:
            event_type = t
            break

    if event_type:
        result["type"] = event_type

    # Add ledger from meta
    meta = event.get("meta", {})
    if "ledgerSequence" in meta:
        result["ledger"] = meta["ledgerSequence"]

    # Add tx hash (shortened)
    if "txHash" in meta:
        result["tx"] = meta["txHash"][:12] + "..."

    # Add event-specific fields
    if event_type and event_type in event:
        evt_data = event[event_type]

        if "from" in evt_data:
            result["from"] = evt_data["from"]
        if "to" in evt_data:
            result["to"] = evt_data["to"]
        if "amount" in evt_data:
            result["amount"] = evt_data["amount"]
        if "assetCode" in evt_data:
            result["asset"] = evt_data["assetCode"]

    return result
