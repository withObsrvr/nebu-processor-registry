"""Pipeline execution tool for nebu."""

import asyncio
import json
import re
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
from .extract import EXTRACTION_TIMEOUT, find_processor

# Pattern for validating pipeline components
# Allows alphanumeric processor names with dashes/underscores and simple arguments
PIPELINE_FORBIDDEN = re.compile(r'[`$]|&&|;;|>\s*>|<\s*<|[\'"]')


def _validate_pipeline(pipeline: str) -> str | None:
    """Validate pipeline string to prevent command injection.

    Returns None if valid, or an error message if invalid.
    """
    if PIPELINE_FORBIDDEN.search(pipeline):
        return "Invalid pipeline: contains forbidden shell metacharacters"
    return None


async def run_pipeline(
    pipeline: str,
    start_ledger: int,
    end_ledger: int,
    limit: int = DEFAULT_LIMIT,
    output_format: Literal["full", "compact", "summary"] = DEFAULT_FORMAT,
) -> dict[str, Any]:
    """Run a multi-processor pipeline.

    Args:
        pipeline: Pipeline command with processors (e.g., 'token-transfer | usdc-filter | amount-filter --min 1000000')
        start_ledger: First ledger to process
        end_ledger: Last ledger to process (max 100 ledgers per call)
        limit: Maximum events to return (default 100, max 1000)
        output_format: Output format (full, compact, summary)

    Returns:
        Pipeline output (limited to prevent context overflow)
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

    # Validate pipeline for security
    pipeline_error = _validate_pipeline(pipeline)
    if pipeline_error:
        return {"error": pipeline_error}

    # Enforce limit
    limit = min(limit, MAX_LIMIT)

    # Parse pipeline to extract first processor and add ledger args
    parts = pipeline.strip().split("|")
    if not parts:
        return {"error": "Empty pipeline"}

    first_processor_cmd = parts[0].strip()
    rest_of_pipeline = " | ".join(p.strip() for p in parts[1:])

    # Extract processor name (first word) and find its path
    first_processor_name = first_processor_cmd.split()[0]
    processor_path = find_processor(first_processor_name)
    if not processor_path:
        return {
            "error": f"First processor '{first_processor_name}' not found",
            "suggestion": f"Install with: nebu install {first_processor_name}",
        }

    # Replace processor name with full path in command
    first_processor_cmd = first_processor_cmd.replace(
        first_processor_name, processor_path, 1
    )

    # Build full command
    # Add ledger args to first processor
    cmd = f"{first_processor_cmd} --start-ledger {start_ledger} --end-ledger {end_ledger} -q"

    # Add rest of pipeline (resolve paths for each processor)
    if rest_of_pipeline:
        resolved_rest = []
        for part in parts[1:]:
            part = part.strip()
            if part:
                proc_name = part.split()[0]
                proc_path = find_processor(proc_name)
                if proc_path:
                    part = part.replace(proc_name, proc_path, 1)
                resolved_rest.append(part)
        if resolved_rest:
            cmd += " | " + " | ".join(resolved_rest)

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
            "error": f"Pipeline timed out after {EXTRACTION_TIMEOUT}s",
            "suggestion": "Try a smaller ledger range or simpler pipeline",
        }

    if returncode != 0:
        return {"error": f"Pipeline failed: {stderr_str.strip()}"}

    # Parse events, tracking malformed lines
    events = []
    malformed_count = 0
    for line in stdout_str.strip().split("\n"):
        if line:
            try:
                events.append(json.loads(line))
            except json.JSONDecodeError:
                malformed_count += 1

    # Format output
    truncated = len(events) >= limit

    base_result: dict[str, Any] = {
        "count": len(events),
        "pipeline": pipeline,
        "truncated": truncated,
    }

    # Include malformed count if any were skipped
    if malformed_count > 0:
        base_result["malformed_lines_skipped"] = malformed_count

    if output_format == FORMAT_SUMMARY:
        result = summarize_events(events, start_ledger, end_ledger, limit)
        result["pipeline"] = pipeline
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
