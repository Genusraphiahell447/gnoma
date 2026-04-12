# gnoma

**A provider-agnostic agentic coding assistant built in Go.** gnoma routes tasks to the best available LLM — cloud or local — through a multi-armed bandit router, while tools, hooks, skills, MCP servers, and plugins keep it extensible. Named after the northern pygmy-owl (*Glaucidium gnoma*); agents are called **elfs** (elf owl).

## Quickstart

```sh
# Install
go install somegit.dev/Owlibou/gnoma/cmd/gnoma@latest

# Or build from source
git clone https://somegit.dev/Owlibou/gnoma && cd gnoma
make build    # binary at ./bin/gnoma

# Set at least one provider key
export ANTHROPIC_API_KEY=sk-ant-...   # or OPENAI_API_KEY, MISTRAL_API_KEY, GEMINI_API_KEY

# Run
gnoma                                 # interactive TUI
echo "list files" | gnoma             # pipe mode
gnoma --provider ollama               # use a local model
```

## Build

```sh
make build          # ./bin/gnoma
make install        # $GOPATH/bin/gnoma
```

## Providers

### Anthropic

```sh
export ANTHROPIC_API_KEY=sk-ant-...
./bin/gnoma --provider anthropic
./bin/gnoma --provider anthropic --model claude-opus-4-5-20251001
```

Integration tests hit the real API — keep a key in env:

```sh
go test -tags integration ./internal/provider/...
```

---

### OpenAI

```sh
export OPENAI_API_KEY=sk-proj-...
./bin/gnoma --provider openai
./bin/gnoma --provider openai --model gpt-4o
```

---

### Mistral

```sh
export MISTRAL_API_KEY=...
./bin/gnoma --provider mistral
```

---

### Google (Gemini)

```sh
export GEMINI_API_KEY=AIza...
./bin/gnoma --provider google
./bin/gnoma --provider google --model gemini-2.0-flash
```

---

### Ollama (local)

Start Ollama and pull a model, then:

```sh
./bin/gnoma --provider ollama --model gemma4:latest
./bin/gnoma --provider ollama --model qwen3:8b     # default if --model omitted
```

Default endpoint: `http://localhost:11434/v1`. Override via config or env:

```sh
# .gnoma/config.toml
[provider]
default = "ollama"
model   = "gemma4:latest"

[provider.endpoints]
ollama = "http://myhost:11434/v1"
```

---

### llama.cpp (local)

Start the llama.cpp server:

```sh
llama-server --model /path/to/model.gguf --port 8080 --ctx-size 8192
```

Then:

```sh
./bin/gnoma --provider llamacpp
# model name is taken from the server's /v1/models response
```

Default endpoint: `http://localhost:8080/v1`. Override:

```sh
[provider.endpoints]
llamacpp = "http://localhost:9090/v1"
```

---

## Extensibility (M8)

gnoma supports hooks, skills, MCP servers, and plugins.

### MCP Servers

Connect any [MCP](https://modelcontextprotocol.io)-compatible tool server:

```toml
[[mcp_servers]]
name    = "git"
command = "mcp-server-git"
args    = ["--repo", "."]
timeout = "30s"

# Replace a built-in tool with an MCP tool
[mcp_servers.replace_default]
exec = "bash"   # MCP tool "exec" replaces gnoma's built-in "bash"
```

MCP tools appear as `mcp__{server}__{tool}` (e.g., `mcp__git__status`), or under the built-in name when using `replace_default`.

### Skills

Drop markdown files into `.gnoma/skills/` or `~/.config/gnoma/skills/`:

```
/skillname          # invoke a skill
/skills             # list available skills
```

### Hooks

Run shell commands on tool events:

```toml
[[hooks]]
name         = "block-rm-rf"
event        = "pre_tool_use"
type         = "command"
exec         = "bash-safety-check.sh"
tool_pattern = "bash*"
```

### Plugins

Bundle skills, hooks, and MCP configs into installable plugins:

```sh
gnoma plugin install ./my-plugin    # install from directory
gnoma plugin list                   # list installed plugins
```

---

## Session Persistence

Conversations are auto-saved to `.gnoma/sessions/` after each completed turn. On a crash you lose at most the current in-flight turn; all previously completed turns are safe.

### Resume a session

```sh
gnoma --resume              # interactive session picker (↑↓ navigate, Enter load, Esc cancel)
gnoma --resume <id>         # restore directly by ID
gnoma -r                    # shorthand
```

Inside the TUI:

```
/resume                     # open picker
/resume <id>                # restore by ID
```

### Incognito mode

```sh
gnoma --incognito           # no session saved, no quality scores updated
```

Toggle at runtime with `Ctrl+X`.

### Config

```toml
[session]
max_keep = 20   # how many sessions to retain per project (default: 20)
```

Sessions are stored per-project under `.gnoma/sessions/<id>/`. Quality scores (EMA routing data) are stored globally at `~/.config/gnoma/quality.json`.

---

## Config

Config is read in priority order:

1. `~/.config/gnoma/config.toml` — global
2. `.gnoma/config.toml` — project-local (next to `go.mod` / `.git`)
3. Environment variables

Example `.gnoma/config.toml`:

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
mode = "auto"   # auto | accept_edits | bypass | deny | plan
```

Environment variable overrides: `GNOMA_PROVIDER`, `GNOMA_MODEL`.

---

## Testing

```sh
make test               # unit tests
make test-integration   # integration tests (require real API keys)
make cover              # coverage report → coverage.html
make lint               # golangci-lint
make check              # fmt + vet + lint + test
```

Integration tests are gated behind `//go:build integration` and skipped by default.
