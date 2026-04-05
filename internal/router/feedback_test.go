package router_test

import (
	"testing"

	"somegit.dev/Owlibou/gnoma/internal/provider"
	"somegit.dev/Owlibou/gnoma/internal/router"
)

func TestQualityTracker_NoDataReturnsHeuristic(t *testing.T) {
	qt := router.NewQualityTracker()
	_, hasData := qt.Quality("arm:model", router.TaskGeneration)
	if hasData {
		t.Error("expected no data for unobserved arm")
	}
}

func TestQualityTracker_RecordUpdatesEMA(t *testing.T) {
	qt := router.NewQualityTracker()
	for i := 0; i < 3; i++ {
		qt.Record("arm:model", router.TaskGeneration, true)
	}
	score, hasData := qt.Quality("arm:model", router.TaskGeneration)
	if !hasData {
		t.Fatal("expected data after 3 observations")
	}
	if score <= 0 || score > 1 {
		t.Errorf("score out of range [0,1]: %f", score)
	}
}

func TestQualityTracker_AllFailuresLowScore(t *testing.T) {
	qt := router.NewQualityTracker()
	for i := 0; i < 5; i++ {
		qt.Record("arm:model", router.TaskDebug, false)
	}
	score, _ := qt.Quality("arm:model", router.TaskDebug)
	if score > 0.3 {
		t.Errorf("expected low score after all failures, got %f", score)
	}
}

func TestQualityTracker_ConcurrentSafe(t *testing.T) {
	qt := router.NewQualityTracker()
	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func(success bool) {
			qt.Record("arm:model", router.TaskReview, success)
			done <- struct{}{}
		}(i%2 == 0)
	}
	for i := 0; i < 10; i++ {
		<-done
	}
	score, _ := qt.Quality("arm:model", router.TaskReview)
	if score < 0 || score > 1 {
		t.Errorf("invalid score after concurrent writes: %f", score)
	}
}

func TestQualityTracker_InfluencesArmSelection(t *testing.T) {
	// After enough observations, the arm with a higher quality history should
	// be preferred by Router.Select() over an identically-heuristic arm.
	caps := provider.Capabilities{ToolUse: true}
	armA := &router.Arm{ID: "test/arm-a", ModelName: "arm-a", Capabilities: caps}
	armB := &router.Arm{ID: "test/arm-b", ModelName: "arm-b", Capabilities: caps}

	r := router.New(router.Config{})
	r.RegisterArm(armA)
	r.RegisterArm(armB)

	// Record 5 successes for A, 5 failures for B — enough to exceed minObservations=3.
	task := router.Task{Type: router.TaskGeneration, RequiresTools: true, Priority: router.PriorityNormal}
	for range 5 {
		r.ReportOutcome(router.Outcome{ArmID: "test/arm-a", TaskType: router.TaskGeneration, Success: true})
		r.ReportOutcome(router.Outcome{ArmID: "test/arm-b", TaskType: router.TaskGeneration, Success: false})
	}

	decision := r.Select(task)
	if decision.Error != nil {
		t.Fatalf("Select: %v", decision.Error)
	}
	defer decision.Rollback()

	if decision.Arm.ID != "test/arm-a" {
		t.Errorf("expected arm-a (high quality history) to be selected, got %s", decision.Arm.ID)
	}
}

func TestQualityTracker_InsufficientDataFallsBackToHeuristic(t *testing.T) {
	// Below minObservations (3), Quality() returns hasData=false and routing
	// must still succeed (falls back to heuristic scoring).
	caps := provider.Capabilities{ToolUse: true}
	arm := &router.Arm{ID: "test/arm-x", ModelName: "arm-x", Capabilities: caps}

	r := router.New(router.Config{})
	r.RegisterArm(arm)

	// Only 1 observation — below the minimum.
	r.ReportOutcome(router.Outcome{ArmID: "test/arm-x", TaskType: router.TaskGeneration, Success: true})

	qt := r.QualityTracker()
	_, hasData := qt.Quality("test/arm-x", router.TaskGeneration)
	if hasData {
		t.Error("expected no usable data below minObservations")
	}

	// Router.Select must still succeed despite no quality data.
	task := router.Task{Type: router.TaskGeneration, RequiresTools: true}
	decision := r.Select(task)
	if decision.Error != nil {
		t.Errorf("Select should succeed via heuristic fallback: %v", decision.Error)
	}
	decision.Rollback()
}
