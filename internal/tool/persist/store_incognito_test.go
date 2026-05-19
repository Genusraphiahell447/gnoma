package persist_test

import (
	"os"
	"strings"
	"testing"

	"somegit.dev/Owlibou/gnoma/internal/tool/persist"
)

// stubMode implements the incognito-gate interface persist depends on.
type stubMode struct {
	persist bool
}

func (m *stubMode) ShouldPersist() bool { return m.persist }

func TestStore_NilModeStillPersists(t *testing.T) {
	// Existing callers that pass nil for the mode (tests, legacy paths)
	// must behave exactly like the pre-W2-2 store. nil = no gate.
	s := persist.New("test-nil-mode", nil)
	t.Cleanup(func() { _ = os.RemoveAll(s.Dir()) })

	content := strings.Repeat("x", 1024)
	_, ok := s.Save("bash", "call-001", content)
	if !ok {
		t.Error("nil mode should not block persistence")
	}
}

func TestStore_IncognitoActiveSkipsSave(t *testing.T) {
	mode := &stubMode{persist: false}
	s := persist.New("test-incognito-active", mode)
	t.Cleanup(func() { _ = os.RemoveAll(s.Dir()) })

	content := strings.Repeat("x", 1024)
	path, ok := s.Save("bash", "call-001", content)
	if ok {
		t.Errorf("incognito-active mode must block Save, got path %q", path)
	}
	if _, err := os.Stat(s.Dir()); !os.IsNotExist(err) {
		t.Errorf("directory should not exist when persistence is blocked: stat err=%v", err)
	}
}

func TestStore_IncognitoInactiveStillSaves(t *testing.T) {
	mode := &stubMode{persist: true}
	s := persist.New("test-incognito-inactive", mode)
	t.Cleanup(func() { _ = os.RemoveAll(s.Dir()) })

	content := strings.Repeat("x", 1024)
	_, ok := s.Save("bash", "call-001", content)
	if !ok {
		t.Error("inactive incognito mode must not block persistence")
	}
}

func TestStore_FilePermissionsAre0600(t *testing.T) {
	s := persist.New("test-file-perms", nil)
	t.Cleanup(func() { _ = os.RemoveAll(s.Dir()) })

	content := strings.Repeat("x", 1024)
	path, ok := s.Save("bash", "call-001", content)
	if !ok {
		t.Fatal("expected persistence to succeed")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat persisted file: %v", err)
	}
	// Tool-result files contain post-redaction output but may still carry
	// project context. 0o600 prevents other local users from reading
	// session artefacts on multi-user hosts.
	if info.Mode().Perm() != 0o600 {
		t.Errorf("file perm = %o, want 0600", info.Mode().Perm())
	}
}

func TestStore_DirPermissionsAre0700(t *testing.T) {
	s := persist.New("test-dir-perms", nil)
	t.Cleanup(func() { _ = os.RemoveAll(s.Dir()) })

	// Trigger directory creation.
	content := strings.Repeat("x", 1024)
	if _, ok := s.Save("bash", "call-001", content); !ok {
		t.Fatal("expected persistence to succeed")
	}
	info, err := os.Stat(s.Dir())
	if err != nil {
		t.Fatalf("stat session dir: %v", err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Errorf("dir perm = %o, want 0700", info.Mode().Perm())
	}
}
