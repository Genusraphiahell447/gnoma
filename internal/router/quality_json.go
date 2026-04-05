package router

// QualitySnapshot is the serializable form of QualityTracker EMA data.
type QualitySnapshot struct {
	Scores map[string]map[string]*EMAScore `json:"scores"` // ArmID -> TaskType.String() -> score
}

// Snapshot returns a serializable copy of current quality data.
func (qt *QualityTracker) Snapshot() QualitySnapshot {
	qt.mu.RLock()
	defer qt.mu.RUnlock()
	snap := QualitySnapshot{
		Scores: make(map[string]map[string]*EMAScore),
	}
	for armID, tasks := range qt.scores {
		snap.Scores[string(armID)] = make(map[string]*EMAScore)
		for taskType, score := range tasks {
			snap.Scores[string(armID)][taskType.String()] = &EMAScore{
				Value: score.Value,
				Count: score.Count,
			}
		}
	}
	return snap
}

// Restore loads previously persisted quality data.
// Replaces all current scores.
func (qt *QualityTracker) Restore(snap QualitySnapshot) {
	qt.mu.Lock()
	defer qt.mu.Unlock()
	qt.scores = make(map[ArmID]map[TaskType]*EMAScore)
	for armID, tasks := range snap.Scores {
		qt.scores[ArmID(armID)] = make(map[TaskType]*EMAScore)
		for taskTypeStr, score := range tasks {
			taskType, ok := parseTaskType(taskTypeStr)
			if !ok {
				continue // skip unrecognized task types
			}
			qt.scores[ArmID(armID)][taskType] = &EMAScore{
				Value: score.Value,
				Count: score.Count,
			}
		}
	}
}

// parseTaskType maps a TaskType string representation back to its constant.
func parseTaskType(s string) (TaskType, bool) {
	for _, tt := range allTaskTypes {
		if tt.String() == s {
			return tt, true
		}
	}
	return 0, false
}

// allTaskTypes lists every known TaskType constant for reverse lookup.
var allTaskTypes = []TaskType{
	TaskBoilerplate,
	TaskGeneration,
	TaskRefactor,
	TaskReview,
	TaskUnitTest,
	TaskPlanning,
	TaskOrchestration,
	TaskSecurityReview,
	TaskDebug,
	TaskExplain,
}
