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
	Capabilities provider.Capabilities
	Pools        []*LimitPool

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
