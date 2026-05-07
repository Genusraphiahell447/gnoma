# ADR-001: Single Binary with Goroutines

**Status:** Accepted
**Date:** 2026-04-02

## Context

gnoma needs to decouple the UI from the engine to support multiple frontends (TUI, CLI, future IDE extensions). Options were: (a) single binary with goroutines + channels, (b) client-server with gRPC IPC, (c) embedded library.

## Decision

Single Go binary. Engine runs as goroutines within the same process. UI communicates with engine via the `Session` interface over channels. Future serve mode adds a Unix socket listener for external clients — still the same process.

## Alternatives Considered

### Alternative A: gRPC IPC (gnoma + gnomad)

- **Pros:** Process isolation, true sandboxing, multiple clients to one daemon
- **Cons:** Protobuf codegen dependency, daemon lifecycle management, two binaries to distribute

### Alternative B: Embedded library

- **Pros:** Maximum flexibility for embedders
- **Cons:** No standalone binary, API stability burden, harder to ship

## Consequences

**Positive:** Simple deployment, no daemon, no codegen, Go's goroutine model provides sufficient isolation.
**Negative:** No process-level sandboxing for tools. Multi-client scenarios require serve mode (future).

---

# ADR-002: Pull-Based Stream Interface

**Status:** Accepted
**Date:** 2026-04-02

## Context

Need a unified streaming abstraction across 4 SDKs with different patterns: Anthropic/OpenAI/Mistral use pull-based `Next()/Current()`, Google uses range-based `for chunk, err := range iter`.

## Decision

Pull-based `Stream` interface: `Next() bool`, `Current() Event`, `Err() error`, `Close() error`. Google adapter bridges via goroutine + buffered channel.

## Alternatives Considered

### Alternative A: Channel-based

- **Pros:** Go-idiomatic, works with `select`
- **Cons:** Requires goroutine per stream, less control over backpressure, no `Close()` for cleanup

### Alternative B: iter.Seq (range-over-func)

- **Pros:** Modern Go pattern, clean `for event := range stream`
- **Cons:** No `Close()` for resource cleanup, no separate error retrieval, doesn't match SDK patterns

## Consequences

**Positive:** Zero-overhead adapter for 3 of 4 SDKs. Explicit resource cleanup. Consumer controls pace.
**Negative:** Google needs a goroutine bridge. Slightly more verbose than range-based.

---

# ADR-003: Mistral as M1 Reference Provider

**Status:** Accepted
**Date:** 2026-04-02

## Context

Need to pick one provider to implement first as the reference adapter. Candidates: Anthropic (richest model), OpenAI (most popular), Mistral (user maintains SDK).

## Decision

Mistral first. The user maintains `github.com/VikingOwl91/mistral-go-sdk` and knows its internals. The API shape is similar to OpenAI, making it a good baseline. Anthropic's unique features (thinking blocks, cache tokens) are better tested as M2 extensions.

## Alternatives Considered

### Alternative A: Anthropic first

- **Pros:** Richest content model, most features to test
- **Cons:** Anthropic-specific features (thinking, caching) could bias the abstraction

### Alternative B: OpenAI first

- **Pros:** Most widely used, well-documented
- **Cons:** No special insight into SDK internals

## Consequences

**Positive:** Fast iteration on reference adapter. SDK bugs fixed directly.
**Negative:** Thinking block support tested later (M2).

---

# ADR-004: Discriminated Union Structs

**Status:** Accepted
**Date:** 2026-04-02

## Context

Go lacks sum types. Need to represent Content variants (text, tool call, tool result, thinking) and Event variants.

## Decision

Struct with `Type` discriminant field. Exactly one payload field is set per type value. Consumer switches on `Type`.

## Alternatives Considered

### Alternative A: Interface hierarchy

- **Pros:** Extensible, familiar OOP pattern
- **Cons:** Heap allocation per variant, type assertion overhead, no exhaustive switch checking

### Alternative B: Generics-based enum

- **Pros:** Type-safe, compile-time checked
- **Cons:** Complex, unfamiliar, Go's generics don't support sum types well

## Consequences

**Positive:** Zero allocation, cache-friendly, fast switch dispatch, simple.
**Negative:** New variants require updating all switch statements. Acceptable for small, stable sets.

---

# ADR-005: json.RawMessage for Tool Schemas

**Status:** Accepted
**Date:** 2026-04-02

## Context

Tool parameters (JSON Schema) and tool call arguments need to flow between providers and tools. Options: typed schema library, code generation, or raw JSON passthrough.

## Decision

`json.RawMessage` for both tool parameter schemas and tool call arguments. Zero-cost passthrough between provider and tool. Tools parse their own arguments.

## Alternatives Considered

### Alternative A: JSON Schema library

- **Pros:** Centralized validation, type-safe schema construction
- **Cons:** Core dependency, serialization overhead, schema library selection lock-in

### Alternative B: Code generation from schemas

- **Pros:** Full type safety, compile-time checks
- **Cons:** Build complexity, generated code maintenance, rigid

## Consequences

**Positive:** No JSON Schema dependency. Providers and tools speak JSON natively. Minimal overhead.
**Negative:** Validation at tool boundary only, not centralized.

---

# ADR-006: Multi-Provider Collaboration as Core Identity

**Status:** Accepted
**Date:** 2026-04-02

## Context

Most AI coding assistants are single-provider. gnoma already supports multiple providers, but the question is whether multi-provider collaboration (elfs on different providers working together) is a nice-to-have or a core architectural feature.

## Decision

Multi-provider collaboration is a core feature and part of gnoma's identity. The architecture must support elfs running on different providers simultaneously, with routing rules directing tasks by capability, cost, or latency. This is not an afterthought — it shapes how we design the elf system, provider registry, and session management.

## Alternatives Considered

### Alternative A: Multi-provider as optional extension

- **Pros:** Simpler MVP, routing added later
- **Cons:** Architectural decisions made without routing in mind may need rework

## Consequences

**Positive:** Clear differentiator from all existing tools. Shapes architecture from day one.
**Negative:** Elf system design must account for per-elf provider config from the start.

---

# ADR-007: Security Firewall as Core (Not Plugin)

**Status:** Accepted
**Date:** 2026-04-03

## Context

gnoma needs to prevent secrets and sensitive data from leaking to LLM providers. Options: build it as an MCP server (plugin), or bake it into the core.

## Decision

Security firewall is a core component (`internal/security/`), not a plugin. It wraps all provider calls and tool results. Everyone benefits by default.

## Alternatives Considered

### Alternative A: MCP-based security server

- **Pros:** Modular, replaceable, user can choose their own
- **Cons:** Users must opt-in. Default-off security is no security. MCP adds latency.

## Consequences

**Positive:** Every gnoma user gets secret scanning, unicode sanitization, and incognito mode out of the box.
**Negative:** Core binary is larger. False positives affect all users (mitigated by configurable sensitivity).

---

# ADR-008: Router Split into Foundation + Advanced

**Status:** Accepted
**Date:** 2026-04-03

## Context

The smart router is gnoma's core differentiator but is a massive system (arm registry, limit pools, task classification, bandit learning, feedback, ensemble strategies, state persistence). Building it all at once blocks other milestones.

## Decision

Split into M4 (foundation: arm registry, pools, task classifier, heuristic selection) and M9 (advanced: bandit, feedback, ensemble, persistence). M4 gives the engine a routing abstraction early. M9 adds learning after elfs provide real feedback signals.

## Alternatives Considered

### Alternative A: Full router in one milestone

- **Pros:** Complete system from day one
- **Cons:** Massive milestone, blocks TUI and other features, bandit needs elf feedback that doesn't exist yet

## Consequences

**Positive:** Engine routes from M4 onward. Heuristic selection is good enough for daily use. Bandit learning lands when feedback is available.
**Negative:** Two integration points instead of one.

---

# ADR-009: Thompson Sampling for Multi-Armed Bandit

**Status:** Superseded by ADR-013
**Date:** 2026-04-03

## Context

The router needs to learn which arm (provider+model) performs best per task type. Options: epsilon-greedy, UCB, LinUCB, Thompson Sampling.

## Decision

Discounted Thompson Sampling with per-arm, per-task-type Beta(α, β) distributions. No ML framework dependency — Beta distribution sampling via Marsaglia-Tsang Gamma (~30 lines of Go).

## Alternatives Considered

### Alternative A: LinUCB (contextual bandit)

- **Pros:** Uses full task feature vector, theoretically optimal
- **Cons:** Matrix inversion per decision, complex implementation, marginal gain at v1 scale

### Alternative B: Epsilon-greedy

- **Pros:** Simplest to implement
- **Cons:** Fixed exploration rate, doesn't adapt, wastes budget on known-bad arms

## Consequences

**Positive:** Natural exploration via sampling. Handles non-stationarity with discounting. No external deps. Fast (<1ms per decision).
**Negative:** Per-task-type, not contextual — can't generalize across task clusters. Contextual bandit (v2) planned as future upgrade.

---

# ADR-010: MCP Tool Replaceability via Priority Registry

**Status:** Accepted
**Date:** 2026-04-03

## Context

MCP servers provide tools. Some users want MCP tools to replace gnoma's built-in tools (e.g., a custom file system tool). Need a mechanism for this.

## Decision

Tool registry has a priority system. MCP servers can declare `replace_default = "fs"` in config to replace all `fs.*` built-in tools. Resolution: MCP override > built-in.

## Consequences

**Positive:** Users can swap any built-in tool via config. No code changes needed.
**Negative:** MCP tool must implement the same contract (same parameter schema). Mismatch → runtime errors.

---

# ADR-011: Task Learning as Late-Stage Feature (M11)

**Status:** Accepted
**Date:** 2026-04-03

## Context

Task learning (detecting recurring patterns, suggesting persistent tasks) could be built early or late.

## Decision

M11 — after router advanced (M9) and persistence (M10). Task learning needs: (1) router feedback signals to understand quality, (2) session persistence to observe patterns across sessions, (3) enough real usage to detect meaningful repetitions.

## Consequences

**Positive:** Built on solid foundations. Feedback signals are real, not synthetic.
**Negative:** Users don't benefit from task learning until late in the roadmap.

---

# ADR-012: Incognito Mode as Core Security Feature

**Status:** Accepted
**Date:** 2026-04-03

## Context

Users working with sensitive code need a way to prevent any data from being persisted, logged, or fed back to the learning system.

## Decision

Incognito mode is part of the security firewall (M3). When active: no session persistence, no router learning, no logging of content, optional local-only routing. Activated via `--incognito` flag or TUI toggle. Visual indicator in status bar.

## Consequences

**Positive:** Strong privacy guarantee. Users can work on sensitive projects without worrying about data leakage to disk or learning systems.
**Negative:** No learning improvement from incognito sessions. Router stays static.

## Changelog

- 2026-04-02: Initial decisions (ADR-001 through ADR-006)
- 2026-04-03: Added ADR-007 through ADR-012 (security, router split, Thompson Sampling, MCP replaceability, task learning, incognito)
