# Router Benchmarks

Tracking how gnoma's multi-armed bandit router (M4 heuristic, M9 bandit) performs across providers, task types, and cost envelopes.

## Methodology

Each benchmark run:

1. Registers a set of arms (provider/model pairs) with known cost profiles
2. Generates synthetic tasks across all 10 task types with varying complexity
3. Runs N routing decisions and records: arm selected, latency, quality score, cost
4. Reports convergence metrics after simulated quality feedback

## Metrics

| Metric | Description |
|--------|-------------|
| **Selection accuracy** | % of tasks routed to the optimal arm (vs. oracle with perfect knowledge) |
| **Cost efficiency** | Total cost relative to always-cheapest and always-best-quality baselines |
| **Convergence speed** | Observations needed before bandit matches heuristic on quality (M9) |
| **Pool utilization** | % of rate limit budget consumed before exhaustion |
| **Latency overhead** | Time spent in Select() excluding provider round-trip |

## Running

```sh
# Go benchmarks (in-process, no real API calls)
go test -bench=. -benchmem ./internal/router/

# Synthetic routing simulation (when available)
go run ./cmd/gnoma-bench/ --arms=5 --tasks=1000 --seed=42
```

## Results (M4 heuristic, 2026-04-12)

5 arms (Sonnet, Opus, GPT-4o, Qwen3:8b local, Mistral Large), 10 task types. AMD Ryzen 7 3700X.

```
BenchmarkScoreArm-16                   3046383     392.5 ns/op     0 B/op    0 allocs/op
BenchmarkSelectBest-16                   276529      4347 ns/op     0 B/op    0 allocs/op
BenchmarkFilterFeasible-16              1200006      1003 ns/op   504 B/op   10 allocs/op
BenchmarkRouterSelect-16                 177916      6794 ns/op  1224 B/op   40 allocs/op
BenchmarkRouterSelectWithQuality-16      152180      7885 ns/op  1224 B/op   40 allocs/op
BenchmarkClassifyTask-16                 122278      9780 ns/op  1536 B/op   14 allocs/op
```

Key observations:
- `ScoreArm` is zero-alloc at ~400ns — good headroom for M9 bandit sampling overhead
- Full `Select` (filter + score + pool reserve + commit) is ~7us per routing decision
- Quality tracker adds ~1us overhead (7.9us vs 6.8us) — acceptable for EMA lookups
- `ClassifyTask` at ~10us is dominated by `strings.Contains` keyword matching; a trie or compiled regex could reduce this if it becomes a bottleneck, but for per-request overhead it's negligible

### Planned comparisons (M9)

- Heuristic-only (M4) vs. bandit (M9) after 50, 200, 1000 observations
- 2-arm (local + cloud) vs. 5-arm (mixed providers) scenarios
- Cost-capped routing: $5/day budget with mixed task load
- Quality degradation under rate limit pressure (pool scarcity)
