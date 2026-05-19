package router

import (
	"context"
	"testing"

	"somegit.dev/Owlibou/gnoma/internal/message"
)

// TestHeuristicClassifier_ParityWithClassifyTask verifies that
// HeuristicClassifier.Classify produces identical results to ClassifyTask.
func TestHeuristicClassifier_ParityWithClassifyTask(t *testing.T) {
	prompts := []string{
		"debug the failing test",
		"explain how generics work",
		"implement a new HTTP handler",
		"refactor the auth middleware",
		"security audit the login flow",
		"write unit tests for the parser",
		"scaffold a new service",
		"plan the migration strategy",
		"orchestrate the deployment pipeline",
		"review the pull request",
	}

	cls := HeuristicClassifier{}
	ctx := context.Background()
	var noHistory []message.Message

	for _, p := range prompts {
		want := ClassifyTask(p)
		got, err := cls.Classify(ctx, p, noHistory)
		if err != nil {
			t.Errorf("Classify(%q) unexpected error: %v", p, err)
			continue
		}
		if got.Type != want.Type {
			t.Errorf("Classify(%q).Type = %s, want %s", p, got.Type, want.Type)
		}
		if got.ComplexityScore != want.ComplexityScore {
			t.Errorf("Classify(%q).ComplexityScore = %v, want %v", p, got.ComplexityScore, want.ComplexityScore)
		}
		if got.RequiresTools != want.RequiresTools {
			t.Errorf("Classify(%q).RequiresTools = %v, want %v", p, got.RequiresTools, want.RequiresTools)
		}
		if got.Priority != want.Priority {
			t.Errorf("Classify(%q).Priority = %v, want %v", p, got.Priority, want.Priority)
		}
	}
}

func TestHeuristicClassifier_SetsClassifierSource(t *testing.T) {
	got, err := HeuristicClassifier{}.Classify(context.Background(), "implement foo", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.ClassifierSource != ClassifierHeuristic {
		t.Errorf("ClassifierSource = %v, want ClassifierHeuristic", got.ClassifierSource)
	}
}

func TestClassifierSource_String(t *testing.T) {
	cases := []struct {
		src  ClassifierSource
		want string
	}{
		{ClassifierUnknown, "unknown"},
		{ClassifierHeuristic, "heuristic"},
		{ClassifierSLM, "slm"},
		{ClassifierSLMFallback, "slm_fallback"},
		{ClassifierSource(999), "unknown"},
	}
	for _, tc := range cases {
		if got := tc.src.String(); got != tc.want {
			t.Errorf("ClassifierSource(%d).String() = %q, want %q", tc.src, got, tc.want)
		}
	}
}

// TestHeuristicClassifier_IgnoresHistory verifies that history has no effect
// on the heuristic classifier (it operates only on the prompt).
func TestHeuristicClassifier_IgnoresHistory(t *testing.T) {
	cls := HeuristicClassifier{}
	ctx := context.Background()
	prompt := "implement a binary search function"

	withoutHistory, _ := cls.Classify(ctx, prompt, nil)
	withHistory, _ := cls.Classify(ctx, prompt, []message.Message{
		{Role: message.RoleUser, Content: []message.Content{{Type: message.ContentText, Text: "previous message"}}},
	})

	if withoutHistory.Type != withHistory.Type {
		t.Errorf("history should not affect HeuristicClassifier: got %s vs %s",
			withoutHistory.Type, withHistory.Type)
	}
}
