"""Discovery tools for nebu processors."""

import json
import subprocess
from typing import Any, Literal


async def list_processors(
    type: Literal["origin", "transform", "sink", "all"] = "all",
) -> dict[str, Any]:
    """List available processors for Stellar data extraction.

    Args:
        type: Filter by processor type (origin, transform, sink, or all)

    Returns:
        List of processors with name, type, and description
    """
    cmd = ["nebu", "list", "--json"]

    result = subprocess.run(cmd, capture_output=True, text=True)

    if result.returncode != 0:
        return {"error": f"Failed to list processors: {result.stderr}"}

    try:
        processors = json.loads(result.stdout)
    except json.JSONDecodeError as e:
        return {"error": f"Failed to parse processor list: {e}"}

    # Filter by type if specified
    if type != "all":
        processors = [p for p in processors if p.get("type") == type]

    # Return simplified list
    return {
        "processors": [
            {
                "name": p["name"],
                "type": p["type"],
                "description": p["description"],
            }
            for p in processors
        ],
        "count": len(processors),
    }


async def describe_processor(name: str) -> dict[str, Any]:
    """Get detailed info about a processor including schema and examples.

    Args:
        name: Processor name (e.g., token-transfer)

    Returns:
        Full processor documentation including output schema and examples
    """
    cmd = ["nebu", "describe", name, "--json"]

    result = subprocess.run(cmd, capture_output=True, text=True)

    if result.returncode != 0:
        # Try to extract helpful error message
        error_msg = result.stderr.strip()
        if "not found" in error_msg.lower():
            return {
                "error": f"Processor '{name}' not found",
                "suggestion": "Use nebu_list_processors to see available processors",
            }
        return {"error": f"Failed to describe processor: {error_msg}"}

    try:
        processor = json.loads(result.stdout)
    except json.JSONDecodeError as e:
        return {"error": f"Failed to parse processor details: {e}"}

    return processor
