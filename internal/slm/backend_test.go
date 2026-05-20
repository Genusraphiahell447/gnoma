package slm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestStartBackend_Disabled(t *testing.T) {
	boot, err := StartBackend(context.Background(), BackendConfig{Backend: BackendDisabled}, nil)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if boot != nil {
		t.Errorf("expected nil boot for disabled backend, got %+v", boot)
	}
}

func TestStartBackend_UnknownBackend(t *testing.T) {
	_, err := StartBackend(context.Background(), BackendConfig{Backend: "not-a-real-backend"}, nil)
	if err == nil {
		t.Fatal("expected error for unknown backend")
	}
}

func TestStartBackend_Ollama_WithModel(t *testing.T) {
	// Just construct; we don't actually call the model in this test.
	boot, err := StartBackend(context.Background(), BackendConfig{
		Backend: BackendOllama,
		Model:   "ministral-3:3b",
		BaseURL: "http://localhost:11434",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if boot == nil || boot.Provider == nil {
		t.Fatal("expected non-nil boot with provider")
	}
	if boot.Model != "ministral-3:3b" {
		t.Errorf("model = %q, want ministral-3:3b", boot.Model)
	}
	if boot.Backend != BackendOllama {
		t.Errorf("backend = %q, want ollama", boot.Backend)
	}
	if err := boot.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestStartBackend_Ollama_WithoutModel_PicksSmallest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"models":[
				{"name":"big:34b","size":18000000000},
				{"name":"small:1b","size":1200000000},
				{"name":"medium:7b","size":4500000000}
			]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	boot, err := StartBackend(context.Background(), BackendConfig{
		Backend: BackendOllama,
		BaseURL: srv.URL,
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if boot.Model != "small:1b" {
		t.Errorf("auto-picked model = %q, want small:1b", boot.Model)
	}
}

func TestStartBackend_Ollama_NoModels_FailsClean(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"models":[]}`))
	}))
	defer srv.Close()

	_, err := StartBackend(context.Background(), BackendConfig{
		Backend: BackendOllama,
		BaseURL: srv.URL,
	}, nil)
	if err == nil {
		t.Fatal("expected error when Ollama has no models and no [slm] model")
	}
}

func TestStartBackend_OpenAICompat_RequiresBaseURLAndModel(t *testing.T) {
	cases := []BackendConfig{
		{Backend: BackendOpenAICompat, Model: "x"},                          // missing base_url
		{Backend: BackendOpenAICompat, BaseURL: "http://localhost:1234/v1"}, // missing model
	}
	for i, c := range cases {
		if _, err := StartBackend(context.Background(), c, nil); err == nil {
			t.Errorf("case %d: expected error for %+v", i, c)
		}
	}
}

func TestStartBackend_OpenAICompat_HappyPath(t *testing.T) {
	boot, err := StartBackend(context.Background(), BackendConfig{
		Backend: BackendOpenAICompat,
		BaseURL: "http://localhost:1234/v1",
		Model:   "llama-3.2-3b-instruct",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if boot.Backend != BackendOpenAICompat {
		t.Errorf("backend = %q", boot.Backend)
	}
	if boot.Model != "llama-3.2-3b-instruct" {
		t.Errorf("model = %q", boot.Model)
	}
}

func TestStartBackend_LlamaCpp_DefaultModelName(t *testing.T) {
	boot, err := StartBackend(context.Background(), BackendConfig{
		Backend: BackendLlamaCpp,
		BaseURL: "http://localhost:8080",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if boot.Model != "default" {
		t.Errorf("model = %q, want default", boot.Model)
	}
}

func TestStartBackend_Auto_NothingReachable(t *testing.T) {
	// Point at definitely-unreachable URLs to exercise the no-backend path
	// without depending on what's running on this machine.
	cfg := BackendConfig{
		Backend: BackendAuto,
		// no ModelURL → no llamafile hint; the default DataDir might happen
		// to have a manifest, so this test asserts the *type* of behavior
		// rather than the exact nil-vs-something outcome.
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	boot, err := StartBackend(ctx, cfg, nil)
	if err != nil {
		t.Fatalf("auto must not error when nothing is reachable, got %v", err)
	}
	// We can't strictly assert nil here because the test machine *may*
	// have a working backend. The contract is: never panic, never error.
	if boot != nil {
		t.Logf("auto picked backend %s (model=%s)", boot.Backend, boot.Model)
		_ = boot.Close()
	}
}

func TestPickSmallestOllamaModel_OrdersBySize(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"models":[
			{"name":"a","size":3000},
			{"name":"b","size":1000},
			{"name":"c","size":2000}
		]}`))
	}))
	defer srv.Close()
	got, ok := pickSmallestOllamaModel(srv.URL)
	if !ok {
		t.Fatal("ok=false")
	}
	if got != "b" {
		t.Errorf("got %q, want b", got)
	}
}

func TestPickSmallestOllamaModel_Unreachable(t *testing.T) {
	_, ok := pickSmallestOllamaModel("http://127.0.0.1:1")
	if ok {
		t.Error("ok=true for unreachable URL")
	}
}
