"""MCP server for nebu - Stellar blockchain data extraction.

This server provides agent-friendly access to nebu processors with
safe defaults to prevent context overflow.
"""

import asyncio
from typing import Literal

from mcp.server import Server
from mcp.server.stdio import stdio_server
from mcp.types import TextContent, Tool

from .config import DEFAULT_FORMAT, DEFAULT_LIMIT, MAX_LEDGER_RANGE, MAX_LIMIT
from .tools.discovery import describe_processor, list_processors
from .tools.extract import extract_events
from .tools.fetch import fetch_ledgers
from .tools.pipeline import run_pipeline

# Create server instance
server = Server("nebu-mcp")


@server.list_tools()
async def list_tools() -> list[Tool]:
    """List available MCP tools."""
    return [
        Tool(
            name="nebu_debug_env",
            description="Debug: show environment variables and processor paths",
            inputSchema={
                "type": "object",
                "properties": {},
            },
        ),
        Tool(
            name="nebu_debug_extract",
            description="Debug: run extraction with verbose output",
            inputSchema={
                "type": "object",
                "properties": {
                    "ledger": {
                        "type": "integer",
                        "description": "Single ledger to test",
                    }
                },
                "required": ["ledger"],
            },
        ),
        Tool(
            name="nebu_list_processors",
            description="List available processors for Stellar data extraction",
            inputSchema={
                "type": "object",
                "properties": {
                    "type": {
                        "type": "string",
                        "enum": ["origin", "transform", "sink", "all"],
                        "description": "Filter by processor type (default: all)",
                        "default": "all",
                    }
                },
            },
        ),
        Tool(
            name="nebu_describe_processor",
            description="Get detailed info about a processor including schema and examples",
            inputSchema={
                "type": "object",
                "properties": {
                    "name": {
                        "type": "string",
                        "description": "Processor name (e.g., token-transfer)",
                    }
                },
                "required": ["name"],
            },
        ),
        Tool(
            name="nebu_extract_events",
            description=f"Extract blockchain events from Stellar ledgers. Returns up to {MAX_LIMIT} events. Max {MAX_LEDGER_RANGE} ledgers per call.",
            inputSchema={
                "type": "object",
                "properties": {
                    "processor": {
                        "type": "string",
                        "description": "Processor to use (e.g., token-transfer, contract-events)",
                    },
                    "start_ledger": {
                        "type": "integer",
                        "description": "First ledger to process",
                    },
                    "end_ledger": {
                        "type": "integer",
                        "description": f"Last ledger to process (max {MAX_LEDGER_RANGE} ledgers per call)",
                    },
                    "filter": {
                        "type": "string",
                        "description": "Optional jq filter expression (e.g., 'select(.transfer.assetCode == \"USDC\")')",
                    },
                    "limit": {
                        "type": "integer",
                        "description": f"Maximum events to return (default {DEFAULT_LIMIT}, max {MAX_LIMIT})",
                        "default": DEFAULT_LIMIT,
                    },
                    "format": {
                        "type": "string",
                        "enum": ["full", "compact", "summary"],
                        "description": "Output format: full (all fields), compact (essential fields), summary (counts only)",
                        "default": DEFAULT_FORMAT,
                    },
                },
                "required": ["processor", "start_ledger", "end_ledger"],
            },
        ),
        Tool(
            name="nebu_fetch_ledgers",
            description="Fetch raw ledger data (XDR) to a file - use nebu_extract_events for most cases",
            inputSchema={
                "type": "object",
                "properties": {
                    "start_ledger": {
                        "type": "integer",
                        "description": "First ledger to fetch",
                    },
                    "end_ledger": {
                        "type": "integer",
                        "description": f"Last ledger to fetch (max {MAX_LEDGER_RANGE} ledgers per call)",
                    },
                    "output_file": {
                        "type": "string",
                        "description": "File path to save XDR data",
                    },
                },
                "required": ["start_ledger", "end_ledger", "output_file"],
            },
        ),
        Tool(
            name="nebu_run_pipeline",
            description="Run a multi-processor pipeline (e.g., 'token-transfer | usdc-filter | amount-filter --min 1000000')",
            inputSchema={
                "type": "object",
                "properties": {
                    "pipeline": {
                        "type": "string",
                        "description": "Pipeline command with processors separated by |",
                    },
                    "start_ledger": {
                        "type": "integer",
                        "description": "First ledger to process",
                    },
                    "end_ledger": {
                        "type": "integer",
                        "description": f"Last ledger to process (max {MAX_LEDGER_RANGE} ledgers per call)",
                    },
                    "limit": {
                        "type": "integer",
                        "description": f"Maximum events to return (default {DEFAULT_LIMIT}, max {MAX_LIMIT})",
                        "default": DEFAULT_LIMIT,
                    },
                    "format": {
                        "type": "string",
                        "enum": ["full", "compact", "summary"],
                        "description": "Output format: full, compact, or summary",
                        "default": DEFAULT_FORMAT,
                    },
                },
                "required": ["pipeline", "start_ledger", "end_ledger"],
            },
        ),
    ]


@server.call_tool()
async def call_tool(name: str, arguments: dict) -> list[TextContent]:
    """Handle tool calls."""
    import json
    import os
    import shutil

    result: dict

    if name == "nebu_debug_env":
        # Debug tool to check environment and paths
        home = os.path.expanduser("~")
        result = {
            "NEBU_RPC_AUTH": "***" if os.environ.get("NEBU_RPC_AUTH") else "NOT SET",
            "NEBU_RPC_URL": os.environ.get("NEBU_RPC_URL", "NOT SET"),
            "PATH": os.environ.get("PATH", "")[:200] + "...",
            "token_transfer_path": shutil.which("token-transfer") or os.path.join(home, "go", "bin", "token-transfer"),
            "token_transfer_exists": os.path.isfile(os.path.join(home, "go", "bin", "token-transfer")),
            "nebu_path": shutil.which("nebu") or os.path.join(home, "go", "bin", "nebu"),
            "nebu_exists": os.path.isfile(os.path.join(home, "go", "bin", "nebu")),
        }

    elif name == "nebu_debug_extract":
        import asyncio
        ledger = arguments["ledger"]
        home = os.path.expanduser("~")
        processor_path = os.path.join(home, "go", "bin", "token-transfer")
        cmd = f"{processor_path} --start-ledger {ledger} --end-ledger {ledger} -q 2>&1 | head -3"

        try:
            proc = await asyncio.create_subprocess_shell(
                cmd,
                stdin=asyncio.subprocess.DEVNULL,
                stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.PIPE,
            )
            stdout, stderr = await asyncio.wait_for(
                proc.communicate(),
                timeout=30,
            )
            result = {
                "command": cmd,
                "returncode": proc.returncode,
                "stdout": stdout.decode()[:1000] if stdout else "",
                "stderr": stderr.decode()[:500] if stderr else "",
            }
        except asyncio.TimeoutError:
            result = {
                "command": cmd,
                "error": "Timed out after 30s",
            }
        except Exception as e:
            result = {
                "command": cmd,
                "error": str(e),
            }

    elif name == "nebu_list_processors":
        proc_type = arguments.get("type", "all")
        result = await list_processors(proc_type)

    elif name == "nebu_describe_processor":
        proc_name = arguments.get("name", "")
        result = await describe_processor(proc_name)

    elif name == "nebu_extract_events":
        result = await extract_events(
            processor=arguments["processor"],
            start_ledger=arguments["start_ledger"],
            end_ledger=arguments["end_ledger"],
            filter=arguments.get("filter"),
            limit=arguments.get("limit", DEFAULT_LIMIT),
            format=arguments.get("format", DEFAULT_FORMAT),
        )

    elif name == "nebu_fetch_ledgers":
        result = await fetch_ledgers(
            start_ledger=arguments["start_ledger"],
            end_ledger=arguments["end_ledger"],
            output_file=arguments["output_file"],
        )

    elif name == "nebu_run_pipeline":
        result = await run_pipeline(
            pipeline=arguments["pipeline"],
            start_ledger=arguments["start_ledger"],
            end_ledger=arguments["end_ledger"],
            limit=arguments.get("limit", DEFAULT_LIMIT),
            format=arguments.get("format", DEFAULT_FORMAT),
        )

    else:
        result = {"error": f"Unknown tool: {name}"}

    return [TextContent(type="text", text=json.dumps(result, indent=2))]


async def main():
    """Run the MCP server."""
    async with stdio_server() as (read_stream, write_stream):
        await server.run(
            read_stream,
            write_stream,
            server.create_initialization_options(),
        )


def run():
    """Entry point for the CLI."""
    asyncio.run(main())


if __name__ == "__main__":
    run()
