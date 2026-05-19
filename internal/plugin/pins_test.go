package plugin

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFilePinStore_MissingFileIsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pins.toml")
	s, err := NewFilePinStore(path)
	if err != nil {
		t.Fatalf("NewFilePinStore on missing file: %v", err)
	}
	if _, ok := s.Get("anything"); ok {
		t.Error("empty store should not return any pins")
	}
}

func TestFilePinStore_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pins.toml")
	s, err := NewFilePinStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Set("git-tools", "abc123"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// Re-open from disk: pin must persist.
	s2, err := NewFilePinStore(path)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := s2.Get("git-tools")
	if !ok {
		t.Fatal("pin missing after reload")
	}
	if got != "abc123" {
		t.Errorf("Get = %q, want abc123", got)
	}
}

func TestFilePinStore_OverwriteExisting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pins.toml")
	s, _ := NewFilePinStore(path)
	if err := s.Set("p", "v1"); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("p", "v2"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get("p")
	if got != "v2" {
		t.Errorf("Get = %q, want v2 after overwrite", got)
	}
}

func TestFilePinStore_PreservesOtherPinsOnWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pins.toml")
	s, _ := NewFilePinStore(path)
	if err := s.Set("a", "h1"); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("b", "h2"); err != nil {
		t.Fatal(err)
	}

	s2, _ := NewFilePinStore(path)
	if h, _ := s2.Get("a"); h != "h1" {
		t.Errorf("Get(a) = %q, want h1", h)
	}
	if h, _ := s2.Get("b"); h != "h2" {
		t.Errorf("Get(b) = %q, want h2", h)
	}
}

func TestFilePinStore_CreatesParentDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "deeper", "pins.toml")
	s, err := NewFilePinStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Set("x", "h"); err != nil {
		t.Fatalf("Set into nested dir: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("pin file missing: %v", err)
	}
}
