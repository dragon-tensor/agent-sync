# agent-sync — Build Workflow

## Current State

The project is a Go binary with a full CLI, Bubbletea TUI, SQLite + FTS5 storage, 3 sync providers (Claude Code, OpenCode, Cursor), context store with merge engine, entity extraction engine, knowledge graph, conflict detection, snapshots, agent groups, MCP server, and embedded web API. **Phase 3 complete.**

### What's Done
- CLI (cobra) — sync, list, show, search, context, group, export, serve, stats, detect, tui, snapshot, extract, entities, graph, graph-tree
- TUI (bubbletea) — 5 tabs: Dashboard, Sessions, Context (with sub-modes: entries/entities/conflicts/snapshots), Groups, Providers
- SQLite schema — providers, sessions, messages, context_entries, context_merges, agent_groups, entities, entity_relations, snapshots
- FTS5 full-text search on messages and context
- Sync adapters: Claude Code (JSONL), OpenCode (JSON), Cursor (detect only)
- Context store + merge engine (append/dedup/summarize strategies)
- Entity extraction engine — regex-based extraction of decisions, facts, code patterns, preferences, goals
- Knowledge graph — co-occurrence edges, cross-session clustering, path finding, text tree display
- Conflict detection — word-overlap comparison, contradiction flagging
- Snapshots — CRUD, markdown/prompt/json export formats
- Agent groups (selected-universal context sharing)
- MCP server (stdio) — 7 tools (including list_entities, list_conflicts)
- Web API (chi) — REST endpoints on port 9688 with embedded SPA
- Config in `~/.agent-sync/config.json`
- Animated logo in TUI and web UI

### What's Left (per PRD)
- **Phase 4**: More adapters (ChatGPT, Claude Web, Gemini, Copilot, Codex, generic), WASM plugins, daemon mode, packaging

---

## Workflow Process

```
1. Work on a phase
2. Verify: make test && go build ./...
3. Report phase results below
4. git add . && git commit -m "<message>"
5. Move to next phase
```

---

## Phase 3 — Context Engine & Knowledge Graph

**Goal**: Extract structured entities from conversations, build a knowledge graph, detect conflicts, and create exportable context snapshots.

### Tasks

- [x] **3.1 Entity extraction engine**
  - Create `internal/context/extractor.go`
  - Implement regex-based entity extraction from message content:
    - Decisions: `"decided to"`, `"we chose"`, `"let's use"`, `"going with"`
    - Code patterns: code block detection with language tags
    - Facts: `"uses X for Y"`, `"is running on"`, `"depends on"`
    - Preferences: `"I prefer"`, `"I like"`, `"I'd rather"`
    - Goals: `"TODO:"`, `"next step"`, `"we need to"`
  - Normalize entity names (lowercase, stem, strip punctuation)
  - Score confidence based on pattern match quality
  - Add `entities` and `entity_relations` tables to schema

- [x] **3.2 Knowledge graph builder**
  - Create `internal/context/graph.go`
  - Build entity graph from co-occurrence: entities mentioned in same session get an edge
  - Cross-session references: same entity name in multiple sessions → cluster
  - Export graph as JSON for TUI visualization

- [x] **3.3 Conflict detection**
  - Created in `internal/context/extractor.go` (word overlap check in `checkConflicts`)
  - Compare entity summaries across sources using word overlap
  - Flag contradictions when same entity name has <40% summary word overlap
  - Store conflicts as `entity_relations` with type `contradicts`
  - List conflicts in CLI: `agent-sync context conflicts`

- [x] **3.4 Context snapshots**
  - Add `snapshots` table and CRUD in store
  - CLI: `agent-sync snapshot create <name> --entity-ids <ids>`
  - CLI: `agent-sync snapshot list`
  - CLI: `agent-sync snapshot export <id> [--format prompt|json|md]`
  - Prompt format: compact context for tool handoff

- [x] **3.5 TUI: Context tab upgrade**
  - Added sub-mode tabs (1:Entries, 2:Entities, 3:Conflicts, 4:Snapshots)
  - Entity list view with type tags and confidence scores
  - Conflict panel showing contradictions
  - Snapshot list view
  - Keyboard navigation with `1-4` to switch sub-modes

- [x] **3.6 Auto-extraction during sync**
  - `agent-sync sync --extract` flag triggers extraction after sync
  - `runExtraction` helper iterates sessions and calls `ExtractFromMessages`
  - Rebuilds knowledge graph and reports entity/conflict counts

### Verification

```bash
make test
go build ./...
agent-sync context list
agent-sync context conflicts
agent-sync snapshot list
```

### End-of-Phase Report Template

```
...

---

## Phase 3 Report — Context Engine & Knowledge Graph

Status: **complete**

### What was built
- **Entity extraction**: 5 regex patterns for decisions, facts, code patterns, preferences, goals; confidence scoring; normalization (lowercase, stem, punctuation strip); co-occurrence edge building
- **Knowledge graph**: `GraphQuery` with `BuildGraph`, `GetNeighbors`, `FindPath`, `GetClusters`, `TextTree`, `Summary`, `GetStats`
- **Conflict detection**: word-overlap comparison (<40% threshold), stored as `entity_relations` with type `contradicts`
- **Snapshots**: CRUD operations, export formats (prompt, json, md), CLI commands
- **TUI**: context tab sub-modes (Entries/Entities/Conflicts/Snapshots), entity list with type tags & confidence, conflict panel, snapshot list
- **Auto-extraction**: `sync --extract` flag, runs `ExtractFromMessages` on all provider sessions, rebuilds graph, reports entity/conflict counts
- **API**: 9 entity/graph/snapshot REST endpoints + 2 MCP tools (list_entities, list_conflicts)

### Verification
- Build clean: **yes** (`go build ./...` and `go vet ./...` pass)
- All CLI commands functional: extract, entities, conflicts, graph, graph-tree, snapshot create/list/export
- TUI context tab shows entities, conflicts, and snapshots with sub-mode tabs

### Git commit
```bash
git add .
git commit -m "feat(context): add entity extraction, knowledge graph, conflict detection, snapshots, and TUI upgrade"
```

---

## Phase 4 — Adapters, Plugins & Polish

**Goal**: Support all major AI platforms, add WASM plugin system for community adapters, daemon mode for auto-sync, and package for distribution.

### Tasks

- [ ] **4.1 ChatGPT adapter**
  - Parse OpenAI export ZIP (`conversations.json`)
  - Handle branching conversations (tree structure)
  - Map roles: user ↔ assistant ↔ system ↔ tool
  - Extract model info and timestamps

- [ ] **4.2 Claude Web adapter**
  - Parse Claude.ai export JSON
  - Handle multi-turn conversations with attachments
  - Map project/workspace metadata

- [ ] **4.3 Gemini adapter**
  - Parse Google Takeout export
  - Handle Gemini-specific message format

- [ ] **4.4 GitHub Copilot adapter**
  - Read VS Code `workspaceStorage` chat sessions
  - Parse `chatSessions/*.jsonl` and chat index
  - Handle Agent Sessions cache

- [ ] **4.5 Codex adapter**
  - Parse `~/.codex/history.jsonl`
  - Handle rollout JSONL and session indexes

- [ ] **4.6 Generic JSONL/MD adapter**
  - User-provided JSONL files with flexible schema
  - User-provided markdown files with frontmatter
  - Auto-detect format heuristics

- [ ] **4.7 WASM plugin system**
  - Add `wasmtime-go` dependency
  - Define WIT interface for adapters
  - Plugin registry: list, install, enable, disable
  - CLI: `agent-sync adapters repo add <url>`

- [ ] **4.8 Daemon mode**
  - Background sync service (`agent-sync daemon`)
  - cron/systemd/launchd integration
  - Configurable sync interval
  - Watch mode: file watcher for new chat files

- [ ] **4.9 Packaging**
  - Homebrew formula
  - GitHub Actions release workflow
  - Prebuilt binaries for Linux (x86_64, ARM64) and macOS (Intel, Apple Silicon)
  - AUR package

### Verification

```bash
make test
go build ./...
agent-sync detect
agent-sync sync
agent-sync daemon --dry-run
agent-sync adapters list
```

### End-of-Phase Report Template

```
## Phase 4 Report — Adapters, Plugins & Polish

Status: [complete / partial]

### What was built
- ChatGPT adapter: [ZIP parsing, branching convos]
- Claude Web adapter: [export JSON parsing]
- Gemini adapter: [Takeout parsing]
- Copilot adapter: [VS Code chat sessions]
- Codex adapter: [history.jsonl]
- Generic adapter: [JSONL, markdown]
- WASM plugins: [interface, registry, CLI]
- Daemon mode: [background sync, cron/systemd]
- Packaging: [Homebrew, releases, AUR]

### Verification
- Tests passing: [yes/no]
- Build clean: [yes/no]
- Manual smoke test: [passed/failed]

### Remaining work (if partial)
- [ ] ...

### Git commit
```bash
git add .
git commit -m "feat(adapters): add ChatGPT, Claude Web, Gemini, Copilot, Codex, and generic adapters; WASM plugin system; daemon mode; packaging"
```
```

---

## Commit Convention

```
<type>(<scope>): <description>
```

**Types**: `feat`, `fix`, `refactor`, `chore`, `docs`, `test`, `perf`
**Scopes**: `cli`, `tui`, `db`, `sync`, `context`, `adapters`, `mcp`, `api`, `config`, `groups`, `core`

Examples:
```
feat(context): add entity extraction with regex patterns
feat(adapters): implement ChatGPT export ZIP parser
feat(tui): add conflict panel to context tab
refactor(db): optimize session listing query
fix(sync): handle malformed JSONL files gracefully
```

---

## Running Build

```bash
make build        # go build -o agent-sync ./cmd/agent-sync/
make test         # go test ./...
make run          # build + run TUI
make serve        # build + start web server on :9688
make install      # build + install to /usr/local/bin
make clean        # remove binary
```

---

## Project Structure

```
agent-sync/
├── cmd/agent-sync/main.go          # CLI entrypoint
├── internal/
│   ├── api/server.go               # Web API (chi) + embedded SPA
│   ├── api/web/index.html          # Embedded web UI
│   ├── config/config.go            # Config loading
│   ├── context/store.go            # Context CRUD
│   ├── context/merge.go            # Merge engine
│   ├── db/db.go                    # SQLite database + CRUD
│   ├── db/schema.go                # Schema DDL
│   ├── groups/manager.go           # Agent group management
│   ├── mcp/server.go               # MCP stdio server
│   ├── sync/provider.go            # Provider interface
│   ├── sync/registry.go            # Provider registry
│   ├── sync/providers/             # Adapter implementations
│   │   ├── config.go
│   │   ├── claude_code.go
│   │   ├── opencode.go
│   │   └── cursor.go
│   └── tui/
│       ├── tui.go                  # Bubbletea TUI
│       └── styles.go               # Lipgloss styles
├── pkg/types/types.go              # Shared types
├── web/src/index.html              # Web UI source (dev copy)
├── PRD.md                          # Product requirements
├── workflow.md                     # This file
├── Makefile
├── go.mod / go.sum
└── README.md
```
