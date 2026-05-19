# Post-SLM-Unlock Plan — 2026-05-19

Supersedes the smallcode-audit follow-ups in
[`2026-05-07-gnoma-roadmap.md`](2026-05-07-gnoma-roadmap.md) Phase 4
("Router Revisit") and adds two new feature tracks (CLI agent binary
override, user profiles) discovered during the SLM unlock session.

**State as of this writing:** the SLM is functional end-to-end:

- Pluggable backend (ollama / llamacpp / llamafile / openaicompat /
  auto / disabled).
- Auto-detection of tool capability per model.
- Trivial prompts route to the SLM arm; complex prompts (per-task
  complexity floor) route to bigger arms.
- Telemetry surface (`gnoma router stats`) tracks classifier source
  and arm × task EMA quality.
- TUI surfaces SLM badge in the status bar and per-turn classifier in
  chat history.

Five commits landed: `b136a30` (early-stop), `bf05a58` (lexical
JSON repair), `17f3e02` (classifier telemetry), `bbe20c1` + `64d448b`
(pluggable backend + TUI), `9b62445` (router fixes).

What remains, in suggested execution order:

---

## Phase A: Two-Stage Tool Routing

Carry-over from the original smallcode audit (Tier 2, Item 3). The
only audit-derived item that has not yet shipped.

**Problem.** `buildRequest()` (`internal/engine/loop.go`) always sends
the full tool catalogue (~12 schemas, ~1500 tokens) to every arm. Small
local models with ≤16 k context windows waste a non-trivial fraction
of every prompt on schemas they don't end up calling.

**Approach.** When the active arm is local and has a small context
window, replace `req.Tools` with a single synthetic `select_category`
tool whose enum lists tool categories (`read`, `write`, `search`,
`exec`, `meta`). The model picks a category in round 1; round 2 sends
only that category's real schemas.

### Tasks

- [ ] `Category()` method on `tool.Tool` (or a registry-side mapping).
  Default category for unspecified tools: `meta`.
- [ ] New `engine.useTwoStageTools(arm)` predicate. Gates on
  `arm.IsLocal && arm.Capabilities.ContextWindow <= 16384`, with an
  optional `[router].force_two_stage = true` config override.
- [ ] Synthetic `select_category` tool definition emitted in
  `buildRequest()` when the predicate is true and we're in the first
  round of a turn.
- [ ] Engine recognises a `select_category` tool result and filters
  the next round's tool schemas. The selection itself doesn't run a
  real tool — it's consumed internally.
- [ ] Integration test covering the round-trip with a mocked
  openaicompat arm.

**Exit criteria:** for an SLM-arm turn with ≤16 k context, the first
request contains exactly one synthetic tool schema; the second
contains only the schemas of the selected category. Real tool
selection still works (the second-round tool call executes normally).

**Effort:** ~150 LOC + tests.

---

## Phase B: CLI Agent Binary Override

The CLI-agent discovery (`internal/provider/subprocess/agent.go`)
hard-codes the binary names `claude`, `gemini`, `vibe`. Users with
aliased binaries — `claude-work` / `claude-priv`, `gemini-personal`,
etc. — currently can't connect those to gnoma's auto-discovery.

**Approach.** Per-agent binary override in config. Empty value falls
back to the canonical name.

```toml
[cli_agents]
claude = "claude-priv"      # use this binary instead of "claude"
gemini = "gemini-work"
# vibe falls back to "vibe" because no override is set
```

### Tasks

- [ ] `CLIAgentsSection map[string]string` in `internal/config/`. Keys
  are canonical agent names; values are override binary names.
- [ ] `DiscoverCLIAgents(ctx, overrides)` consults the map before
  falling back to the canonical name. The returned `DiscoveredAgent`
  records the resolved binary path so downstream logs are accurate.
- [ ] `gnoma providers` shows the resolved binary name when an
  override is in effect (e.g. `claude-priv (via [cli_agents].claude)`).
- [ ] Unit tests against a mock PATH-resolver that confirms override
  precedence, fallback when override is empty, and graceful behavior
  when the overridden binary isn't on PATH.

**Exit criteria:** with `[cli_agents].claude = "claude-priv"`, the
discovery picks up `claude-priv` as the Claude Code arm and the
`subprocess/claude` arm in the router routes through it.

**Effort:** ~80 LOC + tests.

---

## Phase C: User Profiles

Lets users keep multiple gnoma configurations and switch between them.
Common use cases:

- `work` vs `private` — different API keys, different CLI binaries,
  stricter or looser permissions per profile.
- `experiment` — non-default SLM model, plan mode, no persistence.

### Config layout

A base config picks the default profile and holds profile-agnostic
settings. Profile files live alongside:

```
~/.config/gnoma/
├── config.toml                 # base config + default_profile
├── profiles/
│   ├── work.toml
│   ├── private.toml
│   └── experiment.toml
```

`config.toml` (base):

```toml
default_profile = "work"

# Settings here apply to every profile unless the profile overrides them.
[tools]
bash_timeout = "30s"
```

`profiles/work.toml`:

```toml
[provider]
default = "anthropic"
[provider.api_keys]
anthropic = "${ANTHROPIC_WORK_KEY}"

[cli_agents]
claude = "claude-work"

[permission]
mode = "default"

[slm]
backend = "ollama"
model = "reecdev/tiny3.5:1.5b"
```

`profiles/private.toml`:

```toml
[provider]
default = "openai"
[provider.api_keys]
openai = "${OPENAI_PRIVATE_KEY}"

[cli_agents]
claude = "claude-priv"

[permission]
mode = "auto"

[slm]
backend = "ollama"
model = "reecdev/tiny3.5:500m"
```

### Switching

- Default at startup: `default_profile` from `config.toml`.
- Override per invocation: `gnoma --profile experiment`.
- Inside the TUI: `/profile <name>` (resets the engine and
  re-initialises router arms; surfaces a notice in the status bar).

### Tasks

- [ ] Config loader merges `config.toml` base + selected profile
  (profile overrides base, env vars override profile).
- [ ] `--profile <name>` CLI flag.
- [ ] `gnoma profile list` / `gnoma profile show <name>` subcommands.
- [ ] TUI `/profile` slash command (with autocomplete on profile
  names, requires engine restart on switch).
- [ ] Status-bar indicator shows the active profile (dim, next to the
  SLM badge: `· profile: work`).
- [ ] Migration path for existing single-config users: if no
  `profiles/` directory exists, fall back to the current behaviour
  (load `config.toml` as the sole config).
- [ ] Docs page with two or three full example profiles.

### Open design questions

- Should profile selection persist (last-used) or always come from
  `default_profile` on restart? Lean: always default unless `--profile`
  is set, and `/profile` in TUI is per-session.
- Where do session files (`~/.local/share/gnoma/sessions/`) live —
  global or per-profile? Lean: per-profile, so resuming `work` doesn't
  surface `private` sessions.
- Per-profile `quality.json` (router telemetry) — yes, otherwise the
  bandit cross-contaminates between profile workloads.

**Effort:** ~400 LOC across config loader, CLI, TUI; non-trivial because
the config layering is foundational.

---

## Phase D: Per-Arm Capability Tags (Phase-4 prep)

Currently the router's `armTier` rule dominates selection:
CLI agent > local > API. Within a tier, `scoreArm` differentiates by
cost-adjusted quality but doesn't capture per-task strengths
(e.g. Opus is better at planning than Mistral, Sonnet is better at
coding than Haiku).

**Approach.** Add explicit per-arm task-type strengths and a
configurable cost weight per task type.

```go
type Arm struct {
    ...
    Strengths  []TaskType  // task types where this arm is empirically best
    CostWeight float64     // 0.0 = ignore cost (use raw quality); 1.0 = current behaviour
}
```

`scoreArm` adds a bonus when `task.Type ∈ arm.Strengths` and uses
`CostWeight` to dampen the cost-denominator for tasks where cost
shouldn't dominate (e.g. SecurityReview).

### Tasks

- [ ] Add `Strengths` and `CostWeight` to `router.Arm`.
- [ ] Config schema for per-arm overrides — likely
  `[arms.<id>.strengths] = ["planning", "orchestration"]`.
- [ ] `scoreArm` consults both fields.
- [ ] Bandit signal feeds back into a per-arm-per-task affinity over
  time (≥10 observations needed). Currently `QualityTracker` already
  tracks per-arm × per-task EMA; what's missing is letting that
  signal *promote* an arm out of its default tier.
- [ ] Tests that show Opus winning over Gemini for SecurityReview
  when `arms.anthropic_opus.strengths = ["security_review"]`.

**Exit criteria:** with explicit per-task strengths set, the router
picks the strongest available arm for that task type, not the
lowest-tier one.

**Effort:** ~300 LOC + tests. Touches `selector.go`, `arm.go`, config.

---

## Phase E: Compound Tools (deferred until usage data exists)

From the smallcode audit (Tier 3). Compound tools like
`fs.read_and_edit`, `fs.find_and_read`, `bash.run_after_write` reduce
the call-chain depth small models have to maintain.

**Trigger:** the SLM arm now actually executes tasks (verified
post-`9b62445`). After ~50 observations on the SLM arm, inspect which
chain patterns fail and design compound primitives for those
specifically — not speculatively.

**Hold this until:**

- `gnoma router stats` shows `slm/<backend>` row at ≥50 observations.
- The `early_stop_*` log lines (patch spirals, repetition) show a
  recognisable pattern on the SLM arm specifically.

No tasks scoped until that trigger fires.

---

## Out of scope

Items previously considered and explicitly dropped:

- Bayesian tool scorer (gnoma's bandit covers it differently and
  better-grounded).
- BoneScript / MarrowScript or any DSL-shaped feature.
- Execute-after-write verifier (security regression vs. USP roadmap).
- Cloud "escalation" as a named provider tier (regression on
  provider-agnostic design).
- Multi-format Hermes / XML / YAML tool-call parser (no concrete
  model emits these natively today; the lexical JSON repair we have
  is sufficient).
- SLM-tier repair for malformed tool args (the lexical repair shipped
  in `bf05a58` handles the cases that matter; adding a model
  round-trip is the smallcode mistake we explicitly avoided).

---

## Suggested execution order

1. **Phase A (two-stage routing)** — small, self-contained, closes the
   last original-plan item. Ship first.
2. **Phase B (CLI agent override)** — small, unblocks Phase C and is
   independently useful. Can land in parallel with A.
3. **Phase D (per-arm capability tags)** — substantive, gives
   meaningful control over routing. Needed before profiles can express
   per-profile strength preferences meaningfully.
4. **Phase C (user profiles)** — foundational config change, depends
   on B (so profiles can override CLI binaries) and ideally D (so
   profiles can express per-task arm preferences).
5. **Phase E (compound tools)** — re-evaluate once the SLM arm has
   produced enough telemetry to justify specific primitives.

Or pause and let SLM data accumulate before committing to any of the
larger phases (D, C).

---

## Changelog

- 2026-05-19: Initial. Captures outstanding work after the SLM
  unlock session.
