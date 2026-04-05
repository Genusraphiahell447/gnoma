---
essential: milestones
status: complete
last_updated: 2026-04-06
project: gnoma
depends_on: [vision]
---

# Milestones

## Overview

| # | Name | Core Deliverable | Deps |
|---|------|-----------------|------|
| M1 | Core Engine | Pipe mode, Mistral, tools, agentic loop | — |
| M2 | Multi-Provider | All providers, config, dynamic switching | M1 |
| M3 | Security Firewall | Request/response scanning, redaction, incognito | M2 |
| M4 | Router Foundation | Arm registry, pools, task classifier, heuristic selection | M2 |
| M5 | TUI | Bubble Tea, 6 permission modes, config screen | M3, M4 |
| M6 | Context Intelligence | Local tokenizer, fixed context prefix, full compaction | M5 |
| M7 | Elfs | Router-integrated sub-agents, parallel work | M4, M6 |
| M8 | Extensibility | Hooks, skills, MCP client, MCP tool replaceability, plugins | M7 |
| M9 | Router Advanced | Bandit core, feedback, ensemble strategies, state persistence | M7 |
| M10 | Persistence & Serve | SQLite sessions, serve mode, coordinator | M7 |
| M11 | Task Learning | Pattern recognition, task suggestions, persistent tasks | M9 |
| M12 | Thinking, Multimodality & Structured Output | Thinking, multimodal I/O, schema validation | M2 |
| M13 | Auth | OAuth PKCE, keyring, multi-account | M5 |
| M14 | Observability | Feature flags, telemetry, cost dashboards | M10 |
| M15 | Web UI | `gnoma web` CLI flag, browser UI via serve mode | M10 |

---

## M1: Core Engine (MVP)

**Scope:** First working assistant. CLI pipe mode. Mistral as reference provider. Bash + file tools (with 7 critical security checks). No TUI, no permissions, no config file.

**Deliverables:**

- [x] Architecture docs in `docs/essentials/`
- [ ] Foundation types (`internal/message/`)
- [ ] Streaming abstraction (`internal/stream/`)
- [ ] Provider interface + Mistral adapter
- [ ] Tool system: bash (with security checks), fs.read, fs.write, fs.edit, fs.glob, fs.grep
- [ ] Engine agentic loop (stream → tool → re-query → done)
- [ ] CLI pipe mode (`echo "list files" | gnoma`)
- [ ] System package inventory: detect installed tools/packages at startup, include in system prompt so the LLM knows what's available

**Exit criteria:** Pipe a coding question in, get a response that uses tools, answer on stdout.

## M2: Multi-Provider

**Scope:** All remaining providers. TOML config with layered loading. Dynamic provider switching.

**Deliverables:**

- [ ] TOML config system (defaults → user → project → env → flags)
- [ ] API key resolution from env vars and config
- [ ] Anthropic provider (streaming + tool use + thinking blocks)
- [ ] OpenAI provider (streaming + tool use)
- [ ] Google provider (streaming + function calling, goroutine bridge)
- [ ] OpenAI-compat for Ollama and llama.cpp
- [ ] `--provider` / `--model` flag switching

**Exit criteria:** `echo "hello" | gnoma --provider openai` works. All 5+ providers functional.

## M3: Security Firewall

**Scope:** Core security layer built into gnoma. Scans outgoing LLM requests and incoming tool results for sensitive data. Redacts or blocks. Incognito mode.

**Deliverables:**

- [ ] Secret scanner (gitleaks-derived, 40+ regex patterns, Shannon entropy detection)
- [ ] Unicode sanitization (NFKC + Cf/Co/Cn stripping, recursive on nested structs)
- [ ] Redactor (replace matched groups with `[REDACTED]`, preserve context)
- [ ] Configurable rules (regex patterns, action: redact/block/warn)
- [ ] Remaining bash security checks (checks 8-23 from CC bashSecurity.ts)
- [ ] Incognito mode: no persistence, no learning, no logging, optional local-only routing
- [ ] `--incognito` CLI flag

**Exit criteria:** Provider requests with embedded API keys get redacted. Incognito suppresses all persistence. Unicode attack vectors sanitized.

## M4: Router Foundation

**Scope:** Arm registry, limit pools, task classification, heuristic selection. Engine switches from direct provider calls to `router.Select()`.

**Deliverables:**

- [ ] Arm type (provider+model pair) with capability introspection
- [ ] Limit pools (RPM, RPD, tokens/day, cost caps, custom units)
- [ ] Pool tracker with optimistic reservation and scarcity multipliers
- [ ] Task classifier (10 types: Boilerplate, Generation, Refactor, Review, UnitTest, Planning, Orchestration, SecurityReview, Debug, Explain)
- [ ] Complexity scoring and value scoring
- [ ] Heuristic arm selection (score = quality × value / effective_cost)
- [ ] Background provider discovery (poll ollama, llama.cpp, API providers)
- [ ] Engine integration: `router.Select()` replaces direct provider calls

**Exit criteria:** Engine routes tasks through router. Limit pools track consumption. Task classification works for 10 types.

## M5: TUI

**Scope:** Interactive terminal UI. Full 6-mode permission system. Session management. In-app config. Incognito toggle.

**Deliverables:**

- [ ] Permission system with all 6 modes:
  - `default` — prompt for each tool invocation
  - `acceptEdits` — auto-allow file ops, prompt for bash/destructive
  - `bypass` — allow everything
  - `deny` — deny all unless explicit allow rule
  - `plan` — read-only tools only
  - `auto` — router task classification + tool risk scoring
- [ ] Permission rules with compound bash command decomposition (via `mvdan.cc/sh` AST)
- [ ] 7-step permission decision flow (deny gates → tool check → safety → mode → allow → passthrough → hooks)
- [ ] Bubble Tea TUI: chat panel, input, streaming output
- [ ] Status bar (provider, model, tokens, incognito indicator)
- [ ] Permission prompt overlay
- [ ] Model picker overlay
- [ ] In-app config editor (`/config` command)
- [ ] Incognito toggle (`/incognito` command)
- [ ] Interactive shell pane: `/shell` command or keybinding opens PTY-connected shell
  - For commands needing user input (sudo, ssh, git push with auth, passwd prompts)
  - Bash tool detects potentially interactive commands and suggests take-over
  - PTY-based execution for flagged commands
- [ ] Session management (channel-based)

**Exit criteria:** Launch TUI, chat interactively, 6 permission modes work, config editable in-app, incognito toggleable, `/shell` opens interactive terminal for password prompts.

## M6: Context Intelligence

**Scope:** Long sessions. Local tokenizer. Full compaction with both truncation and LLM summarization.

**Deliverables:**

- [ ] Local tokenizer for accurate token counting
- [ ] Token tracker with warning states (OK / Warning / Critical)
- [ ] Fixed context prefix: system prompt + loaded md files (CLAUDE.md, project docs) pinned as immutable prefix. Only conversation history after the prefix gets compacted.
- [ ] TruncateStrategy: drop oldest, preserve system + fixed prefix + recent
- [ ] SummarizeStrategy: spawn compaction elf, LLM-powered summary, image stripping, boundary messages
- [ ] Auto-compaction triggers (threshold-based, reactive on 413, circuit breaker after 3 failures)
- [ ] Pre/post compact hooks
- [ ] Tool result persistence (>50KB → disk, 2KB preview + filepath)
- [ ] Deferred tool loading (`ShouldDefer()`, full schema on demand)
- [ ] Post-compact restoration budget (50K total, 5K/file, 25K/skill)

**Exit criteria:** 100+ turn conversation stays coherent. Summarization produces useful summaries. Token counting within 5% of provider.

## M7: Elfs (Router-Integrated)

**Scope:** Sub-agents using router for provider selection. Parallel work. Feedback to router.

**Deliverables:**

- [x] Elf interface + BackgroundElf implementation
- [x] ElfManager: spawn, monitor, cancel, collect results
- [x] Router-integrated spawning (`router.Select()` picks arm per elf)
- [x] Parent ↔ elf communication via typed channels (elf.Progress)
- [x] Concurrent tool execution (read-only parallel via WaitGroup, writes serial)
- [x] `agent` tool: single elf spawn with tree progress view
- [x] `spawn_elfs` tool: batch N elfs in one call, all run in parallel
- [x] CC-style tree view: ├─/└─ branches, tool uses, tokens, activity, Done(duration)
- [x] Elf output truncated to 2000 chars for parent context protection
- [x] Elf results feed back to router as quality signals
- [x] Coordinator mode: orchestrator dispatches to worker elfs

**Exit criteria:** Parent spawns 3 elfs via `spawn_elfs`, all run in parallel (chosen by router), tree shows live progress, results synthesized.

## M8: Extensibility

**Scope:** Hooks, skills, MCP client with tool replaceability, plugin system.

**Deliverables:**

- [ ] Hook system: PreToolUse, PostToolUse, SessionStart/End, PreCompact, Stop
- [ ] Hook protocol: stdin JSON, stdout JSON, exit codes (0=allow, 2=deny)
- [ ] Hook command types: command (shell), prompt (LLM), agent (spawn elf)
- [ ] Skill loading from .gnoma/skills/, ~/.config/gnoma/skills/, bundled, plugins
- [ ] Skill frontmatter: YAML (name, description, whenToUse, allowedTools, paths)
- [ ] MCP client: JSON-RPC over stdio, tool discovery
- [ ] MCP tool naming: `mcp__{server}__{tool}`
- [ ] MCP tool replaceability: `replace_default` config swaps built-in tools
- [ ] Plugin system: plugin.json manifest, install/enable/disable lifecycle
- [ ] `/batch` skill: decompose work into N units, spawn all via `spawn_elfs`, track progress (CC-inspired)
- [ ] Coordinator mode prompt: fan-out guidance for parallel elf dispatch, concurrency rules (read vs write)

**Exit criteria:** MCP tools appear in gnoma. `replace_default` swaps built-ins. Skills invocable. Hooks fire on tool use. `/batch` decomposes and parallelizes work.

## M9: Router Advanced

**Scope:** Full bandit learning. Feedback collection. Ensemble execution strategies. State persistence.

**Deliverables:**

- [ ] Discounted Thompson Sampling (per-arm, per-task-type Beta distributions)
- [ ] Feedback collection: implicit (acceptance, edit distance, escalation) + explicit
- [ ] Delayed attribution for orchestration/planning tasks
- [ ] Execution strategies: SingleArm, CascadeWithReview, ParallelEnsemble, MultiRoundSynthesis
- [ ] Strategy selection as learned routing decision
- [ ] Background arm benchmarking (TTFT, tok/s)
- [ ] State persistence (gob, versioned schema, atomic writes, CRC32)
- [ ] Cold start: shipped default.state with embedded priors
- [ ] Heuristic fallback for <5 observations per arm-task pair

**Exit criteria:** Bandit converges after ~50 observations. Ensemble outperforms single-arm on complex tasks. State persists across restarts.

## M10: Persistence & Serve

**Scope:** SQLite session persistence. Serve mode. Coordinator mode.

**Deliverables:**

- [ ] SQLite session storage (messages, parentUuid chain, tombstones)
- [ ] Session memory: background elf extracts notes from conversation
- [ ] Incognito enforcement: sessions NOT persisted
- [ ] Serve mode: Unix socket listener, spawn session goroutine per client
- [ ] Coordinator mode: orchestrator dispatches to restricted worker elfs
- [ ] Task framework: registered tasks with lifecycle (pending/running/completed/failed), abort controllers (CC-inspired AppState.tasks)
- [ ] Task notification system: completed background elfs inject `<task-notification>` messages into parent conversation (CC-inspired)
- [ ] StreamingToolExecutor: concurrent-safe tool classification, sibling abort on failure (CC-inspired)
- [ ] Git worktree isolation: `isolation: "worktree"` gives each elf a separate working copy (CC-inspired)

**Exit criteria:** Resume yesterday's conversation. External client connects via serve mode. Task notifications flow from background elfs to parent.

## M11: Task Learning

**Scope:** Detect recurring task patterns. Suggest persistent tasks. Refinement loop.

**Deliverables:**

- [ ] Pattern detector: observe turn sequences, identify repeats (≥3 times)
- [ ] Task suggestion UX: prompt user to save as persistent task
- [ ] Persistent task definitions: parameterized sequences, stored in .gnoma/tasks/ or ~/.config/gnoma/tasks/
- [ ] `/task <name> [args]` execution command
- [ ] Router feedback integration: learn which arm works best per task step
- [ ] Task refinement: re-split tasks, measure improvement

**Exit criteria:** gnoma suggests a persistent task after 3+ repetitions. `/task release v1.2.0` executes a saved workflow.

## M12: Thinking, Structured Output, Notebook & Multimodality

**Deliverables:**

- [ ] Thinking mode (disabled / enabled with budget / adaptive)
- [ ] Thinking block streaming and TUI display
- [ ] Structured output with JSON schema validation
- [ ] Retry logic for schema validation failures
- [ ] NotebookEdit tool: read/write/edit Jupyter notebook cells (.ipynb)
- [ ] Multimodal input: image support (Anthropic image blocks, OpenAI content parts, Google inline data)
- [ ] Multimodal input: audio support (where provider supports it)
- [ ] Multimodal output: image rendering in TUI (sixel/kitty protocol)

## M13: Auth

**Deliverables:**

- [ ] OAuth 2.0 + PKCE flow (browser → callback → token exchange)
- [ ] Proactive token refresh (before expiry)
- [ ] OS keyring integration for credential storage
- [ ] Multi-account support per provider

## M14: Observability

**Deliverables:**

- [ ] Feature flag system (local config + optional remote)
- [ ] Opt-in analytics (event queue, local-only by default)
- [ ] Usage dashboards (token spend, provider usage, tool frequency)
- [ ] Cost tracking per provider/model

## M15: Web UI

**Deliverables:**

- [ ] `gnoma web` CLI subcommand starts local web server
- [ ] Connects to serve mode backend (M10 prerequisite)
- [ ] Chat interface with streaming, tool output, permission prompts

## Future

- Voice input/output via provider audio APIs
- Collaborative sessions (multiple humans + elfs)
- Plugin marketplace
- Remote agent execution
- Federated learning for router priors (opt-in, anonymized)

## Changelog

- 2026-04-02: Initial version (M1-M11)
- 2026-04-03: Restructured to M1-M15. Split providers/TUI. Added Security (M3), Router Foundation (M4), Router Advanced (M9), Task Learning (M11). Full 6 permission modes. Full compaction. CC pattern integration.
