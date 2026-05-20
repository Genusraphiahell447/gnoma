# gnoma

**A provider-agnostic agentic coding assistant in Go.** gnoma routes each prompt
to the best available model — cloud or local — through a multi-armed bandit
router, executes tools on your behalf, and stays extensible through hooks,
skills, MCP servers, and plugins.

Named after the northern pygmy-owl (*Glaucidium gnoma*); agents are called
**elfs** (elf owl).

- **Upstream:** <https://somegit.dev/Owlibou/gnoma>
- **GitHub mirror:** <https://github.com/VikingOwl91/gnoma>

---

## Install

### Pre-built binary (no Go toolchain required)

Releases are built by [GoReleaser](.goreleaser.yml) for
`linux`, `darwin`, and `windows` × `amd64`/`arm64` as static (`CGO_ENABLED=0`)
archives. Until the first tag is cut, see "Build from source" below.

Once releases are published:

```sh
# Pick the archive matching your OS/arch from the releases page:
#   https://somegit.dev/Owlibou/gnoma/releases   (upstream)
#   https://github.com/VikingOwl91/gnoma/releases (mirror)

# Linux/macOS one-liner (substitute the asset URL):
curl -fsSL <ARCHIVE_URL> | tar -xz -C /tmp
sudo mv /tmp/gnoma /usr/local/bin/
gnoma --version
```

Windows: download the `_windows_*.zip`, extract `gnoma.exe`, and put it on
`%PATH%`.

### Docker

Multi-arch images (`linux/amd64`, `linux/arm64`) are published to GitHub
Container Registry on each tagged release:

```sh
docker pull ghcr.io/vikingowl91/gnoma:latest
docker run --rm -it \
  -v "$PWD:/workspace" \
  -e ANTHROPIC_API_KEY \
  ghcr.io/vikingowl91/gnoma:latest --version
```

Mount your project as `/workspace` (the image's working directory) and pass
provider keys via `-e`.

### Go users

```sh
go install somegit.dev/Owlibou/gnoma/cmd/gnoma@latest   # latest tagged
go install somegit.dev/Owlibou/gnoma/cmd/gnoma@main     # bleeding edge
```

### Build from source

```sh
git clone https://somegit.dev/Owlibou/gnoma && cd gnoma
make build       # → ./bin/gnoma
make install     # → $GOPATH/bin/gnoma
```

Requires Go 1.26+.

---

## Quickstart

```sh
# Set at least one provider key (or run a local model — see Providers below).
export ANTHROPIC_API_KEY=sk-ant-...

gnoma                              # interactive TUI
echo "list files" | gnoma          # pipe / one-shot mode
gnoma --provider ollama            # use a local model
gnoma --version
```

Inside the TUI, `Ctrl+X` toggles **incognito** (no session saved, no router
learning); `/help` lists slash commands; `Esc` cancels an in-flight turn.

---

## Providers

| Provider | Env var | Default model | Also available |
|---|---|---|---|
| Anthropic | `ANTHROPIC_API_KEY` | `claude-sonnet-4-6` | `claude-opus-4-7`, `claude-haiku-4-5-20251001` |
| OpenAI | `OPENAI_API_KEY` | `gpt-5.5` | `gpt-5.5-pro`, `gpt-5.2`, `gpt-5.2-chat-latest` |
| Google (Gemini) | `GEMINI_API_KEY` (alt: `GOOGLE_API_KEY`) | `gemini-3.5-flash` | `gemini-3.1-pro-preview`, `gemini-3.1-flash-lite` |
| Mistral | `MISTRAL_API_KEY` | `mistral-large-latest` (Mistral Large 3) | `mistral-medium-3.5`, `magistral-medium-2509` |
| Ollama (local) | — | `qwen3:8b` (override with `--model`) | any model on your Ollama instance |
| llama.cpp (local) | — | reported by `/v1/models` | n/a |
| Subprocess (`claude`, `gemini`, `agy` CLIs) | provider-specific | binary name | configurable via `[cli_agents]` |

Override per-invocation:

```sh
gnoma --provider anthropic --model claude-opus-4-7
gnoma --provider openai    --model gpt-5.5-pro     # GPT-5.5 is the default; pro is the higher-accuracy tier
gnoma --provider google    --model gemini-3.1-pro-preview
gnoma --provider ollama    --model qwen2.5-coder:3b
gnoma --provider llamacpp                          # model picked from server
```

`gnoma providers` prints every discovered provider, model, and CLI agent.

### Local models

Start your local server, then point gnoma at it:

```sh
# Ollama (default http://localhost:11434/v1)
ollama pull qwen2.5-coder:3b
gnoma --provider ollama --model qwen2.5-coder:3b

# llama.cpp (default http://localhost:8080/v1)
llama-server --model /path/to/model.gguf --port 8080 --ctx-size 8192
gnoma --provider llamacpp
```

Override the endpoint in `.gnoma/config.toml`:

```toml
[provider.endpoints]
ollama   = "http://myhost:11434/v1"
llamacpp = "http://localhost:9090/v1"
```

---

## Config

Configuration merges (lowest → highest priority):

1. Built-in defaults
2. `~/.config/gnoma/config.toml` — global base
3. `~/.config/gnoma/profiles/<name>.toml` — active profile (when profile mode is enabled)
4. `<projectRoot>/.gnoma/config.toml` — project override
5. Environment variables (`GNOMA_PROVIDER`, `GNOMA_MODEL`, `*_API_KEY`)

Example global config:

```toml
[provider]
default = "anthropic"
model   = "claude-sonnet-4-6"

[provider.api_keys]
anthropic = "${ANTHROPIC_API_KEY}"

[provider.endpoints]
ollama   = "http://localhost:11434/v1"
llamacpp = "http://localhost:8080/v1"

[permission]
mode = "auto"      # default | accept_edits | bypass | deny | plan | auto

[session]
max_keep = 20      # sessions retained per project
```

### Profiles

Drop multiple configs under `~/.config/gnoma/profiles/` and switch with
`--profile <name>` or `/profile <name>`. Each profile keeps its own router
quality data and session history. Full details: [docs/profiles.md](docs/profiles.md).

---

## SLM (small-language-model) routing

gnoma can run a tiny local model alongside the main provider to:

- **Classify** each prompt (task type + complexity + tool requirement) so the
  router picks the right arm.
- **Execute** trivial tasks itself (knowledge questions, single file reads,
  anything with complexity ≤ 0.3), keeping the heavy provider for real work.

```toml
[slm]
enabled = true
backend = "auto"           # ollama | llamacpp | llamafile | openaicompat | auto | disabled
model   = "reecdev/tiny3.5:500m"
```

Setup, presets, and verification: [docs/slm-backends.md](docs/slm-backends.md).
The `auto` backend probes Ollama → llama.cpp → llamafile on startup and picks
the first reachable option. Inspect with `gnoma slm status` and
`gnoma router stats`.

---

## Session persistence

Sessions are auto-saved per project under `.gnoma/sessions/<id>/` after each
completed turn. On a crash you lose at most the current in-flight turn.

```sh
gnoma --resume              # interactive picker
gnoma --resume <id>         # restore by ID
gnoma -r                    # shorthand
gnoma --incognito           # no save, no router learning
```

Inside the TUI: `/resume`, `/resume <id>`, `Ctrl+X` (incognito toggle).

Router-quality data (EMA scores) is stored at
`~/.config/gnoma/quality.json` (or `quality-<profile>.json` in profile mode).

---

## Extensibility

### MCP servers

Connect any [MCP](https://modelcontextprotocol.io)-compatible server:

```toml
[[mcp_servers]]
name    = "git"
command = "mcp-server-git"
args    = ["--repo", "."]
timeout = "30s"

# Optionally replace a built-in tool with an MCP one
[mcp_servers.replace_default]
exec = "bash"
```

MCP tools appear as `mcp__{server}__{tool}` unless mapped via `replace_default`.

### Skills

Drop markdown files into `.gnoma/skills/` or `~/.config/gnoma/skills/`. Invoke
with `/<skill-name>`. List with `/skills`.

### Hooks

Shell commands run on tool events (`pre_tool_use`, `post_tool_use`, etc.):

```toml
[[hooks]]
name         = "block-rm-rf"
event        = "pre_tool_use"
type         = "command"
exec         = "bash-safety-check.sh"
tool_pattern = "bash*"
```

Ordering rules: [ADR-004](docs/essentials/decisions/004-posttooluse-hook-ordering.md).

### Plugins

Plugins bundle skills, hooks, and MCP server configs. Drop a plugin directory
into `~/.config/gnoma/plugins/` (global) or `<project>/.gnoma/plugins/`
(project-local); gnoma auto-discovers them on startup.

Each plugin's `plugin.json` is pinned by SHA-256 on first load
(Trust-On-First-Use). A manifest that changes between runs is refused with a
clear error and a re-enrolment hint. Full model:
[docs/plugins-trust.md](docs/plugins-trust.md) and
[ADR-003](docs/essentials/decisions/003-plugin-trust.md).

### Elfs (sub-agents)

The `spawn_elfs` tool decomposes work into parallel sub-tasks. See
[`internal/skill/skills/batch.md`](internal/skill/skills/batch.md) for the
built-in batching skill.

---

## Subcommands

| Command | What it does |
|---|---|
| `gnoma providers` | List every discovered provider, model, and CLI agent |
| `gnoma profile list` / `show <name>` | Profile diagnostics |
| `gnoma router stats` | Quality EMA + classifier source breakdown |
| `gnoma slm setup` / `slm status` | Manage the llamafile-backed SLM |

`gnoma --help` for the full flag set.

---

## Security

gnoma runs tools and shell commands on your behalf. The
[`internal/security`](internal/security) package canonicalises every path
(TOCTOU-safe), gates network access through a configurable firewall, and
scans tool output for secrets before it ever reaches the model. The
`SafeProvider` boundary keeps incognito-mode data out of long-lived stores.

Architecture references:

- [docs/essentials/INDEX.md](docs/essentials/INDEX.md) — full architecture map
- [docs/essentials/decisions/](docs/essentials/decisions/) — ADRs 001–004

---

## Development

```sh
make build          # ./bin/gnoma
make test           # unit tests
make test-integration  # //go:build integration — requires real API keys
make cover          # coverage.html
make lint           # golangci-lint
make check          # fmt + vet + lint + test
```

Architecture, conventions, and TDD workflow: [CONTRIBUTING.md](CONTRIBUTING.md).

---

## License

Apache License 2.0. See [LICENSE](LICENSE) and [NOTICE](NOTICE).
