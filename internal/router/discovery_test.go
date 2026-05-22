package router

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"somegit.dev/Owlibou/gnoma/internal/provider"
	"somegit.dev/Owlibou/gnoma/internal/security"
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

func noopFactory(name, model string) SecureProvider { return nil }

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

	factory := func(name, model string) SecureProvider {
		return security.WrapProvider(&stubProvider{name: name, model: model}, nil)
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

// --- DiscoverOllama / cache + default context size ---

// ollamaStub serves a configurable /api/tags response and a no-op /api/show.
// tagsBody is the JSON body returned for /api/tags. showFunc, if set, handles
// /api/show; otherwise the default empty template / parameters response is
// used (probe returns false, 0).
type ollamaStub struct {
	tagsBody string
	showFunc func(model string) (template, parameters string)
}

func (s *ollamaStub) server() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			_, _ = w.Write([]byte(s.tagsBody))
		case "/api/show":
			body := make([]byte, 256)
			n, _ := r.Body.Read(body)
			req := string(body[:n])
			modelName := ""
			// crude parse: pull the value of "name" from {"name":"..."}
			if i := strings.Index(req, `"name":"`); i >= 0 {
				rest := req[i+len(`"name":"`):]
				if j := strings.IndexByte(rest, '"'); j >= 0 {
					modelName = rest[:j]
				}
			}
			tmpl, params := "", ""
			if s.showFunc != nil {
				tmpl, params = s.showFunc(modelName)
			}
			_, _ = fmt.Fprintf(w, `{"template":%q,"parameters":%q}`, tmpl, params)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

// TestDiscoverOllama_AppliesDefaultContextSize verifies that a model whose
// /api/show response contains no num_ctx still gets the conservative default
// rather than ContextSize=0 (which the router treats as "tiny").
func TestDiscoverOllama_AppliesDefaultContextSize(t *testing.T) {
	stub := &ollamaStub{
		tagsBody: `{"models":[{"name":"llama3:8b","size":1}]}`,
		showFunc: func(_ string) (string, string) {
			return ".Tool", "" // tool support yes, no num_ctx line
		},
	}
	srv := stub.server()
	defer srv.Close()

	cache := map[string]OllamaProbeResult{}
	models, err := DiscoverOllama(context.Background(), srv.URL, cache)
	if err != nil {
		t.Fatalf("DiscoverOllama: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("got %d models, want 1", len(models))
	}
	if models[0].ContextSize != defaultOllamaContextSize {
		t.Errorf("ContextSize = %d, want %d", models[0].ContextSize, defaultOllamaContextSize)
	}
	if !models[0].SupportsTools {
		t.Error("SupportsTools should be true (template contained .Tool)")
	}
}

// TestDiscoverOllama_PrunesCacheOnDisappearance verifies that toolCache entries
// for models no longer present in /api/tags are pruned, preventing unbounded
// cache growth and stale verdicts on reappearing models.
func TestDiscoverOllama_PrunesCacheOnDisappearance(t *testing.T) {
	stub := &ollamaStub{
		tagsBody: `{"models":[{"name":"alive:latest","size":1}]}`,
	}
	srv := stub.server()
	defer srv.Close()

	cache := map[string]OllamaProbeResult{
		"alive:latest":  {SupportsTools: true},
		"ghost:latest":  {SupportsTools: true}, // not in tags response — must be pruned
		"another-ghost": {},
	}
	if _, err := DiscoverOllama(context.Background(), srv.URL, cache); err != nil {
		t.Fatalf("DiscoverOllama: %v", err)
	}

	if _, ok := cache["alive:latest"]; !ok {
		t.Error("alive:latest should remain in cache")
	}
	if _, ok := cache["ghost:latest"]; ok {
		t.Error("ghost:latest should have been pruned from cache")
	}
	if _, ok := cache["another-ghost"]; ok {
		t.Error("another-ghost should have been pruned from cache")
	}
}

// llamaCPPStub serves configurable /v1/models and /props responses.
type llamaCPPStub struct {
	modelsBody string // body for /v1/models, or empty to return 404
	propsBody  string // body for /props, or empty to return 404
	modelsCode int    // override status code for /v1/models (0 = 200)
	propsCode  int    // override status code for /props (0 = 200)
}

func (s *llamaCPPStub) server() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			if s.modelsBody == "" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			if s.modelsCode != 0 {
				w.WriteHeader(s.modelsCode)
			}
			_, _ = w.Write([]byte(s.modelsBody))
		case "/props":
			if s.propsBody == "" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			if s.propsCode != 0 {
				w.WriteHeader(s.propsCode)
			}
			_, _ = w.Write([]byte(s.propsBody))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

// TestDiscoverLlamaCPP_EnumeratesMultipleModels verifies that a server (or
// front-proxy like llama-swap) returning multiple /v1/models entries is fully
// enumerated, not collapsed into a single placeholder.
func TestDiscoverLlamaCPP_EnumeratesMultipleModels(t *testing.T) {
	stub := &llamaCPPStub{
		modelsBody: `{"data":[{"id":"qwen2.5-coder:7b"},{"id":"llama-3.2:3b"}]}`,
		propsBody:  `{"default_generation_settings":{"n_ctx":16384}}`,
	}
	srv := stub.server()
	defer srv.Close()

	models, err := DiscoverLlamaCPP(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("DiscoverLlamaCPP: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("got %d models, want 2", len(models))
	}
	want := map[string]bool{"qwen2.5-coder:7b": true, "llama-3.2:3b": true}
	for _, m := range models {
		if !want[m.ID] {
			t.Errorf("unexpected model id %q", m.ID)
		}
		if m.ContextSize != 16384 {
			t.Errorf("ContextSize = %d, want 16384 (shared from /props)", m.ContextSize)
		}
		if !m.SupportsTools {
			t.Errorf("model %q: SupportsTools should be true", m.ID)
		}
	}
}

// TestDiscoverLlamaCPP_PropsFailsFallsBackToDefault verifies that when /props
// is unreachable (older builds, custom proxies that don't expose it), we
// still enumerate models and apply the conservative context-size default
// rather than aborting discovery.
func TestDiscoverLlamaCPP_PropsFailsFallsBackToDefault(t *testing.T) {
	stub := &llamaCPPStub{
		modelsBody: `{"data":[{"id":"only-model"}]}`,
		// propsBody empty -> 404
	}
	srv := stub.server()
	defer srv.Close()

	models, err := DiscoverLlamaCPP(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("DiscoverLlamaCPP: %v", err)
	}
	if len(models) != 1 || models[0].ID != "only-model" {
		t.Fatalf("got %+v, want one entry with id=only-model", models)
	}
	if models[0].ContextSize != defaultLlamaCppContextSize {
		t.Errorf("ContextSize = %d, want %d (fallback)", models[0].ContextSize, defaultLlamaCppContextSize)
	}
}

// TestDiscoverLlamaCPP_NoModelsIsError verifies that a /v1/models response
// with an empty list errors out instead of registering a phantom arm.
func TestDiscoverLlamaCPP_NoModelsIsError(t *testing.T) {
	stub := &llamaCPPStub{
		modelsBody: `{"data":[]}`,
		propsBody:  `{"default_generation_settings":{"n_ctx":8192}}`,
	}
	srv := stub.server()
	defer srv.Close()

	if _, err := DiscoverLlamaCPP(context.Background(), srv.URL); err == nil {
		t.Error("expected error when /v1/models returns no entries, got nil")
	}
}
