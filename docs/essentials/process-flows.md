---
essential: process-flows
status: complete
last_updated: 2026-04-02
project: gnoma
depends_on: [architecture]
---

# Process Flows

## Bootstrap / Initialization

```mermaid
sequenceDiagram
    participant User
    participant Main as cmd/gnoma
    participant Cfg as config.Load()
    participant Auth as auth.KeySource
    participant PR as ProviderRegistry
    participant TR as ToolRegistry
    participant PM as PermissionChecker
    participant SM as SessionManager
    participant UI as TUI / CLI

    User->>Main: gnoma [flags]
    Main->>Cfg: Load()
    Note over Cfg: defaults → ~/.config/gnoma/config.toml<br/>→ .gnoma/config.toml → env → flags
    Cfg-->>Main: Config

    Main->>Auth: NewKeySource(config.APIKeys)
    Main->>PR: NewRegistry()

    loop each provider
        Main->>Auth: Resolve(providerName)
        Auth-->>Main: apiKey
        Main->>PR: Register(name, factory)
    end

    Main->>TR: NewRegistry()
    Main->>TR: Register(bash, fs.read, fs.write, ...)
    Main->>PM: NewChecker(mode, rules, promptFn)

    Main->>SM: NewManager(config)

    alt stdin is TTY
        Main->>UI: LaunchTUI(sessionManager)
    else stdin is pipe
        Main->>UI: RunCLI(sessionManager)
    end
```

## User Message → Response (Full Agentic Turn)

**Happy path:**

```mermaid
sequenceDiagram
    participant UI as TUI
    participant Sess as Session<br/>(goroutine)
    participant Eng as Engine
    participant Prov as Provider
    participant Acc as Accumulator
    participant TR as ToolRegistry
    participant PM as Permissions

    UI->>Sess: Send(ctx, "user input")
    Sess->>Eng: Submit(ctx, input, callback)

    Note over Eng: Append user message to history

    loop Agentic Loop (until EndTurn or MaxTurns)
        Eng->>Prov: Stream(ctx, Request)
        Prov-->>Eng: stream.Stream

        loop Stream consumption
            Eng->>Eng: stream.Next()
            Eng->>Acc: Apply(event)
            Eng->>Sess: callback(event)
            Sess-->>UI: event via channel
            UI->>UI: render delta
        end

        Eng->>Acc: Response()
        Note over Eng: Append assistant message to history

        alt StopReason == EndTurn
            Note over Eng: Done — return Turn
        else StopReason == ToolUse
            loop each ToolCall
                Eng->>PM: Check(toolName, args)
                alt Denied
                    Note over Eng: Add error ToolResult
                else Prompt needed
                    Eng->>Sess: callback(PermissionEvent)
                    Sess-->>UI: show permission dialog
                    UI-->>Sess: user decision
                    Sess-->>Eng: approved/denied
                end
                Eng->>TR: Get(toolName)
                Eng->>TR: tool.Execute(ctx, args)
                TR-->>Eng: Result
            end
            Note over Eng: Append ToolResults, continue loop
        else StopReason == MaxTokens
            Note over Eng: Return Turn with truncation warning
        end
    end

    Eng-->>Sess: Turn
    Sess-->>UI: TurnResult()
```

**Key decision points:**

- StopReason determines whether the loop continues (ToolUse), ends (EndTurn), or warns (MaxTokens)
- Permission check can block tool execution — denied tools get error results sent back to the model
- MaxTurns is a safety limit to prevent runaway loops

## Streaming Pipeline

```mermaid
graph LR
    subgraph "Provider SDK"
        SDK[SDK Stream]
    end

    subgraph "Provider Adapter"
        Adapt[translate SDK event<br/>→ stream.Event]
    end

    subgraph "Engine"
        CB[Callback func]
        ACC[Accumulator]
    end

    subgraph "Session"
        CH[Event Channel<br/>buffered 64]
    end

    subgraph "TUI"
        Render[Render delta<br/>to terminal]
    end

    SDK -->|SDK-specific type| Adapt
    Adapt -->|stream.Event| CB
    CB --> ACC
    CB --> CH
    CH --> Render
```

## Tool Execution Flow

```mermaid
sequenceDiagram
    participant Eng as Engine
    participant PM as Permissions
    participant UI as UI (via callback)
    participant Reg as ToolRegistry
    participant Tool as Tool impl

    Note over Eng: Extract []ToolCall from response

    loop each ToolCall
        Eng->>PM: Check(ctx, toolName, args)

        alt mode == Allow
            PM-->>Eng: nil (allowed)
        else mode == Prompt
            PM->>UI: PromptFunc(toolName, args)
            UI->>UI: Show permission dialog
            UI-->>PM: true/false
            alt denied
                PM-->>Eng: ErrDenied
                Note over Eng: ToolResult{IsError: true}
            end
        else mode == Deny + no allow rule
            PM-->>Eng: ErrDenied
        end

        Eng->>Reg: Get(toolName)
        alt tool not found
            Note over Eng: ToolResult{IsError: true, "unknown tool"}
        else found
            Eng->>Tool: Execute(ctx, args)
            Tool-->>Eng: Result
        end
    end

    Note over Eng: Append ToolResults to history
```

## Context Compaction Flow

```mermaid
sequenceDiagram
    participant Eng as Engine
    participant Win as Context Window
    participant Trk as Token Tracker
    participant Strat as Compaction Strategy

    Eng->>Win: Append(message)
    Win->>Trk: Add(usage)
    Trk-->>Win: State()

    alt TokensOK
        Note over Win: No action
    else TokensWarning
        Note over Win: Log warning, continue
    else TokensCritical
        Win->>Strat: Compact(messages, budget)
        alt TruncateStrategy
            Note over Strat: Keep system prompt + last N messages
        else SummarizeStrategy
            Note over Strat: LLM summarizes old messages
        end
        Strat-->>Win: compacted messages
        Win->>Trk: Reset + recount
    end
```

## Cancellation Propagation

```mermaid
sequenceDiagram
    participant UI
    participant Sess as Session goroutine
    participant Eng as Engine
    participant Prov as Provider.Stream
    participant SDK as SDK HTTP

    UI->>Sess: Cancel()
    Note over Sess: cancel context
    Sess->>Eng: ctx.Done() propagates
    Eng->>Prov: ctx.Done() propagates
    Prov->>SDK: HTTP request cancelled
    SDK-->>Prov: context.Canceled
    Prov-->>Eng: stream.Err() = context.Canceled
    Eng-->>Sess: Turn with error
    Sess->>Sess: close events channel
    Sess-->>UI: TurnResult() returns error
```

## Changelog

- 2026-04-02: Initial version
