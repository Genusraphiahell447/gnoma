---
essential: uml-diagrams
status: complete
last_updated: 2026-04-02
project: gnoma
depends_on: [architecture, process-flows]
---

# UML Diagrams

Go doesn't have class hierarchies. These diagrams show struct relationships, interface implementations, and state machines.

## State Diagram: Engine Turn

```mermaid
stateDiagram-v2
    [*] --> Idle

    Idle --> Streaming: Submit(input)
    Streaming --> Accumulating: stream exhausted
    Accumulating --> ToolExec: StopReason == ToolUse
    Accumulating --> Complete: StopReason == EndTurn
    Accumulating --> Complete: StopReason == MaxTokens
    ToolExec --> Streaming: tools executed, continue loop
    ToolExec --> PermissionWait: tool needs approval

    PermissionWait --> ToolExec: user approves
    PermissionWait --> ToolExec: user denies (error result)

    Streaming --> Cancelled: ctx.Done()
    ToolExec --> Cancelled: ctx.Done()
    PermissionWait --> Cancelled: ctx.Done()

    Complete --> Idle: turn returned
    Cancelled --> Idle: turn returned with error
    Streaming --> Error: provider error
    ToolExec --> Error: fatal tool error
    Error --> Idle: error returned
```

## State Diagram: Session Lifecycle

```mermaid
stateDiagram-v2
    [*] --> Idle

    Idle --> Active: Send(input)
    Active --> Idle: Turn complete (events channel closed)
    Active --> Cancelling: Cancel()
    Cancelling --> Idle: cancellation propagated

    Idle --> Closed: Close()
    Active --> Closed: Close() (cancels first)
    Closed --> [*]
```

## State Diagram: Stream

```mermaid
stateDiagram-v2
    [*] --> Open

    Open --> HasEvent: Next() returns true
    HasEvent --> Open: caller reads Current()
    Open --> Exhausted: Next() returns false, Err()==nil
    Open --> Errored: Next() returns false, Err()!=nil

    Exhausted --> [*]: Close()
    Errored --> [*]: Close()
```

## Component Diagram: Provider Adapter Stack

```mermaid
graph TD
    subgraph "Provider Interface"
        PI[provider.Provider]
        PS[stream.Stream]
    end

    subgraph "Adapters"
        MA[mistral adapter]
        AA[anthropic adapter]
        OA[openai adapter]
        GA[google adapter]
        OC[openaicompat adapter]
    end

    subgraph "SDKs"
        MS[mistral-go-sdk]
        AS[anthropic-sdk-go]
        OS[openai-go]
        GS[genai-go]
    end

    PI --> MA
    PI --> AA
    PI --> OA
    PI --> GA
    PI --> OC

    MA --> MS
    AA --> AS
    OA --> OS
    GA --> GS
    OC --> OS

    MA --> PS
    AA --> PS
    OA --> PS
    GA --> PS
    OC --> PS
```

## Component Diagram: Streaming Event Translation

```mermaid
graph TB
    subgraph "Anthropic SDK"
        A1[ContentBlockDeltaEvent TextDelta] -->|→| E2[EventTextDelta]
        A2[ContentBlockDeltaEvent InputJSONDelta] -->|→| E3[EventToolCallDelta]
        A3[ContentBlockDeltaEvent ThinkingDelta] -->|→| E4[EventThinkingDelta]
        A4[ContentBlockStartEvent tool_use] -->|→| E1[EventToolCallStart]
        A5[ContentBlockStopEvent] -->|→| E5[EventToolCallDone]
    end

    subgraph "OpenAI SDK"
        O1[Chunk.Delta.Content] -->|→| E2
        O2[Chunk.Delta.ToolCalls start] -->|→| E1
        O3[Chunk.Delta.ToolCalls delta] -->|→| E3
        O4[Chunk.FinishReason=tool_calls] -->|→| E5
    end

    subgraph "Google SDK"
        G1[Response.Part.Text] -->|→| E2
        G2[Response.Part.FunctionCall] -->|→| E1
        G3[same FunctionCall] -->|→| E5
    end

    subgraph "Mistral SDK"
        M1[Chunk.Delta.Content] -->|→| E2
        M2[Chunk ToolCalls start] -->|→| E1
        M3[Chunk ToolCalls delta] -->|→| E3
        M4[Chunk.FinishReason=tool_calls] -->|→| E5
    end
```

## Struct Relationships: Elf System (Future)

```mermaid
classDiagram
    class Elf {
        <<interface>>
        +ID() string
        +Status() ElfStatus
        +Send(msg) error
        +Events() chan Event
        +Cancel()
        +Wait() ElfResult
    }

    class SyncElf {
        -engine Engine
        -parentCtx context.Context
        runs on parent goroutine
    }

    class BackgroundElf {
        -engine Engine
        -goroutine
        -events chan Event
        runs independently
    }

    class ElfManager {
        -elfs map~string, Elf~
        +Spawn(config) Elf
        +Get(id) Elf
        +List() []Elf
        +CancelAll()
    }

    Elf <|.. SyncElf
    Elf <|.. BackgroundElf
    ElfManager --> Elf
```

## Changelog

- 2026-04-02: Initial version
