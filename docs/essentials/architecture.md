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
| `internal/security` | Firewall, secret scanner, unicode sanitizer, incognito mode | message, config | Security boundary |
| `internal/router` | Smart router: arm registry, pools, task classifier, selection | provider, message, config | Internal |
| `internal/engine` | Agentic query loop, tool execution orchestration | router, security, tool, stream, context | Internal |
| `internal/session` | Session lifecycle, channel-based UI decoupling | engine, stream | Internal |
| `internal/elf` | Sub-agent spawning, lifecycle, communication | engine, router, session | Internal |
| `internal/tui` | Terminal UI: chat, input, status, permission dialogs, config screen | session, stream, permission | Internal |
| `internal/hook` | Hook system: events, protocol, registration | message, tool | Internal |
| `internal/skill` | Skill loading, frontmatter parsing, discovery | message | Internal |
| `internal/mcp` | MCP client, tool discovery, tool replaceability | tool, config | External (stdio) |
| `internal/plugin` | Plugin manifest, loader, lifecycle | config | Internal |
| `internal/tasklearn` | Repetitive task detection, suggestions, persistent tasks | router, engine | Internal |

## Package Dependency Graph

```mermaid
graph BT
    message["message"]
    stream["stream"]
    provider["provider"]
    tool["tool"]
    permission["permission"]
    security["security"]
    router["router"]
    context_mgr["context"]
    config["config"]
    auth["auth"]
    engine["engine"]
    session["session"]
    elf["elf"]
    tui["tui"]
    hook["hook"]
    skill["skill"]
    mcp["mcp"]
    plugin["plugin"]
    tasklearn["tasklearn"]
    cmd["cmd/gnoma"]

    stream --> message
    provider --> message
    provider --> stream
    tool --> message
    permission --> message
    permission --> config
    security --> message
    security --> config
    router --> provider
    router --> message
    router --> config
    context_mgr --> message
    context_mgr --> provider
    engine --> router
    engine --> security
    engine --> tool
    engine --> permission
    engine --> stream
    engine --> context_mgr
    session --> engine
    session --> stream
    elf --> engine
    elf --> router
    elf --> session
    hook --> message
    hook --> tool
    skill --> message
    mcp --> tool
    mcp --> config
    plugin --> config
    tasklearn --> router
    tasklearn --> engine
    tui --> session
    tui --> stream
    tui --> permission
    cmd --> tui
    cmd --> config
    cmd --> auth
    cmd --> session
    cmd --> provider
    cmd --> tool
    cmd --> router
    cmd --> security
```

## Scope

**In scope:**
- Streaming chat with tool execution across 5+ LLM providers
- Agentic loop (stream → tool calls → re-query → until done)
- Security firewall with secret scanning, redaction, incognito mode
- Smart router with bandit-based multi-provider collaboration
- 6-mode permission system for tool execution
- TUI and CLI pipe modes
- TOML configuration with layering
- Context management and compaction (truncation + LLM summarization)
- Multi-agent (elfs) with router-integrated provider selection
- Hook, skill, MCP, and plugin extensibility
- Repetitive task learning and persistent tasks
- Session persistence (SQLite) and serve mode

**Out of scope:**
- Web UI (M15, via serve mode)
- Cloud hosting / SaaS deployment
- Training or fine-tuning models
- IDE extension authoring (gnoma provides the backend, not the extension itself)

## Deployment

Single statically-linked Go binary. No runtime dependencies. Runs on Linux, macOS, Windows — anywhere Go compiles. Distributed via `go install`, release binaries, or package managers.

## Changelog

- 2026-04-02: Initial version
