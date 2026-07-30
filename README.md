# DRAGON SYNC

> A local, terminal-first workspace for continuing one AI conversation across coding agents.

Dragon Sync keeps a private conversation ledger on your machine while each agent keeps its own native session. Start with one tool, switch whenever you need a different perspective, and return later without replaying context the agent has already seen.

```text
  DRAGON SYNC
  ───────────
  one workspace · many agents · no lost context
```

## What it does

- Starts a local chat with Claude Code, Codex, OpenCode, or Gemini.
- Keeps a canonical, local-only conversation ledger in SQLite.
- Resumes previous Dragon Sync chats with `/resume`.
- Switches tools with `/switch` while transferring only the work the destination agent has not yet received.
- Imports detected local history into a new Dragon Sync chat with `/import`.
- Persists queued messages across Dragon Sync restarts.
- Keeps structured or native-control agent processes alive in background tabs.
- Routes the active agent's slash commands directly—there is no `/native` mode.
- Supports interactive native pickers and prompts inside an embedded Agent Control terminal.
- Cancels an active turn with two presses of `Esc`.
- Shows the agent chain plus any model, effort, token, context-window, and cost data the provider reports.

## Quick start

### Requirements

- Go 1.25 or newer
- At least one supported local agent CLI in your `PATH`:
  `claude`, `codex`, `opencode`, or `gemini`

```bash
git clone https://github.com/agent-sync/agent-sync.git
cd agent-sync

GOCACHE=/tmp/dragon-sync-gocache GOFLAGS=-mod=mod go run ./cmd/dragon-sync
```

Or build a reusable binary:

```bash
GOCACHE=/tmp/dragon-sync-gocache GOFLAGS=-mod=mod go build -o dragon-sync ./cmd/dragon-sync
./dragon-sync
```

`sync` is an equivalent short executable name:

```bash
GOCACHE=/tmp/dragon-sync-gocache GOFLAGS=-mod=mod go run ./cmd/sync
```

Dragon Sync stores its local database and configuration under `~/.agent-sync/`.
Set `DRAGON_SYNC_DATA_DIR` or `DRAGON_SYNC_DB_PATH` to use another local
location, which is useful for isolated development and smoke tests.

## First chat

1. Run `dragon-sync`.
2. Type `/start` and press Enter.
3. Choose an installed agent with the arrow keys.
4. Send a message.
5. Type `/switch` whenever you want to move to another agent.

When switching back to an agent, Dragon Sync resumes that agent's original session and sends only the conversation that happened while it was away.

```text
Claude  ── work ──► OpenCode  ── work ──► Claude
   ▲                                           │
   └──── receives only OpenCode's new work ────┘
```

## Terminal controls

| Input | Action |
| --- | --- |
| `/start` | Create a new chat and choose an installed agent. |
| `/resume` | Open a previous Dragon Sync chat. |
| `/switch` | Change the active agent in the current chat. |
| `/import` | Scan local supported-tool histories and import a session. |
| `/` | Show merged Dragon Sync and active-agent command suggestions. |
| `//command` | Force `/command` to the active agent if its name collides with a Dragon Sync command. |
| `↑` / `↓` | Move through command or picker suggestions. |
| `Tab` | Complete the highlighted command. |
| `Enter` | Send a message, queue one while work is running, or confirm a selection. |
| `Esc`, `Esc` | Cancel the active agent run. |
| `Ctrl+]` | Leave Agent Control and return to the unified chat. |
| `Ctrl+C` / `Ctrl+Q` | Exit Dragon Sync. |

## Agent support

| Agent | Turn runtime | Native commands | Session switching | Imported history |
| --- | --- | --- | --- | --- |
| Claude Code | Structured CLI + background PTY | Embedded native terminal | Yes | Yes |
| Codex | Structured CLI + background PTY | Embedded native terminal | Yes | Yes |
| OpenCode | Persistent ACP | ACP-advertised commands | Yes | Yes |
| Gemini | Persistent ACP | ACP-advertised commands | Yes | Yes |
| Cursor | Import only | — | — | Yes, when detected |

OpenCode and Gemini run as long-lived ACP subprocesses and report command,
configuration, permission, usage, and message events to Dragon Sync. Claude and
Codex retain their real resumable TUI in a PTY-backed background tab; commands
that open native menus switch the conversation panel into Agent Control.

PTY-backed Agent Control currently targets Linux and macOS terminals.

## How background sessions work

Each `(Dragon chat, agent)` pair owns an independent runtime:

```text
Dragon chat
├── Claude session    idle / working / waiting / crashed
├── Codex session     idle / working / waiting / crashed
└── OpenCode session  idle / working / waiting / crashed
```

Switching changes the active tab without discarding its native session ID.
Dragon Sync serializes workspace turns in the single-chat MVP, preventing two
agents from editing the same working tree simultaneously. Messages entered
while a turn is running are stored in SQLite and processed in order.

If a process exits unexpectedly, its tab is marked as crashed. The next turn
restarts or resumes it from the persisted native session where the provider
supports resumption.

## Development

Run the full test suite:

```bash
GOCACHE=/tmp/dragon-sync-gocache GOFLAGS=-mod=mod go test ./...
```

Build both executable names:

```bash
GOCACHE=/tmp/dragon-sync-gocache GOFLAGS=-mod=mod go build -o /tmp/dragon-sync ./cmd/dragon-sync
GOCACHE=/tmp/dragon-sync-gocache GOFLAGS=-mod=mod go build -o /tmp/sync ./cmd/sync
```

## Project shape

```text
cmd/dragon-sync      main CLI entry point
cmd/sync             short CLI alias
internal/tui         minimal terminal interface
internal/chat        canonical ledger, durable queue, runtime manager, switching
internal/agenthost   ACP and PTY-backed local agent-process hosts
internal/db          local SQLite storage and migrations
internal/sync        read-only history import adapters
```

## Current MVP boundary

Dragon Sync is local-first and intentionally keeps the conversation ledger on
your machine. Multi-thread workspaces and cloud sync remain outside this MVP;
the current implementation focuses on one dependable, switchable local chat
with persistent queues and direct access to each supported agent's commands.

---

Built for people who use more than one coding agent and do not want to restart the conversation every time.
