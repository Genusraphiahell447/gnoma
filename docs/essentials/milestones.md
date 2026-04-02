---
essential: milestones
status: complete
last_updated: 2026-04-02
project: gnoma
depends_on: [vision]
---

# Milestones

## M1: Core Engine (MVP)

**Scope:** First working assistant. CLI pipe mode. Mistral as reference provider. Bash + file tools. No TUI, no permissions, no config file.

**Deliverables:**

- [ ] Architecture docs in `docs/essentials/`
- [ ] Foundation types (`internal/message/`)
- [ ] Streaming abstraction (`internal/stream/`)
- [ ] Provider interface + Mistral adapter
- [ ] Tool system: bash, fs.read, fs.write, fs.edit, fs.glob, fs.grep
- [ ] Engine agentic loop (stream → tool → re-query → done)
- [ ] CLI pipe mode (`echo "list files" | gnoma`)

**Exit criteria:** Pipe a coding question in, get a response that uses tools, answer on stdout.

## M2: Multi-Provider

**Scope:** All remaining providers. Config file. Dynamic provider switching.

**Deliverables:**

- [ ] Anthropic provider (streaming + tool use + thinking blocks)
- [ ] OpenAI provider (streaming + tool use)
- [ ] Google provider (streaming + function calling)
- [ ] OpenAI-compat for Ollama and llama.cpp
- [ ] TOML config (global + project + env + flags)
- [ ] `/model provider/model` switching mid-session

**Exit criteria:** Chat with any configured provider via CLI pipe. Switch providers mid-session.

## M3: TUI

**Scope:** Interactive terminal UI. Permission system.

**Deliverables:**

- [ ] Bubble Tea TUI: chat panel, input box, streaming output
- [ ] Status bar (provider, model, token usage)
- [ ] Permission system (allow / deny / prompt modes)
- [ ] Permission dialog overlay
- [ ] Model picker overlay
- [ ] Input history (up/down)

**Exit criteria:** Launch TUI, chat interactively, tools execute with permission prompts.

## M4: Context Intelligence

**Scope:** Long sessions. Token tracking. Compaction. Local tokenizer.

**Deliverables:**

- [ ] Local tokenizer for accurate token counting without provider round-trips
- [ ] Token tracker (cumulative usage, OK/warning/critical states)
- [ ] Truncate compaction (drop old messages, keep system + recent)
- [ ] Summarize compaction (LLM summarizes dropped messages)
- [ ] Compact boundaries (transaction markers for crash recovery)
- [ ] Deferred tool loading (non-essential tools loaded on demand)
- [ ] Result persistence (large tool outputs written to disk)

**Exit criteria:** 100+ turn conversation stays coherent within token budget. Local token counting matches provider reports within 5%.

## M5: Elfs (Multi-Agent + Multi-Provider Routing)

**Scope:** Sub-agents on different providers. Parallel work. Provider routing.

**Deliverables:**

- [ ] Elf spawning (`Engine.SpawnElf` with per-elf provider config)
- [ ] Background elfs (independent goroutine + engine)
- [ ] Parent ↔ elf communication via typed channels
- [ ] Concurrent tool execution (read-only parallel, writes sequential)
- [ ] Provider routing rules (route by capability, cost, latency) — research needed
- [ ] Coordinator dispatches tasks to elfs on different providers

**Exit criteria:** Coordinator on Claude spawns research elf on local Qwen + review elf on OpenAI, collects and synthesizes results.

## M6: Extensibility

**Scope:** Hooks, skills, MCP, plugin foundation.

**Deliverables:**

- [ ] Hook system (PreToolUse / PostToolUse, stdin/stdout protocol)
- [ ] Skill loading (`.gnoma/skills/*.md` with frontmatter)
- [ ] MCP client (JSON-RPC over stdio, tool discovery)
- [ ] Plugin foundation (manifest, install, lifecycle)

**Exit criteria:** MCP server tools appear in gnoma. Skills invocable by model. Hook logs all bash commands.

## M7: Persistence & Serve

**Scope:** Session persistence via SQLite. Serve mode for external clients. Coordinator mode.

**Deliverables:**

- [ ] Session persistence with SQLite (save/restore conversations across restarts)
- [ ] Serve mode (Unix socket listener, external UI clients)
- [ ] Coordinator mode (orchestrator dispatches to worker elfs)

**Exit criteria:** Resume yesterday's conversation. VS Code extension connects via serve mode. Coordinator parallelizes subtasks.

## M8: Thinking & Structured Output

**Scope:** Extended thinking support across providers. Schema-validated structured output.

**Deliverables:**

- [ ] Thinking mode (disabled / enabled with budget / adaptive)
- [ ] Thinking block streaming and display in TUI
- [ ] Structured output with JSON schema validation
- [ ] Retry logic for schema validation failures

**Exit criteria:** Extended thinking with budget works on Anthropic. Structured output validates against schema on all providers that support it.

## M9: Auth

**Scope:** OAuth 2.0 + PKCE for cloud providers. Credential management.

**Deliverables:**

- [ ] OAuth 2.0 + PKCE flow (browser redirect → callback → token exchange)
- [ ] Token refresh (proactive, before expiry)
- [ ] OS keyring integration for secure credential storage
- [ ] Multi-account support per provider

**Exit criteria:** `gnoma login anthropic` opens browser, completes OAuth flow, stores token in keyring. Automatic refresh works.

## M10: Observability

**Scope:** Feature flags. Opt-in telemetry and analytics.

**Deliverables:**

- [ ] Feature flag system (local config + optional remote evaluation)
- [ ] Opt-in analytics (event queue, local-only by default)
- [ ] Usage dashboards (token spend, provider usage, tool frequency)
- [ ] Cost tracking per provider/model

**Exit criteria:** Feature flags gate experimental features. User can view their token spend breakdown. Analytics disabled by default.

## M11: Web UI

**Scope:** Browser-based UI as alternative to TUI. Requires serve mode (M7).

**Deliverables:**

- [ ] `gnoma web` CLI subcommand (or `gnoma --web`) starts local web server
- [ ] Web UI connects to serve mode backend
- [ ] Chat interface with streaming, tool output, permission prompts
- [ ] Responsive design for desktop browsers

**Exit criteria:** `gnoma web` opens browser, full chat with streaming and tool execution. Serve mode required as prerequisite.

## Future

Ideas not yet committed:

- Voice input/output via provider audio APIs
- Collaborative sessions (multiple humans + elfs)
- Plugin marketplace
- Remote agent execution

## Changelog

- 2026-04-02: Initial version — M1-M6
- 2026-04-02: Split M2 into providers (M2) and TUI (M3). Added M8-M11 for thinking, auth, observability, web UI. Local tokenizer in M4. SQLite for session persistence in M7.
