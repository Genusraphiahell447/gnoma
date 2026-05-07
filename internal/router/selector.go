package router

import (
	"math"
)

// Strategy identifies how a task should be executed.
type Strategy int

const (
	StrategySingleArm Strategy = iota
	// Future (M9): StrategyCascade, StrategyParallelEnsemble, StrategyMultiRound
)

// RoutingDecision is the result of arm selection.
type RoutingDecision struct {
	Strategy     Strategy
	Arm          *Arm // primary arm
	Error        error
	reservations []*Reservation // pool reservations held until commit/rollback
}

// Commit finalizes the routing decision, recording actual token consumption.
// Must be called when the request completes successfully.
func (d RoutingDecision) Commit(actualTokens int) {
	for _, r := range d.reservations {
		r.Commit(actualTokens)
	}
}

// Rollback releases the routing decision's pool reservations without recording usage.
// Must be called when the request fails before any tokens are consumed.
func (d RoutingDecision) Rollback() {
	for _, r := range d.reservations {
		r.Rollback()
	}
}

// armTier returns the routing tier for an arm.
// Lower tier = higher preference: 0=CLI agent, 1=local model, 2=API provider.
func armTier(arm *Arm) int {
	if arm.IsCLIAgent {
		return 0
	}
	if arm.IsLocal {
		return 1
	}
	return 2
}

// selectBest picks the best arm, preferring lower-tier arms first.
// Within a tier, the highest-scoring arm (by quality/cost) wins.
func selectBest(qt *QualityTracker, arms []*Arm, task Task) *Arm {
	if len(arms) == 0 {
		return nil
	}

	for tier := 0; tier <= 2; tier++ {
		var inTier []*Arm
		for _, arm := range arms {
			if armTier(arm) == tier {
				inTier = append(inTier, arm)
			}
		}
		if len(inTier) > 0 {
			return bestScored(qt, inTier, task)
		}
	}
	return nil
}

// bestScored returns the highest-scoring arm within a set.
func bestScored(qt *QualityTracker, arms []*Arm, task Task) *Arm {
	var best *Arm
	bestScore := math.Inf(-1)
	for _, arm := range arms {
		score := scoreArm(qt, arm, task)
		if score > bestScore {
			bestScore = score
			best = arm
		}
	}
	return best
}

// scoreArm computes a quality/cost score for an arm.
// When the quality tracker has sufficient observations, blends observed EMA
// (70%) with heuristic (30%). Falls back to pure heuristic otherwise.
// Score = (quality × value) / effective_cost
func scoreArm(qt *QualityTracker, arm *Arm, task Task) float64 {
	hq := heuristicQuality(arm, task)
	quality := hq
	if qt != nil {
		if observed, hasData := qt.Quality(arm.ID, task.Type); hasData {
			quality = 0.7*observed + 0.3*hq
		}
	}
	value := task.ValueScore()
	cost := effectiveCost(arm, task)
	if cost <= 0 {
		cost = 0.001
	}
	return (quality * value) / cost
}

// heuristicQuality estimates arm quality without historical data.
func heuristicQuality(arm *Arm, task Task) float64 {
	score := 0.5 // base

	// Larger context window = better for complex tasks
	if arm.Capabilities.ContextWindow >= 100000 {
		score += 0.1
	}
	if arm.Capabilities.ContextWindow >= 200000 {
		score += 0.05
	}

	// Thinking capability valuable for planning/orchestration/security
	if arm.Capabilities.SupportsThinking() {
		switch task.Type {
		case TaskPlanning, TaskOrchestration, TaskSecurityReview:
			score += 0.2
		case TaskDebug, TaskRefactor:
			score += 0.1
		}
	}

	// Tool support required — arm without tools gets heavy penalty
	if task.RequiresTools && !arm.SupportsTools() {
		score *= 0.1
	}

	// Local models get a small boost (no network latency, privacy)
	if arm.IsLocal {
		score += 0.05
	}

	// Complexity adjustment — complex tasks penalize small/local models
	if task.ComplexityScore > 0.7 && arm.IsLocal {
		score *= 0.7
	}

	// Clamp
	if score > 1.0 {
		score = 1.0
	}
	if score < 0.0 {
		score = 0.0
	}
	return score
}

// effectiveCost returns the base cost inflated by pool scarcity.
func effectiveCost(arm *Arm, task Task) float64 {
	base := arm.EstimateCost(task.EstimatedTokens)
	if base <= 0 {
		base = 0.001 // local models are ~free but not zero for scoring
	}

	// Apply maximum scarcity multiplier across all pools
	maxMultiplier := 1.0
	for _, pool := range arm.Pools {
		m := pool.ScarcityMultiplier()
		if m > maxMultiplier {
			maxMultiplier = m
		}
	}

	return base * maxMultiplier
}

// filterFeasible returns arms that can handle the task (tools, pool capacity, quality).
// Arms that pass tool and pool checks but fall below the task's minimum quality threshold
// are collected separately and used as a last resort if no arm meets the threshold.
func filterFeasible(arms []*Arm, task Task) []*Arm {
	threshold := DefaultThresholds[task.Type]

	var feasible []*Arm
	var belowQuality []*Arm // passed tool+pool but scored below minimum quality

	for _, arm := range arms {
		// Complexity ceiling: zero means no ceiling (preserves behavior for all existing arms).
		if arm.MaxComplexity > 0 && task.ComplexityScore > arm.MaxComplexity {
			continue
		}

		// Must support tools if task requires them
		if task.RequiresTools && !arm.SupportsTools() {
			continue
		}

		// Must support the required effort level (EffortAuto always passes)
		if !arm.Capabilities.SupportsEffort(task.RequiredEffort) {
			continue
		}

		// Check all pools have capacity
		poolsOK := true
		for _, pool := range arm.Pools {
			pool.CheckReset()
			if !pool.CanAfford(arm.ID, task.EstimatedTokens) {
				poolsOK = false
				break
			}
		}
		if !poolsOK {
			continue
		}

		// Quality floor: arms below minimum are set aside, not discarded
		if heuristicQuality(arm, task) < threshold.Minimum {
			belowQuality = append(belowQuality, arm)
			continue
		}

		feasible = append(feasible, arm)
	}

	// Degrade gracefully: if no arm meets quality threshold, use below-quality ones
	if len(feasible) == 0 && len(belowQuality) > 0 {
		return belowQuality
	}

	// If still empty and task requires tools, relax pool checks (last resort)
	if len(feasible) == 0 && task.RequiresTools {
		for _, arm := range arms {
			if !arm.Capabilities.ToolUse {
				continue
			}
			poolsOK := true
			for _, pool := range arm.Pools {
				if !pool.CanAfford(arm.ID, task.EstimatedTokens) {
					poolsOK = false
					break
				}
			}
			if poolsOK {
				feasible = append(feasible, arm)
			}
		}
	}

	return feasible
}
