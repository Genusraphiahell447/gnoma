---
essential: architecture
status: complete
last_updated: 2026-04-02
project: gnoma
depends_on: [domain-model]
---

# Architecture

## System Context

```mermaid
graph TB
    User([Developer]) -->|TUI / CLI pipe| gnoma[gnoma binary]
    gnoma -->|HTTPS| Anthropic[Anthropic API]
    gnoma -->|HTTPS| OpenAI[OpenAI API]
    gnoma -->|HTTPS| Google[Google GenAI API]
    gnoma -->|HTTPS| Mistral[Mistral API]
    gnoma -->|HTTP| Local[Ollama / llama.cpp]
    gnoma -->|stdio JSON-RPC| MCP[MCP Servers]
    gnoma -->|exec| Tools[Local Tools<br/>bash, file ops]
```

## Container View

```mermaid
graph TB
    subgraph "gnoma (single binary, single process)"
        CLI[CLI Parser] --> Router{Mode?}
        Router -->|TTY| TUI[TUI — Bubble Tea]
        Router -->|Pipe| Pipe[CLI Pipe Mode]

        TUI --> SM[Session Manager]
        Pipe --> SM

        SM --> S1[Session goroutine]
        SM --> SN[Session N goroutine]

        S1 --> E1[Engine]
        SN --> EN[Engine N]

        E1 --> PR[Provider Registry]
        EN --> PR

        PR --> Anthropic[Anthropic adapter]
        PR --> OpenAI[OpenAI adapter]
        PR --> Google[Google adapter]
        PR --> Mistral[Mistral adapter]
        PR --> OAICompat[OpenAI-compat adapter]

        E1 --> TR[Tool Registry]
        EN --> TR

        TR --> Bash[bash]
        TR --> FS[fs.read / write / edit / glob / grep]

        E1 --> PM[Permission Checker]
        EN --> PM

        E1 --> CTX[Context Window]
        EN --> CTX
    end

    subgraph "Config Stack"
        Defaults --> Global["~/.config/gnoma/config.toml"]
        Global --> Project[".gnoma/config.toml"]
        Project --> Env[Environment Variables]
        Env --> Flags[CLI Flags]
    end
```

## Component Overview

| Component | Responsibility | Technology | Boundary |
|-----------|---------------|------------|----------|
| `cmd/gnoma` | Binary entrypoint, flag parsing, mode routing | Go stdlib | Internal |
| `internal/message` | Foundation types: Message, Content, Usage, Response | Pure Go, zero deps | Internal |
| `internal/stream` | Streaming interface, Event types, Accumulator | Depends on message | Internal |
| `internal/provider` | Provider interface, Registry, error taxonomy | Depends on message, stream | Internal |
| `internal/provider/{anthropic,openai,google,mistral}` | SDK adapters: translate + stream | SDK dependencies | Network boundary |
| `internal/provider/openaicompat` | Thin wrapper for Ollama/llama.cpp | Reuses openai adapter | Network boundary |
| `internal/tool` | Tool interface, Registry, bash, file ops | Go stdlib, doublestar | Local system boundary |
| `internal/permission` | Permission modes, rule matching, user prompts | Pure Go | Internal |
| `internal/context` | Token tracking, compaction strategies, sliding window | Depends on message, provider | Internal |
| `internal/config` | TOML layered config loading | BurntSushi/toml | Internal |
| `internal/auth` | API key resolution from env/config | Pure Go | Internal |
| `internal/engine` | Agentic query loop, tool execution orchestration | Depends on all above | Internal |
| `internal/session` | Session lifecycle, channel-based UI decoupling | Depends on engine, stream | Internal |
| `internal/tui` | Terminal UI: chat, input, status, permission dialogs | Bubble Tea, lipgloss | Internal |

## Package Dependency Graph

```mermaid
graph BT
    message["message"]
    stream["stream"]
    provider["provider"]
    tool["tool"]
    permission["permission"]
    context_mgr["context"]
    config["config"]
    auth["auth"]
    engine["engine"]
    session["session"]
    tui["tui"]
    cmd["cmd/gnoma"]

    stream --> message
    provider --> message
    provider --> stream
    tool --> message
    permission --> message
    context_mgr --> message
    context_mgr --> provider
    config --> permission
    engine --> provider
    engine --> tool
    engine --> permission
    engine --> stream
    engine --> context_mgr
    session --> engine
    session --> stream
    tui --> session
    tui --> stream
    cmd --> tui
    cmd --> config
    cmd --> auth
    cmd --> session
    cmd --> provider
    cmd --> tool
```

## Scope

**In scope:**
- Streaming chat with tool execution across 5+ LLM providers
- Agentic loop (stream → tool calls → re-query → until done)
- Permission system for tool execution
- TUI and CLI pipe modes
- TOML configuration with layering
- Context management and compaction
- Multi-agent (elfs) with per-elf provider routing
- Hook, skill, and MCP extensibility

**Out of scope:**
- Web UI (future, via serve mode)
- Cloud hosting / SaaS deployment
- Training or fine-tuning models
- IDE extension authoring (gnoma provides the backend, not the extension itself)

## Deployment

Single statically-linked Go binary. No runtime dependencies. Runs on Linux, macOS, Windows — anywhere Go compiles. Distributed via `go install`, release binaries, or package managers.

## Changelog

- 2026-04-02: Initial version
