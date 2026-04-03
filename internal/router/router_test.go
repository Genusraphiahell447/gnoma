package router

import (
	"math"
	"testing"
	"time"

	"somegit.dev/Owlibou/gnoma/internal/provider"
)

// --- Task Classification ---

func TestClassifyTask(t *testing.T) {
	tests := []struct {
		prompt string
		want   TaskType
	}{
		{"fix the bug in auth module", TaskDebug},
		{"review this pull request", TaskReview},
		{"refactor the database layer", TaskRefactor},
		{"write unit tests for the handler", TaskUnitTest},
		{"explain how the router works", TaskExplain},
		{"create a new REST endpoint", TaskGeneration},
		{"plan the migration strategy", TaskPlanning},
		{"audit security of the API", TaskSecurityReview},
		{"scaffold a new service", TaskBoilerplate},
		{"coordinate the deployment pipeline", TaskOrchestration},
		{"hello", TaskGeneration}, // default
	}
	for _, tt := range tests {
		task := ClassifyTask(tt.prompt)
		if task.Type != tt.want {
			t.Errorf("ClassifyTask(%q).Type = %s, want %s", tt.prompt, task.Type, tt.want)
		}
	}
}

func TestClassifyTask_RequiresTools(t *testing.T) {
	// Explain tasks don't require tools
	task := ClassifyTask("explain how generics work")
	if task.RequiresTools {
		t.Error("explain task should not require tools")
	}

	// Debug tasks require tools
	task = ClassifyTask("debug the failing test")
	if !task.RequiresTools {
		t.Error("debug task should require tools")
	}
}

func TestTaskValueScore(t *testing.T) {
	low := Task{Type: TaskBoilerplate, Priority: PriorityLow}
	high := Task{Type: TaskSecurityReview, Priority: PriorityCritical}

	if low.ValueScore() >= high.ValueScore() {
		t.Errorf("low priority boilerplate (%f) should score less than critical security (%f)",
			low.ValueScore(), high.ValueScore())
	}
}

func TestEstimateComplexity(t *testing.T) {
	simple := estimateComplexity("rename the variable")
	complex := estimateComplexity("design and implement a distributed caching system with migration support and integration testing across multiple environments")

	if simple >= complex {
		t.Errorf("simple (%f) should be less than complex (%f)", simple, complex)
	}
}

// --- Arm ---

func TestArmEstimateCost(t *testing.T) {
	arm := &Arm{
		CostPer1kInput:  0.003,
		CostPer1kOutput: 0.015,
	}
	cost := arm.EstimateCost(10000)
	// 6000 input tokens * 0.003/1k + 4000 output tokens * 0.015/1k
	// = 0.018 + 0.060 = 0.078
	if cost < 0.07 || cost > 0.09 {
		t.Errorf("EstimateCost(10000) = %f, want ~0.078", cost)
	}
}

func TestArmEstimateCost_Free(t *testing.T) {
	arm := &Arm{} // local model, zero cost
	cost := arm.EstimateCost(10000)
	if cost != 0 {
		t.Errorf("free model should have zero cost, got %f", cost)
	}
}

// --- Pool ---

func TestLimitPool_RemainingFraction(t *testing.T) {
	p := &LimitPool{TotalLimit: 100, Used: 30, Reserved: 20}
	f := p.RemainingFraction()
	if f != 0.5 {
		t.Errorf("RemainingFraction = %f, want 0.5", f)
	}
}

func TestLimitPool_ScarcityMultiplier(t *testing.T) {
	// Half remaining, k=2: 1/0.5^2 = 4
	p := &LimitPool{TotalLimit: 100, Used: 50, ScarcityK: 2}
	m := p.ScarcityMultiplier()
	if m < 3.9 || m > 4.1 {
		t.Errorf("ScarcityMultiplier = %f, want ~4.0", m)
	}
}

func TestLimitPool_ScarcityMultiplier_Exhausted(t *testing.T) {
	p := &LimitPool{TotalLimit: 100, Used: 100}
	m := p.ScarcityMultiplier()
	if !math.IsInf(m, 1) {
		t.Errorf("exhausted pool should return +Inf, got %f", m)
	}
}

func TestLimitPool_ScarcityMultiplier_UseItOrLoseIt(t *testing.T) {
	p := &LimitPool{
		TotalLimit: 100, Used: 30, // 70% remaining
		ScarcityK:  2,
		ResetAt:    time.Now().Add(30 * time.Minute), // reset in 30 min
	}
	m := p.ScarcityMultiplier()
	if m != 0.5 {
		t.Errorf("use-it-or-lose-it discount: ScarcityMultiplier = %f, want 0.5", m)
	}
}

func TestLimitPool_ReserveAndCommit(t *testing.T) {
	p := &LimitPool{
		TotalLimit: 1000,
		ArmRates:   map[ArmID]float64{"test/model": 5.0}, // 5 units per 1k tokens
		ScarcityK:  2,
	}

	res, ok := p.Reserve("test/model", 10000) // 5 * 10 = 50 units
	if !ok {
		t.Fatal("reservation should succeed")
	}
	if p.Reserved != 50 {
		t.Errorf("Reserved = %f, want 50", p.Reserved)
	}

	res.Commit(8000) // actual: 5 * 8 = 40 units
	if p.Used != 40 {
		t.Errorf("Used = %f, want 40", p.Used)
	}
	if p.Reserved != 0 {
		t.Errorf("Reserved = %f, want 0 after commit", p.Reserved)
	}
}

func TestLimitPool_ReserveExhausted(t *testing.T) {
	p := &LimitPool{
		TotalLimit: 100,
		Used:       90,
		ArmRates:   map[ArmID]float64{"test/model": 5.0},
		ScarcityK:  2,
	}

	_, ok := p.Reserve("test/model", 10000) // needs 50, only 10 available
	if ok {
		t.Error("reservation should fail when exhausted")
	}
}

func TestLimitPool_Rollback(t *testing.T) {
	p := &LimitPool{
		TotalLimit: 1000,
		ArmRates:   map[ArmID]float64{"test/model": 5.0},
		ScarcityK:  2,
	}

	res, _ := p.Reserve("test/model", 10000)
	if p.Reserved != 50 {
		t.Fatalf("Reserved = %f, want 50", p.Reserved)
	}

	res.Rollback()
	if p.Reserved != 0 {
		t.Errorf("Reserved = %f, want 0 after rollback", p.Reserved)
	}
	if p.Used != 0 {
		t.Errorf("Used = %f, want 0 after rollback", p.Used)
	}
}

func TestLimitPool_CheckReset(t *testing.T) {
	p := &LimitPool{
		TotalLimit:  1000,
		Used:        500,
		Reserved:    100,
		ResetPeriod: time.Hour,
		ResetAt:     time.Now().Add(-time.Minute), // already passed
		ScarcityK:   2,
	}

	p.CheckReset()
	if p.Used != 0 {
		t.Errorf("Used = %f after reset, want 0", p.Used)
	}
	if p.Reserved != 0 {
		t.Errorf("Reserved = %f after reset, want 0", p.Reserved)
	}
}

// --- Selector ---

func TestSelectBest_PrefersToolSupport(t *testing.T) {
	withTools := &Arm{
		ID: "a/with-tools", ModelName: "with-tools",
		Capabilities: provider.Capabilities{ToolUse: true, ContextWindow: 128000},
	}
	withoutTools := &Arm{
		ID: "b/no-tools", ModelName: "no-tools",
		Capabilities: provider.Capabilities{ToolUse: false, ContextWindow: 128000},
	}

	task := Task{Type: TaskGeneration, RequiresTools: true, Priority: PriorityNormal}
	best := selectBest([]*Arm{withoutTools, withTools}, task)

	if best.ID != "a/with-tools" {
		t.Errorf("should prefer arm with tool support, got %s", best.ID)
	}
}

func TestSelectBest_PrefersThinkingForPlanning(t *testing.T) {
	thinking := &Arm{
		ID: "a/thinking", ModelName: "thinking",
		Capabilities:   provider.Capabilities{ToolUse: true, Thinking: true, ContextWindow: 200000},
		CostPer1kInput: 0.01, CostPer1kOutput: 0.05,
	}
	noThinking := &Arm{
		ID: "b/basic", ModelName: "basic",
		Capabilities:   provider.Capabilities{ToolUse: true, ContextWindow: 128000},
		CostPer1kInput: 0.01, CostPer1kOutput: 0.05,
	}

	task := Task{Type: TaskPlanning, RequiresTools: true, Priority: PriorityNormal, EstimatedTokens: 5000}
	best := selectBest([]*Arm{noThinking, thinking}, task)

	if best.ID != "a/thinking" {
		t.Errorf("should prefer thinking model for planning, got %s", best.ID)
	}
}

func TestFilterFeasible_ExcludesExhausted(t *testing.T) {
	pool := &LimitPool{
		TotalLimit: 100,
		Used:       100, // exhausted
		ArmRates:   map[ArmID]float64{"a/model": 1.0},
		ScarcityK:  2,
	}
	arm := &Arm{
		ID: "a/model", ModelName: "model",
		Capabilities: provider.Capabilities{ToolUse: true},
		Pools:        []*LimitPool{pool},
	}

	task := Task{Type: TaskGeneration, RequiresTools: true, EstimatedTokens: 1000}
	feasible := filterFeasible([]*Arm{arm}, task)

	if len(feasible) != 0 {
		t.Error("exhausted arm should not be feasible")
	}
}

// --- Router ---

func TestRouter_SelectForced(t *testing.T) {
	r := New(Config{})
	r.RegisterArm(&Arm{ID: "a/model1", Capabilities: provider.Capabilities{ToolUse: true}})
	r.RegisterArm(&Arm{ID: "b/model2", Capabilities: provider.Capabilities{ToolUse: true}})

	r.ForceArm("b/model2")

	decision := r.Select(Task{Type: TaskGeneration})
	if decision.Error != nil {
		t.Fatalf("Select: %v", decision.Error)
	}
	if decision.Arm.ID != "b/model2" {
		t.Errorf("forced arm should be selected, got %s", decision.Arm.ID)
	}
}

func TestRouter_SelectNoArms(t *testing.T) {
	r := New(Config{})
	decision := r.Select(Task{Type: TaskGeneration})
	if decision.Error == nil {
		t.Error("should error with no arms")
	}
}

func TestRouter_SelectForcedNotFound(t *testing.T) {
	r := New(Config{})
	r.ForceArm("nonexistent/model")
	decision := r.Select(Task{Type: TaskGeneration})
	if decision.Error == nil {
		t.Error("should error when forced arm not found")
	}
}
