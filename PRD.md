# agent-sync — PRD & Implementation Plan

## 1. Product Overview

**agent-sync** is a TUI-first tool that syncs AI chat histories from every major platform into a unified local store, extracts structured context (decisions, facts, code patterns, preferences), detects conflicts across sources, builds a knowledge graph, and lets users merge/browse/export context snapshots for seamless tool handoff.

### 1.1 Elevator Pitch

> One TUI to explore everything Claude, ChatGPT, Codex, Cursor, Ollama, and more ever told you. Extract decisions, facts, and patterns. Merge context across sessions. Hand off coherent snapshots between tools. 100% local.

### 1.2 Problem Statement

- AI conversations are scattered across silos — Claude Code, ChatGPT, Codex, Cursor, Ollama, Gemini, Copilot — each with its own storage format and no cross-tool visibility.
- Switching tools means losing context: rate limits, context window exhaustion, or wanting a different model all force a reset.
- No existing tool builds a unified knowledge graph from all conversations — they index raw text but don't extract decisions, detect contradictions, or let you compose a coherent context for handoff.

### 1.3 Target Audience

- **Individual power-users**: Manage personal AI chat histories across many tools.
- **Developer teams**: Share project context across team members and toolchains.

---

## 2. Core Features

### 2.1 Multi-Source Ingestion

| Platform | Adapter | Method | Priority |
|----------|---------|--------|----------|
| Claude Code | `claude-code` | `~/.claude/projects/*/transcript.jsonl` | P0 |
| OpenAI ChatGPT | `chatgpt` | Export ZIP (`conversations.json`) | P0 |
| OpenCode | `opencode` | `~/.local/share/opencode/opencode.db` SQLite | P0 |
| OpenAI Codex | `codex` | `~/.codex/history.jsonl` | P1 |
| Cursor | `cursor` | `workspaceStorage/*.vscdb` SQLite | P1 |
| Ollama | `ollama` | Conversation files | P1 |
| Claude.ai | `claude-web` | Export JSON | P2 |
| Google Gemini | `gemini` | Google Takeout export | P2 |
| GitHub Copilot | `copilot` | VS Code `workspaceStorage` chat sessions | P2 |
| Aider | `aider` | `.aider.chat.history.md` | P2 |
| Open WebUI | `openwebui` | SQLite | P2 |
| Generic JSONL/MD | `generic` | User-provided files | P2 |

### 2.2 Context Merge Engine

- **Entity extraction**: Parse messages for decisions, facts, code patterns, preferences, goals.
- **Deduplication**: LSH-based near-dedupe across sources; merge source lists and evidence.
- **Conflict detection**: Same entity with contradictory summaries → flag for user review.
- **Knowledge graph**: Entities as nodes, co-occurrence/references as edges.
- **Context snapshots**: Select entity subsets → export as compact handoff prompt.

### 2.3 TUI

- **Dashboard**: Source status, stats, recent sync log.
- **Browse**: 3-pane layout — conversation list → message thread → message detail. Full-text search via SQLite FTS5.
- **Context**: Entity graph (force-directed) + entity detail + merge/conflict panel.
- **Sources/Settings**: Add/remove sources, configure sync, theme selection.

### 2.4 Export & Interop

- Export conversations/context as markdown, JSON, compact handoff prompts.
- MCP server (Phase 4) so AI agents can query their own history mid-session.

---

## 3. Data Model (SQLite)

```sql
CREATE TABLE sources (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    adapter TEXT NOT NULL,
    path TEXT,
    last_synced_at TEXT,
    enabled INTEGER DEFAULT 1
);

CREATE TABLE conversations (
    id TEXT PRIMARY KEY,
    source_id TEXT REFERENCES sources(id),
    title TEXT,
    project TEXT,
    model TEXT,
    created_at TEXT,
    updated_at TEXT,
    message_count INTEGER,
    token_estimate INTEGER,
    metadata TEXT
);

CREATE TABLE messages (
    id TEXT PRIMARY KEY,
    conversation_id TEXT REFERENCES conversations(id),
    role TEXT NOT NULL,
    content TEXT NOT NULL,
    tool_calls TEXT,
    tool_results TEXT,
    parent_id TEXT,
    created_at TEXT,
    token_count INTEGER
);

CREATE VIRTUAL TABLE messages_fts USING fts5(
    content, title='messages', content='messages', content_rowid='rowid'
);

CREATE TABLE entities (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    entity_type TEXT,
    summary TEXT,
    source_ids TEXT,
    confidence REAL,
    created_at TEXT,
    updated_at TEXT
);

CREATE TABLE entity_relations (
    id TEXT PRIMARY KEY,
    source_entity_id TEXT REFERENCES entities(id),
    target_entity_id TEXT REFERENCES entities(id),
    relation_type TEXT,
    weight REAL,
    evidence TEXT
);

CREATE TABLE context_snapshots (
    id TEXT PRIMARY KEY,
    name TEXT,
    description TEXT,
    entity_ids TEXT,
    created_at TEXT,
    tags TEXT
);

CREATE TABLE sync_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    source_id TEXT REFERENCES sources(id),
    started_at TEXT,
    completed_at TEXT,
    new_conversations INTEGER,
    new_messages INTEGER,
    errors TEXT
);
```

---

## 4. Architecture

```
┌──────────────────────────────────────────────┐
│                  CLI (main.rs)                │
│   agent-sync → TUI    agent-sync sync → CLI   │
└──────────────────┬───────────────────────────┘
                   │
      ┌────────────┼────────────┬──────────────┐
      ▼            ▼            ▼              ▼
┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐
│   TUI    │ │  Ingest  │ │ Context  │ │   MCP    │
│ (ratatui)│ │(adapters)│ │  Engine  │ │ (Phase 4)│
└────┬─────┘ └────┬─────┘ └────┬─────┘ └──────────┘
     │            │            │
     └────────────┼────────────┘
                  ▼
          ┌──────────────┐
          │   Core       │
          │  (SQLite +   │
          │   config)    │
          └──────────────┘
```

### 4.1 Tech Stack

| Layer | Choice |
|-------|--------|
| Language | Rust (edition 2024) |
| TUI | ratatui + crossterm |
| Storage | rusqlite + FTS5 |
| Config | toml + serde |
| Async | tokio |
| WASM runtime | wasmtime (Phase 3) |
| gRPC | tonic (Phase 4) |

### 4.2 Crate Structure

```
agent-sync/
├── Cargo.toml (workspace)
├── crates/
│   ├── core/          # schema, models, store, config
│   ├── ingest/        # adapter trait + platform adapters
│   ├── context/       # entity extraction, merge, graph
│   ├── tui/           # ratatui screens & widgets
│   └── cli/           # binary entrypoint + CLI commands
```

---

## 5. CLI Commands

| Command | Description |
|---------|-------------|
| `agent-sync` | Launch TUI |
| `agent-sync sync` | Sync all enabled sources |
| `agent-sync scan` | Auto-detect local sources |
| `agent-sync search <query>` | Full-text search, JSON output |
| `agent-sync list` | List conversations (filterable) |
| `agent-sync show <id>` | Print conversation to stdout |
| `agent-sync export <id>` | Export as markdown/json |
| `agent-sync merge` | Run context merge, show conflicts |
| `agent-sync snapshot create <name>` | Create context snapshot |
| `agent-sync snapshot export <name>` | Export snapshot as prompt |
| `agent-sync config show\|set\|path` | Manage config |
| `agent-sync stats` | DB statistics |
| `agent-sync daemon` | Background auto-sync |
| `agent-sync mcp` | Start MCP server |

---

## 6. TUI Screens

### Dashboard
- Source list with sync status and message counts
- Stats cards: conversations, messages, entities, conflicts
- Recent sync log (scrollable)
- Actions: `s` sync all, `S` sync selected

### Browse (3-pane)
- **Left**: Conversation list, filterable by `/` search, `j/k` navigate
- **Center**: Full message thread, `Enter` for detail
- **Right**: Message content with syntax-highlighted code blocks
- Actions: `e` export, `x` extract entities

### Context
- **Graph view**: Force-directed entity graph, color-coded by type
- **List view**: Flat entity table, filterable by type/source/confidence
- **Merge panel**: Conflicts side-by-side, accept/reject per entity
- **Snapshots**: Create/view/export context snapshots

### Sources/Settings
- Add/remove/enable/disable sources with auto-detect
- Theme picker, sync interval, extraction mode
- About: version, DB size, adapter versions

---

## 7. Adapter Interface

```rust
#[async_trait]
pub trait Adapter: Send + Sync {
    fn id(&self) -> &'static str;
    fn display_name(&self) -> &'static str;
    fn auto_detect(&self) -> Result<Vec<SourceConfig>, AdapterError>;
    async fn sync(&self, config: &SourceConfig, store: &Store) -> Result<SyncResult, AdapterError>;
}
```

Phase 3+: WASM plugins using wasmtime runtime:

```wit
adapter: func(id) -> string
display-name: func() -> string
auto-detect: func() -> list<source-config>
sync: func(config: source-config, db-path: string) -> sync-result
```

---

## 8. Context Merge Engine

### Extraction Pipeline

1. Parse each message for structural patterns:
   - **Decisions**: "decided to", "we chose", "let's use"
   - **Code patterns**: Code blocks with language detection
   - **Facts**: Key-value patterns ("uses X for Y")
   - **Preferences**: "I prefer", "I like", "I'd rather"
   - **Goals**: "TODO:", "next step", "we need to"
2. Normalize entities to canonical names (stemming, case folding)
3. Store with source attribution and confidence score

### Dedup & Merge

- LSH minhash on entity summaries for near-duplicate detection
- Same canonical name from multiple sources → merge source lists, average confidence
- Evidence snippets concatenated with source attribution

### Conflict Detection

- Same entity name with <40% word overlap in summaries → conflict flag
- Same relationship with opposite direction → conflict flag
- Conflicts stored in `entity_relations` with `relation_type = "contradicts"`

### Graph Building

- Entities as nodes
- Co-occurrence in same conversation session → edge
- Explicit cross-references → directed edge
- Same project/workspace → cluster

---

## 9. Configuration (`~/.config/agent-sync/config.toml`)

```toml
[core]
db_path = "~/.local/share/agent-sync/store.db"
blob_path = "~/.local/share/agent-sync/blobs"

[sync]
auto_sync = false
parallel_adapters = 2
on_conflict = "prompt"

[context]
extraction = "heuristic"
confidence_threshold = 0.5
auto_merge = false

[sources.claude-code]
enabled = true
path = "~/.claude/projects"

[sources.chatgpt]
enabled = false
path = "~/Downloads/chatgpt-export"

[ui]
theme = "catppuccin-mocha"
show_line_numbers = true
default_view = "dashboard"
```

---

## 10. Implementation Roadmap

### Phase 1 — Foundation (Weeks 1-2)

- [ ] Cargo workspace with `core`, `ingest`, `cli` crates
- [ ] SQLite schema + migrations + Store CRUD
- [ ] 2 adapters: `claude-code` (JSONL), `chatgpt` (export ZIP)
- [ ] CLI commands: `sync`, `search`, `list`, `show`, `stats`
- [ ] Minimal TUI skeleton (dashboard screen)
- [ ] Config file parsing (TOML)

### Phase 2 — TUI + Adapters (Weeks 3-4)

- [ ] Full TUI: Dashboard, Browse, Context, Sources
- [ ] 4 more adapters: `opencode`, `codex`, `cursor`, `ollama`
- [ ] FTS5 full-text search with filters
- [ ] Export to markdown, JSON
- [ ] Theme system (catppuccin, dracula, etc.)

### Phase 3 — Context Engine (Weeks 5-6)

- [ ] Entity extraction (heuristic: regex + pattern matching)
- [ ] Deduplication (LSH minhash)
- [ ] Conflict detection
- [ ] Knowledge graph builder
- [ ] Context snapshots (create, list, export)
- [ ] Merge preview TUI screen

### Phase 4 — Polish & Extend (Weeks 7-8)

- [ ] Remaining adapters: `gemini`, `copilot`, `claude-web`, `generic`
- [ ] WASM plugin system (wasmtime)
- [ ] Daemon mode with launchd/cron auto-sync
- [ ] MCP server (stdio gRPC)
- [ ] Packaging: Homebrew, AUR, cargo install
- [ ] Documentation site

---

## 11. Risks & Mitigations

| Risk | Mitigation |
|------|------------|
| Platform JSONL format changes | Adapter version pinning + CI test fixtures |
| SQLite perf at scale (100k+ messages) | FTS5, pagination, cursor-based queries, WAL mode |
| WASM sandbox escape | wasmtime with controlled FS access and resource limits |
| Daemon resource usage | Single-pass sync, SQLite WAL mode, no in-memory index |
| Conflicting info across platforms | Always surface conflicts; never auto-merge without explicit config |
| Privacy concerns | 100% local, zero telemetry, remote sync is user-initiated and opt-in |

---

## 12. Testing Strategy

- **Unit tests**: Each adapter with fixture data files. Entity extraction with known inputs/outputs.
- **Integration tests**: Full sync pipeline against in-memory SQLite with small fixture databases.
- **Snapshot tests**: Export output compared to gold files.
- **TUI snapshot tests**: ratatui frame rendering comparison.

---

## 13. Success Metrics

- Sync from 6+ platforms in under 5 seconds (cold) / 2 seconds (incremental)
- Entity extraction with >80% precision on decisions and facts
- TUI startup under 500ms
- Single binary under 15MB
