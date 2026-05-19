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

func TestClassifyTask_OrchestrationNotFalsePositive(t *testing.T) {
	// Words like "coordinator", "pipeline", "dispatch" appear in non-orchestration contexts.
	// More specific classifications (debug, review, refactor, explain) must win.
	tests := []struct {
		prompt string
		want   TaskType
	}{
		{"fix the coordinator bug", TaskDebug},           // "coordinator" contains "coordinate"
		{"review the orchestration layer", TaskReview},   // "orchestrat" present but review wins
		{"refactor the pipeline dispatch", TaskRefactor}, // "dispatch" present but refactor wins
		{"explain how coordination works", TaskExplain},  // "coordinat" present but explain wins
		{"debug the dispatch table", TaskDebug},          // "dispatch" present but debug wins
	}
	for _, tt := range tests {
		task := ClassifyTask(tt.prompt)
		if task.Type != tt.want {
			t.Errorf("ClassifyTask(%q).Type = %s, want %s", tt.prompt, task.Type, tt.want)
		}
	}
}

func TestClassifyTask_OrchestrationKeywords(t *testing.T) {
	// Explicit orchestration-intent phrases should still classify correctly.
	tests := []struct {
		prompt string
		want   TaskType
	}{
		{"orchestrate the migration across services", TaskOrchestration},
		{"fan out the work to 5 elfs", TaskOrchestration},
		{"split this into subtasks and run them in parallel", TaskOrchestration},
		{"delegate to worker elfs for parallel processing", TaskOrchestration},
		{"spawn elfs to handle this", TaskOrchestration},
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
	best := selectBest(nil, []*Arm{withoutTools, withTools}, task)

	if best.ID != "a/with-tools" {
		t.Errorf("should prefer arm with tool support, got %s", best.ID)
	}
}

func TestSelectBest_PrefersThinkingForPlanning(t *testing.T) {
	thinking := &Arm{
		ID: "a/thinking", ModelName: "thinking",
		Capabilities:   provider.Capabilities{ToolUse: true, ThinkingModes: []provider.EffortLevel{provider.EffortLow, provider.EffortMedium, provider.EffortHigh}, ContextWindow: 200000},
		CostPer1kInput: 0.01, CostPer1kOutput: 0.05,
	}
	noThinking := &Arm{
		ID: "b/basic", ModelName: "basic",
		Capabilities:   provider.Capabilities{ToolUse: true, ContextWindow: 128000},
		CostPer1kInput: 0.01, CostPer1kOutput: 0.05,
	}

	task := Task{Type: TaskPlanning, RequiresTools: true, Priority: PriorityNormal, EstimatedTokens: 5000}
	best := selectBest(nil, []*Arm{noThinking, thinking}, task)

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

func TestRouter_SelectForcedNonLocalUnderLocalOnlyErrors(t *testing.T) {
	// Audit finding: --provider anthropic pins a cloud arm, then Ctrl+X
	// enables local-only. Select used to short-circuit on forcedArm and
	// return the cloud arm anyway, breaking the "local-only routing"
	// promise the UI badge makes. Must now error out.
	r := New(Config{})
	r.RegisterArm(&Arm{ID: "anthropic/sonnet", IsLocal: false, Capabilities: provider.Capabilities{ToolUse: true}})
	r.ForceArm("anthropic/sonnet")
	r.SetLocalOnly(true)

	decision := r.Select(Task{Type: TaskGeneration})
	if decision.Error == nil {
		t.Fatal("expected error: forced cloud arm under local-only must not select")
	}
	if decision.Arm != nil {
		t.Errorf("decision.Arm = %v, want nil", decision.Arm)
	}
}

func TestRouter_SelectForcedLocalUnderLocalOnlyAllowed(t *testing.T) {
	r := New(Config{})
	r.RegisterArm(&Arm{ID: "ollama/qwen", IsLocal: true, Capabilities: provider.Capabilities{ToolUse: true}})
	r.ForceArm("ollama/qwen")
	r.SetLocalOnly(true)

	decision := r.Select(Task{Type: TaskGeneration})
	if decision.Error != nil {
		t.Fatalf("forced local arm under local-only should select: %v", decision.Error)
	}
	if decision.Arm == nil || decision.Arm.ID != "ollama/qwen" {
		t.Errorf("decision.Arm = %v, want ollama/qwen", decision.Arm)
	}
}

// --- Gap A: Pool Reservations ---

func TestRoutingDecision_CommitReleasesReservation(t *testing.T) {
	pool := &LimitPool{
		TotalLimit: 1000,
		ArmRates:   map[ArmID]float64{"a/model": 1.0},
		ScarcityK:  2,
	}
	arm := &Arm{
		ID:           "a/model",
		Capabilities: provider.Capabilities{ToolUse: true},
		Pools:        []*LimitPool{pool},
	}

	r := New(Config{})
	r.RegisterArm(arm)

	task := Task{Type: TaskGeneration, RequiresTools: true, EstimatedTokens: 500, Priority: PriorityNormal}
	decision := r.Select(task)
	if decision.Error != nil {
		t.Fatalf("Select: %v", decision.Error)
	}

	// After Select: tokens should be reserved
	if pool.Reserved == 0 {
		t.Error("Select should reserve pool capacity")
	}

	// After Commit: reserved released, used incremented
	decision.Commit(400)
	if pool.Reserved != 0 {
		t.Errorf("Reserved = %f after Commit, want 0", pool.Reserved)
	}
	if pool.Used == 0 {
		t.Error("Used should be non-zero after Commit")
	}
}

func TestRoutingDecision_RollbackReleasesReservation(t *testing.T) {
	pool := &LimitPool{
		TotalLimit: 1000,
		ArmRates:   map[ArmID]float64{"a/model": 1.0},
		ScarcityK:  2,
	}
	arm := &Arm{
		ID:           "a/model",
		Capabilities: provider.Capabilities{ToolUse: true},
		Pools:        []*LimitPool{pool},
	}

	r := New(Config{})
	r.RegisterArm(arm)

	task := Task{Type: TaskGeneration, RequiresTools: true, EstimatedTokens: 500, Priority: PriorityNormal}
	decision := r.Select(task)
	if decision.Error != nil {
		t.Fatalf("Select: %v", decision.Error)
	}

	decision.Rollback()
	if pool.Reserved != 0 {
		t.Errorf("Reserved = %f after Rollback, want 0", pool.Reserved)
	}
	if pool.Used != 0 {
		t.Errorf("Used = %f after Rollback, want 0", pool.Used)
	}
}

func TestSelect_ConcurrentReservationPreventsOvercommit(t *testing.T) {
	// Pool with very limited capacity: only 1 request can fit
	pool := &LimitPool{
		TotalLimit: 10,
		ArmRates:   map[ArmID]float64{"a/model": 1.0},
		ScarcityK:  2,
	}
	arm := &Arm{
		ID:           "a/model",
		Capabilities: provider.Capabilities{ToolUse: true},
		Pools:        []*LimitPool{pool},
	}

	r := New(Config{})
	r.RegisterArm(arm)

	task := Task{Type: TaskGeneration, RequiresTools: true, EstimatedTokens: 8000, Priority: PriorityNormal}

	// First select should succeed and reserve
	d1 := r.Select(task)
	// Second concurrent select should fail — capacity reserved by first
	d2 := r.Select(task)

	if d1.Error != nil && d2.Error != nil {
		t.Error("at least one selection should succeed")
	}
	if d1.Error == nil && d2.Error == nil {
		t.Error("second selection should fail: pool overcommit prevented")
	}

	// Cleanup
	d1.Rollback()
	d2.Rollback()
}

// --- Gap B: ArmPerf ---

func TestArmPerf_Update_FirstSample(t *testing.T) {
	var p ArmPerf
	p.Update(50*time.Millisecond, 100, 2*time.Second)

	if p.Samples != 1 {
		t.Errorf("Samples = %d, want 1", p.Samples)
	}
	if p.TTFTMs != 50 {
		t.Errorf("TTFTMs = %f, want 50", p.TTFTMs)
	}
	if p.ToksPerSec != 50 { // 100 tokens / 2s
		t.Errorf("ToksPerSec = %f, want 50", p.ToksPerSec)
	}
}

func TestArmPerf_Update_EMA(t *testing.T) {
	var p ArmPerf
	p.Update(100*time.Millisecond, 100, time.Second)
	p.Update(50*time.Millisecond, 100, time.Second) // faster second response

	if p.Samples != 2 {
		t.Errorf("Samples = %d, want 2", p.Samples)
	}
	// EMA: new = 0.3*50 + 0.7*100 = 85
	if p.TTFTMs < 80 || p.TTFTMs > 90 {
		t.Errorf("TTFTMs = %f, want ~85 (EMA of 100→50)", p.TTFTMs)
	}
}

func TestArmPerf_Update_ZeroDuration(t *testing.T) {
	var p ArmPerf
	p.Update(10*time.Millisecond, 100, 0) // zero stream duration

	if p.Samples != 1 {
		t.Errorf("Samples = %d, want 1", p.Samples)
	}
	if p.ToksPerSec != 0 { // undefined throughput → 0
		t.Errorf("ToksPerSec = %f, want 0 for zero duration", p.ToksPerSec)
	}
}

// --- Gap C: QualityThreshold ---

func TestFilterFeasible_RejectsLowQualityArm(t *testing.T) {
	// Arm with no capabilities — heuristicQuality ≈ 0.5, below security_review minimum (0.88)
	lowQualityArm := &Arm{
		ID:           "a/basic",
		Capabilities: provider.Capabilities{ToolUse: true, ContextWindow: 4096},
	}
	highQualityArm := &Arm{
		ID: "b/powerful",
		Capabilities: provider.Capabilities{
			ToolUse:       true,
			ThinkingModes: []provider.EffortLevel{provider.EffortLow, provider.EffortMedium, provider.EffortHigh},
			ContextWindow: 200000,
		},
	}

	task := Task{
		Type:          TaskSecurityReview,
		RequiresTools: true,
		Priority:      PriorityHigh,
	}

	feasible := filterFeasible([]*Arm{lowQualityArm, highQualityArm}, task)

	// highQualityArm should be in feasible; lowQualityArm should be filtered
	if len(feasible) != 1 {
		t.Fatalf("len(feasible) = %d, want 1", len(feasible))
	}
	if feasible[0].ID != "b/powerful" {
		t.Errorf("feasible[0] = %s, want b/powerful", feasible[0].ID)
	}
}

func TestFilterFeasible_FallsBackWhenAllBelowQuality(t *testing.T) {
	// Only arm available, but quality is low — should still be returned as fallback
	onlyArm := &Arm{
		ID:           "a/only",
		Capabilities: provider.Capabilities{ToolUse: true, ContextWindow: 4096},
	}

	task := Task{Type: TaskSecurityReview, RequiresTools: true}
	feasible := filterFeasible([]*Arm{onlyArm}, task)

	if len(feasible) == 0 {
		t.Error("should fall back to low-quality arm when no better option exists")
	}
}

// --- Tier-based routing (P0c) ---

func TestArmTier(t *testing.T) {
	tests := []struct {
		name string
		arm  *Arm
		task Task
		want int
	}{
		{"CLI agent", &Arm{IsCLIAgent: true}, Task{}, 1},
		{"local model", &Arm{IsLocal: true}, Task{}, 2},
		{"API model", &Arm{}, Task{}, 3},
		{"IsCLIAgent overrides IsLocal", &Arm{IsCLIAgent: true, IsLocal: true}, Task{}, 1},
		{
			name: "specialized small arm fitting task → tier 0",
			arm:  &Arm{IsLocal: true, MaxComplexity: 0.3},
			task: Task{ComplexityScore: 0.1},
			want: 0,
		},
		{
			name: "specialized small arm not fitting task falls back to local",
			arm:  &Arm{IsLocal: true, MaxComplexity: 0.3},
			task: Task{ComplexityScore: 0.5},
			want: 2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := armTier(tt.arm, tt.task); got != tt.want {
				t.Errorf("armTier = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestSelectBest_SmallArmWinsTrivialTask verifies the SLM-style arm
// (MaxComplexity > 0) beats a CLI agent for a trivial task, satisfying the
// "small stuff stays on the small model" intent.
func TestSelectBest_SmallArmWinsTrivialTask(t *testing.T) {
	cliArm := &Arm{
		ID:           "subprocess/claude",
		IsCLIAgent:   true,
		Capabilities: provider.Capabilities{ToolUse: true, ContextWindow: 200000},
	}
	smallArm := &Arm{
		ID:            "slm/llamafile",
		IsLocal:       true,
		MaxComplexity: 0.3,
		Capabilities:  provider.Capabilities{ToolUse: false},
	}
	task := Task{Type: TaskExplain, ComplexityScore: 0.05, RequiresTools: false}
	got := selectBest(nil, []*Arm{cliArm, smallArm}, task)
	if got != smallArm {
		t.Errorf("selectBest = %v, want smallArm", got)
	}
}

// TestSelectBest_CLIAgentWinsComplexTask verifies a non-trivial task still
// prefers the CLI agent over the small arm — the small arm's MaxComplexity
// ceiling pushes it back below the CLI agent for harder work.
func TestSelectBest_CLIAgentWinsComplexTask(t *testing.T) {
	cliArm := &Arm{
		ID:           "subprocess/claude",
		IsCLIAgent:   true,
		Capabilities: provider.Capabilities{ToolUse: true, ContextWindow: 200000},
	}
	smallArm := &Arm{
		ID:            "slm/llamafile",
		IsLocal:       true,
		MaxComplexity: 0.3,
		Capabilities:  provider.Capabilities{ToolUse: false},
	}
	task := Task{Type: TaskRefactor, ComplexityScore: 0.7, RequiresTools: true}
	got := selectBest(nil, []*Arm{cliArm, smallArm}, task)
	if got != cliArm {
		t.Errorf("selectBest = %v, want cliArm", got)
	}
}

func TestSelectBest_TierPreference(t *testing.T) {
	cliArm := &Arm{
		ID:           "subprocess/claude",
		IsCLIAgent:   true,
		Capabilities: provider.Capabilities{ToolUse: true, ContextWindow: 200000},
	}
	localArm := &Arm{
		ID:           "ollama/llama3",
		IsLocal:      true,
		Capabilities: provider.Capabilities{ToolUse: true, ContextWindow: 32000},
	}
	apiArm := &Arm{
		ID:             "mistral/mistral-large",
		Capabilities:   provider.Capabilities{ToolUse: true, ContextWindow: 128000},
		CostPer1kInput: 0.002, CostPer1kOutput: 0.006,
	}
	task := Task{Type: TaskGeneration, Priority: PriorityNormal, EstimatedTokens: 1000}

	t.Run("CLI beats local and API", func(t *testing.T) {
		best := selectBest(nil, []*Arm{apiArm, localArm, cliArm}, task)
		if best.ID != "subprocess/claude" {
			t.Errorf("want subprocess/claude (tier 0), got %s", best.ID)
		}
	})

	t.Run("local beats API when no CLI", func(t *testing.T) {
		best := selectBest(nil, []*Arm{apiArm, localArm}, task)
		if best.ID != "ollama/llama3" {
			t.Errorf("want ollama/llama3 (tier 1), got %s", best.ID)
		}
	})

	t.Run("API selected when only option", func(t *testing.T) {
		best := selectBest(nil, []*Arm{apiArm}, task)
		if best == nil || best.ID != "mistral/mistral-large" {
			t.Errorf("want mistral/mistral-large (tier 2), got %v", best)
		}
	})
}

// --- Disabled arms ---

func TestRouter_DisabledArm_ExcludedFromRouting(t *testing.T) {
	r := New(Config{})
	r.RegisterArm(&Arm{
		ID:           "a/enabled",
		Capabilities: provider.Capabilities{ToolUse: true},
	})
	r.RegisterArm(&Arm{
		ID:           "b/disabled",
		Disabled:     true,
		Capabilities: provider.Capabilities{ToolUse: true},
	})

	decision := r.Select(Task{Type: TaskGeneration, RequiresTools: true})
	if decision.Error != nil {
		t.Fatalf("Select: %v", decision.Error)
	}
	if decision.Arm.ID != "a/enabled" {
		t.Errorf("disabled arm should not be selected, got %s", decision.Arm.ID)
	}
}

func TestRouter_DisabledArm_ForcedBypasses(t *testing.T) {
	r := New(Config{})
	r.RegisterArm(&Arm{
		ID:           "a/disabled",
		Disabled:     true,
		Capabilities: provider.Capabilities{ToolUse: true},
	})
	r.ForceArm("a/disabled")

	decision := r.Select(Task{Type: TaskGeneration})
	if decision.Error != nil {
		t.Fatalf("forced disabled arm should be selectable, got error: %v", decision.Error)
	}
	if decision.Arm.ID != "a/disabled" {
		t.Errorf("want a/disabled, got %s", decision.Arm.ID)
	}
}

func TestRouter_AllDisabled_ReturnsError(t *testing.T) {
	r := New(Config{})
	r.RegisterArm(&Arm{
		ID:       "a/disabled",
		Disabled: true,
		Capabilities: provider.Capabilities{ToolUse: true},
	})

	decision := r.Select(Task{Type: TaskGeneration})
	if decision.Error == nil {
		t.Error("should error when all arms disabled")
	}
}

func TestFilterFeasible_MaxComplexity(t *testing.T) {
	slmArm := &Arm{
		ID:           "slm/tiny",
		IsLocal:      true,
		MaxComplexity: 0.3,
		Capabilities: provider.Capabilities{ToolUse: false},
	}
	apiArm := &Arm{
		ID:           "api/big",
		Capabilities: provider.Capabilities{ToolUse: true, ContextWindow: 200000},
	}

	// Low-complexity task: SLM arm passes the ceiling.
	lowTask := Task{Type: TaskBoilerplate, ComplexityScore: 0.2}
	got := filterFeasible([]*Arm{slmArm, apiArm}, lowTask)
	found := false
	for _, a := range got {
		if a.ID == "slm/tiny" {
			found = true
		}
	}
	if !found {
		t.Error("slm arm should pass filterFeasible for low-complexity task")
	}

	// High-complexity task: SLM arm must be excluded.
	highTask := Task{Type: TaskPlanning, ComplexityScore: 0.8, RequiresTools: false}
	got = filterFeasible([]*Arm{slmArm, apiArm}, highTask)
	for _, a := range got {
		if a.ID == "slm/tiny" {
			t.Error("slm arm should be excluded for high-complexity task")
		}
	}
}

func TestFilterFeasible_MaxComplexity_Zero_MeansNoLimit(t *testing.T) {
	// MaxComplexity == 0 means "no ceiling" — existing arms are unaffected.
	arm := &Arm{
		ID:           "api/arm",
		MaxComplexity: 0, // zero = no ceiling
		Capabilities: provider.Capabilities{ToolUse: true, ContextWindow: 200000},
	}
	task := Task{Type: TaskOrchestration, ComplexityScore: 0.99}
	got := filterFeasible([]*Arm{arm}, task)
	if len(got) == 0 {
		t.Error("arm with MaxComplexity=0 should never be excluded by complexity ceiling")
	}
}

