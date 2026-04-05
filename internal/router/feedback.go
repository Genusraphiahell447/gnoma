package router

import "sync"

const (
	qualityAlpha    = 0.3 // EMA smoothing factor (~3-sample memory)
	minObservations = 3   // min samples before observed score overrides heuristic
)

// EMAScore tracks an exponential moving average quality score.
type EMAScore struct {
	Value float64
	Count int
}

// QualityTracker records per-arm, per-task-type EMA quality scores from elf outcomes.
type QualityTracker struct {
	mu     sync.RWMutex
	scores map[ArmID]map[TaskType]*EMAScore
}

// NewQualityTracker returns an empty QualityTracker.
func NewQualityTracker() *QualityTracker {
	return &QualityTracker{
		scores: make(map[ArmID]map[TaskType]*EMAScore),
	}
}

// Record updates the EMA score for the given arm and task type.
func (qt *QualityTracker) Record(armID ArmID, taskType TaskType, success bool) {
	observation := 0.0
	if success {
		observation = 1.0
	}
	qt.mu.Lock()
	defer qt.mu.Unlock()
	if qt.scores[armID] == nil {
		qt.scores[armID] = make(map[TaskType]*EMAScore)
	}
	s := qt.scores[armID][taskType]
	if s == nil {
		s = &EMAScore{}
		qt.scores[armID][taskType] = s
	}
	if s.Count == 0 {
		s.Value = observation
	} else {
		s.Value = qualityAlpha*observation + (1-qualityAlpha)*s.Value
	}
	s.Count++
}

// Quality returns the observed EMA score for an arm+task combination.
// Returns (0, false) when fewer than minObservations have been recorded.
func (qt *QualityTracker) Quality(armID ArmID, taskType TaskType) (score float64, hasData bool) {
	qt.mu.RLock()
	defer qt.mu.RUnlock()
	m, ok := qt.scores[armID]
	if !ok {
		return 0, false
	}
	s, ok := m[taskType]
	if !ok || s.Count < minObservations {
		return 0, false
	}
	return s.Value, true
}
