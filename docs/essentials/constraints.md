---
essential: constraints
status: complete
last_updated: 2026-04-02
project: gnoma
depends_on: [domain-model]
---

# Constraints & Trade-offs

## Non-Functional Requirements

| Constraint | Target | Measurement |
|-----------|--------|-------------|
| First token latency | Dominated by provider, not gnoma overhead | Time from Submit() to first EventTextDelta |
| Binary size | < 20 MB (static, no CGO) | `ls -lh bin/gnoma` |
| Memory per session | < 50 MB baseline (excluding context window) | `runtime.MemStats` |
| Startup time | < 200ms to TUI ready | Wall clock from exec to first render |
| Provider support | 5+ providers from M2 | Count of passing provider integration tests |
| Context window | Up to 200k tokens managed | Token tracker reports |

## Trade-offs

### Single binary over daemon architecture

- **Chose:** Single Go binary, goroutines + channels for all communication
- **Over:** Client-server split with gRPC IPC (gnoma + gnomad)
- **Because:** Simpler deployment, no daemon lifecycle, no protobuf codegen. Go's goroutine model provides sufficient isolation.
- **Consequence:** True process isolation for tool sandboxing requires future work. Multi-client scenarios (IDE + TUI) need serve mode added later.

### Pull-based stream over channels or iter.Seq

- **Chose:** `Next() / Current() / Err() / Close()` interface
- **Over:** Channel-based streaming or Go 1.23+ `iter.Seq` range functions
- **Because:** Matches 3 of 4 SDKs natively (zero-overhead adapter). Supports explicit resource cleanup via `Close()`. Consumer controls backpressure.
- **Consequence:** Google's range-based SDK needs a goroutine bridge. Slightly more verbose than range-based iteration.

### json.RawMessage passthrough over typed schemas

- **Chose:** Tool parameters and arguments as `json.RawMessage`
- **Over:** Typed JSON Schema library or code-generated types
- **Because:** Zero-cost passthrough — no serialize/deserialize between provider and tool. No JSON Schema library as a core dependency.
- **Consequence:** Schema validation happens at tool boundaries, not centrally. Type safety relies on tool implementations parsing their own args.

### Sequential tool execution (MVP) over parallel

- **Chose:** Execute tools one at a time in the agentic loop
- **Over:** Parallel execution via errgroup with read/write partitioning
- **Because:** Simpler to test, debug, and implement permission prompts. Parallel execution adds complexity around error collection and ordering.
- **Consequence:** Multiple tool calls in a single turn execute sequentially. Performance impact is minimal for most workloads. Parallel execution planned for post-MVP.

### Discriminated union structs over interface hierarchies

- **Chose:** Struct with Type discriminant field for Content and Event types
- **Over:** Interface-based variant types (e.g., `TextContent`, `ToolCallContent` implementing `Content`)
- **Because:** Zero allocation, cache-friendly, works with switch exhaustiveness. Go interfaces for data variants incur boxing overhead.
- **Consequence:** Adding a new content type requires updating switch statements. Acceptable for a small, stable set of variants.

### Mistral as M1 reference provider over Anthropic

- **Chose:** Implement Mistral adapter first as the reference
- **Over:** Starting with Anthropic (richest content model)
- **Because:** User maintains the Mistral Go SDK, knows its internals. Good baseline — similar to OpenAI's API shape. Anthropic's unique features (thinking blocks, cache tokens) are better added as an M2 extension.
- **Consequence:** Thinking block support tested later. Cache token tracking added with Anthropic provider.

## Changelog

- 2026-04-02: Initial version
