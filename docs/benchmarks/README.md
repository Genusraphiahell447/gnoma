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

## Results

No benchmark results yet. This scaffold will be populated as M9 (Router Advanced) lands.

### Planned comparisons

- Heuristic-only (M4) vs. bandit (M9) after 50, 200, 1000 observations
- 2-arm (local + cloud) vs. 5-arm (mixed providers) scenarios
- Cost-capped routing: $5/day budget with mixed task load
- Quality degradation under rate limit pressure (pool scarcity)
