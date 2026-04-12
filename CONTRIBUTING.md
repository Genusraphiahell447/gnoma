# Contributing to gnoma

## Setup

```sh
git clone https://somegit.dev/Owlibou/gnoma && cd gnoma
make build   # requires Go 1.26+
make test
make lint    # requires golangci-lint
```

## Development workflow

1. Create a branch from `main`
2. Write tests first (TDD) — table-driven, `t.TempDir()` for filesystem tests
3. `make check` (fmt + vet + lint + test) must pass
4. Commit with conventional messages: `feat:`, `fix:`, `refactor:`, `test:`, `docs:`

## Code style

- Go 1.26 idioms (`new(expr)`, `errors.AsType[E]`)
- Structured logging with `log/slog`
- `json.RawMessage` for tool schemas (zero-cost passthrough)
- Functional options for complex configuration
- Short, lowercase package names — no underscores

## Testing

- Unit tests: `make test`
- Integration tests (require API keys): `make test-integration`
- Coverage: `make cover`
- Benchmarks: `go test -bench=. ./internal/router/`

Integration tests use `//go:build integration` and are skipped by default.

## Architecture

Read `docs/essentials/INDEX.md` before making architectural changes. Key packages:

| Package | Purpose |
|---------|---------|
| `internal/engine` | Agentic loop (stream → tool → re-query) |
| `internal/router` | Multi-armed bandit arm selection |
| `internal/provider` | LLM provider adapters |
| `internal/tool` | Tool interface + registry |
| `internal/mcp` | MCP client (JSON-RPC over stdio) |
| `internal/plugin` | Plugin manifest, loader, manager |
| `internal/elf` | Sub-agent (elf) system |
| `internal/tui` | Bubble Tea terminal UI |

## Issues

Use the issue templates when filing bugs or requesting features. Include reproduction steps, expected behavior, and gnoma version (`gnoma --version`).
