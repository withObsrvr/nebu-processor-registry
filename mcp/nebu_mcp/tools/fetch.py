"""Raw ledger fetch tool for nebu."""

import asyncio
import os
import shutil
from typing import Any

from ..config import MAX_LEDGER_RANGE
from .extract import EXTRACTION_TIMEOUT


def find_nebu() -> str | None:
    """Find the nebu binary."""
    if shutil.which("nebu"):
        return "nebu"

    home = os.path.expanduser("~")
    common_paths = [
        os.path.join(home, "go", "bin", "nebu"),
        os.path.join(home, ".local", "bin", "nebu"),
        "/usr/local/bin/nebu",
    ]

    for path in common_paths:
        if os.path.isfile(path) and os.access(path, os.X_OK):
            return path

    return None


async def fetch_ledgers(
    start_ledger: int,
    end_ledger: int,
    output_file: str,
) -> dict[str, Any]:
    """Fetch raw ledger data (XDR) - use nebu_extract_events for most cases.

    This tool fetches raw ledger data and saves it to a file for later processing.
    For extracting events, use nebu_extract_events instead.

    Args:
        start_ledger: First ledger to fetch
        end_ledger: Last ledger to fetch (max 100 ledgers per call)
        output_file: File path to save XDR data

    Returns:
        File path where ledger data was saved
    """
    # Validate ledger range
    ledger_range = end_ledger - start_ledger
    if ledger_range < 0:
        return {"error": "end_ledger must be >= start_ledger"}

    if ledger_range > MAX_LEDGER_RANGE:
        return {
            "error": f"Ledger range too large ({ledger_range}). Maximum is {MAX_LEDGER_RANGE} ledgers per call.",
            "suggestion": f"Try a smaller range, e.g., start={start_ledger} end={start_ledger + MAX_LEDGER_RANGE}",
        }

    # Find nebu binary
    nebu_path = find_nebu()
    if not nebu_path:
        return {
            "error": "nebu CLI not found",
            "suggestion": "Install with: go install github.com/withObsrvr/nebu/cmd/nebu@latest",
        }

    # Validate output path
    output_dir = os.path.dirname(output_file)
    if output_dir and not os.path.exists(output_dir):
        return {"error": f"Output directory does not exist: {output_dir}"}

    # Build command
    cmd = f"{nebu_path} fetch -q {start_ledger} {end_ledger} > {output_file}"

    # Execute command with timeout
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
        stderr_str = stderr.decode() if stderr else ""
        returncode = proc.returncode
    except asyncio.TimeoutError:
        proc.kill()
        await proc.wait()
        return {
            "error": f"Fetch timed out after {EXTRACTION_TIMEOUT}s",
            "suggestion": "Try a smaller ledger range",
        }

    if returncode != 0:
        return {"error": f"Fetch failed: {stderr_str.strip()}"}

    # Get file size
    try:
        file_size = os.path.getsize(output_file)
    except OSError:
        file_size = 0

    return {
        "file": output_file,
        "ledger_range": [start_ledger, end_ledger],
        "ledgers": ledger_range + 1,
        "bytes": file_size,
    }
