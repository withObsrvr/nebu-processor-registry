"""Summary output formatter for aggregated statistics."""

from collections import Counter
from typing import Any


def summarize_events(
    events: list[dict[str, Any]],
    start_ledger: int,
    end_ledger: int,
    limit: int,
) -> dict[str, Any]:
    """Create a summary of events with aggregated statistics.

    Summary format includes:
    - total_events: Count of events
    - by_type: Events grouped by type
    - by_asset: Events grouped by asset code
    - ledger_range: [start, end]
    - truncated: Whether output was limited
    """
    type_counter: Counter = Counter()
    asset_counter: Counter = Counter()

    for event in events:
        # Count by type
        for t in ["transfer", "mint", "burn", "clawback", "fee"]:
            if t in event:
                type_counter[t] += 1

                # Count by asset
                evt_data = event.get(t, {})
                asset = evt_data.get("assetCode", "unknown")
                asset_counter[asset] += 1
                break

    return {
        "total_events": len(events),
        "by_type": dict(type_counter),
        "by_asset": dict(asset_counter),
        "ledger_range": [start_ledger, end_ledger],
        "truncated": len(events) >= limit,
    }
