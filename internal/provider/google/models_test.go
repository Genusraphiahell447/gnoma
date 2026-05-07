package google

import (
	"context"
	"testing"

	"somegit.dev/Owlibou/gnoma/internal/provider"
)

func TestModels_Fallback(t *testing.T) {
	// Test with invalid API key - should fall back to hardcoded list
	cfg := provider.ProviderConfig{
		APIKey:  "invalid-key",
		BaseURL: "https://generativelanguage.googleapis.com",
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
	expectedModels := []string{"gemini-2.5-pro", "gemini-2.5-flash", "gemini-2.0-flash"}
	for _, expected := range expectedModels {
		if !modelIDs[expected] {
			t.Errorf("Expected model %q not found in fallback list", expected)
		}
	}
}
