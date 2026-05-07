package anthropic

import (
	"context"
	"testing"

	"somegit.dev/Owlibou/gnoma/internal/provider"
)

func TestModels_Fallback(t *testing.T) {
	// Test with invalid API key - should fall back to hardcoded list
	cfg := provider.ProviderConfig{
		APIKey:  "invalid-key",
		BaseURL: "https://api.anthropic.com/v1",
	}
	p, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	models, err := p.Models(context.Background())
	if err != nil {
		t.Fatalf("Models() error = %v", err)
	}

	// Should return fallback models
	if len(models) == 0 {
		t.Fatal("Models() returned empty list, expected fallback models")
	}

	// Check that we have the expected fallback models
	modelIDs := make(map[string]bool)
	for _, m := range models {
		modelIDs[m.ID] = true
	}

	// Verify some expected models are present
	expectedModels := []string{"claude-opus-4-20250514", "claude-sonnet-4-20250514", "claude-haiku-4-5-20251001"}
	for _, expected := range expectedModels {
		if !modelIDs[expected] {
			t.Errorf("Expected model %q not found in fallback list", expected)
		}
	}
}

func TestInferAnthropicModelCapabilities(t *testing.T) {
	tests := []struct {
		modelID string
		want    provider.Capabilities
	}{
		{
			modelID: "claude-opus-4-20250514",
			want: provider.Capabilities{
				ToolUse:       true,
				JSONOutput:    true,
				Vision:        true,
				ThinkingModes: []provider.EffortLevel{provider.EffortLow, provider.EffortMedium, provider.EffortHigh},
				ContextWindow: 200000,
				MaxOutput:     32000,
			},
		},
		{
			modelID: "claude-3-opus-20240229",
			want: provider.Capabilities{
				ToolUse:       true,
				JSONOutput:    true,
				Vision:        true,
				ThinkingModes: []provider.EffortLevel{provider.EffortLow, provider.EffortMedium, provider.EffortHigh},
				ContextWindow: 200000,
				MaxOutput:     4096,
			},
		},
		{
			modelID: "claude-2",
			want: provider.Capabilities{
				ToolUse:       true,
				JSONOutput:    true,
				Vision:        true,
				ThinkingModes: nil,
				ContextWindow: 100000,
				MaxOutput:     4096,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			got := inferAnthropicModelCapabilities(tt.modelID)
			if got.ToolUse != tt.want.ToolUse {
				t.Errorf("ToolUse = %v, want %v", got.ToolUse, tt.want.ToolUse)
			}
			if got.JSONOutput != tt.want.JSONOutput {
				t.Errorf("JSONOutput = %v, want %v", got.JSONOutput, tt.want.JSONOutput)
			}
			if got.Vision != tt.want.Vision {
				t.Errorf("Vision = %v, want %v", got.Vision, tt.want.Vision)
			}
			if got.ContextWindow != tt.want.ContextWindow {
				t.Errorf("ContextWindow = %v, want %v", got.ContextWindow, tt.want.ContextWindow)
			}
			if got.MaxOutput != tt.want.MaxOutput {
				t.Errorf("MaxOutput = %v, want %v", got.MaxOutput, tt.want.MaxOutput)
			}
			// Check ThinkingModes
			if tt.want.ThinkingModes == nil {
				if len(got.ThinkingModes) != 0 {
					t.Errorf("ThinkingModes should be empty, got %v", got.ThinkingModes)
				}
			} else if len(got.ThinkingModes) != len(tt.want.ThinkingModes) {
				t.Errorf("ThinkingModes length = %v, want %v", len(got.ThinkingModes), len(tt.want.ThinkingModes))
			}
		})
	}
}
