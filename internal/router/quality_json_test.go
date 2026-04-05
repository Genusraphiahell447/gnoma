package router_test

import (
	"encoding/json"
	"testing"

	"somegit.dev/Owlibou/gnoma/internal/router"
)

func TestQualityTracker_SnapshotRestore_RoundTrip(t *testing.T) {
	qt := router.NewQualityTracker()
	// Record some outcomes
	qt.Record("anthropic/claude-3-5-sonnet", router.TaskGeneration, true)
	qt.Record("anthropic/claude-3-5-sonnet", router.TaskGeneration, true)
	qt.Record("anthropic/claude-3-5-sonnet", router.TaskGeneration, false)
	qt.Record("ollama/gemma3", router.TaskBoilerplate, true)

	snap := qt.Snapshot()

	// Verify snapshot has the data
	if len(snap.Scores) == 0 {
		t.Fatal("snapshot scores should not be empty")
	}

	// Marshal and unmarshal to simulate disk persistence
	data, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	var restored router.QualitySnapshot
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}

	// Restore into a fresh tracker
	qt2 := router.NewQualityTracker()
	qt2.Restore(restored)

	// After restore, Quality() should return data (Count >= minObservations=3)
	score, hasData := qt2.Quality("anthropic/claude-3-5-sonnet", router.TaskGeneration)
	if !hasData {
		t.Error("expected quality data after restore")
	}
	if score <= 0 {
		t.Errorf("expected positive score, got %f", score)
	}
}

func TestQualityTracker_Snapshot_Empty(t *testing.T) {
	qt := router.NewQualityTracker()
	snap := qt.Snapshot()
	if snap.Scores == nil {
		t.Error("scores map should be initialized (not nil)")
	}
	if len(snap.Scores) != 0 {
		t.Errorf("expected empty scores, got %d", len(snap.Scores))
	}
}

func TestQualityTracker_Restore_Replaces(t *testing.T) {
	qt := router.NewQualityTracker()
	qt.Record("arm-a", router.TaskDebug, true)
	qt.Record("arm-a", router.TaskDebug, true)
	qt.Record("arm-a", router.TaskDebug, true)

	// Restore with different data — old data should be gone
	empty := router.QualitySnapshot{Scores: make(map[string]map[string]*router.EMAScore)}
	qt.Restore(empty)

	_, hasData := qt.Quality("arm-a", router.TaskDebug)
	if hasData {
		t.Error("old data should be gone after restore with empty snapshot")
	}
}
