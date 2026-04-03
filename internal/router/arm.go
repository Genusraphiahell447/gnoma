package router

import (
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
}

// NewArmID creates an arm ID from provider name and model.
func NewArmID(providerName, model string) ArmID {
	return ArmID(providerName + "/" + model)
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

// ArmPerf holds live performance metrics for an arm.
type ArmPerf struct {
	TTFT_P50_ms   float64 // time to first token, p50
	TTFT_P95_ms   float64 // time to first token, p95
	ToksPerSec    float64 // tokens per second throughput
}
