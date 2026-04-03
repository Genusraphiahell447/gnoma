---
essential: tech-stack
status: complete
last_updated: 2026-04-02
project: gnoma
depends_on: []
---

# Tech Stack & Conventions

## Languages

| Language | Version | Role |
|----------|---------|------|
| Go | 1.26 | Primary — all application code |

## Frameworks & Libraries

| Library | Module | Purpose |
|---------|--------|---------|
| Mistral SDK | `github.com/VikingOwl91/mistral-go-sdk` | Mistral API client (user-maintained) |
| Anthropic SDK | `github.com/anthropics/anthropic-sdk-go` | Anthropic API client |
| OpenAI SDK | `github.com/openai/openai-go` | OpenAI API client (+ compat endpoints) |
| Google GenAI | `google.golang.org/genai` | Google Gemini API client |
| TOML | `github.com/BurntSushi/toml` | Configuration file parsing |
| Bubble Tea | `github.com/charmbracelet/bubbletea/v2` | Terminal UI framework |
| Lip Gloss | `github.com/charmbracelet/lipgloss` | Terminal styling |
| Bubbles | `github.com/charmbracelet/bubbles` | TUI components (input, viewport) |
| Doublestar | `github.com/bmatcuk/doublestar/v4` | Glob with `**` support |

## Go 1.26 Features Used

| Feature | Where |
|---------|-------|
| `new(expr)` | Optional pointer fields in config/params |
| `errors.AsType[E](err)` | Provider error handling |
| `sync.WaitGroup.Go(f)` | Goroutine management |
| `slog.NewMultiHandler()` | Fan-out logging |
| `testing/synctest` | Concurrent test support |
| Green Tea GC (default) | No action needed — 10-40% less GC overhead |
| `io.ReadAll` 2x faster | File tool reads |

## Tooling

- **Build:** `go build` via Makefile
- **CI/CD:** none yet (planned)
- **Linting:** `golangci-lint`
- **Testing:** stdlib `testing`, `testing/synctest`
- **Package management:** Go modules

## Conventions

### Naming

- Files: lowercase, underscores for multi-word (`tool_result.go`)
- Packages: short, lowercase, no underscores (`provider`, `stream`)
- Functions/methods: camelCase (`NewUserText`, `HasToolCalls`)
- Types/structs: PascalCase (`ToolCall`, `ProviderError`)
- Constants: PascalCase for exported (`StopEndTurn`), camelCase for unexported
- Interfaces: describe behavior (`Provider`, `Stream`, `Tool`), not implementation

### Error Handling

- Explicit error types with `%w` wrapping
- `errors.AsType[E]` for type-safe extraction (Go 1.26)
- `Err` prefix for sentinel errors (`ErrDenied`)
- `*Error` suffix for error types (`ProviderError`)
- Fail fast — never swallow errors
- Include context in error messages

### File Organization

- By layer within `internal/`: `message/`, `stream/`, `provider/`, `tool/`, `engine/`, `session/`
- Provider adapters: one directory per provider under `internal/provider/`
- Tool implementations: one directory per tool type under `internal/tool/`
- Three files per provider adapter: `provider.go`, `translate.go`, `stream.go`

### Commit Style

- Conventional commits: `feat:`, `fix:`, `refactor:`, `test:`, `docs:`, `chore:`
- No co-signing

## Changelog

- 2026-04-02: Initial version
