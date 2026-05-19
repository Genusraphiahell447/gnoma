# Security Hardening Wave 2 — Incognito Coherence — 2026-05-19

Follow-up to
[`2026-05-19-security-wave1-safeprovider.md`](2026-05-19-security-wave1-safeprovider.md)
(merged via PR #1). Addresses audit findings 5, 6, 7 — the cluster
where gnoma's incognito promise leaks because state is duplicated
across the CLI flag, the firewall's `IncognitoMode`, the router's
`localOnly` flag, and the TUI's local `m.incognito` field, and these
four go out of sync on the realistic paths.

The audit's UI string is "🔒 incognito ON — no persistence, no
learning, local-only routing." Today, depending on which path the
user took, any of those three promises can be silently false. Wave 2
makes the promise hold.

---

## Findings to close

### 1. Forced cloud arm beats local-only

`internal/router/router.go:67-73` short-circuits on `forcedArm` before
the `localOnly` filter at line 81. Concretely: launch with
`--provider anthropic`, then `Ctrl+X` to enter incognito. The TUI
says "local-only routing"; the next turn streams to Anthropic.

### 2. Tool-result store persists in incognito

`cmd/gnoma/main.go:575` calls `persist.New(sessionID)` unconditionally.
The engine's `Save` path at `internal/engine/loop.go:709-714` only
checks `e.cfg.Store != nil`. With `--incognito` *or* with
`Ctrl+X`-activated incognito, tool results ≥1 KiB still land in
`/tmp/gnoma-<sessionID>/tool-results/`. Audit also flags 0o755 / 0o644
perms.

### 3. TUI `m.incognito` not initialized from firewall

`internal/tui/app.go:133` declares `incognito bool` but `tui.New(...)`
doesn't seed it from `fw.Incognito().Active()`. Launch with
`--incognito`: the firewall is on, but `m.incognito = false`. First
`Ctrl+X` calls `Toggle()`, flips the firewall off, and now incognito
is silently disabled.

### 4. `saveQuality` and `ReportOutcome` gate on the wrong source

`cmd/gnoma/main.go:373` checks `*incognito` (the CLI flag). If the
user entered incognito via TUI at runtime, the bandit snapshot still
writes on exit. Likewise `internal/engine/loop.go:85` calls
`Router.ReportOutcome` unconditionally — every successful turn during
incognito updates the per-arm × per-task EMA quality.

### 5. Session files are 0o644

`internal/session/store.go:167` writes session JSON at world-readable
0o644. Not a network leak, but inconsistent with the audit's "0600
everywhere for sensitive local files" recommendation.

### 6. `IncognitoMode.LocalOnly` is dead weight

`internal/security/incognito.go:12` declares `LocalOnly bool` as a
public field. Nothing reads it; the router uses its own `localOnly`
state set via `SetLocalOnly`. Two state pointers for the same idea.

---

## Approach

The pattern across all six findings is the same: there's no single
source of truth for "is the session in incognito?" Wave 2 makes
`security.IncognitoMode` that source.

- All gates (persist, learn, route, log) read from
  `fw.Incognito().Should*()` rather than the CLI flag or local state.
- The router rejects forced non-local arms when `localOnly` is on
  (rather than silently bypassing).
- The TUI seeds its display flag from the firewall on startup; the
  firewall stays canonical thereafter.
- File modes harden to 0o600 / 0o700.
- The unused `IncognitoMode.LocalOnly` field gets removed.

### Why not move `localOnly` onto `IncognitoMode`?

Tempting — it would centralise everything on one type. But the router
already has the access pattern (mutex around arm selection) and the
firewall has no business carrying routing state. Wave 2 keeps the
existing split: firewall owns the *intent* flag (`Active()`); router
owns the *enforcement* flag (`localOnly`). What changes is that they
must agree, which is a TUI/CLI concern, not a router/firewall concern.

---

## Tasks

### W2-1 — Router rejects forced non-local arms under local-only

- [ ] `internal/router/router.go:Select` — when `r.forcedArm != ""`
  and the forced arm `IsLocal == false` and `r.localOnly == true`,
  return `RoutingDecision{Error: ...}` with an actionable message
  ("incognito requires a local arm; current force is %s — clear the
  --provider pin or disable incognito").
- [ ] Test: forced cloud arm + `SetLocalOnly(true)` → `Select` error.
- [ ] Test: forced local arm + `SetLocalOnly(true)` → `Select`
  returns that arm.
- [ ] `internal/tui/app.go` — incognito toggle path must check
  `rtr.ForcedArm()` before flipping. If a non-local arm is pinned,
  refuse with a message ("clear `--provider` to enter incognito").
- [ ] CLI: when both `--incognito` and `--provider <cloud>` are set
  at launch, fail fast in `main.go` with the same message rather than
  starting a broken session.

### W2-2 — Persist store consults IncognitoMode

- [ ] `internal/tool/persist/store.go` — `New(sessionID, *security.IncognitoMode)`
  signature. `Save` returns `("", false)` when
  `mode != nil && !mode.ShouldPersist()`. Existing
  `len(content) < minPersistSize` check stays.
- [ ] Permission tightening: `MkdirAll(..., 0o700)`,
  `WriteFile(..., 0o600)`. No reason these need group/world read.
- [ ] `cmd/gnoma/main.go:575` passes `fw.Incognito()` to `persist.New`.
- [ ] `internal/persist` tests cover (a) incognito-active store
  returns false from `Save`, (b) directory mode is 0o700 when created,
  (c) file mode is 0o600.
- [ ] Audit note: persist mode is *dynamic* (consults
  `ShouldPersist()` on every call), not frozen at construction. So
  TUI-runtime toggles work without needing to swap the store.

### W2-3 — TUI seeds incognito from firewall

- [ ] `internal/tui/app.go:tui.New` (or wherever the model is
  constructed) — `m.incognito = cfg.Firewall.Incognito().Active()`.
- [ ] Render the status-bar badge from the firewall, not from local
  state, on every render. `m.incognito` becomes a UI cache that's
  re-read from the firewall on each `View()` (or kept in sync
  explicitly).
- [ ] Test: launch with `--incognito`, hit `Ctrl+X` once → firewall
  is off, badge gone. Currently fails because first toggle ON->OFF
  reads from wrong state.

### W2-4 — Quality + outcome gates read firewall, not CLI flag

- [ ] `cmd/gnoma/main.go:saveQuality` — replace `if *incognito` with
  `if !fw.Incognito().ShouldLearn()`.
- [ ] Quality *restore* path at `main.go:351-360` — when
  `fw.Incognito().Active()` at startup, don't read the snapshot at
  all. Avoids leaking learned quality into an incognito session.
- [ ] `internal/engine/loop.go:reportOutcome` (the inner closure) —
  gate on `e.cfg.Firewall != nil && e.cfg.Firewall.Incognito().ShouldLearn()`.
- [ ] Elf result reporting (`internal/elf/manager.go:ReportResult`)
  needs the same gate. Pass `fw` into the manager or thread the
  predicate.
- [ ] Tests: confirm `Router.ReportOutcome` is *not* called from the
  engine path when incognito is active; quality snapshot not written
  on exit; quality snapshot not loaded on startup under
  `--incognito`.

### W2-5 — Session file perms

- [ ] `internal/session/store.go:167` — `os.WriteFile(tmp, data, 0o600)`.
- [ ] Any `MkdirAll` in the session path → 0o700.
- [ ] Test: session file written from a default-umask process has
  mode `0o600`.

### W2-6 — Remove dead `IncognitoMode.LocalOnly` field

- [ ] `internal/security/incognito.go:12` — drop `LocalOnly bool`.
  Audit grep confirms no readers.
- [ ] If any tests reference it, remove those lines.

---

## Exit criteria

- Launching with `--incognito` and `--provider anthropic` fails fast
  with an actionable error (not a silent cloud routing under an
  "incognito" badge).
- During an incognito session — entered either via `--incognito` or
  `Ctrl+X` — no files appear under `/tmp/gnoma-<sessionID>/`, no
  bandit snapshot is written on exit, no `Router.ReportOutcome` calls
  fire from the engine or elves.
- `Ctrl+X` on a `--incognito` launch flips the firewall *off* on
  first press (matches the badge state at launch).
- All persisted files (sessions, tool-results) are mode 0o600,
  directories 0o700.
- `IncognitoMode` no longer has a `LocalOnly` field.
- `make test`, `make lint`, `go test -race ./...` green.

---

## Out of scope (Wave 3 or beyond)

- **Permission mode** under incognito (audit didn't flag; current
  behaviour is intentional — incognito doesn't change *what tools can
  run*, only what persists / where it routes).
- **Provider request body logging** — slog output today may include
  redacted-but-still-context-rich data. If we want
  `ShouldLogContent` to actually gate something, that's its own
  finding; current `ShouldLogContent` has zero readers and is part
  of the same dead-weight pattern as `LocalOnly`. Decision: keep
  `ShouldLogContent` because it's the natural extension point if we
  later add structured request logging; remove only `LocalOnly`
  (which is structurally wrong, not just unused).
- **Re-exec carrying incognito state** — `argsWithProfileReplaced`
  preserves `--incognito` through profile switches via `os.Args`.
  Verified, no work needed.

---

## Effort estimate

- W2-1 (router gate + UI refuse): ~80 LOC + ~50 LOC tests.
- W2-2 (persist incognito): ~50 LOC + ~80 LOC tests.
- W2-3 (TUI seed): ~30 LOC + ~40 LOC test.
- W2-4 (gates on firewall state): ~60 LOC across 4 files + ~80 LOC tests.
- W2-5 (file modes): ~10 LOC + ~30 LOC tests.
- W2-6 (dead field): ~5 LOC.

Total: ~400 LOC including tests. One PR.

---

## Suggested execution order

1. **W2-6** first — tiny cleanup, surfaces any test that was
   relying on the dead field.
2. **W2-1** next — the router gate is independently useful and
   doesn't depend on TUI work.
3. **W2-2** — persist gate. Wave 2's biggest individual change.
4. **W2-4** — outcome/quality gates. Touches engine + elf manager;
   easier with a clean baseline from W2-2.
5. **W2-3** — TUI seed. Last because TUI tests are slowest to write
   and easiest to land once the underlying state is coherent.
6. **W2-5** — file modes. Trivial, but adding a test that asserts
   `Stat().Mode().Perm() == 0o600` should land together with the
   change.

---

## Changelog

- 2026-05-19: Initial.
