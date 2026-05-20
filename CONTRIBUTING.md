# Contributing to gnoma

The upstream repository lives at
<https://somegit.dev/Owlibou/gnoma> and is mirrored to
<https://github.com/VikingOwl91/gnoma>. PRs are accepted on the upstream
(Gitea) instance; the GitHub mirror is read-only.

## Setup

```sh
git clone https://somegit.dev/Owlibou/gnoma && cd gnoma
make build   # requires Go 1.26+
make test
make lint    # requires golangci-lint
```

## Development workflow

1. Branch from `main`.
2. Write tests first (TDD). Table-driven where possible, `t.TempDir()` for
   filesystem tests, `testing/synctest` for concurrent ones.
3. `make check` (fmt + vet + lint + test) must pass.
4. Conventional commits: `feat:`, `fix:`, `refactor:`, `test:`, `docs:`,
   `chore:`. **No co-signing or "Generated-by" trailers.**

## Code style

- Go 1.26 idioms (`new(expr)`, `errors.AsType[E]`, `sync.WaitGroup.Go`).
- Structured logging with `log/slog`.
- `json.RawMessage` for tool schemas (zero-cost passthrough).
- Functional options for complex configuration.
- Short, lowercase package names — no underscores.
- Discriminated unions via struct + type discriminant, not interfaces.
- Pull-based stream iterators: `Next() / Current() / Err() / Close()`.

## Testing

| Command | What it runs |
|---|---|
| `make test` | unit tests |
| `make test-integration` | tests behind `//go:build integration` — requires real API keys |
| `make cover` | coverage → `coverage.html` |
| `make lint` | `golangci-lint run ./...` |
| `make check` | fmt + vet + lint + test |
| `go test -bench=. ./internal/router/` | router benchmarks |

Integration tests are skipped by default.

## Architecture

Read [`docs/essentials/INDEX.md`](docs/essentials/INDEX.md) before changing
architectural boundaries. Key packages:

| Package | Purpose |
|---|---|
| `internal/engine` | Agentic loop (stream → tool → re-query) |
| `internal/router` | Multi-armed bandit arm selection |
| `internal/provider` | LLM provider adapters |
| `internal/tool` | Tool interface + registry |
| `internal/mcp` | MCP client (JSON-RPC over stdio) |
| `internal/plugin` | Plugin manifest, loader, manager |
| `internal/elf` | Sub-agent (elf) system |
| `internal/security` | SafeProvider boundary, firewall, output scanner |
| `internal/skill` | Skill registry and templating |
| `internal/slm` | Small-language-model classifier + arm |
| `internal/tui` | Bubble Tea v2 terminal UI |

ADRs live in [`docs/essentials/decisions/`](docs/essentials/decisions/).

## Reporting issues

File issues on the upstream Gitea instance with:

- A short reproduction (commands, prompts, configs that triggered the bug).
- Expected vs. actual behavior.
- `gnoma --version` output and OS / architecture.
- Provider and model in use, if relevant.
- `--verbose` log output if it sheds light.

## License

By contributing you agree your work is licensed under the
[Apache License 2.0](LICENSE).
