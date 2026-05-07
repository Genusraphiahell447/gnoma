# ADR-013: SLM-First Routing (Supersedes ADR-009)

**Status:** Accepted
**Date:** 2026-05-07
**Supersedes:** ADR-009 (Thompson Sampling for Multi-Armed Bandit)

## Context

ADR-009 committed to Discounted Thompson Sampling as the primary routing intelligence for M9.
That decision was made before SLM-driven task classification was on the roadmap.

With an SLM classifier (Phase 3 of the 2026-05-07 roadmap) as the preflight dispatcher, the
routing stack changes fundamentally:

- The SLM has richer context than a Beta(α, β) distribution: task semantics, conversation
  history, model performance history, and cost envelope are all available at classification time.
- A bandit running in parallel with an SLM introduces competing signals — the bandit's numeric
  EMA score may contradict the SLM's intent classification without a clear resolution rule.
- The current heuristic tier order (CLI > local > API) in `armTier()` is a pragmatic placeholder
  that the SLM can supersede with semantically-grounded decisions.

## Decision

1. **SLM classifier first.** Implement a `TaskClassifier` interface with two implementations:
   `HeuristicClassifier` (default, wraps existing `ClassifyTask()`) and `SLMClassifier` (Ollama
   HTTP via the existing `openaicompat` provider, opt-in via `router.slm_model` config key).

2. **Defer Thompson Sampling.** ADR-009's Thompson Sampling plan is deferred. It will be
   re-evaluated after the SLM classifier has been in production and generating real signals.
   The decision at that point will be one of:
   - Keep bandit as a feedback loop the SLM reads (outcome telemetry → SLM context)
   - Retire numeric EMA in favour of qualitative outcome summaries fed to the SLM
   - Keep both with explicit, non-overlapping responsibilities

3. **Go implementation constraint.** The SLM runtime must respect `CGO_ENABLED=0`. LiteRT-LM,
   daemon processes, and CGO bindings are out of scope. Ollama HTTP is the only supported
   runtime — it is an opt-in user dependency, not a bundled one.

## Alternatives Considered

### Alternative A: Implement ADR-009 as planned, add SLM later

- **Pros:** Incremental; Thompson Sampling is already designed
- **Cons:** Two competing learning signals from day one. Unclear which wins on conflict.
  Wasted implementation effort if Thompson Sampling is retired post-SLM.

### Alternative B: Full SLM takeover — retire bandit entirely

- **Pros:** Single source of routing truth
- **Cons:** Premature. SLM quality is unknown before production data. Bandit may still add value
  for cost/quality feedback within a tier where the SLM doesn't have preferences.

### Alternative C: This ADR — SLM first, bandit deferred and re-evaluated

- **Pros:** No competing signals. Real data informs the bandit re-evaluation. Implementation
  effort is not wasted on a system that may be retired.
- **Cons:** Thompson Sampling (ADR-009) ships later than originally planned.

## Consequences

**Positive:**
- Clean routing signal: one classifier, no conflicts
- SLM produces semantic task types that heuristic scoring cannot match
- Zero new runtime deps for users who don't have Ollama (heuristic fallback always active)
- `CGO_ENABLED=0` constraint preserved

**Negative:**
- ADR-009 (Thompson Sampling) ships later, or not at all
- Users without Ollama get heuristic routing only (same as today)
- SLM quality depends on the model available in the user's Ollama installation

**Neutral:**
- `QualityTracker` / EMA in `feedback.go` remains as infrastructure; no behavior change until
  Phase 4 re-evaluation decides its fate
