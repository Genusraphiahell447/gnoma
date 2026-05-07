package router

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"somegit.dev/Owlibou/gnoma/internal/provider"
	"somegit.dev/Owlibou/gnoma/internal/stream"
)

// Router selects the best arm for a given task.
// M4: heuristic selection. M9: bandit learning.
type Router struct {
	mu     sync.RWMutex
	arms   map[ArmID]*Arm
	logger *slog.Logger

	// Optional: force a specific arm (--provider flag override)
	forcedArm ArmID
	// When true, only local arms are considered (incognito mode)
	localOnly bool

	quality *QualityTracker
}

type Config struct {
	Logger *slog.Logger
}

func New(cfg Config) *Router {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Router{
		arms:    make(map[ArmID]*Arm),
		logger:  logger,
		quality: NewQualityTracker(),
	}
}

// RegisterArm adds an arm to the router.
func (r *Router) RegisterArm(arm *Arm) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.arms[arm.ID] = arm
	r.logger.Debug("arm registered", "id", arm.ID, "local", arm.IsLocal, "cli", arm.IsCLIAgent, "tools", arm.SupportsTools(), "disabled", arm.Disabled)
}

// ForceArm overrides routing to always select a specific arm.
// Used for --provider CLI flag.
func (r *Router) ForceArm(id ArmID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.forcedArm = id
}

// Select picks the best arm for the given task.
func (r *Router) Select(task Task) RoutingDecision {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// If an arm is forced, use it directly
	if r.forcedArm != "" {
		arm, ok := r.arms[r.forcedArm]
		if !ok {
			return RoutingDecision{Error: fmt.Errorf("forced arm %q not found", r.forcedArm)}
		}
		return RoutingDecision{Strategy: StrategySingleArm, Arm: arm}
	}

	// Collect all arms (excluding disabled, excluded, in-backoff, and local-only mismatches)
	allArms := make([]*Arm, 0, len(r.arms))
	for _, arm := range r.arms {
		if arm.Disabled || arm.InBackoff() {
			continue
		}
		if r.localOnly && !arm.IsLocal {
			continue
		}
		
		isExcluded := false
		for _, ex := range task.ExcludedArms {
			if arm.ID == ex {
				isExcluded = true
				break
			}
		}
		if isExcluded {
			continue
		}

		allArms = append(allArms, arm)
	}

	if len(allArms) == 0 {
		return RoutingDecision{Error: fmt.Errorf("no arms registered")}
	}

	// Filter to feasible arms
	feasible := filterFeasible(allArms, task)
	if len(feasible) == 0 {
		return RoutingDecision{Error: fmt.Errorf("no feasible arm for task type %s", task.Type)}
	}

	// Select best
	best := selectBest(r.quality, feasible, task)
	if best == nil {
		return RoutingDecision{Error: fmt.Errorf("selection failed")}
	}

	// Reserve capacity on all pools so concurrent selects don't overcommit.
	// If a reservation fails (race between CanAfford and Reserve), return an error.
	var reservations []*Reservation
	for _, pool := range best.Pools {
		res, ok := pool.Reserve(best.ID, task.EstimatedTokens)
		if !ok {
			for _, prev := range reservations {
				prev.Rollback()
			}
			return RoutingDecision{Error: fmt.Errorf("pool capacity exhausted for arm %s", best.ID)}
		}
		reservations = append(reservations, res)
	}

	r.logger.Debug("arm selected",
		"arm", best.ID,
		"task_type", task.Type,
		"complexity", task.ComplexityScore,
	)

	return RoutingDecision{Strategy: StrategySingleArm, Arm: best, reservations: reservations}
}

// ForcedArm returns the currently forced arm ID, or "" if none.
func (r *Router) ForcedArm() ArmID {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.forcedArm
}

// reconcileForcedArm atomically renames the forced arm from oldID to newID.
// The arm's Provider, Pools, and other fields are preserved; only ID, ModelName,
// and the forcedArm pointer are updated.
func (r *Router) reconcileForcedArm(oldID, newID ArmID, newModelName string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	arm, ok := r.arms[oldID]
	if !ok {
		return
	}
	arm.ID = newID
	arm.ModelName = newModelName
	delete(r.arms, oldID)
	r.arms[newID] = arm
	r.forcedArm = newID
}

// SetLocalOnly constrains routing to local arms only (for incognito mode).
func (r *Router) SetLocalOnly(v bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.localOnly = v
}

// LocalOnly returns whether routing is constrained to local arms.
func (r *Router) LocalOnly() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.localOnly
}

// RemoveArm removes an arm from the router.
func (r *Router) RemoveArm(id ArmID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.arms, id)
}

// Outcome records the result of a task execution for quality feedback.
type Outcome struct {
	ArmID           ArmID
	TaskType        TaskType
	Success         bool
	Tokens          int
	Duration        time.Duration
	ResultFilePaths []string // paths to /tmp tool result files (for M9 analysis)
}

// ReportOutcome records a task execution result for quality tracking.
func (r *Router) ReportOutcome(o Outcome) {
	r.quality.Record(o.ArmID, o.TaskType, o.Success)
	r.logger.Debug("outcome recorded",
		"arm", o.ArmID,
		"task", o.TaskType,
		"success", o.Success,
		"tokens", o.Tokens,
		"duration", o.Duration,
		"result_files", len(o.ResultFilePaths),
	)
}

// LookupArm returns the arm with the given ID, if registered.
func (r *Router) LookupArm(id ArmID) (*Arm, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	arm, ok := r.arms[id]
	return arm, ok
}

// QualityTracker returns the router's quality tracker for persistence.
func (r *Router) QualityTracker() *QualityTracker {
	return r.quality
}

// Arms returns all registered arms.
func (r *Router) Arms() []*Arm {
	r.mu.RLock()
	defer r.mu.RUnlock()
	arms := make([]*Arm, 0, len(r.arms))
	for _, a := range r.arms {
		arms = append(arms, a)
	}
	return arms
}

// RegisterProvider registers all models from a provider as arms.
func (r *Router) RegisterProvider(ctx context.Context, prov provider.Provider, isLocal bool, costs map[string][2]float64) {
	models, err := prov.Models(ctx)
	if err != nil {
		r.logger.Debug("failed to list models", "provider", prov.Name(), "error", err)
		// Register at least the default model
		id := NewArmID(prov.Name(), prov.DefaultModel())
		r.RegisterArm(&Arm{
			ID:        id,
			Provider:  prov,
			ModelName: prov.DefaultModel(),
			IsLocal:   isLocal,
			Capabilities: provider.Capabilities{ToolUse: true}, // optimistic
		})
		return
	}

	for _, m := range models {
		id := NewArmID(prov.Name(), m.ID)
		arm := &Arm{
			ID:           id,
			Provider:     prov,
			ModelName:    m.ID,
			IsLocal:      isLocal,
			Capabilities: m.Capabilities,
		}
		if c, ok := costs[m.ID]; ok {
			arm.CostPer1kInput = c[0]
			arm.CostPer1kOutput = c[1]
		}
		r.RegisterArm(arm)
	}
}

// Stream selects an arm and streams from it, returning the RoutingDecision so the
// caller can commit or rollback pool reservations when the request completes.
// Call decision.Commit(actualTokens) on success, decision.Rollback() on failure.
func (r *Router) Stream(ctx context.Context, task Task, req provider.Request) (stream.Stream, RoutingDecision, error) {
	decision := r.Select(task)
	if decision.Error != nil {
		return nil, decision, decision.Error
	}

	req.Model = decision.Arm.ModelName

	s, err := decision.Arm.Provider.Stream(ctx, req)
	if err != nil {
		decision.Rollback()
		return nil, decision, err
	}
	return s, decision, nil
}

// Backoff temporarily disables an arm (e.g. after a 429 error).
func (r *Router) Backoff(id ArmID, duration time.Duration) {
	r.mu.RLock()
	arm, ok := r.arms[id]
	r.mu.RUnlock()
	if ok {
		arm.SetBackoff(time.Now().Add(duration))
	}
}
