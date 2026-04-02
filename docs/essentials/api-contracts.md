---
essential: api-contracts
status: complete
last_updated: 2026-04-02
project: gnoma
depends_on: [architecture]
---

# API Contracts

gnoma has no external HTTP API. All interfaces are internal Go APIs between packages. The stability guarantees below define how these internal boundaries evolve.

## Core Interfaces

| Interface | Package | Stability | Description |
|-----------|---------|-----------|-------------|
| `Provider` | `provider` | stable | LLM backend adapter contract |
| `Stream` | `stream` | stable | Unified streaming event iterator |
| `Tool` | `tool` | stable | Tool execution contract |
| `Session` | `session` | stable | UI ↔ engine decoupling boundary |
| `Strategy` | `context` | experimental | Compaction strategy contract |
| `Elf` | TBD | experimental | Sub-agent contract (future) |

## Provider Interface

```go
type Provider interface {
    Stream(ctx context.Context, req Request) (stream.Stream, error)
    Name() string
}
```

**Stability:** Stable. Adding methods requires a new interface (e.g., `ProviderV2`) or optional interface assertion pattern.

## Stream Interface

```go
type Stream interface {
    Next() bool
    Current() Event
    Err() error
    Close() error
}
```

**Stability:** Stable. The pull-based iterator contract is locked.

## Tool Interface

```go
type Tool interface {
    Name() string
    Description() string
    Parameters() json.RawMessage
    Execute(ctx context.Context, args json.RawMessage) (Result, error)
    IsReadOnly() bool
}
```

**Stability:** Stable. New capabilities added via optional interfaces:

```go
// Future: tools that support streaming output
type StreamingTool interface {
    Tool
    ExecuteStream(ctx context.Context, args json.RawMessage) (stream.Stream, error)
}
```

## Session Interface

```go
type Session interface {
    Send(ctx context.Context, input string) error
    Events() <-chan stream.Event
    TurnResult() (*engine.Turn, error)
    Cancel()
    Close() error
    Status() SessionStatus
}
```

**Stability:** Stable. This is the boundary that enables future transport implementations (Unix socket, WebSocket) without changing the engine or UI.

## Event Schema

Events flow from provider → engine → session → UI. The `stream.Event` struct is the wire format:

| Event Type | Fields Set | Direction |
|-----------|-----------|-----------|
| `EventTextDelta` | `Text` | Provider → UI |
| `EventThinkingDelta` | `Text` | Provider → UI |
| `EventToolCallStart` | `ToolCallID`, `ToolCallName` | Provider → UI |
| `EventToolCallDelta` | `ToolCallID`, `ArgDelta` | Provider → UI |
| `EventToolCallDone` | `ToolCallID`, `Args` | Provider → UI |
| `EventUsage` | `Usage` | Provider → Engine |
| `EventError` | `Err` | Any → Consumer |

## Versioning Strategy

Internal packages under `internal/` have no versioning — they change freely. The `Provider`, `Stream`, `Tool`, and `Session` interfaces are considered public contracts even though they're internal. Breaking changes to these require migration notes in the changelog.

Future public API (if gnoma becomes embeddable as a library) would live under a `pkg/` directory with semantic versioning.

## Changelog

- 2026-04-02: Initial version
