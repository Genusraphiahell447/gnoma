package router_test

import (
	"testing"

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
