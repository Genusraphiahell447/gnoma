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

- [x] `Category()` method on `tool.Tool` (or a registry-side mapping).
  Default category for unspecified tools: `meta`.
- [x] New `engine.useTwoStageTools()` predicate. Gates on
  `arm.IsLocal && arm.Capabilities.ContextWindow <= 16384`, with the
  `[router].force_two_stage = true` config override.
- [x] Synthetic `select_category` tool definition emitted in
  `buildRequest()` when the predicate is true and we're in the first
  round of a turn. Round 1 also forces `ToolChoiceRequired` so SLMs
  don't fall back to prose instead of calling the tool.
- [x] Engine recognises a `select_category` tool call and filters
  the next round's tool schemas. The selection itself doesn't run a
  real tool — it's consumed internally; `select_category` remains
  available in round 2+ so the model can switch categories mid-turn.
- [x] Integration test covering the round-trip with a recording
  mock provider.

**Status: shipped.** Phase A landed via the two-stage routing commit
on `main`. Module map:
- `internal/tool/category.go` — `Category` type, optional
  `Categorized` interface, `CategoryOf()` helper with `meta` fallback.
- Real tools now declare categories: `fs.read`/`fs.ls` → read,
  `fs.write`/`fs.edit` → write, `fs.glob`/`fs.grep` → search,
  `bash` → exec. Agent/sysinfo fall through to meta.
- `internal/engine/twostage.go` — synthetic tool definition, intercept
  helper, per-turn state (`selectedCategory`).
- `internal/engine/engine.go` — `Config.ForceTwoStageTools`,
  `useTwoStageTools()` predicate, `twoStageContextLimit = 16384`.
- `internal/config/config.go` — `[router].force_two_stage` TOML key
  wired through `cmd/gnoma/main.go`.

**Exit criteria — met:** for an SLM-arm turn with ≤16 k context, the
first request contains exactly one synthetic tool schema with
`ToolChoiceRequired`; the second contains only schemas of the
selected category plus `select_category`. Real tool selection still
works end-to-end (verified by `TestTwoStage_FullRoundTrip`).

**Effort:** ~250 LOC + tests (including 4 test files).

**Deferred for follow-up (not Phase A blockers):**
- Elf engines spawned from `internal/elf/manager.go` don't pass
  through `ForceTwoStageTools` — small local elves still get the full
  tool catalogue. Add per-elf two-stage detection mirroring the main
  engine's auto-activation when telemetry shows it's worth it.

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

- [x] `CLIAgentsSection map[string]string` in `internal/config/`. Keys
  are canonical agent names; values are override binary names. Empty
  value (`claude = ""`) is treated as "no override".
- [x] `DiscoverCLIAgents(ctx, overrides)` consults the map. Extracted
  `resolveAgentBinary()` with an injectable `lookPath` for test
  isolation. `DiscoveredAgent` gains an `OverrideBinary` field
  (empty when canonical name was used).
- [x] `gnoma providers` shows `claude-priv (via [cli_agents].claude)`
  when an override is in effect; canonical-only agents print just
  `claude`.
- [x] Unit tests against a mock PATH-resolver covering override
  precedence, empty-value fallback, missing-canonical-binary, and
  missing-overridden-binary (which warns + skips rather than silently
  falling back to canonical — masks user typos).

**Status: shipped.** Module map:
- `internal/config/config.go` — `CLIAgentsSection` map and TOML key.
- `internal/provider/subprocess/agent.go` — package-level `lookPath`
  for test override, `resolveAgentBinary()` helper, new
  `DiscoveredAgent.OverrideBinary` field.
- `cmd/gnoma/main.go` — passes `cfg.CLIAgents` to both discovery
  call sites and formats the "via" annotation in the providers list.

**Exit criteria — met:** with `[cli_agents].claude = "claude-priv"`,
discovery resolves `claude-priv` to the Claude Code arm with
`OverrideBinary="claude-priv"`. Router routes through the binary at
that resolved path. If the override binary isn't on PATH, the agent
is skipped with a warning (no silent fallback).

**Effort:** ~150 LOC + tests (5 resolver cases + 4 discovery tests +
2 config tests).

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

C-1 (foundational config + CLI) shipped 2026-05-19:

- [x] Config loader merges `config.toml` base + selected profile
  (profile overrides base, env vars override profile).
- [x] `--profile <name>` CLI flag.
- [x] Migration path for existing single-config users: if no
  `profiles/` directory exists, fall back to the current behaviour
  (load `config.toml` as the sole config).
- [x] Docs page with three full example profiles
  (`docs/profiles.md`).

C-2 (CLI surface) shipped 2026-05-19:

- [x] `gnoma profile list` / `gnoma profile show <name>` subcommands.
  Both work even when profile resolution is otherwise broken — they're
  the recovery affordance for diagnosing misconfigurations. List marks
  the default-but-missing case explicitly (`ghost (default, missing)`).
  Show never prints API key *values*, only the set of configured
  provider names.

C-3 (TUI integration) shipped 2026-05-19:

- [x] TUI `/profile` slash command (with autocomplete on profile
  names, re-execs gnoma on switch — see note below on the engine-restart
  approach).
- [x] Status-bar indicator shows the active profile (dim, next to the
  SLM badge: `· profile: work`).

**Engine restart approach (C-3 implementation note):** rather than
attempting in-process teardown and reinitialisation of the engine,
router, providers, and session store, `/profile <name>` calls
`syscall.Exec` to replace the current gnoma process with a fresh one
under `--profile <name>`. Critical cleanups (quality.json snapshot,
SLM backend shutdown, session close) fire explicitly before exec
because defers don't run after a successful `syscall.Exec`.

The trade-off: conversation history is not preserved across a switch.
This matches the plan's stated semantics — a profile change implies
different context, different keys, different permission mode — so
preserving chat state across the boundary would be confusing rather
than helpful.

### Open design questions — resolved

- **Profile selection persistence**: per-session only. Restart
  re-reads `default_profile`; `--profile` overrides for one
  invocation; TUI `/profile` (C-3) will be session-scoped.
- **Session file location**: per-profile, at
  `<projectRoot>/.gnoma/sessions/<profile>/`. When no `profiles/`
  directory exists, legacy `<projectRoot>/.gnoma/sessions/` path
  is preserved (no migration).
- **Per-profile `quality.json`**: yes, at
  `~/.config/gnoma/quality-<profile>.json`. Legacy path preserved
  for single-config installations.

### C-1 module map (shipped)

- `internal/config/profile.go` — `Profile` struct,
  `LoadWithProfile()`, `ListProfiles()`, slice-merge helpers
  (`mergeArmsByID`, `mergeMCPServersByName`),
  `validateProfileName()` (rejects path traversal), and the
  `ErrProfileResolution` sentinel for actionable misconfigurations.
- `internal/config/load.go` — `Load()` now delegates to
  `LoadWithProfile("")` for backward compatibility.
- `internal/config/config.go` — `DefaultProfile string` TOML key.
- `internal/session/store.go` — `NewSessionStoreAt(dir, ...)`
  constructor accepting an explicit sessions directory.
- `cmd/gnoma/main.go` — `--profile` flag, fatal exit on
  `ErrProfileResolution`, profile-aware quality.json and session
  paths.
- `cmd/gnoma/router_cmd.go` — `gnoma router stats` reads the
  active profile's `quality-<name>.json` and prefixes output
  with `Profile: <name>`.

**Effort:** C-1 shipped at ~250 LOC + ~370 LOC tests + docs page.
C-2 and C-3 still scoped at ~80 / ~120 LOC respectively.

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

- [x] Add `Strengths []TaskType` and `CostWeight float64` to
  `router.Arm`. Zero values preserve current behavior.
- [x] Config schema: `[[arms]]` array of tables — `id`, `strengths`
  (string list, parsed via new `ParseTaskTypeStrict`), `cost_weight`.
- [x] `scoreArm` consults both fields: strength match adds a tunable
  bonus (`strengthScoreBonus = 0.15`); `CostWeight` linearly dampens
  cost via `effectiveCost = 1 + CostWeight*(cost-1)` — monotone on
  both sides of cost=1.
- [x] `selectBest` cross-tier promotion: arms whose `Strengths`
  contain `task.Type` are evaluated as one set before falling through
  to default tier order. Strengths are a preference, not a pin —
  backoff/feasibility filtering at the router level removes promoted
  arms when unavailable, and selection falls through.
- [x] `Router.ApplyArmOverrides()` applies config overrides post
  arm-registration. Unknown arm IDs surfaced via return value; main
  logs a warning. Unknown strength names skipped with per-strength
  warning.
- [x] Tests: Opus with `Strengths=[security_review]` beats CLI-agent
  tier-1 arm; empty Strengths preserves tier order; promoted arm in
  backoff falls through (via full `Router.Select` path); two
  strength-tagged arms decided by observed quality; CostWeight
  direction across two arms; linear-formula monotonicity regression
  test for the cost^weight bug avoided.

**Status: shipped (static portion).** Module map:
- `internal/router/arm.go` — `Strengths`, `CostWeight`,
  `HasStrength()`, `ResolvedCostWeight()`.
- `internal/router/selector.go` — `scoreArm` updated, `selectBest`
  cross-tier promotion path.
- `internal/router/router.go` — `ArmOverride` type and
  `ApplyArmOverrides()`.
- `internal/router/task.go` — `ParseTaskTypeStrict()` (returns ok
  bool) for typo-resistant config parsing.
- `internal/config/config.go` — `ArmConfig` struct and `[[arms]]`
  TOML wiring.
- `cmd/gnoma/main.go` — applies overrides after all initial arms
  register; warns on unknown IDs.

**Exit criteria — met:** with `[[arms]] id="anthropic/..."
strengths=["security_review"]`, the router picks Opus over a
higher-tier CLI agent for that task type. Verified by
`TestSelectBest_StrengthPromotedArmBeatsCLIAgent`.

**Effort:** ~350 LOC + tests.

**Deferred to D-2:** dynamic bandit-driven promotion (≥10 observations
threshold + per-arm × per-task affinity that overrides tier order
without static config). Holding until telemetry from real workloads
informs the quality bar — same rationale as Phase E.

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
