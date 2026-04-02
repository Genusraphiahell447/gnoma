---
essential: patterns
status: complete
last_updated: 2026-04-02
project: gnoma
depends_on: [architecture]
---

# Patterns

## Discriminated Unions

- **What:** Struct with a `Type` field discriminant; exactly one payload field is set per type value. Used instead of Go interfaces for data variants.
- **Where:** `message.Content`, `stream.Event`
- **Why:** Zero allocation (no interface boxing), cache-friendly, works with `switch` statements. Go lacks sum types — this is the pragmatic equivalent.
- **Example:**
  ```go
  type Content struct {
      Type       ContentType
      Text       string      // set when Type == ContentText
      ToolCall   *ToolCall   // set when Type == ContentToolCall
      ToolResult *ToolResult // set when Type == ContentToolResult
      Thinking   *Thinking   // set when Type == ContentThinking
  }

  switch c.Type {
  case ContentText:
      fmt.Print(c.Text)
  case ContentToolCall:
      execute(c.ToolCall)
  }
  ```

## Pull-Based Stream Iterator

- **What:** `Next() / Current() / Err() / Close()` interface for consuming streaming data.
- **Where:** `stream.Stream` interface, all provider adapters
- **Why:** Matches 3 of 4 SDKs (Anthropic, OpenAI, Mistral) natively. Gives consumer explicit backpressure control. Supports `Close()` for resource cleanup, unlike `iter.Seq`. Only Google needs a goroutine bridge.
- **Example:**
  ```go
  for s.Next() {
      event := s.Current()
      process(event)
  }
  if err := s.Err(); err != nil {
      handle(err)
  }
  s.Close()
  ```

## Accumulator

- **What:** Shared component that assembles a `message.Response` from a sequence of `stream.Event` values. Separated from provider-specific translation.
- **Where:** `stream.Accumulator`, used by every provider adapter
- **Why:** Provider adapters become thin translation layers. Accumulation logic (text building, tool call JSON fragment assembly, thinking blocks) is tested once, not per-provider.
- **Example:**
  ```go
  acc := stream.NewAccumulator()
  for s.Next() {
      acc.Apply(s.Current())
  }
  response := acc.Response()
  ```

## Factory Registry

- **What:** Map of names to factory functions. Creates instances on demand with config.
- **Where:** `provider.Registry`, `tool.Registry`
- **Why:** Decouples creation from usage. Makes testing easy — register mock factories. Enables dynamic provider switching.
- **Example:**
  ```go
  registry.Register("mistral", mistral.NewProvider)
  provider, err := registry.Create("mistral", cfg)
  ```

## Functional Options

- **What:** Variadic option functions for configuring complex objects.
- **Where:** Session creation, provider construction
- **Why:** Clean API for objects with many optional parameters. Self-documenting, extensible without breaking changes.
- **Example:**
  ```go
  session, err := manager.NewSession(
      WithProvider(mistral),
      WithModel("mistral-large-latest"),
      WithMaxTurns(20),
  )
  ```

## Callback Event Propagation

- **What:** The engine accepts a `Callback func(stream.Event)` and calls it for each event. The session wraps this to push events into a channel.
- **Where:** `engine.Submit()` → `session/local.go`
- **Why:** Keeps the engine testable without concurrency. The engine knows nothing about channels, TUI, or goroutines. The session implementation decides how to propagate events.
- **Example:**
  ```go
  // In session/local.go:
  cb := func(evt stream.Event) {
      select {
      case s.events <- evt:
      case <-ctx.Done():
      }
  }
  turn, err := s.engine.Submit(ctx, input, cb)
  ```

## Error Wrapping with errors.AsType

- **What:** Provider adapters wrap SDK errors into typed `ProviderError` with classification. Consumers extract using Go 1.26's `errors.AsType[E]`.
- **Where:** All provider adapters, retry logic, engine error handling
- **Why:** Enables error classification (transient vs auth vs bad request) for retry decisions. Type-safe extraction without pointer indirection.
- **Example:**
  ```go
  if pErr, ok := errors.AsType[*ProviderError](err); ok {
      if pErr.Retryable {
          // exponential backoff
      }
  }
  ```

## Anti-Patterns

Patterns explicitly avoided in this project:

| Anti-Pattern | Why we avoid it | What to do instead |
|---|---|---|
| Interface-based unions | Heap allocation, type assertion overhead, no exhaustive matching | Discriminated union structs with Type field |
| Channel-based streams | Requires goroutine management, harder to control backpressure | Pull-based iterator interface |
| Global state | Untestable, race-prone, hidden dependencies | Dependency injection via config structs |
| Shared mutable state between elfs | Race conditions, complex synchronization | Each elf owns its own engine; communicate via channels |
| Over-abstraction | Premature generalization obscures intent | Three similar lines > one premature abstraction |

## Changelog

- 2026-04-02: Initial version
