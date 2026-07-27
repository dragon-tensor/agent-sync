# Agent Sync MVP reset: research, risks, and build plan

Status: planning only. No MVP implementation has started.

## 1. New product definition

The product is a local-first workspace for carrying useful AI context across
different tools and running several AI conversations at the same time.

The MVP has exactly three primary capabilities:

1. **Universal context manager** — retain imported and newly-created chat
   history, session/chat/tool context, project context, user instructions, and
   reusable skills in one searchable store.
2. **Single portable chat** — let a user change the active provider/model/tool
   between turns while preserving the app-owned context and visible history.
3. **Multi-thread chat** — let multiple provider runs or provider combinations
   operate beside each other, with clear thread boundaries and comparison.

“No context loss” must mean that the app owns the canonical conversation and
rebuilds a provider request when the user switches tools. It cannot mean that
OpenAI, Anthropic, Google, or a local agent can all resume the same hidden
provider session.

## 2. Repository reset performed

### Kept as reusable foundation

- SQLite database and local configuration.
- Normalized provider/session/message types.
- Existing history import adapters for Claude Code, OpenCode, Cursor, ChatGPT,
  Claude Web, Gemini, Copilot, Codex, and generic JSONL/Markdown.
- Context entry storage, search, merge, snapshots, and the existing
  rule-based extraction/conflict work as possible building blocks.
- Provider registry and deduplication/time-parsing fixes already present in the
  working tree.

### Removed as out of scope for the new MVP

- Old CLI entrypoint and browse-only Bubble Tea TUI.
- Knowledge graph implementation.
- Agent groups, including their manager, model, and schema table.
- Daemon and filesystem-watching mode.
- Plugin and WASM-stub system.
- MCP server, REST API, embedded web UI, and duplicate web source.
- Release packaging, Homebrew/AUR files, Goreleaser, Makefile, and CI/release
  workflows.
- Old PRD, workflow, migration handoff, and README documents.

No database file, exported chat, or other user data was deleted. The current
repository intentionally has no application entrypoint while the MVP surface
and runtime boundary are being decided.

## 3. Important research findings

Provider APIs expose different state models and different event formats:

- OpenAI’s Responses API supports `previous_response_id` and provider-managed
  conversations, but its documentation says previous instructions are not
  carried over automatically. The app must therefore resend its stable
  instructions/context when switching or changing behavior.
  [OpenAI conversation state](https://platform.openai.com/docs/guides/conversation-state)
  · [OpenAI Responses quickstart](https://platform.openai.com/docs/quickstart/make-your-first-api-request)
- Gemini’s current Interactions API supports chaining with
  `previous_interaction_id` and can store conversation state server-side. That
  state is useful as a provider optimization, but is not portable to another
  provider.
  [Gemini Interactions](https://ai.google.dev/gemini-api/docs/interactions-overview)
- Anthropic’s Messages API represents tool use as provider-specific content
  blocks and expects the client to handle client-side tool execution and send
  tool results back in the required structure.
  [Claude Messages API](https://docs.anthropic.com/en/api/messages)
  · [Claude tool definitions](https://platform.claude.com/docs/en/agents-and-tools/tool-use/define-tools)
- Gemini function calling also requires the application to execute custom
  functions and return matching results; Gemini 3 reasoning/tool flows can
  require preserving opaque thought signatures exactly. This is a strong
  reason to preserve raw provider events in addition to normalized messages.
  [Gemini function calling](https://ai.google.dev/gemini-api/docs/function-calling)
  · [Gemini thought signatures](https://ai.google.dev/gemini-api/docs/generate-content/thought-signatures)
- MCP is useful as a future tool-connection protocol, but the MVP needs an MCP
  **client/bridge** only if users must attach external MCP tools. The removed
  MCP server was for exposing this app’s data to other agents and is not a
  prerequisite for the core portable-chat loop.
  [MCP tools specification](https://modelcontextprotocol.io/specification/2025-06-18/server/tools)

## 4. Core architecture recommendation

Use an app-owned event and context layer between the UI and provider
connectors:

```text
UI
  -> Chat/session service
      -> context compiler
      -> run coordinator
          -> provider connector (OpenAI / Anthropic / local / ...)
          -> tool executor (later, with approval)
      -> event store
  -> SQLite + encrypted secret storage
```

The canonical hierarchy should be:

```text
Workspace/project
  -> Chat (user-visible conversation)
      -> Thread (branch or parallel line of work)
          -> Turn (one user request and one or more provider runs)
              -> Events/messages/tool calls/tool results
```

Context should have explicit scopes and precedence:

```text
global user context
  < workspace/project context
  < chat context
  < thread context
  < turn-only instructions
```

Each context item needs at least: stable ID, scope, content, type, source,
enabled state, priority, created/updated timestamps, and optional skill/tool
metadata. The compiler decides what is included for a particular provider
request and records that decision for debugging.

Each provider connector should translate the canonical request into its own
request format and return:

- normalized text/content blocks;
- tool calls and tool results;
- usage and finish state;
- provider request/response IDs;
- raw request/response/event payloads where policy permits;
- a capability declaration for streaming, images, tools, reasoning, and
  provider-managed state.

## 5. Build challenges and risks

### P0: decisions that control the whole build

**What counts as a tool?** It may mean a model provider, an app such as
Claude Code or Cursor, a built-in capability such as web search, a user
function, or an MCP server. These are different integration types. The MVP
should define a provider as the thing that generates a response and a tool as
an executable capability exposed to a provider.

**Live chat versus imported history.** The retained adapters read local files
and exports. They do not provide live API calls. They should stay in an
`import` boundary and must not be mistaken for the provider connector needed
by portable chat.

**Canonical conversation model.** A simple role/content table cannot fully
represent reasoning, multimodal parts, citations, tool calls, tool results,
parallel calls, provider metadata, retries, or branches. The model must be
event-oriented while still exposing a simple message view to the UI.

**Context contract.** “All skills and chat and etc.” needs a deterministic
definition. Without explicit scopes, users will see either missing context or
unbounded prompts containing unrelated private material.

**Switch semantics.** Decide whether switching occurs only between completed
turns in the MVP. Mid-stream switching requires cancellation, partial-output
semantics, provider replay, and possible duplicate side effects; it should be
out of the first release.

### P0: provider and context engineering

- Translate canonical roles and content parts into provider-specific schemas.
- Handle different system/developer instruction rules and tool-call formats.
- Preserve provider IDs without making them the source of truth.
- Rebuild context after every switch, retry, or resume.
- Track token limits, input/output usage, cached tokens, and cost estimates per
  run.
- Compact long chats without silently changing facts, decisions, or skills.
- Distinguish durable context from ordinary conversation text.
- Prevent duplicate assistant/tool events when a stream reconnects.
- Preserve raw provider events for replay and bug reports without exposing
  hidden reasoning or sensitive payloads in the UI.

### P0: reliability and concurrency

- Model run states: queued, running, waiting-for-tool, completed, failed,
  cancelled, and interrupted.
- Make writes idempotent so retries do not duplicate turns or tool effects.
- Run several threads concurrently without cross-thread context leakage.
- Decide whether threads share live context, use immutable snapshots, or merge
  approved changes. Shared mutable context is the most dangerous default.
- Handle cancellation while a provider is streaming or a tool is executing.
- Recover after process termination and resume an interrupted run safely.
- Maintain ordering when parallel provider responses arrive out of order.
- Apply per-provider rate limits, exponential backoff, timeout, and quota
  handling.
- Keep SQLite writes serialized and ensure readers can continue during active
  runs.

### P0: security, privacy, and user trust

- Do not store API keys in the SQLite database or normal config JSON. Use
  environment variables for the first local MVP, then OS keychain/secret
  storage.
- Show which provider receives which context item before a request is sent.
- Make data retention explicit. Provider-managed conversation state may store
  data outside the local app; the app must offer a stateless/replay mode where
  supported and document the difference.
- Never execute arbitrary model-requested tools without an approval boundary.
- Separate read-only tools from side-effecting tools and record approvals.
- Treat imported chat content as untrusted prompt data; defend against prompt
  injection in old history and tool output.
- Provide deletion/export controls for local raw events, attachments, and
  derived context.

### P1: tool and skill system

- Normalize tools to name, description, JSON Schema, provider visibility,
  permissions, timeout, and version.
- Avoid name collisions across providers and MCP servers.
- Translate schemas where providers impose different limits or naming rules.
- Handle parallel and sequential calls, malformed arguments, tool errors,
  partial results, and maximum tool-loop depth.
- Decide whether a “skill” is static instructions, a tool bundle, executable
  code, or all three. For the MVP, make it an explicit versioned instruction
  bundle; defer arbitrary skill code execution.
- Keep tool context small. Provider docs explicitly warn that tool schemas and
  tool results consume context/tokens.

### P1: UX and product behavior

- Make the active provider/model obvious on every turn.
- Let the user compare responses without confusing a comparison run with the
  canonical chat thread.
- Show context included, omitted, summarized, or rejected due to limits.
- Support retry-as-new-thread so a failed or alternative response never
  corrupts the original line of work.
- Render streaming text, tool activity, errors, usage, and cancellation.
- Decide whether a multi-tool “combo” means fan-out comparison, sequential
  delegation, or one model using several executable tools. These require
  different orchestration rules.

### P1: history and migration

- Source formats change and some have branching trees, incomplete timestamps,
  attachments, or tool events that cannot be losslessly normalized.
- Use stable source IDs plus content hashes for idempotent imports.
- Preserve the original payload and adapter metadata when normalization loses
  detail.
- Separate imported historical sessions from new portable chats so a malformed
  export cannot affect live runs.
- Add fixture tests for every retained adapter before depending on it.

### P2: performance and operations

- Long context compilation can dominate latency and cost even when provider
  inference is fast.
- Search must remain responsive with large histories; FTS is useful, but
  semantic retrieval should not be a hidden MVP dependency.
- Multi-thread fan-out multiplies latency, token cost, rate-limit pressure, and
  local storage volume.
- Add structured run logs, correlation IDs, provider timings, token usage, and
  safe error details before debugging becomes guesswork.
- Provider models and limits change frequently; capabilities must be data-driven
  rather than hardcoded in UI logic.

## 6. Recommended MVP boundary

The safest first release is:

- one local app process;
- one user and one local workspace;
- direct API connectors for two providers first, recommended OpenAI and
  Anthropic;
- completed-turn provider switching;
- text context plus a small, explicit skill/instruction bundle;
- streaming text responses;
- multiple independent threads and side-by-side comparison;
- local SQLite event history;
- environment-variable API keys;
- imported history as a separate read/search path;
- no arbitrary code execution, no MCP server, no background daemon, no cloud
  sync, no team sharing, no knowledge graph, and no automatic semantic memory.

The first implementation should prove this sequence:

1. Create a chat and send a turn to provider A.
2. Switch to provider B and send a follow-up that requires facts from turn 1.
3. Start two threads from the same context snapshot and run them concurrently.
4. Compare them, continue one thread, and keep the other unchanged.
5. Kill/restart the app and recover the canonical history and run states.

## 7. Proposed implementation phases after approval

### Phase 0 — product contract

- Define provider, tool, skill, chat, thread, turn, context item, and combo.
- Decide the first UI surface. A local web UI is recommended for streaming and
  multi-pane comparison; the removed TUI can be recreated later if required.
- Write provider capability and data-retention policy.
- Define acceptance fixtures and the exact meaning of “context preserved.”

### Phase 1 — canonical storage and context compiler

- Replace the old session-centric live model with chat/thread/turn/run/event
  records while keeping import records compatible.
- Add context scopes, explicit snapshots, provenance, priority, and compiler
  receipts.
- Add event replay, idempotency keys, and migration tests.

### Phase 2 — one provider end to end

- Implement streaming, cancellation, retries, usage capture, and restart
  recovery for one direct provider.
- Build the minimal chat UI and prove the single-thread loop.

### Phase 3 — provider switching and second provider

- Implement canonical-to-provider translation for the second provider.
- Add context-loss test cases, provider-specific capabilities, and fallback
  behavior.

### Phase 4 — multi-thread orchestration

- Add immutable thread snapshots, concurrent runs, comparison UI, cancellation,
  and safe retry-as-new-thread behavior.

### Phase 5 — tools and skills

- Add versioned instruction bundles first.
- Add read-only tools with approval and a strict execution boundary.
- Add MCP client support only if the first user workflow requires it.

## 8. Definition of done for the MVP

- A user can switch between the two supported providers between completed
  turns without manually copying context.
- The next provider receives the selected stable context and the relevant
  canonical chat history, subject to an explicit context-budget receipt.
- Two or more threads can run at the same time and remain isolated.
- A comparison result is stored and can be continued as a chosen branch.
- Streaming, cancellation, retry, provider failure, and process restart do not
  lose or duplicate canonical events.
- API secrets are not written to SQLite or normal logs.
- Imported history is searchable and cannot mutate live chat state implicitly.
- Adapter fixtures, context compilation, provider translation, concurrency,
  and recovery tests pass.

## 9. Current next step

Do not implement features yet. First approve or revise the product contract in
Section 6, especially:

- the first two live providers;
- whether “tool” means provider, executable capability, or both;
- whether the first UI should be local web or terminal;
- whether combo means comparison fan-out or sequential orchestration;
- whether context is strictly local or may use provider-managed state.
