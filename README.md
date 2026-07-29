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
- Queues messages while an agent is working and supports cancellation with `Esc`.
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
| `/` | Show Dragon Sync command suggestions. |
| `↑` / `↓` | Move through command or picker suggestions. |
| `Tab` | Complete the highlighted Dragon Sync command. |
| `Enter` | Send a message, queue one while work is running, or confirm a selection. |
| `Esc` | Cancel the active agent run. |
| `Ctrl+C` / `Ctrl+Q` | Exit Dragon Sync. |

## Agent support

| Agent | Local chat | Session switching | Imported history |
| --- | --- | --- | --- |
| Claude Code | Yes | Yes | Yes |
| Codex | Yes | Yes | Yes |
| OpenCode | Yes | Yes | Yes |
| Gemini | Yes | Yes | Yes |
| Cursor | Import only | — | Yes, when detected |

OpenCode and Gemini are being moved to persistent ACP-backed background sessions. Claude and Codex currently use their native persisted-session command paths while their background adapters are completed.

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
internal/chat        canonical ledger, switching, queueing, metrics
internal/agenthost   persistent local agent-process host
internal/db          local SQLite storage and migrations
internal/sync        read-only history import adapters
```

## Current MVP boundary

Dragon Sync is local-first and intentionally keeps the conversation ledger on your machine. Multi-thread workspaces, cloud sync, and complete provider-native command/control surfaces are the next layers; the current focus is making one dependable, switchable local chat work well.

---

Built for people who use more than one coding agent and do not want to restart the conversation every time.
