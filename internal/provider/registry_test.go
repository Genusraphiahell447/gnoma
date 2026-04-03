package provider

import (
	"context"
	"errors"
	"slices"
	"sort"
	"testing"

	"somegit.dev/Owlibou/gnoma/internal/stream"
)

// mockProvider implements Provider for testing.
type mockProvider struct {
	name string
}

func (m *mockProvider) Stream(_ context.Context, _ Request) (stream.Stream, error) {
	return nil, nil
}

func (m *mockProvider) Name() string         { return m.name }
func (m *mockProvider) DefaultModel() string  { return "mock-model" }
func (m *mockProvider) Models(_ context.Context) ([]ModelInfo, error) {
	return []ModelInfo{{ID: "mock-model", Name: "mock-model", Provider: m.name}}, nil
}

func TestRegistry_RegisterAndCreate(t *testing.T) {
	r := NewRegistry()

	r.Register("mock", func(cfg ProviderConfig) (Provider, error) {
		return &mockProvider{name: cfg.Name}, nil
	})

	p, err := r.Create("mock", ProviderConfig{})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if p.Name() != "mock" {
		t.Errorf("Name() = %q, want %q", p.Name(), "mock")
	}
}

func TestRegistry_Create_Unknown(t *testing.T) {
	r := NewRegistry()

	_, err := r.Create("nonexistent", ProviderConfig{})
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
	want := `unknown provider: "nonexistent"`
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}

func TestRegistry_Create_FactoryError(t *testing.T) {
	r := NewRegistry()
	r.Register("broken", func(cfg ProviderConfig) (Provider, error) {
		return nil, errors.New("missing api key")
	})

	_, err := r.Create("broken", ProviderConfig{})
	if err == nil {
		t.Fatal("expected error from factory")
	}
	if err.Error() != "missing api key" {
		t.Errorf("error = %q", err.Error())
	}
}

func TestRegistry_Create_SetsName(t *testing.T) {
	r := NewRegistry()

	var receivedName string
	r.Register("test", func(cfg ProviderConfig) (Provider, error) {
		receivedName = cfg.Name
		return &mockProvider{name: cfg.Name}, nil
	})

	_, _ = r.Create("test", ProviderConfig{APIKey: "sk-123"})
	if receivedName != "test" {
		t.Errorf("factory received Name = %q, want %q", receivedName, "test")
	}
}

func TestRegistry_Has(t *testing.T) {
	r := NewRegistry()
	r.Register("exists", func(cfg ProviderConfig) (Provider, error) {
		return nil, nil
	})

	if !r.Has("exists") {
		t.Error("Has(exists) = false, want true")
	}
	if r.Has("nope") {
		t.Error("Has(nope) = true, want false")
	}
}

func TestRegistry_Names(t *testing.T) {
	r := NewRegistry()
	r.Register("alpha", func(cfg ProviderConfig) (Provider, error) { return nil, nil })
	r.Register("beta", func(cfg ProviderConfig) (Provider, error) { return nil, nil })
	r.Register("gamma", func(cfg ProviderConfig) (Provider, error) { return nil, nil })

	names := r.Names()
	sort.Strings(names)

	want := []string{"alpha", "beta", "gamma"}
	if !slices.Equal(names, want) {
		t.Errorf("Names() = %v, want %v", names, want)
	}
}

func TestRegistry_Register_Overwrite(t *testing.T) {
	r := NewRegistry()

	r.Register("dup", func(cfg ProviderConfig) (Provider, error) {
		return &mockProvider{name: "old"}, nil
	})
	r.Register("dup", func(cfg ProviderConfig) (Provider, error) {
		return &mockProvider{name: "new"}, nil
	})

	p, err := r.Create("dup", ProviderConfig{})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if p.Name() != "new" {
		t.Errorf("Name() = %q, want %q (overwritten factory)", p.Name(), "new")
	}
}
