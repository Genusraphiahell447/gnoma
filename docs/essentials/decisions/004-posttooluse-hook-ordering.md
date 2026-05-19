# ADR-004: PostToolUse Hook Ordering vs. Firewall Redaction

**Status:** Accepted
**Date:** 2026-05-19

## Context

The 2026-05-19 external security audit raised this concern as a "High" finding:

> Ablauf in `internal/engine/loop.go`:
>   tool executes → PostToolUse hook bekommt raw output → danach `Firewall.ScanToolResult()`
>
> Das heißt: Ein `type = "prompt"` Hook kann raw Tool-Output in ein LLM schicken,
> bevor Redaction passiert.

In source order, the relevant code is at `internal/engine/loop.go:699-712`:

```go
// PostToolUse hook: can transform result (Deny treated as Skip).
output := result.Output
if e.cfg.Hooks != nil {
    payload := hook.MarshalPostToolPayload(call.Name, args, output, result.Metadata)
    transformed, _, _ := e.cfg.Hooks.Fire(hook.PostToolUse, payload)
    if s := hook.ExtractTransformedOutput(transformed); s != "" {
        output = s
    }
}

// Scan tool result through firewall
if e.cfg.Firewall != nil {
    output = e.cfg.Firewall.ScanToolResult(output)
}
```

The audit's literal observation is correct: the hook receives `result.Output` before
`Firewall.ScanToolResult` runs. The audit's preferred fix was either:

- **Reorder:** scan first, then fire the hook with redacted input.
- **Split contract:** introduce `PostToolUseRawLocalOnly` (shell hooks only) and
  `PostToolUseRedactedForLLM` (prompt/agent hooks only) as distinct events.

Either fix is non-trivial. The reorder regresses legitimate shell-hook use cases
that need raw access (auditing, log forwarding, transformation). The split is a
contract change that requires migrating every existing hook config.

Before adopting either, we re-examined whether the leak path is actually open
after Wave 1 (SafeProvider boundary) merged.

### What Wave 1 changed

The audit was performed against `main(2).zip`, which did not include the
SafeProvider boundary. With Wave 1 (ADR-pending; PR #1 merged), every
`provider.Provider` registered with the router — and every provider handed to a
non-engine consumer (SLM classifier, summarizer, hook streamer) — is wrapped in
`security.SafeProvider`. The wrapper scans outgoing messages and the system
prompt through the firewall before delegating to the inner provider.

This affects PostToolUse hooks as follows, by hook type:

- **`type = "command"` (shell):** runs a local subprocess. No LLM round-trip.
  The hook sees raw output, but the output stays local. The audit's threat
  model (raw output → cloud LLM) doesn't apply.
- **`type = "prompt"`:** uses `hook.PromptExecutor`, which calls
  `p.streamer.Stream(...)`. In production, `streamer` is the
  `routerStreamer` adapter from `cmd/gnoma/main.go`, which delegates to
  `router.Router.Stream(...)`. The router selects an arm; each arm's `Provider`
  is a `*SafeProvider` after Wave 1. Outbound messages — including the
  rendered template containing `{{.Result}}` — are scanned at the SafeProvider
  boundary before the inner provider sees them.
- **`type = "agent"`:** uses `hook.AgentExecutor`, which spawns an elf via
  `elf.Manager`. The elf engine is constructed with `Firewall: m.firewall`, so
  the elf's `buildRequest()` scans inline. Same effect as the main engine's
  request path.

So the leak path — *raw tool output reaching an LLM* — is already closed
transitively by Wave 1 for both LLM-bound hook types. The audit's literal
observation about source ordering remains true; the practical leak it implied
does not.

### What's left

Two residual concerns survive the transitive guarantee:

1. **Output transformation produces unscannable downstream payloads.** A
   PostToolUse hook (any type) may transform raw output into a form that
   defeats regex-based scanning — e.g. base64-encoding, JSON-wrapping with
   reformulated content, summarisation via LLM that drops the secret-shaped
   string. The firewall scan at line 711 runs on the transformed payload,
   which may no longer match patterns the raw output would have. This is
   *user-configured behaviour* (they chose to add the hook), but it's worth
   acknowledging as a known limit.
2. **Transitive safety is non-obvious.** The "leak is closed" property
   depends on every LLM-bound hook path going through SafeProvider. A future
   change that adds a new hook type with its own LLM transport, or replaces
   the `routerStreamer` adapter with something that bypasses the router,
   would silently reopen the leak. There is no compile-time or test-time
   guarantee that this property holds.

## Decision

**Accept the current ordering. Document the transitive guarantee.**

Concretely:

1. **No reorder, no split.** The PostToolUse contract stays: hooks see raw
   `result.Output`; the firewall scan runs after hook transformation.
2. **Add a doc comment** to `hook.PostToolUse` in
   `internal/hook/event.go` and to the dispatcher call site in
   `internal/engine/loop.go` making the transitive guarantee explicit:
   *"PostToolUse hooks receive pre-scan output. LLM-bound paths
   (prompt/agent) inherit firewall scanning at the SafeProvider boundary;
   shell hooks stay local. Any new hook type that talks to an LLM
   must route through SafeProvider or perform its own scan."*
3. **Add a regression test** that asserts a `type = "prompt"` PostToolUse
   hook firing on a tool result containing a known secret-shaped string
   does not deliver that string verbatim to an inner provider. The test
   builds a recording fake provider, registers it via a router arm wrapped
   in SafeProvider, configures a prompt-type PostToolUse hook that
   includes `{{.Result}}` in its template, fires the hook, and asserts
   the recorded request was redacted.

### Why not reorder?

Reorder eliminates Concern 2 cheaply, but it regresses Concern 1's mirror
case: shell hooks that need raw output for legitimate local-only purposes.
Examples we want to preserve:

- Audit-log hooks that record exact bash output for later inspection.
- Forensic hooks that hash raw output before any transformation.
- Local-only alerting (e.g. fire pagerduty when a specific stderr pattern
  appears in a build hook) that needs the unredacted signal.

A redacted payload changes byte offsets, regex matches, and content size —
all of which break legitimate shell-hook code that's not LLM-bound.

### Why not split the contract?

A split (`PostToolUseRaw` vs. `PostToolUseRedacted`) is technically clean
but introduces a migration cost out of proportion to the residual risk.
Every existing hook config has to be re-categorised. The split also
encourages a footgun: a hook author who picks the wrong variant gets
either unsafe behaviour or broken transformation. The audit-flagged risk
doesn't justify that tax against an already-closed leak path.

### When to revisit

Re-open this decision if any of the following happen:

- A new hook type lands that performs LLM round-trips outside the router
  (e.g. a hook that calls a vendor SDK directly). At that point the
  transitive safety argument breaks and Position D (dispatcher-level
  redaction for LLM-bound paths) becomes necessary.
- The SafeProvider boundary is removed or relaxed (Wave 1 plan flagged
  this as a possible one-release follow-up; if engine-level scanning is
  removed AND a future change weakens SafeProvider, the transitive chain
  loses its second link).
- Telemetry shows that a meaningful number of users configure prompt-type
  PostToolUse hooks. The split contract's migration cost becomes
  proportional to the population it protects.
- A real-world incident proves Concern 1 is exploitable — e.g. a
  user-configured transformation that the firewall fails to scan.

## Alternatives Considered

### Alternative A: Reorder (scan before hook)

- **Pros:** Defense in depth at the hook boundary; eliminates the
  "transitive safety is non-obvious" concern.
- **Cons:** Breaks shell hooks that need raw output. Redacts content
  that may never leave the local machine.

### Alternative B: Split contract
(`PostToolUseRawLocalOnly` + `PostToolUseRedactedForLLM`)

- **Pros:** Each hook type sees exactly what it should. Compile-time
  clarity for hook authors.
- **Cons:** Migration cost for every existing config. New footgun
  (wrong variant chosen → wrong semantics). Two near-identical event
  types in the API surface.

### Alternative C: Dispatcher routes by command type

- **Pros:** Single event, transparent split. Hook authors don't pick.
- **Cons:** Dispatcher needs access to the firewall (new dependency).
  Per-handler redaction means firing the hook chain multiple times
  with different payloads, or pre-computing both raw and scanned
  variants. Increases dispatcher complexity meaningfully.

### Alternative D: Accept current ordering, document, test
(this ADR)

- **Pros:** No code reorder. No migration. Acknowledges the existing
  transitive guarantee from Wave 1. Closes the "non-obvious" concern
  via doc + regression test.
- **Cons:** Concern 1 (transformation defeats scanning) remains as a
  user-configured behaviour. Future hook types that bypass the router
  silently reopen the leak — the regression test catches one shape
  but not all shapes.

## Consequences

**Positive:**
- No churn to the hook contract or existing configs.
- Existing shell-hook use cases (audit, forensic, local alert) keep
  raw access.
- Regression test makes the transitive guarantee load-bearing in CI.

**Negative:**
- The transitive guarantee is documented but not type-enforced. A
  motivated change can still reopen the leak.
- Output-transforming hooks can produce unscannable downstream
  payloads. Same as today; no improvement.

**Neutral:**
- The audit finding is closed by reframing, not by code change.
  This ADR is the artifact that records why.

## Implementation

Three small changes land with this ADR:

1. `internal/hook/event.go` — extend the `PostToolUse` constant's
   doc comment with the transitive-guarantee statement.
2. `internal/engine/loop.go:699` — extend the inline comment above
   the hook fire to point at this ADR.
3. `internal/hook/posttooluse_redaction_test.go` (new) — regression
   test as described in the Decision section.

Total: ~80 LOC including the test.
