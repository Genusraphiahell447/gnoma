# SLM Backends

The small-language-model (SLM) layer has two jobs:

- **Classify** every prompt into a `TaskType` + complexity score, feeding the router's arm selection.
- **Execute** trivial tasks itself — anything with complexity ≤ 0.3 and no tool use — so the heavy provider arms only see real work.

Gnoma supports several backends for the SLM role. Pick the one that matches what you already run; you don't need to install anything new for most setups.

Copy a preset into `~/.config/gnoma/config.toml` (or the project-local `.gnoma/config.toml`) and adjust the model name to one you have available.

## Choosing a backend

| Backend | Cold start | External daemon | Setup | Good for |
|---|---|---|---|---|
| `ollama` | none (already running) | Ollama daemon | `ollama pull <model>` once | Most local-model users |
| `llamacpp` | none (already running) | `llama-server` | manual server launch | llama.cpp users |
| `llamafile` | 15–30 s on first prompt | none — gnoma manages the process | `gnoma slm setup` | Zero-dependency single-binary setups |
| `openaicompat` | none | user-managed | point at any OpenAI-compatible URL | LM Studio, vLLM, remote API, etc. |
| `auto` | depends on what's reachable | depends | none | Lazy default — gnoma probes and picks |
| `disabled` | n/a | n/a | n/a | Skip the SLM entirely; classifier stays heuristic |

The "ollama" path is the easiest if you're already running a local model — it has no cold-start cost. The "llamafile" path is the most portable (gnoma owns the lifecycle) but pays a one-time boot per gnoma invocation.

## Presets

Presets use `reecdev/tiny3.5:500m` as the default model — a 500 M-parameter Qwen3.5 distillation with tool support, available on Ollama. Pull it once with:

```bash
ollama pull reecdev/tiny3.5:500m   # ~1 GB
# or the 1.5 B variant for slightly better quality:
ollama pull reecdev/tiny3.5:1.5b   # ~3 GB
```

Substitute any small Ollama model you prefer. The probe at startup reads each model's actual capability — `tools` enables the SLM arm to handle simple file reads; without it, the SLM only handles knowledge-only prompts.

### Preset 1 — Ollama (recommended for most users)

```toml
[slm]
enabled = true
backend = "ollama"
model   = "reecdev/tiny3.5:500m"
# base_url defaults to http://localhost:11434
```

Prereq: `ollama pull reecdev/tiny3.5:500m` (or any model you'd rather use).

### Preset 2 — llama.cpp server

```toml
[slm]
enabled = true
backend = "llamacpp"
# base_url defaults to http://localhost:8080
# model defaults to "default" — llama.cpp's server ignores the field
```

Prereq: a running `llama-server` (or `llama.cpp` server) on the configured port. Model is determined by what you launched the server with.

### Preset 3 — Llamafile (gnoma-managed)

```toml
[slm]
enabled = true
backend  = "llamafile"
# Optional overrides:
# model_url       = "https://huggingface.co/Mozilla/Qwen2.5-0.5B-Instruct-llamafile/resolve/main/Qwen2.5-0.5B-Instruct-Q6_K.llamafile"
# data_dir        = ""        # empty = XDG default (~/.local/share/gnoma/slm)
# startup_timeout = "10s"     # how long to block on first-boot before falling back
```

Prereq: `gnoma slm setup` once to download the binary. After that gnoma starts/stops the llamafile process automatically. Expect ~15–30 s cold start on the first prompt of each gnoma invocation.

### Preset 4 — LM Studio / generic OpenAI-compatible

```toml
[slm]
enabled = true
backend = "openaicompat"
base_url = "http://localhost:1234/v1"   # LM Studio's default
model    = "qwen2.5-0.5b"
```

Use this for any OpenAI-compatible endpoint that isn't Ollama or llama.cpp: LM Studio, vLLM, llamaedge, a remote relay, etc.

### Preset 5 — Auto (default)

```toml
[slm]
enabled = true
backend = "auto"
```

Gnoma probes in this order on startup:

1. If you have `model_url` configured **and** llamafile is set up → use llamafile.
2. Ollama at `localhost:11434` → use it, picking the smallest model available.
3. llama.cpp at `localhost:8080` → use it.
4. Llamafile (if it happens to be set up).
5. Nothing reachable → SLM stays disabled, classifier stays heuristic.

This is what you get if you don't set `backend` at all.

### Preset 6 — Disabled

```toml
[slm]
enabled = false
```

Skips the SLM entirely. The router uses only the keyword-based heuristic classifier; the SLM arm isn't registered. Useful for slow systems or air-gapped setups.

## Custom backends

The `openaicompat` backend IS the escape hatch — point it at any OpenAI-compatible URL and any model name. If you can curl it with a standard chat-completion payload, gnoma can use it as the SLM.

## What the SLM actually does

| Role | Triggered by | Effect |
|---|---|---|
| Classifier | Every prompt | Returns `task_type` (debug / generation / refactor / …), `complexity` (0–1), `requires_tools` (bool). Drives router arm selection. |
| Arm | Tasks with complexity ≤ 0.3 | Gnoma routes the task to the SLM directly, including simple tool calls like `fs.read foo.go`. |

Both roles use the same backend + model. The SLM arm is registered with `MaxComplexity=0.3` so anything more complex automatically routes to a bigger arm. Trivial work — knowledge questions, short explanations, single file reads — stays local on the small model.

### Picking a model

The two roles have different demands:

- **Arm execution** is forgiving. The model just has to answer the prompt or emit a single tool call. Tiny3.5-500M handles general chat, trivia, and simple file reads (when `tools` capability is present). Any small model with tool support works here.
- **Classifier** is stricter. The model has to follow a JSON output schema. **Models below ~3 B parameters frequently fail this contract** — they emit prose, partial JSON, or thinking tokens instead of clean output. The classifier then falls back to the heuristic, which is fine but means the SLM signal isn't contributing. If `gnoma router stats` shows a high `slm_fallback` share, the model is missing the JSON contract; bumping to ~3 B parameters (`qwen2.5-coder:3b`, `phi-3-mini`, `ministral-3:3b`) typically resolves this.

You don't have to pick a model that does both well. The common shape is:

- Use a small (500 M – 1.5 B) tool-capable model as the SLM **arm** — it answers trivial questions and runs simple tool calls without going up to a bigger model.
- Accept that the **classifier** role falls back to the keyword heuristic on small models. The heuristic is good enough for routing.
- Watch `gnoma router stats` to see the actual mix — the `slm/<backend>` arm row counts tells you whether the SLM is executing real work, which is what matters most.

## Verifying

After picking a preset:

```bash
gnoma slm status
```

Output looks like:

```
slm enabled: true
slm backend: ollama
  model:   reecdev/tiny3.5:500m

live probe:
  ✓ ollama ready (model=reecdev/tiny3.5:500m, boot=0s)
```

Run a few prompts, then check:

```bash
gnoma router stats
```

The classifier-source breakdown reveals what's actually being used:

```
Classifier source breakdown:
  SOURCE        COUNT  SHARE
  slm           18     60.0%
  slm_fallback  4      13.3%
  heuristic     8      26.7%
  total observations: 30
```

A healthy SLM share (≥50 %) means the classifier is firing reliably. High `slm_fallback` means the model is failing to return valid JSON — try a larger model.
