package router

import (
	"math"
	"testing"
	"time"

	"somegit.dev/Owlibou/gnoma/internal/provider"
)

func timeFuture() time.Time { return time.Now().Add(1 * time.Hour) }

func TestArm_HasStrength(t *testing.T) {
	a := &Arm{Strengths: []TaskType{TaskSecurityReview, TaskPlanning}}
	if !a.HasStrength(TaskSecurityReview) {
		t.Error("HasStrength(SecurityReview) = false, want true")
	}
	if !a.HasStrength(TaskPlanning) {
		t.Error("HasStrength(Planning) = false, want true")
	}
	if a.HasStrength(TaskDebug) {
		t.Error("HasStrength(Debug) = true, want false")
	}
	empty := &Arm{}
	if empty.HasStrength(TaskSecurityReview) {
		t.Error("empty Strengths should never match")
	}
}

func TestArm_ResolvedCostWeight(t *testing.T) {
	cases := []struct {
		in, want float64
	}{
		{0, 1.0},   // unset → 1.0
		{1.0, 1.0}, // explicit 1.0 → 1.0
		{0.5, 0.5},
		{0.05, 0.05},
	}
	for _, tc := range cases {
		a := &Arm{CostWeight: tc.in}
		if got := a.ResolvedCostWeight(); got != tc.want {
			t.Errorf("CostWeight=%v: ResolvedCostWeight() = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestScoreArm_CostWeightAffectsArmComparison(t *testing.T) {
	// The semantically meaningful test: two arms with different costs but
	// otherwise identical. At CostWeight=1.0 (current behavior), the cheap
	// arm wins. At CostWeight=0.0 (cost ignored), they tie on quality —
	// and the slightly-higher-quality one wins.
	cheap := &Arm{
		ID:              NewArmID("provA", "small"),
		Capabilities:    provider.Capabilities{ToolUse: true, ContextWindow: 100000},
		CostPer1kInput:  0.0005,
		CostPer1kOutput: 0.0015,
	}
	expensive := &Arm{
		ID:              NewArmID("provB", "big"),
		Capabilities:    provider.Capabilities{ToolUse: true, ContextWindow: 200000}, // slight quality edge
		CostPer1kInput:  0.015,
		CostPer1kOutput: 0.075,
	}
	task := Task{Type: TaskDebug, EstimatedTokens: 5000, RequiresTools: true, Priority: PriorityNormal}

	// CostWeight=1.0: cost dominates, cheap arm wins.
	cheap.CostWeight, expensive.CostWeight = 1.0, 1.0
	if scoreArm(nil, cheap, task) <= scoreArm(nil, expensive, task) {
		t.Errorf("CostWeight=1.0: cheap arm should beat expensive arm; cheap=%v expensive=%v",
			scoreArm(nil, cheap, task), scoreArm(nil, expensive, task))
	}

	// CostWeight=0.0: cost ignored, quality alone decides → expensive (better
	// context window) wins.
	cheap.CostWeight, expensive.CostWeight = 0.001, 0.001
	if scoreArm(nil, expensive, task) <= scoreArm(nil, cheap, task) {
		t.Errorf("CostWeight~0: higher-quality expensive arm should beat cheap arm; expensive=%v cheap=%v",
			scoreArm(nil, expensive, task), scoreArm(nil, cheap, task))
	}
}

func TestScoreArm_LinearFormulaMonotone(t *testing.T) {
	// Regression: the original draft used cost^CostWeight, which inverts
	// direction when cost<1 (local arms). The linear formula
	//   effectiveCost = 1 + CostWeight*(cost-1)
	// is monotone: increasing CostWeight monotonically pulls effectiveCost
	// toward the raw cost regardless of whether cost is above or below 1.
	//
	// Verify monotonicity on both sides of cost=1.
	cheap := &Arm{ // cost < 1
		CostPer1kInput:  0.001,
		CostPer1kOutput: 0.001,
	}
	expensive := &Arm{ // cost > 1 for big tasks
		CostPer1kInput:  0.05,
		CostPer1kOutput: 0.15,
	}
	task := Task{Type: TaskDebug, EstimatedTokens: 20000}

	weights := []float64{0.05, 0.25, 0.5, 0.75, 1.0}
	for _, name := range []string{"cheap", "expensive"} {
		var prev float64
		for i, w := range weights {
			arm := cheap
			if name == "expensive" {
				arm = expensive
			}
			arm.CostWeight = w
			raw := effectiveCost(arm, task)
			weighted := 1.0 + arm.ResolvedCostWeight()*(raw-1.0)
			if i == 0 {
				prev = weighted
				continue
			}
			// As w increases, weighted should move toward raw.
			// For cheap (raw<1), weighted should DECREASE.
			// For expensive (raw>1), weighted should INCREASE.
			if raw < 1 && weighted > prev {
				t.Errorf("%s arm w=%v: weighted (%v) increased from prev (%v); raw=%v",
					name, w, weighted, prev, raw)
			}
			if raw > 1 && weighted < prev {
				t.Errorf("%s arm w=%v: weighted (%v) decreased from prev (%v); raw=%v",
					name, w, weighted, prev, raw)
			}
			prev = weighted
		}
	}
}

func TestScoreArm_StrengthBonus(t *testing.T) {
	withoutStrength := &Arm{
		ID:           NewArmID("anthropic", "opus"),
		Capabilities: provider.Capabilities{ToolUse: true, ContextWindow: 200000},
	}
	withStrength := &Arm{
		ID:           NewArmID("anthropic", "opus"),
		Capabilities: provider.Capabilities{ToolUse: true, ContextWindow: 200000},
		Strengths:    []TaskType{TaskSecurityReview},
	}
	task := Task{Type: TaskSecurityReview, EstimatedTokens: 5000, RequiresTools: true, Priority: PriorityNormal}

	a := scoreArm(nil, withoutStrength, task)
	b := scoreArm(nil, withStrength, task)
	if !(b > a) {
		t.Errorf("strength-tagged arm score (%v) should exceed plain arm score (%v)", b, a)
	}
}

func TestScoreArm_StrengthBonusDoesNotApplyToOtherTasks(t *testing.T) {
	// Strengths apply only to listed task types.
	tagged := &Arm{
		ID:           NewArmID("anthropic", "opus"),
		Capabilities: provider.Capabilities{ToolUse: true, ContextWindow: 200000},
		Strengths:    []TaskType{TaskSecurityReview},
	}
	plain := &Arm{
		ID:           NewArmID("anthropic", "opus"),
		Capabilities: provider.Capabilities{ToolUse: true, ContextWindow: 200000},
	}
	task := Task{Type: TaskDebug, EstimatedTokens: 5000, RequiresTools: true, Priority: PriorityNormal}

	a := scoreArm(nil, plain, task)
	b := scoreArm(nil, tagged, task)
	if math.Abs(a-b) > 1e-9 {
		t.Errorf("non-matching task should ignore Strengths: plain=%v tagged=%v", a, b)
	}
}

func TestSelectBest_StrengthPromotedArmBeatsCLIAgent(t *testing.T) {
	// Plan exit criteria: with Strengths set, Opus (tier 3) wins over a CLI
	// agent (tier 1) for SecurityReview.
	cliAgent := &Arm{
		ID:           NewArmID("subprocess", "claude"),
		IsCLIAgent:   true,
		Capabilities: provider.Capabilities{ToolUse: true, ContextWindow: 200000},
	}
	opus := &Arm{
		ID:              NewArmID("anthropic", "opus"),
		Capabilities:    provider.Capabilities{ToolUse: true, ContextWindow: 200000},
		Strengths:       []TaskType{TaskSecurityReview},
		CostPer1kInput:  0.015,
		CostPer1kOutput: 0.075,
	}

	task := Task{Type: TaskSecurityReview, EstimatedTokens: 5000, RequiresTools: true, Priority: PriorityNormal}
	got := selectBest(nil, []*Arm{cliAgent, opus}, task)
	if got == nil {
		t.Fatal("selectBest returned nil")
	}
	if got.ID != opus.ID {
		t.Errorf("selectBest = %s, want %s (strength-promoted arm should beat tier-1 CLI agent)", got.ID, opus.ID)
	}
}

func TestSelectBest_EmptyStrengthsPreservesTierOrder(t *testing.T) {
	// Regression: without Strengths, CLI-agent tier-1 still wins over API tier-3.
	cliAgent := &Arm{
		ID:           NewArmID("subprocess", "claude"),
		IsCLIAgent:   true,
		Capabilities: provider.Capabilities{ToolUse: true, ContextWindow: 200000},
	}
	opus := &Arm{
		ID:              NewArmID("anthropic", "opus"),
		Capabilities:    provider.Capabilities{ToolUse: true, ContextWindow: 200000},
		CostPer1kInput:  0.015,
		CostPer1kOutput: 0.075,
	}

	task := Task{Type: TaskSecurityReview, EstimatedTokens: 5000, RequiresTools: true, Priority: PriorityNormal}
	got := selectBest(nil, []*Arm{cliAgent, opus}, task)
	if got.ID != cliAgent.ID {
		t.Errorf("without Strengths, CLI-agent tier-1 should win; got %s", got.ID)
	}
}

func TestRouter_Select_PromotedArmInBackoffFallsThroughToTierOrder(t *testing.T) {
	// Strengths are preference, not pin. Full Router.Select path: backoff
	// filtering removes the promoted arm; selectBest then falls through to
	// the default tier order and picks the CLI agent.
	cliAgent := &Arm{
		ID:           NewArmID("subprocess", "claude"),
		IsCLIAgent:   true,
		Capabilities: provider.Capabilities{ToolUse: true, ContextWindow: 200000},
	}
	opus := &Arm{
		ID:           NewArmID("anthropic", "opus"),
		Capabilities: provider.Capabilities{ToolUse: true, ContextWindow: 200000},
		Strengths:    []TaskType{TaskSecurityReview},
	}
	opus.SetBackoff(timeFuture())

	r := New(Config{})
	r.RegisterArm(cliAgent)
	r.RegisterArm(opus)

	task := Task{Type: TaskSecurityReview, EstimatedTokens: 5000, RequiresTools: true, Priority: PriorityNormal}
	decision := r.Select(task)
	if decision.Error != nil {
		t.Fatalf("Select: %v", decision.Error)
	}
	if decision.Arm.ID != cliAgent.ID {
		t.Errorf("promoted arm in backoff should fall through to CLI agent; got %s", decision.Arm.ID)
	}
}

func TestApplyArmOverrides_ApplyStrengthsAndCostWeight(t *testing.T) {
	r := New(Config{})
	opus := &Arm{
		ID:           NewArmID("anthropic", "opus"),
		Capabilities: provider.Capabilities{ToolUse: true, ContextWindow: 200000},
	}
	r.RegisterArm(opus)

	unknown := r.ApplyArmOverrides([]ArmOverride{
		{
			ID:         "anthropic/opus",
			Strengths:  []string{"security_review", "planning"},
			CostWeight: 0.3,
		},
	})
	if len(unknown) != 0 {
		t.Errorf("unknown = %v, want empty", unknown)
	}

	got, _ := r.LookupArm(NewArmID("anthropic", "opus"))
	if !got.HasStrength(TaskSecurityReview) {
		t.Error("opus should have SecurityReview strength after override")
	}
	if !got.HasStrength(TaskPlanning) {
		t.Error("opus should have Planning strength after override")
	}
	if got.CostWeight != 0.3 {
		t.Errorf("opus.CostWeight = %v, want 0.3", got.CostWeight)
	}
}

func TestApplyArmOverrides_UnknownIDReported(t *testing.T) {
	r := New(Config{})
	r.RegisterArm(&Arm{
		ID:           NewArmID("anthropic", "opus"),
		Capabilities: provider.Capabilities{ToolUse: true},
	})

	unknown := r.ApplyArmOverrides([]ArmOverride{
		{ID: "anthropic/opus", Strengths: []string{"debug"}},
		{ID: "anthropic/typo-here", Strengths: []string{"refactor"}},
	})
	if len(unknown) != 1 || unknown[0] != "anthropic/typo-here" {
		t.Errorf("unknown = %v, want [anthropic/typo-here]", unknown)
	}
}

func TestApplyArmOverrides_UnknownStrengthSkipped(t *testing.T) {
	r := New(Config{})
	arm := &Arm{
		ID:           NewArmID("anthropic", "opus"),
		Capabilities: provider.Capabilities{ToolUse: true},
	}
	r.RegisterArm(arm)

	r.ApplyArmOverrides([]ArmOverride{
		{ID: "anthropic/opus", Strengths: []string{"security_review", "bogus-type"}},
	})

	got, _ := r.LookupArm(NewArmID("anthropic", "opus"))
	if !got.HasStrength(TaskSecurityReview) {
		t.Error("security_review should be applied")
	}
	if len(got.Strengths) != 1 {
		t.Errorf("got.Strengths = %v, want [security_review] only (bogus skipped)", got.Strengths)
	}
}

func TestSelectBest_MultiplePromotedArmsBestQualityWins(t *testing.T) {
	// Tunability check: when two arms both have Strengths for the same task,
	// observed quality (via QualityTracker) should determine the winner, not
	// the static strength bonus alone.
	armA := &Arm{
		ID:           NewArmID("provA", "model"),
		Capabilities: provider.Capabilities{ToolUse: true, ContextWindow: 200000},
		Strengths:    []TaskType{TaskSecurityReview},
	}
	armB := &Arm{
		ID:           NewArmID("provB", "model"),
		Capabilities: provider.Capabilities{ToolUse: true, ContextWindow: 200000},
		Strengths:    []TaskType{TaskSecurityReview},
	}

	qt := NewQualityTracker()
	// armB has consistently succeeded — minObservations=3 is enough to flip
	// the score blend.
	for i := 0; i < 5; i++ {
		qt.Record(armB.ID, TaskSecurityReview, true)
	}
	// armA fails consistently.
	for i := 0; i < 5; i++ {
		qt.Record(armA.ID, TaskSecurityReview, false)
	}

	task := Task{Type: TaskSecurityReview, EstimatedTokens: 5000, RequiresTools: true, Priority: PriorityNormal}
	got := selectBest(qt, []*Arm{armA, armB}, task)
	if got == nil {
		t.Fatal("selectBest returned nil")
	}
	if got.ID != armB.ID {
		t.Errorf("observed-quality winner should beat tied-strength loser; got %s", got.ID)
	}
}
