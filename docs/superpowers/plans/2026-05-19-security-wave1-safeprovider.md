# Security Hardening Wave 1 — SafeProvider Boundary — 2026-05-19

Addresses findings 1–4 of the 2026-05-19 external audit
(`docs/audits/2026-05-19-main2-firewall-audit.md`, if archived; otherwise
the audit transcript in conversation). The audit's central architectural
critique is that the `Firewall` is wired at call sites (only inside
`engine.buildRequest()`), not at the provider boundary. As a result,
every non-engine consumer of a `provider.Provider` skips outgoing
redaction.

Verified non-engine consumers that today send raw payloads:

- `internal/slm/classifier.go:105` — sends raw user prompt to the SLM
  provider. Risk depends on backend (low for `ollama`/`llamafile`,
  real for `openaicompat` against a remote endpoint).
- `internal/context/summarize.go:109` — sends conversation history to
  the primary provider. Tool outputs in history are already redacted
  (`loop.go:706`), but user input and assistant prose are not.
- `cmd/gnoma/main.go:1225` (`routerStreamer`) — `/init` and similar
  flows build a raw request and call `rs.router.Stream(...)`.
- `internal/hook/prompt.go:80` — prompt-type hooks call the streamer
  directly with hook-supplied prompts.

Inside the main engine loop, `Router.Stream` and `prov.Stream` calls
at `loop.go:127, 158, 191, 206, 769, 772` consume requests already
scanned in `buildRequest()` — they're fine, just listed by the audit
for completeness.

This wave is scoped to closing the bypass at the provider boundary.
Incognito coherence (Wave 2) and scanner/path hygiene (Wave 3) are
separate plan docs.

---

## Approach

Wrap every `provider.Provider` with a decorator (`security.SafeProvider`)
that runs outgoing-message and system-prompt redaction before delegating
to the inner provider. Apply the wrapper at every registration boundary:

- primary provider construction in `cmd/gnoma/main.go`
- router arm `Provider` field (initial registration + discovery
  factory + CLI agent registration)
- SLM `Classifier.provider` construction
- summarizer `SummarizeStrategy.Provider` construction
- hook streamer construction

Tool-result redaction stays in the engine (it depends on per-tool
context — origin tool name for logging, etc., which the engine has and
the provider boundary doesn't). The engine's existing scan at
`loop.go:704-707` is unchanged.

### The construction-order problem

Today `security.NewFirewall()` runs at `main.go:509`, after every
provider arm has been registered (lines 404, 421-427, 436). A naive
"wrap at registration" implementation would require pulling Firewall
construction earlier, which entangles config validation,
custom-pattern wiring, and incognito state initialization.

**Solution:** `SafeProvider` accepts a `*FirewallRef` — a tiny
indirection holding an `atomic.Pointer[Firewall]`. The wrapper checks
the pointer on each call; if nil, it delegates without scanning
(matching today's behavior for early-init code paths). Firewall is
installed into the ref once it's constructed. Late-binding keeps the
init order intact and stays goroutine-safe without locking.

```go
type FirewallRef struct {
    p atomic.Pointer[Firewall]
}

func (r *FirewallRef) Set(fw *Firewall) { r.p.Store(fw) }
func (r *FirewallRef) Get() *Firewall   { return r.p.Load() }
```

### SafeProvider sketch

```go
type SafeProvider struct {
    inner provider.Provider
    fwRef *FirewallRef
}

func WrapProvider(inner provider.Provider, ref *FirewallRef) *SafeProvider {
    return &SafeProvider{inner: inner, fwRef: ref}
}

func (p *SafeProvider) Stream(ctx context.Context, req provider.Request) (stream.Stream, error) {
    if fw := p.fwRef.Get(); fw != nil {
        req.Messages = fw.ScanOutgoingMessages(req.Messages)
        req.SystemPrompt = fw.ScanSystemPrompt(req.SystemPrompt)
    }
    return p.inner.Stream(ctx, req)
}

func (p *SafeProvider) Name() string         { return p.inner.Name() }
func (p *SafeProvider) DefaultModel() string { return p.inner.DefaultModel() }
func (p *SafeProvider) Models(ctx context.Context) ([]provider.ModelInfo, error) {
    return p.inner.Models(ctx)
}
```

`SafeProvider` lives in `internal/security/` because it consumes
`*Firewall`. Importing `security` from `provider` would create a
cycle; the wrapper lives below both. Engine consumers always already
import `security`.

### Engine reconciliation

Once `SafeProvider` runs on every arm, `engine.buildRequest()` does
redundant scanning. Options:

1. **Leave it.** Two scans cost the same as one in steady state
   (redaction is idempotent — second pass finds nothing). Slight
   defensive belt-and-suspenders.
2. **Remove it.** Simpler. The decorator becomes the sole boundary.
   Engine tests need updating; some currently rely on Firewall being
   on `engine.Config`.

**Recommendation: leave it for one release**, then revisit after we've
seen real telemetry that nothing regresses. The cost is two regex
passes per message; not material vs. provider latency.

---

## Tasks

### W1-1 — SafeProvider + FirewallRef

- [ ] `internal/security/saferef.go` — `FirewallRef` with
  `atomic.Pointer[Firewall]` semantics.
- [ ] `internal/security/safeprovider.go` — `SafeProvider` decorator
  implementing `provider.Provider`. Wraps `Stream`; delegates the rest.
- [ ] Unit tests in `internal/security/safeprovider_test.go`:
  - ref unset → delegates without scanning (recording fake provider
    asserts unmodified request)
  - ref set → outgoing messages and system prompt scanned
  - tool-call args with embedded API key are redacted on outgoing
    path (already covered by `ScanOutgoingMessages` but verify via
    the wrapper)
  - `Name()`, `Models()`, `DefaultModel()` pass through unchanged

### W1-2 — Wire ref into main

- [ ] `cmd/gnoma/main.go` — construct `security.FirewallRef` early,
  before any provider construction. Pass to every site that builds
  a `provider.Provider`.
- [ ] Existing `fw := security.NewFirewall(...)` call site stays where
  it is; immediately call `fwRef.Set(fw)` after construction.

### W1-3 — Apply to all provider sites

- [ ] Primary provider: wrap return from `createProvider()` (or wrap
  at the call site in `main.go` around line 395 — `armProvider`).
- [ ] Discovered local models: the factory in
  `router.RegisterDiscoveredModels(...)` at `main.go:421-427` returns
  the inner provider; wrap before returning.
- [ ] Background-discovery factory at `main.go:479-485` — same.
- [ ] CLI agents: `subprocprov.New(agent)` at `main.go:438` — wrap
  before passing to `RegisterArm`.
- [ ] SLM classifier: wrap the provider passed into
  `slm.NewClassifier(...)`. Site is wherever the SLM manager builds
  the classifier (likely `internal/slm/manager.go` or
  `cmd/gnoma/main.go`).
- [ ] Summarizer: wrap the provider passed into
  `gnomactx.NewSummarizeStrategy(...)` at `main.go:703`.
- [ ] Hook streamer: wrap the provider passed into the hook system
  for prompt-type hooks (origin site in `cmd/gnoma/main.go` near
  router init).
- [ ] `routerStreamer`: doesn't take a `provider.Provider` directly —
  it takes a `*router.Router`. Once all arms are wrapped, this site
  inherits the fix. Verify by reading `Router.Stream`'s call into
  `decision.Arm.Provider.Stream(...)` at `internal/router/router.go:319`.

### W1-4 — Tests

- [ ] Integration test: `internal/security/safeprovider_test.go` adds
  a recording fake `Provider` and verifies redaction is applied for
  each of the four bypass paths (SLM, summarizer, hook, router).
  Mock data: a request whose user message contains an
  `ANTHROPIC_API_KEY=sk-ant-...` literal; assert the inner provider
  sees the redacted form.
- [ ] Confirm `make test` is green across the security, slm, context,
  hook, and router packages.

### W1-5 — Docs

- [ ] Update `docs/essentials/decisions/` with a new ADR
  (`004-safe-provider-boundary.md` or next index) capturing:
  - why scanning moved to the provider boundary
  - the FirewallRef late-binding pattern
  - the explicit decision to keep engine-level scanning for one
    release
- [ ] Update `docs/essentials/INDEX.md` to reference the new ADR.

---

## Exit criteria

- Every provider arm registered in `router.Router` is a `SafeProvider`.
  Verified by a startup-time assertion or test that iterates
  `rtr.LookupArm` for each registered ID and type-asserts the
  `Provider` field.
- A request whose user message contains a secret-shaped string
  (`sk-ant-...`) is redacted before reaching the inner provider for
  all four bypass paths (SLM classifier, summarizer, prompt hook,
  router stream).
- `make test` and `make lint` green.
- No change to public `provider.Provider` interface.

---

## Out of scope (deferred to Wave 2 / Wave 3)

- Forced-arm + incognito local-only collision
  (`router.go:67-73`) — Wave 2.
- Unconditional `persist.New(sessionID)` in incognito — Wave 2.
- TUI `m.incognito` not initialized from `fw.Incognito().Active()`
  — Wave 2.
- `saveQuality()` / `ReportOutcome()` gating on CLI flag instead of
  firewall state — Wave 2.
- PEM block regex completion — Wave 3.
- Optional strict high-entropy mode for non-local arms — Wave 3.
- Canonical `AllowedPaths` (Clean+Prefix → Abs+EvalSymlinks) — Wave 3.
- MCP `PathSensitiveTool` policy hook — Wave 3.
- `fs.grep` per-file symlink resolution — Wave 3.
- PostToolUse hook ordering vs. redaction — open question, see
  "Tool-result hook ordering" below.

### Tool-result hook ordering — open question

The audit's finding #4 also covers PostToolUse hooks seeing raw tool
output before `ScanToolResult` runs. Wave 1 doesn't fix this because:

- The audit's preferred fix ("redact, then fire hook") would change
  the contract for command-type hooks (e.g. an audit-logger hook that
  wants to record raw output). Splitting hooks into
  `PostToolUseRawLocalOnly` vs. `PostToolUseRedactedForLLM` is a
  design decision, not a refactor.
- Mitigation already exists: tool output written to the engine's
  message history *is* redacted; the hook leak is only for prompt
  hooks that re-inject hook-supplied content into an LLM. Today, no
  shipped hook does this with raw tool output.

Tracked separately. If/when a prompt-type PostToolUse hook ships, the
split-contract design must land first.

---

## Effort estimate

- W1-1: ~80 LOC + ~120 LOC tests.
- W1-2: ~20 LOC.
- W1-3: ~50 LOC across 5 call sites.
- W1-4: ~150 LOC tests.
- W1-5: ~80 lines of ADR.

Total: ~500 LOC including tests. One PR or two, at your call.

---

## Changelog

- 2026-05-19: Initial. Captures Wave 1 of the post-audit hardening plan.
