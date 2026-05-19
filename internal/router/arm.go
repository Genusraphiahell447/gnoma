package router

import (
	"strings"
	"sync"
	"time"

	"somegit.dev/Owlibou/gnoma/internal/provider"
)

// ArmID uniquely identifies a model+provider pair.
type ArmID string

// Arm represents a provider+model pair available for routing.
type Arm struct {
	ID           ArmID
	Provider     provider.Provider
	ModelName    string
	IsLocal      bool
	IsCLIAgent   bool // subprocess-based CLI agent (claude, gemini, vibe); tier 0 in routing
	Disabled     bool // excluded from auto-routing; still reachable via ForceArm
	Capabilities provider.Capabilities
	Pools        []*LimitPool

	// BackoffUntil is the time until which this arm is temporarily disabled (e.g. 429).
	BackoffUntil time.Time
	mu           sync.RWMutex

	// MaxComplexity is a hard ceiling on task complexity this arm will accept.
	// Zero means no ceiling (default for all existing arms).
	MaxComplexity float64

	// Strengths lists task types where this arm is preferred. When any
	// listed task type matches an incoming task, the arm crosses tier
	// boundaries during selection — Opus tagged with TaskSecurityReview
	// can beat a CLI-agent tier-1 arm for that task type, for example.
	// Strengths are a preference, not a pin: if no strength-matching arm
	// is feasible (rate-limited, backoff), selection falls back to the
	// default tier order.
	Strengths []TaskType

	// CostWeight scales how much per-arm cost matters during scoring.
	// effectiveCost = 1 + CostWeight*(cost-1):
	//   - 1.0 (or zero, which is normalized to 1.0): current behavior.
	//   - 0.5: half-weight cost — pricey arms penalized less.
	//   - 0.0: cost ignored, pure quality wins.
	// Use sub-1.0 values for task types where being right matters more
	// than being cheap (e.g. SecurityReview).
	CostWeight float64

	// Cost per 1k tokens (EUR, estimated)
	CostPer1kInput  float64
	CostPer1kOutput float64

	// Live performance metrics, updated after each completed request.
	Perf ArmPerf
}

// NewArmID creates an arm ID from provider name and model.
func NewArmID(providerName, model string) ArmID {
	return ArmID(providerName + "/" + model)
}

// Provider returns the provider portion of the arm ID (before the first "/").
func (id ArmID) Provider() string {
	if i := strings.IndexByte(string(id), '/'); i >= 0 {
		return string(id[:i])
	}
	return string(id)
}

// Model returns the model portion of the arm ID (after the first "/").
func (id ArmID) Model() string {
	if i := strings.IndexByte(string(id), '/'); i >= 0 {
		return string(id[i+1:])
	}
	return string(id)
}

// EstimateCost returns estimated cost in EUR for a task.
func (a *Arm) EstimateCost(estimatedTokens int) float64 {
	// Rough estimate: 60% input, 40% output
	inputTokens := float64(estimatedTokens) * 0.6
	outputTokens := float64(estimatedTokens) * 0.4
	return (inputTokens/1000)*a.CostPer1kInput + (outputTokens/1000)*a.CostPer1kOutput
}

// SupportsTools returns true if this arm's model supports function calling.
func (a *Arm) SupportsTools() bool {
	return a.Capabilities.ToolUse
}

// HasStrength reports whether the arm is tagged as strong at the given task
// type. Used by the selector to consider cross-tier promotion.
func (a *Arm) HasStrength(t TaskType) bool {
	for _, s := range a.Strengths {
		if s == t {
			return true
		}
	}
	return false
}

// ResolvedCostWeight normalizes the CostWeight field. A zero value means
// "unset" and is treated as 1.0 (current full-cost behavior). Users who
// want minimal cost influence set a small positive value like 0.05 — no
// real use case wants exactly zero ("ignore cost entirely") and 0 doubles
// as the Go zero value for arms registered before this field existed.
func (a *Arm) ResolvedCostWeight() float64 {
	if a.CostWeight == 0 {
		return 1.0
	}
	return a.CostWeight
}

// perfAlpha is the EMA smoothing factor for ArmPerf updates (0.3 = ~3-sample memory).
const perfAlpha = 0.3

// ArmPerf tracks live performance metrics using an exponential moving average.
// Updated after each completed stream. Safe for concurrent use.
type ArmPerf struct {
	mu         sync.Mutex
	TTFTMs     float64 // time to first token, EMA in milliseconds
	ToksPerSec float64 // output throughput, EMA in tokens/second
	Samples    int     // total observations recorded
}

// Update records a single observation into the EMA.
// ttft: elapsed time from stream start to first text token.
// outputTokens: tokens generated in this response.
// streamDuration: total time the stream was active (first call to last event).
func (p *ArmPerf) Update(ttft time.Duration, outputTokens int, streamDuration time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()

	ttftMs := float64(ttft.Milliseconds())
	var tps float64
	if streamDuration > 0 {
		tps = float64(outputTokens) / streamDuration.Seconds()
	}

	if p.Samples == 0 {
		p.TTFTMs = ttftMs
		p.ToksPerSec = tps
	} else {
		p.TTFTMs = perfAlpha*ttftMs + (1-perfAlpha)*p.TTFTMs
		p.ToksPerSec = perfAlpha*tps + (1-perfAlpha)*p.ToksPerSec
	}
	p.Samples++
}

// SetBackoff sets a temporary disablement until the given time.
func (a *Arm) SetBackoff(until time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.BackoffUntil = until
}

// InBackoff returns true if the arm is currently in a backoff period.
func (a *Arm) InBackoff() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return !a.BackoffUntil.IsZero() && time.Now().Before(a.BackoffUntil)
}
