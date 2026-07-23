# agent-sync

Universal AI agent context sync & management tool.

Sync chat histories from all your AI agents (Claude Code, OpenCode, Cursor, etc.) into a unified database. Store and merge context across agents. Share context via "selected-universal" agent groups.

## Quick Start

```bash
# Build & install
make build
sudo make install

# Detect providers on this machine
agent-sync detect

# Sync all chat histories
agent-sync sync

# Search across everything
agent-sync search "database schema"

# Save context
agent-sync context save "The API uses port 9688" --source manual --tags "api,config"

# Merge context entries
agent-sync context merge --ids <id1>,<id2> --name "my-merge" --strategy append

# Create an agent group (selected-universal)
agent-sync group create my-team --description "Dev team agents" --providers "claude-code,opencode"

# Start the web GUI
agent-sync serve
# Open http://localhost:9688

# Run MCP server (for Claude Code integration)
agent-sync serve --mcp
```

## Commands

| Command | Description |
|---------|-------------|
| `sync` | Sync chat histories from all detected agents |
| `list sessions` | List synced sessions |
| `list providers` | List configured providers |
| `show <id>` | Show session details and messages |
| `search <query>` | Full-text search across messages and context |
| `context save` | Save a context entry |
| `context search` | Search context entries |
| `context list` | List all context |
| `context merge` | Merge context entries |
| `context merges` | Show merge history |
| `group create` | Create an agent group (selected-universal sharing) |
| `group list` | List agent groups |
| `export <id> [format]` | Export a session (json, markdown) |
| `serve` | Start API + web GUI server |
| `serve --mcp` | Run MCP stdio server |
| `stats` | Show database statistics |
| `detect` | Detect available providers |

## MCP Integration

Add to your Claude Code config (`~/.claude/settings.json`):

```json
{
  "mcpServers": {
    "agent-sync": {
      "command": "agent-sync",
      "args": ["serve", "--mcp"]
    }
  }
}
```

Tools available: `save_context`, `recall_context`, `search_sessions`, `list_sessions`, `get_stats`.

## Architecture

- **Go binary** — single static binary, no runtime deps
- **SQLite** — local-first, portable, no server
- **Embedded web GUI** — SPA served by binary on port 9688
- **Provider adapters** — pluggable sync for each AI agent
- **MCP server** — stdio-based tool access for AI agents

## Storage

All data lives in `~/.agent-sync/agent-sync.db` (SQLite).
