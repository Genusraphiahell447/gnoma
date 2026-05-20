# AGENTS.md

Conventions for AI assistants working in this repository. CLAUDE.md
covers Go style, commits, and TDD policy; this file adds gnoma-specific
domain knowledge those rules do not capture.

## Domain glossary

| Term | Meaning |
|---|---|
| **Elf** | A sub-agent instance, spawned via `spawn_elfs`. |
| **Turn** | One complete `stream → tool → re-query` cycle in the engine. |
| **Arm** | A `(provider, model)` pair the router can select. Registered with cost and capability metadata. |
| **Router** | Multi-armed-bandit selector that picks an Arm per Turn from the registered set. |
| **SLM** | Small language model running locally for prompt classification and trivial-task execution. |
| **Stream Event** | Discriminated-union update emitted while a provider streams: `EventTextDelta`, `EventToolCallStart`, `EventToolResult`, etc. See `internal/stream/event.go`. |
| **SafeProvider** | The sealed boundary that gates outbound provider calls — every Provider implementation embeds the unexported marker. See `internal/security`. |
| **Incognito** | Per-turn mode that disables session persistence and router learning. |
| **Profile** | A named config overlay under `~/.config/gnoma/profiles/`. Switches keys, models, and per-profile router quality data. |

## Build & test targets (beyond standard)

| Target | Purpose |
|---|---|
| `make test-v` | Verbose unit tests |
| `make test-integration` | Runs `//go:build integration` tests (real API calls) |
| `make check` | fmt + vet + lint + test (use before committing) |
| `go test -bench=. ./internal/router/` | Router benchmarks |

## Provider env vars

| Provider | Primary | Alternative |
|---|---|---|
| Anthropic | `ANTHROPIC_API_KEY` | `ANTHROPICS_API_KEY` |
| OpenAI | `OPENAI_API_KEY` | — |
| Google | `GEMINI_API_KEY` | `GOOGLE_API_KEY` |
| Mistral | `MISTRAL_API_KEY` | — |

`GNOMA_PROVIDER` and `GNOMA_MODEL` override the resolved config.

## Non-obvious conventions

- **Discriminated unions** are structs with a `Type` field and pointer
  payloads — not Go interfaces. See `internal/stream/event.go` and
  `internal/message`.
- **Pull-based iterators** follow the `Next() / Current() / Err() / Close()`
  shape. Streams in `internal/provider/*/stream.go` are the canonical examples.
- **`json.RawMessage`** flows through `tool.Definition.Parameters` and tool
  arguments untouched — never marshal/unmarshal in the middle.
- **Capabilities and ContextWindow** come from `internal/provider`
  `inferXxxModelCapabilities` per provider; updating model lists also updates
  these tables and the `ratelimits.go` map.
- **Hook ordering** matters for `PostToolUse`. See ADR-004.
- **Plugin trust** is TOFU pinning — see `internal/plugin/pinstore.go` and
  ADR-003.

## Sub-agent (elf) etiquette

When spawning elfs:

- One `spawn_elfs` call for all parallel work; never spawn one at a time.
- Read-only tasks on disjoint files parallelize cleanly.
- Writes to the same file must be sequenced into one elf.
- Cap each batch at 5–7 elfs.

See `internal/skill/skills/batch.md` for the canonical batching template.

## Reference docs

- Architecture map: `docs/essentials/INDEX.md`
- ADRs: `docs/essentials/decisions/`
- Profiles: `docs/profiles.md`
- SLM backends: `docs/slm-backends.md`
- Plugin trust: `docs/plugins-trust.md`
- Router benchmarks: `docs/benchmarks/README.md`
