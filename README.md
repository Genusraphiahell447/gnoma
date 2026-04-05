# gnoma

Provider-agnostic agentic coding assistant in Go.
Named after the northern pygmy-owl (*Glaucidium gnoma*). Agents are called **elfs** (elf owl).

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
