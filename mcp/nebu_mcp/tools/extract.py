"""Event extraction tool for nebu."""

import asyncio
import json
import os
import shutil
from typing import Any, Literal

from ..config import (
    DEFAULT_FORMAT,
    DEFAULT_LIMIT,
    FORMAT_COMPACT,
    FORMAT_FULL,
    FORMAT_SUMMARY,
    MAX_LEDGER_RANGE,
    MAX_LIMIT,
)
from ..formatters.compact import compact_event
from ..formatters.summary import summarize_events

# Timeout for extraction commands (seconds)
EXTRACTION_TIMEOUT = 120


def find_processor(name: str) -> str | None:
    """Find the processor binary in common locations."""
    # Check if it's in PATH
    if shutil.which(name):
        return name

    # Check common Go binary locations
    home = os.path.expanduser("~")
    common_paths = [
        os.path.join(home, "go", "bin", name),
        os.path.join(home, ".local", "bin", name),
        f"/usr/local/bin/{name}",
    ]

    for path in common_paths:
        if os.path.isfile(path) and os.access(path, os.X_OK):
            return path

    return None


async def extract_events(
    processor: str,
    start_ledger: int,
    end_ledger: int,
    filter: str | None = None,
    limit: int = DEFAULT_LIMIT,
    format: Literal["full", "compact", "summary"] = DEFAULT_FORMAT,
) -> dict[str, Any]:
    """Extract blockchain events from Stellar ledgers.

    Args:
        processor: Processor to use (e.g., token-transfer, contract-events)
        start_ledger: First ledger to process
        end_ledger: Last ledger to process (max 100 ledgers per call)
        filter: Optional jq filter expression
        limit: Maximum events to return (default 100, max 1000)
        format: Output format (full, compact, summary)

    Returns:
        Extracted events as JSON array, or summary statistics
    """
    # Validate ledger range
    ledger_range = end_ledger - start_ledger
    if ledger_range < 0:
        return {"error": "end_ledger must be >= start_ledger"}

    if ledger_range > MAX_LEDGER_RANGE:
        return {
            "error": f"Ledger range too large ({ledger_range}). Maximum is {MAX_LEDGER_RANGE} ledgers per call.",
            "suggestion": f"Try a smaller range: --start-ledger {start_ledger} --end-ledger {start_ledger + MAX_LEDGER_RANGE}",
        }

    # Find the processor binary
    processor_path = find_processor(processor)
    if not processor_path:
        return {
            "error": f"Processor '{processor}' not found",
            "suggestion": f"Install with: nebu install {processor}",
            "searched": [
                "PATH",
                "~/go/bin",
                "~/.local/bin",
                "/usr/local/bin",
            ],
        }

    # Enforce limit
    limit = min(limit, MAX_LIMIT)

    # Build command - use quiet mode and direct execution
    cmd = f"{processor_path} --start-ledger {start_ledger} --end-ledger {end_ledger} -q"

    # Add jq filter if specified
    if filter:
        # Escape single quotes in filter
        safe_filter = filter.replace("'", "'\\''")
        cmd += f" | jq -c '{safe_filter}'"

    # Add limit
    cmd += f" | head -{limit}"

    # Execute command with timeout using async subprocess
    # IMPORTANT: stdin=DEVNULL prevents subprocess from waiting on MCP's stdin
    try:
        proc = await asyncio.create_subprocess_shell(
            cmd,
            stdin=asyncio.subprocess.DEVNULL,
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.PIPE,
        )
        stdout, stderr = await asyncio.wait_for(
            proc.communicate(),
            timeout=EXTRACTION_TIMEOUT,
        )
        stdout_str = stdout.decode() if stdout else ""
        stderr_str = stderr.decode() if stderr else ""
        returncode = proc.returncode
    except asyncio.TimeoutError:
        proc.kill()
        await proc.wait()
        return {
            "error": f"Extraction timed out after {EXTRACTION_TIMEOUT}s",
            "suggestion": "Try a smaller ledger range or fewer events",
        }

    if returncode != 0:
        if "not found" in stderr_str.lower() or "command not found" in stderr_str.lower():
            return {
                "error": f"Processor '{processor}' not found or not installed",
                "suggestion": "Install the processor with: nebu install " + processor,
            }
        return {"error": f"Extraction failed: {stderr_str.strip()}"}

    # Parse events
    events = []
    for line in stdout_str.strip().split("\n"):
        if line:
            try:
                events.append(json.loads(line))
            except json.JSONDecodeError:
                continue  # Skip malformed lines

    # Format output based on requested format
    truncated = len(events) >= limit

    if format == FORMAT_SUMMARY:
        return summarize_events(events, start_ledger, end_ledger, limit)
    elif format == FORMAT_COMPACT:
        return {
            "events": [compact_event(e) for e in events],
            "count": len(events),
            "truncated": truncated,
        }
    else:  # full
        return {
            "events": events,
            "count": len(events),
            "truncated": truncated,
        }
