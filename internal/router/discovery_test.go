package router

import (
	"context"
	"log/slog"
	"testing"

	"somegit.dev/Owlibou/gnoma/internal/provider"
	"somegit.dev/Owlibou/gnoma/internal/stream"
)

// --- ArmID helpers ---

func TestArmID_Provider(t *testing.T) {
	tests := []struct {
		id   ArmID
		want string
	}{
		{"llamacpp/gemma-26b", "llamacpp"},
		{"anthropic/claude-sonnet", "anthropic"},
		{"single", "single"},
	}
	for _, tt := range tests {
		if got := tt.id.Provider(); got != tt.want {
			t.Errorf("ArmID(%q).Provider() = %q, want %q", tt.id, got, tt.want)
		}
	}
}

func TestArmID_Model(t *testing.T) {
	tests := []struct {
		id   ArmID
		want string
	}{
		{"llamacpp/gemma-26b", "gemma-26b"},
		{"anthropic/claude-sonnet", "claude-sonnet"},
		{"single", "single"},
	}
	for _, tt := range tests {
		if got := tt.id.Model(); got != tt.want {
			t.Errorf("ArmID(%q).Model() = %q, want %q", tt.id, got, tt.want)
		}
	}
}

// --- reconcileArms ---

func noopFactory(name, model string) provider.Provider { return nil }

func dummyArm(id ArmID, local bool) *Arm {
	return &Arm{
		ID:           id,
		ModelName:    id.Model(),
		IsLocal:      local,
		Capabilities: provider.Capabilities{ToolUse: true, ContextWindow: 8192},
	}
}

func TestReconcileArms_ForcedDefaultArm_ReconciledToDiscovered(t *testing.T) {
	r := New(Config{})
	r.RegisterArm(dummyArm("llamacpp/default", true))
	r.ForceArm("llamacpp/default")

	discovered := []DiscoveredModel{
		{ID: "gemma-26b", Provider: "llamacpp", SupportsTools: true},
	}

	var reconciled ArmID
	onReconcile := func(id ArmID) { reconciled = id }

	reconcileArms(r, discovered, noopFactory, slog.Default(), onReconcile)

	if got := r.ForcedArm(); got != "llamacpp/gemma-26b" {
		t.Errorf("ForcedArm() = %q, want %q", got, "llamacpp/gemma-26b")
	}
	if reconciled != "llamacpp/gemma-26b" {
		t.Errorf("onReconcile called with %q, want %q", reconciled, "llamacpp/gemma-26b")
	}

	// Select should succeed with the reconciled arm
	decision := r.Select(Task{Type: TaskGeneration})
	if decision.Error != nil {
		t.Fatalf("Select after reconcile: %v", decision.Error)
	}
	if decision.Arm.ID != "llamacpp/gemma-26b" {
		t.Errorf("Select returned %q, want %q", decision.Arm.ID, "llamacpp/gemma-26b")
	}
}

func TestReconcileArms_ForcedArm_AlreadyCorrect(t *testing.T) {
	r := New(Config{})
	r.RegisterArm(dummyArm("llamacpp/gemma-26b", true))
	r.ForceArm("llamacpp/gemma-26b")

	discovered := []DiscoveredModel{
		{ID: "gemma-26b", Provider: "llamacpp", SupportsTools: true},
	}

	var called bool
	onReconcile := func(id ArmID) { called = true }

	reconcileArms(r, discovered, noopFactory, slog.Default(), onReconcile)

	if got := r.ForcedArm(); got != "llamacpp/gemma-26b" {
		t.Errorf("ForcedArm() = %q, want %q", got, "llamacpp/gemma-26b")
	}
	if called {
		t.Error("onReconcile should not be called when arm is already correct")
	}

	decision := r.Select(Task{Type: TaskGeneration})
	if decision.Error != nil {
		t.Fatalf("Select: %v", decision.Error)
	}
}

func TestReconcileArms_ForcedArm_NonLocal(t *testing.T) {
	r := New(Config{})
	r.RegisterArm(dummyArm("anthropic/claude", false))
	r.ForceArm("anthropic/claude")

	discovered := []DiscoveredModel{
		{ID: "gemma-26b", Provider: "llamacpp", SupportsTools: true},
	}

	reconcileArms(r, discovered, noopFactory, slog.Default(), nil)

	if got := r.ForcedArm(); got != "anthropic/claude" {
		t.Errorf("ForcedArm() = %q, want %q (non-local forced arm should be untouched)", got, "anthropic/claude")
	}
}

func TestReconcileArms_NoForcedArm(t *testing.T) {
	r := New(Config{})
	existing := dummyArm("llamacpp/old-model", true)
	r.RegisterArm(existing)

	discovered := []DiscoveredModel{
		{ID: "gemma-26b", Provider: "llamacpp", SupportsTools: true},
	}

	factory := func(name, model string) provider.Provider {
		return &stubProvider{name: name, model: model}
	}

	reconcileArms(r, discovered, factory, slog.Default(), nil)

	// Old arm should be removed (disappeared)
	if _, ok := r.LookupArm("llamacpp/old-model"); ok {
		t.Error("disappeared arm should be removed")
	}
	// New arm should be registered
	if _, ok := r.LookupArm("llamacpp/gemma-26b"); !ok {
		t.Error("discovered arm should be registered")
	}
}

func TestReconcileArms_MultipleModelsForForcedProvider(t *testing.T) {
	r := New(Config{})
	r.RegisterArm(dummyArm("llamacpp/default", true))
	r.ForceArm("llamacpp/default")

	discovered := []DiscoveredModel{
		{ID: "gemma-26b", Provider: "llamacpp", SupportsTools: true},
		{ID: "phi-3", Provider: "llamacpp", SupportsTools: false},
	}

	var reconciled ArmID
	onReconcile := func(id ArmID) { reconciled = id }

	reconcileArms(r, discovered, noopFactory, slog.Default(), onReconcile)

	// Should reconcile to the first match
	if got := r.ForcedArm(); got != "llamacpp/gemma-26b" {
		t.Errorf("ForcedArm() = %q, want %q", got, "llamacpp/gemma-26b")
	}
	if reconciled != "llamacpp/gemma-26b" {
		t.Errorf("onReconcile = %q, want %q", reconciled, "llamacpp/gemma-26b")
	}
}

func TestReconcileArms_NoModelsForForcedProvider(t *testing.T) {
	r := New(Config{})
	r.RegisterArm(dummyArm("llamacpp/default", true))
	r.ForceArm("llamacpp/default")

	// Discovery returns nothing (server down)
	discovered := []DiscoveredModel{}

	reconcileArms(r, discovered, noopFactory, slog.Default(), nil)

	// Forced arm must NOT be removed
	if got := r.ForcedArm(); got != "llamacpp/default" {
		t.Errorf("ForcedArm() = %q, want %q (forced arm should survive empty discovery)", got, "llamacpp/default")
	}
	if _, ok := r.LookupArm("llamacpp/default"); !ok {
		t.Error("forced arm should not be removed when discovery returns no models")
	}
}

// stubProvider satisfies provider.Provider for tests that need a non-nil provider.
type stubProvider struct {
	name  string
	model string
}

func (s *stubProvider) Name() string         { return s.name }
func (s *stubProvider) DefaultModel() string { return s.model }
func (s *stubProvider) Models(_ context.Context) ([]provider.ModelInfo, error) {
	return nil, nil
}
func (s *stubProvider) Stream(_ context.Context, _ provider.Request) (stream.Stream, error) {
	return nil, nil
}
