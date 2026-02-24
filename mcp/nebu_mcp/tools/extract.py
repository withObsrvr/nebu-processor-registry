"""Event extraction tool for nebu."""

import asyncio
import json
import os
import re
import shutil
from typing import Any, Literal

from ..config import (
    DEFAULT_FORMAT,
    DEFAULT_LIMIT,
    FORMAT_COMPACT,
    FORMAT_SUMMARY,
    MAX_LEDGER_RANGE,
    MAX_LIMIT,
)
from ..formatters.compact import compact_event
from ..formatters.summary import summarize_events

# Timeout for extraction commands (seconds)
EXTRACTION_TIMEOUT = 120

# Pattern for validating jq filter expressions
# Allows common jq operations but blocks shell metacharacters
JQ_FILTER_FORBIDDEN = re.compile(r'[`$]|&&|\|\||;;|>\s*>|<\s*<')


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


def _validate_jq_filter(jq_filter: str) -> str | None:
    """Validate jq filter to prevent command injection.

    Returns None if valid, or an error message if invalid.
    """
    if JQ_FILTER_FORBIDDEN.search(jq_filter):
        return "Invalid jq filter: contains forbidden shell metacharacters"
    return None


async def extract_events(
    processor: str,
    start_ledger: int,
    end_ledger: int,
    jq_filter: str | None = None,
    limit: int = DEFAULT_LIMIT,
    output_format: Literal["full", "compact", "summary"] = DEFAULT_FORMAT,
) -> dict[str, Any]:
    """Extract blockchain events from Stellar ledgers.

    Args:
        processor: Processor to use (e.g., token-transfer, contract-events)
        start_ledger: First ledger to process
        end_ledger: Last ledger to process (max 100 ledgers per call)
        jq_filter: Optional jq filter expression
        limit: Maximum events to return (default 100, max 1000)
        output_format: Output format (full, compact, summary)

    Returns:
        Extracted events as JSON array, or summary statistics
    """
    # Validate ledger range
    ledger_diff = end_ledger - start_ledger
    if ledger_diff < 0:
        return {"error": "end_ledger must be >= start_ledger"}

    if ledger_diff > MAX_LEDGER_RANGE:
        return {
            "error": f"Ledger range too large ({ledger_diff}). Maximum is {MAX_LEDGER_RANGE} ledgers per call.",
            "suggestion": f"Try a smaller range: start={start_ledger} end={start_ledger + MAX_LEDGER_RANGE}",
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

    # Check for jq if filter is specified
    if jq_filter:
        if not shutil.which("jq"):
            return {
                "error": "jq is required for filtering but was not found in PATH",
                "suggestion": "Install with: apt-get install jq or brew install jq",
            }
        # Validate jq filter for security
        filter_error = _validate_jq_filter(jq_filter)
        if filter_error:
            return {"error": filter_error}

    # Enforce limit
    limit = min(limit, MAX_LIMIT)

    # Build command - use quiet mode and direct execution
    cmd = f"{processor_path} --start-ledger {start_ledger} --end-ledger {end_ledger} -q"

    # Add jq filter if specified
    if jq_filter:
        # Escape single quotes in filter for shell
        safe_filter = jq_filter.replace("'", "'\\''")
        cmd += f" | jq -c '{safe_filter}'"

    # Add limit
    cmd += f" | head -{limit}"

    # Execute command with timeout using async subprocess
    # IMPORTANT: stdin=DEVNULL prevents subprocess from waiting on MCP's stdin
    proc = None
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
        if proc:
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

    # Parse events, tracking malformed lines
    events = []
    malformed_count = 0
    for line in stdout_str.strip().split("\n"):
        if line:
            try:
                events.append(json.loads(line))
            except json.JSONDecodeError:
                malformed_count += 1

    # Format output based on requested format
    truncated = len(events) >= limit

    base_result: dict[str, Any] = {
        "count": len(events),
        "truncated": truncated,
    }

    # Include malformed count if any were skipped
    if malformed_count > 0:
        base_result["malformed_lines_skipped"] = malformed_count

    if output_format == FORMAT_SUMMARY:
        result = summarize_events(events, start_ledger, end_ledger, limit)
        if malformed_count > 0:
            result["malformed_lines_skipped"] = malformed_count
        return result
    elif output_format == FORMAT_COMPACT:
        return {
            "events": [compact_event(e) for e in events],
            **base_result,
        }
    else:  # full
        return {
            "events": events,
            **base_result,
        }
