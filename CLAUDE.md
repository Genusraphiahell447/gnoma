# Gnoma — Project Instructions

## What is this?
Provider-agnostic agentic coding assistant in Go 1.26.
Named after the northern pygmy-owl (Glaucidium gnoma).
Agents are called "elfs" (elf owl).

## Module
`somegit.dev/Owlibou/gnoma`

## Build & Test
```sh
make build     # build binary to ./bin/gnoma
make test      # run all tests
make lint      # run golangci-lint
make cover     # test with coverage report
```

## Project Essentials
Project architecture, domain model, and design decisions: `docs/essentials/INDEX.md`
Read INDEX.md before making architectural changes or adding new system boundaries.

## Conventions

### Go Style
- Go 1.26 idioms: `new(expr)`, `errors.AsType[E]`, `sync.WaitGroup.Go`
- Structured logging with `log/slog`
- Discriminated unions via struct + type discriminant (not interfaces)
- Pull-based stream iterators: `Next() / Current() / Err() / Close()`
- `json.RawMessage` for tool schemas and arguments (zero-cost passthrough)
- Functional options for complex configuration
- `errgroup` for parallel work

### Testing
- TDD: write tests first
- Table-driven tests
- Build tag `//go:build integration` for tests hitting real APIs
- `testing/synctest` for concurrent tests
- `t.TempDir()` for file system tests

### Naming
- Packages: short, lowercase, no underscores
- Interfaces: describe behavior, not implementation
- Errors: `Err` prefix for sentinel errors, `*Error` suffix for error types

### Commits
- Conventional commits (feat:, fix:, refactor:, test:, docs:, chore:)
- No co-signing

### Providers
- Mistral: `github.com/VikingOwl91/mistral-go-sdk` (user's own SDK)
- Anthropic: `github.com/anthropics/anthropic-sdk-go`
- OpenAI: `github.com/openai/openai-go`
- Google: `google.golang.org/genai`
- Ollama/llama.cpp: via OpenAI SDK with custom base URL
