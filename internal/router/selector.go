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
	Strategy Strategy
	Arm      *Arm   // primary arm
	Error    error
}

// selectBest picks the highest-scoring feasible arm using heuristic scoring.
// No bandit learning — that's M9. Just smart defaults based on model size,
// locality, task type, cost, and pool scarcity.
func selectBest(arms []*Arm, task Task) *Arm {
	if len(arms) == 0 {
		return nil
	}

	var best *Arm
	bestScore := math.Inf(-1)

	for _, arm := range arms {
		score := scoreArm(arm, task)
		if score > bestScore {
			bestScore = score
			best = arm
		}
	}

	return best
}

// scoreArm computes a heuristic quality/cost score for an arm.
// Score = (quality × value) / effective_cost
func scoreArm(arm *Arm, task Task) float64 {
	quality := heuristicQuality(arm, task)
	value := task.ValueScore()
	cost := effectiveCost(arm, task)

	if cost <= 0 {
		cost = 0.001 // prevent division by zero for free local models
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
	if arm.Capabilities.Thinking {
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

// filterFeasible returns arms that can handle the task (tools, pool capacity).
func filterFeasible(arms []*Arm, task Task) []*Arm {
	var feasible []*Arm
	for _, arm := range arms {
		// Must support tools if task requires them
		if task.RequiresTools && !arm.SupportsTools() {
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

		feasible = append(feasible, arm)
	}

	// If no arm with tools is feasible but task requires them,
	// fall back to any available arm (tool-less is better than nothing)
	if len(feasible) == 0 && task.RequiresTools {
		for _, arm := range arms {
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
