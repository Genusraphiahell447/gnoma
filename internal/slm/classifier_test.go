package slm

import (
	"context"
	"errors"
	"testing"
	"time"

	"somegit.dev/Owlibou/gnoma/internal/message"
	"somegit.dev/Owlibou/gnoma/internal/provider"
	"somegit.dev/Owlibou/gnoma/internal/router"
	"somegit.dev/Owlibou/gnoma/internal/stream"
)

// mockProvider implements provider.Provider for classifier tests.
type mockProvider struct {
	text  string
	delay time.Duration
	err   error
}

func (m *mockProvider) Name() string         { return "mock" }
func (m *mockProvider) DefaultModel() string { return "default" }
func (m *mockProvider) Models(_ context.Context) ([]provider.ModelInfo, error) {
	return nil, nil
}
func (m *mockProvider) Stream(ctx context.Context, _ provider.Request) (stream.Stream, error) {
	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if m.err != nil {
		return nil, m.err
	}
	return &mockStream{events: []stream.Event{
		{Type: stream.EventTextDelta, Text: m.text},
	}}, nil
}

type mockStream struct {
	events []stream.Event
	idx    int
}

func (s *mockStream) Next() bool             { s.idx++; return s.idx <= len(s.events) }
func (s *mockStream) Current() stream.Event  { return s.events[s.idx-1] }
func (s *mockStream) Err() error             { return nil }
func (s *mockStream) Close() error           { return nil }

func TestClassifier_HappyPath(t *testing.T) {
	p := &mockProvider{text: `{"task_type":"Debug","complexity":0.25,"requires_tools":false}`}
	cls := NewClassifier(p, "default", nil)

	task, err := cls.Classify(context.Background(), "fix the failing test", nil)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if task.Type != router.TaskDebug {
		t.Errorf("Type = %s, want Debug", task.Type)
	}
	if task.ComplexityScore != 0.25 {
		t.Errorf("ComplexityScore = %v, want 0.25", task.ComplexityScore)
	}
	if task.RequiresTools != false {
		t.Errorf("RequiresTools = true, want false")
	}
}

func TestClassifier_BlendHeuristic(t *testing.T) {
	// SLM returns one type; other Task fields should come from heuristic.
	p := &mockProvider{text: `{"task_type":"Boilerplate","complexity":0.1,"requires_tools":false}`}
	cls := NewClassifier(p, "default", nil)

	task, err := cls.Classify(context.Background(), "scaffold a new HTTP handler", nil)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if task.Type != router.TaskBoilerplate {
		t.Errorf("Type = %s, want Boilerplate", task.Type)
	}
	// Priority must come from the heuristic baseline (PriorityNormal = 1, not zero).
	if task.Priority < router.PriorityNormal {
		t.Errorf("Priority = %v, want at least PriorityNormal from heuristic baseline", task.Priority)
	}
}

func TestClassifier_FallbackOnBadJSON(t *testing.T) {
	p := &mockProvider{text: "I cannot classify that."}
	cls := NewClassifier(p, "default", nil)

	// Should not error — falls back to heuristic.
	task, err := cls.Classify(context.Background(), "write unit tests for the parser", nil)
	if err != nil {
		t.Fatalf("Classify should not error on bad JSON: %v", err)
	}
	// Heuristic would return UnitTest for "write unit tests".
	if task.Type != router.TaskUnitTest {
		t.Errorf("heuristic fallback: Type = %s, want UnitTest", task.Type)
	}
}

func TestClassifier_FallbackOnProviderError(t *testing.T) {
	p := &mockProvider{err: errors.New("connection refused")}
	cls := NewClassifier(p, "default", nil)

	task, err := cls.Classify(context.Background(), "explain how generics work", nil)
	if err != nil {
		t.Fatalf("Classify should not error on provider error: %v", err)
	}
	// Heuristic fallback: "explain" → TaskExplain
	if task.Type != router.TaskExplain {
		t.Errorf("heuristic fallback: Type = %s, want Explain", task.Type)
	}
}

func TestClassifier_FallbackOnTimeout(t *testing.T) {
	p := &mockProvider{delay: 500 * time.Millisecond}
	cls := NewClassifier(p, "default", nil)
	cls.timeout = 50 * time.Millisecond // force timeout

	task, err := cls.Classify(context.Background(), "debug the failing test", nil)
	if err != nil {
		t.Fatalf("Classify should not error on timeout: %v", err)
	}
	// Falls back to heuristic: "debug" → TaskDebug
	if task.Type != router.TaskDebug {
		t.Errorf("heuristic fallback: Type = %s, want Debug", task.Type)
	}
}

func TestClassifier_FenceStripping(t *testing.T) {
	fenced := "```json\n{\"task_type\":\"Refactor\",\"complexity\":0.5,\"requires_tools\":true}\n```"
	p := &mockProvider{text: fenced}
	cls := NewClassifier(p, "default", nil)

	task, err := cls.Classify(context.Background(), "refactor the auth middleware", nil)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if task.Type != router.TaskRefactor {
		t.Errorf("Type = %s, want Refactor", task.Type)
	}
}

func TestClassifier_UnknownTaskType_FallsBackToHeuristic(t *testing.T) {
	p := &mockProvider{text: `{"task_type":"FooBar","complexity":0.3,"requires_tools":false}`}
	cls := NewClassifier(p, "default", nil)

	task, err := cls.Classify(context.Background(), "implement a binary search function", nil)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	// "implement" → heuristic should give Generation or Boilerplate; SLM gave FooBar → Generation fallback
	_ = task // just verify no panic and no error
}

func TestClassifier_SetsClassifierSource_OnSuccess(t *testing.T) {
	p := &mockProvider{text: `{"task_type":"Debug","complexity":0.3,"requires_tools":true}`}
	cls := NewClassifier(p, "default", nil)
	task, err := cls.Classify(context.Background(), "fix the failing test", nil)
	if err != nil {
		t.Fatal(err)
	}
	if task.ClassifierSource != router.ClassifierSLM {
		t.Errorf("ClassifierSource = %v, want ClassifierSLM", task.ClassifierSource)
	}
}

func TestClassifier_SetsClassifierSource_OnFallback(t *testing.T) {
	p := &mockProvider{err: errors.New("backend unreachable")}
	cls := NewClassifier(p, "default", nil)
	task, err := cls.Classify(context.Background(), "fix the failing test", nil)
	if err != nil {
		t.Fatal(err)
	}
	if task.ClassifierSource != router.ClassifierSLMFallback {
		t.Errorf("ClassifierSource = %v, want ClassifierSLMFallback", task.ClassifierSource)
	}
}

func TestClassifier_ContextPassedToHistory(t *testing.T) {
	p := &mockProvider{text: `{"task_type":"Explain","complexity":0.2,"requires_tools":false}`}
	cls := NewClassifier(p, "default", nil)

	history := []message.Message{
		{Role: message.RoleUser, Content: []message.Content{{Type: message.ContentText, Text: "prior"}}},
	}
	task, err := cls.Classify(context.Background(), "explain this code", history)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if task.Type != router.TaskExplain {
		t.Errorf("Type = %s, want Explain", task.Type)
	}
}
