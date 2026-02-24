"""Raw ledger fetch tool for nebu."""

import asyncio
import os
import shutil
from typing import Any

from ..config import MAX_LEDGER_RANGE
from .extract import EXTRACTION_TIMEOUT

# Allowed base directories for output files
ALLOWED_OUTPUT_DIRS = [
    "/tmp",
    os.path.expanduser("~"),
]


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


def _validate_output_path(output_file: str) -> str | None:
    """Validate output path to prevent path traversal attacks.

    Returns None if valid, or an error message if invalid.
    """
    # Resolve to absolute path and normalize
    abs_path = os.path.abspath(os.path.expanduser(output_file))

    # Check for path traversal attempts
    if ".." in output_file:
        return "Path traversal detected: '..' not allowed in output path"

    # Check if path is under an allowed directory
    allowed = False
    for allowed_dir in ALLOWED_OUTPUT_DIRS:
        allowed_abs = os.path.abspath(allowed_dir)
        if abs_path.startswith(allowed_abs + os.sep) or abs_path == allowed_abs:
            allowed = True
            break

    if not allowed:
        return f"Output path must be under one of: {', '.join(ALLOWED_OUTPUT_DIRS)}"

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
    ledger_diff = end_ledger - start_ledger
    if ledger_diff < 0:
        return {"error": "end_ledger must be >= start_ledger"}

    if ledger_diff > MAX_LEDGER_RANGE:
        return {
            "error": f"Ledger range too large ({ledger_diff}). Maximum is {MAX_LEDGER_RANGE} ledgers per call.",
            "suggestion": f"Try a smaller range: start={start_ledger} end={start_ledger + MAX_LEDGER_RANGE}",
        }

    # Validate output path (security: prevent path traversal)
    path_error = _validate_output_path(output_file)
    if path_error:
        return {"error": path_error}

    # Resolve to absolute path for consistency
    abs_output_file = os.path.abspath(os.path.expanduser(output_file))

    # Find nebu binary
    nebu_path = find_nebu()
    if not nebu_path:
        return {
            "error": "nebu CLI not found",
            "suggestion": "Install with: go install github.com/withObsrvr/nebu/cmd/nebu@latest",
        }

    # Validate output directory exists
    output_dir = os.path.dirname(abs_output_file)
    if output_dir and not os.path.exists(output_dir):
        return {"error": f"Output directory does not exist: {output_dir}"}

    # Execute command with timeout using subprocess_exec (no shell)
    # IMPORTANT: stdin=DEVNULL prevents subprocess from waiting on MCP's stdin
    proc = None
    try:
        with open(abs_output_file, "wb") as out_f:
            proc = await asyncio.create_subprocess_exec(
                nebu_path,
                "fetch",
                "-q",
                str(start_ledger),
                str(end_ledger),
                stdin=asyncio.subprocess.DEVNULL,
                stdout=out_f,
                stderr=asyncio.subprocess.PIPE,
            )
            _, stderr = await asyncio.wait_for(
                proc.communicate(),
                timeout=EXTRACTION_TIMEOUT,
            )
            stderr_str = stderr.decode() if stderr else ""
            returncode = proc.returncode
    except asyncio.TimeoutError:
        if proc:
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
        file_size = os.path.getsize(abs_output_file)
    except OSError:
        file_size = 0

    # ledger_count is inclusive: both start and end ledgers are processed
    ledger_count = ledger_diff + 1

    return {
        "file": abs_output_file,
        "ledger_range": [start_ledger, end_ledger],
        "ledgers": ledger_count,
        "bytes": file_size,
    }
