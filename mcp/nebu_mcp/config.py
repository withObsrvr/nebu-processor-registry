"""Configuration and limits for nebu MCP server."""

# Safe defaults for agent usage
DEFAULT_LIMIT = 100
MAX_LIMIT = 1000
MAX_LEDGER_RANGE = 100

# Output formats
FORMAT_FULL = "full"
FORMAT_COMPACT = "compact"
FORMAT_SUMMARY = "summary"
DEFAULT_FORMAT = FORMAT_COMPACT

# Environment variables
ENV_RPC_URL = "NEBU_RPC_URL"
ENV_RPC_AUTH = "NEBU_RPC_AUTH"
ENV_NETWORK = "NEBU_NETWORK"

# Default RPC endpoint
DEFAULT_RPC_URL = "https://archive-rpc.lightsail.network"
