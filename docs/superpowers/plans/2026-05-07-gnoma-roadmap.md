# Gnoma Roadmap — 2026-05-07

Revised and consolidated plan. Supersedes all prior plan files under `docs/superpowers/plans/`.

**Current state as of this writing:** M1–M8 complete. Subprocess CLI provider (P0b) and tier-based
routing (P0c) landed on `main`. Distribution tooling (goreleaser) already configured.

---

## Phase 1: M8 Cleanup

Small wiring gaps left over from M8 that were deferred during that milestone.

| Task | File | Notes |
|------|------|-------|
| Remove stale `TODO(P0c)` comment | `cmd/gnoma/main.go` | Comment added during P0b, resolved by P0c tier routing |
| Wire `config.Temperature` into engine requests | `internal/engine/`, config load path | Currently ignored after config parsing |
| Enforce skill `allowedTools` → `engine.TurnOptions.AllowedTools` | `internal/skill/`, `internal/engine/` | Frontmatter parsed but not threaded through |
| Enforce skill `paths` (restrict tool file access to declared paths) | `internal/skill/`, `internal/tool/fs/` | Parsed but not enforced |
| Add `WithMaxFileSize` guard to `fs.write` | `internal/tool/fs/write.go` | Prevent LLM-driven write of multi-GB files |
| Wire `router.ReportOutcome` after each engine turn | `internal/engine/` | Stub is present; outcome never recorded |

**Exit criteria:** All six gaps resolved. `make test` green.

---

## Phase 2: PTY Interactive Shell

M5 deferred the interactive shell pane (`/shell` command). The implementation uses Bubble Tea v2's
built-in process execution API — no CGO, no new dependencies.

**Approach:** `tea.ExecProcess(cmd, cb)` hands the terminal to a subprocess. The engine calls
`p.ReleaseTerminal()` / `p.RestoreTerminal()` around the subprocess lifecycle.

### Tasks

- [ ] `/shell` command: detect `$SHELL`, fallback to `/bin/sh` (Unix) or `%COMSPEC%` (Windows)
- [ ] `tea.ExecProcess` integration in TUI: hand terminal to shell, restore on exit
- [ ] Interactive command detection in bash tool: patterns like `sudo `, `ssh `, `passwd`,
      `git push` with credential prompts — suggest takeover to user
- [ ] Bash tool "takeover" path: if user confirms, switch to PTY mode for that command
- [ ] Windows: detect `%COMSPEC%`, fallback to `powershell.exe`

**Dependencies:** `charm.land/bubbletea/v2` — already in go.mod.
**No new deps required.**

**Exit criteria:** `/shell` opens interactive terminal. `sudo apt update` works through it.
Bash tool flags `passwd foo` and offers takeover.

---

## Phase 3: SLM Task Classifier

Add an optional SLM-driven task classifier behind the existing `TaskClassifier` interface. The SLM
calls Ollama HTTP via the existing `openaicompat` provider — zero new dependencies, no CGO, no
daemon management.

**Context:** `gemma-integration-analysis.md` describes how gemini-cli implements this using
LiteRT-LM (a Node.js daemon + PID files). Those specifics do not apply here. The Go approach is
simpler: Ollama HTTP + structured JSON output + hard timeout + heuristic fallback.

### Interface

```go
// internal/router/classifier.go
type TaskClassifier interface {
    Classify(ctx context.Context, input string, history []message.Message) (Task, error)
}

type HeuristicClassifier struct{} // default — wraps existing ClassifyTask()
type SLMClassifier struct {
    provider provider.Provider // openaicompat pointing at Ollama
    model    string
    timeout  time.Duration     // default 2s
}
```

`SLMClassifier.Classify` sends a structured prompt with the Complexity Rubric (adapted from
gemma-integration-analysis.md) and expects a JSON response:

```json
{"task_type": "Generation", "complexity": 0.4, "requires_tools": true}
```

On timeout or parse failure, it falls back to `HeuristicClassifier`.

### Complexity Rubric (prompt fragment)

```
Classify this coding request. Respond with JSON only.
Complexity 0.0–0.3: boilerplate, trivial edits, simple lookups
Complexity 0.4–0.6: moderate — new functions, refactors, unit tests
Complexity 0.7–1.0: architectural, multi-file, security review, planning
```

### Config

```toml
[router]
slm_model = ""   # empty = disabled (HeuristicClassifier used)
                 # e.g. "gemma3:1b" — must be available in Ollama
```

### Tasks

- [ ] `TaskClassifier` interface in `internal/router/classifier.go`
- [ ] `HeuristicClassifier` wraps existing `ClassifyTask()` (zero behavior change)
- [ ] `SLMClassifier`: Ollama HTTP via openaicompat, JSON parse, 2s timeout + fallback
- [ ] Complexity Rubric prompt (task type + complexity float + requires_tools bool)
- [ ] Config key `router.slm_model`; wire into router construction in `cmd/gnoma/main.go`
- [ ] Tests: `HeuristicClassifier` behavior unchanged; `SLMClassifier` fallback on timeout;
      `SLMClassifier` correct parse on valid JSON response

**Dependencies:** existing `internal/provider/openaicompat` — no new deps.

**Exit criteria:** `gnoma` with `slm_model = "gemma3:1b"` routes using SLM classification.
Without config key, behavior is identical to today.

---

## Phase 4: Router Revisit (post-SLM)

Once Phase 3 is in production and generating real classification signals, revisit the routing stack.
This phase cannot start until Phase 3 has been running and producing outcome data.

**Decisions to make at that point:**

1. **Replace `armTier` heuristic selection** (`selector.go`) with SLM-informed decisions. The
   current CLI > local > API tier order is a pragmatic placeholder. The SLM has richer context
   (task semantics, model performance history, cost envelope).

2. **Re-evaluate bandit learning (ADR-009).** With an SLM as the preflight dispatcher, Thompson
   Sampling may introduce conflicting signals. Options:
   - Keep bandit as a feedback loop the SLM can read (outcome telemetry → SLM context)
   - Retire numeric EMA in favour of qualitative outcome summaries fed to the SLM
   - Keep both with explicit responsibilities: SLM = intent routing, bandit = cost/quality
     feedback within a tier

3. **Re-evaluate `filterFeasible` quality thresholds** — static `DefaultThresholds` in `task.go`
   may be redundant once the SLM classifies complexity dynamically.

**See ADR-013** (`docs/essentials/decisions/002-slm-routing.md`) for the formal decision record
superseding ADR-009.

---

## Phase 5: USP Security Integration

Ship Universal Security Pilot capabilities as first-class gnoma features. Skills are embedded Go
binaries, not runtime-parsed Markdown.

### Core Capabilities

- **Security audit engine**: eight-rule zero-trust review (adversarial input, context-aware
  footguns, identity integrity, atomicity, secret hygiene, AI guarding, SSRF/Dial-Control,
  multilingual defense)
- **Wave Protocol enforcement**: W0→W1→W2→W3→W4→W5→W6, blast-radius-descending within each wave
- **Iron Law**: no fix ships without a failing PoC test — enforced in remediation workflow
- **Standards citation**: every finding maps to OWASP / ASVS / LLM Top 10 / MITRE ATLAS / CWE IDs
- **AI hardening**: six-axis LLM hardening (prompt boundaries, output sanitization, BudgetGate,
  Dial-Control, injection vectors, multilingual defense)

### Tasks

- [ ] Implement `sec-audit`, `sec-fix`, `ai-harden`, `sec-init` as bundled gnoma skills
      (`//go:embed skills/*.md` in `internal/skill/embed.go`) — not external file reads
- [ ] Footgun library: embed universal footgun catalog (categories A–D) + framework-specific
      instances as Go structs queryable during audits (not runtime-parsed Markdown)
- [ ] Severity grading: Critical/High/Medium/Low/Info with canonical definitions, used in reports
- [ ] Complexity rubric: language-specific footgun tables (Go, TS/JS, Rust, Python) as Go structs
- [ ] Canonical patterns: BudgetGate, Dial-Control, Envelope Encryption, OIDC state-verification
      as referenceable code templates gnoma can scaffold
- [ ] Wave Protocol as `PreToolUse` hook: blocks out-of-order remediation
- [ ] Per-project override: `.gnoma/security/project-pilot.toml` (tighten only, never loosen)
- [ ] Rationalization resistance: anti-pressure table from `sec-fix` hard-coded, not configurable
- [ ] Report generation: structured Markdown audit reports with standards citations + wave assignment

**Reference:** `~/.security-pilot/` for canonical skills and PILOT.md.

**Exit criteria:** `/sec-audit` produces a finding with OWASP citation and wave assignment.
`/sec-fix` enforces Iron Law. Wave Protocol hook blocks W2 fix attempted before W1.

---

## Phase 6: ELF Binary Support (deferred / opportunistic)

No concrete use case is driving this today. Deferred until a real need emerges.

When implementing, use `debug/elf` (Go standard library) only. No new third-party deps for the
basic tools.

**Tools to implement when the time comes:**
- `elf.parse`: headers, sections, segments via `debug/elf`
- `elf.symbols`: symbol table extraction
- `elf.analyze`: security flag checks (NX, RELRO, stack canary, PIE)
- `elf.disassemble`: delegate to `objdump` if available, error gracefully otherwise

**Register in `cmd/gnoma/main.go` alongside existing tools when implemented.**

---

## Phase 7: Distribution (minimal work remaining)

Goreleaser is already fully configured (`CGO_ENABLED=0`, platforms: linux/darwin/windows,
arches: amd64/arm64). The binary is a single static executable with zero runtime dependencies.

**Remaining work:**
- [ ] CI trigger: add Woodpecker (or GitHub Actions) workflow that runs `goreleaser release` on
      `git tag v*` push
- [ ] Homebrew tap formula (optional, low priority)

**Exit criteria:** Pushing `git tag v0.1.0` triggers a release with pre-built binaries.

---

## Dropped Items

| Item | Reason |
|------|--------|
| `.gnoma/tmp/` local temp directory | `persist.Store` already uses `/tmp/gnoma-<sessionID>/`; adding `.gnoma/tmp/` adds complexity (cleanup, gitignore, collision avoidance) for no benefit |
| LiteRT-LM / CGO SLM runtime | `CGO_ENABLED=0` (goreleaser constraint). Go approach: Ollama HTTP via existing openaicompat |
| Daemon/PID file management for SLM | Node.js-specific pattern from gemma-integration-analysis.md; not applicable to this Go binary |
| PTY via `go-pty` library | Requires CGO. Replaced by `tea.ExecProcess` (already in go.mod, no CGO) |

---

## Changelog

- 2026-05-07: Initial version. Supersedes `2026-04-05-m6-m7-closeout.md`.
