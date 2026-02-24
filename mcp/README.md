# nebu-mcp

MCP (Model Context Protocol) server for nebu - Stellar blockchain data extraction with agent-friendly defaults.

## Overview

While the nebu CLI is designed for humans using Unix pipes, the MCP server provides an agent-friendly interface with:

- **Safe defaults**: Automatic limits prevent context overflow
- **Compact output**: Reduced token usage with essential fields only
- **Discovery tools**: Programmatic access to processor metadata
- **Guardrails**: Max ledger range per request to bound processing time

## Installation

```bash
# Using pip
pip install nebu-mcp

# Using uv
uv pip install nebu-mcp
```

## Prerequisites

The nebu CLI must be installed and processors available in PATH:

```bash
# Install nebu
go install github.com/withObsrvr/nebu/cmd/nebu@latest

# Install processors
nebu install token-transfer
nebu install contract-events
```

## Configuration

Add to your Claude Desktop config (`~/.config/claude/claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "nebu": {
      "command": "nebu-mcp",
      "env": {
        "NEBU_RPC_URL": "https://archive-rpc.lightsail.network",
        "NEBU_RPC_AUTH": "Api-Key your-key-here"
      }
    }
  }
}
```

## Available Tools

### nebu_list_processors

List available processors for Stellar data extraction.

```python
# List all processors
nebu_list_processors()

# List only origin processors
nebu_list_processors(type="origin")
```

### nebu_describe_processor

Get detailed info about a processor including schema and examples.

```python
nebu_describe_processor(name="token-transfer")
```

### nebu_extract_events

Extract blockchain events from Stellar ledgers. This is the main tool for data extraction.

```python
# Basic extraction
nebu_extract_events(
    processor="token-transfer",
    start_ledger=60200000,
    end_ledger=60200050,
    limit=20,
    format="compact"
)

# With jq filter
nebu_extract_events(
    processor="token-transfer",
    start_ledger=60200000,
    end_ledger=60200050,
    filter='select(.transfer.assetCode == "USDC")',
    format="compact"
)

# Summary only (no individual events)
nebu_extract_events(
    processor="token-transfer",
    start_ledger=60200000,
    end_ledger=60200100,
    format="summary"
)
```

### nebu_run_pipeline

Run a multi-processor pipeline.

```python
nebu_run_pipeline(
    pipeline="token-transfer | usdc-filter | amount-filter --min 1000000",
    start_ledger=60200000,
    end_ledger=60200050,
    limit=10
)
```

### nebu_fetch_ledgers

Fetch raw ledger data (XDR) to a file for later processing.

```python
nebu_fetch_ledgers(
    start_ledger=60200000,
    end_ledger=60200010,
    output_file="/tmp/ledgers.xdr"
)
```

## Output Formats

### Full (format="full")

Complete event JSON with all fields:

```json
{
  "_schema": "nebu.token-transfer.v1",
  "meta": {
    "ledgerSequence": 60200000,
    "txHash": "abc123...",
    "closedAt": "2025-01-15T12:00:00Z"
  },
  "transfer": {
    "from": "GA...",
    "to": "GB...",
    "amount": "10000000",
    "assetCode": "USDC"
  }
}
```

### Compact (format="compact", default)

Essential fields only - reduced token usage:

```json
{
  "type": "transfer",
  "ledger": 60200000,
  "tx": "abc123...",
  "from": "GA...",
  "to": "GB...",
  "amount": "10000000",
  "asset": "USDC"
}
```

### Summary (format="summary")

Aggregated statistics only - minimal tokens:

```json
{
  "total_events": 1847,
  "by_type": {"transfer": 1200, "mint": 400, "fee": 247},
  "by_asset": {"USDC": 800, "XLM": 700, "EURC": 347},
  "ledger_range": [60200000, 60200100],
  "truncated": false
}
```

## Safe Defaults

| Parameter | Default | Max | Rationale |
|-----------|---------|-----|-----------|
| limit | 100 | 1000 | Prevents context overflow |
| ledger range | - | 100 | Bounds processing time |
| format | compact | - | Reduces token usage |

## Human vs Agent Interface

| Aspect | CLI (Humans) | MCP (Agents) |
|--------|--------------|--------------|
| Output limit | Unlimited (Ctrl+C) | Default 100, max 1000 |
| Ledger range | Unlimited | Max 100 per call |
| Format | Full JSON | Compact by default |
| Discovery | --help, nebu list | Tool schema + describe |
| Piping | Native Unix | run_pipeline tool |

## Development

```bash
# Clone and install in development mode
git clone https://github.com/withObsrvr/nebu-processor-registry
cd nebu-processor-registry/mcp
pip install -e .

# Run the server
nebu-mcp
```

## License

MIT
