package persist_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"somegit.dev/Owlibou/gnoma/internal/tool/persist"
)

func TestStore_SaveSkipsSmallContent(t *testing.T) {
	s := persist.New("test-session-001", nil)
	t.Cleanup(func() { _ = os.RemoveAll(s.Dir()) })

	path, ok := s.Save("bash", "call-001", "small output")
	if ok {
		t.Errorf("expected not persisted, got path %q", path)
	}
	if path != "" {
		t.Errorf("expected empty path for small content")
	}
}

func TestStore_SavePersistsLargeContent(t *testing.T) {
	s := persist.New("test-session-002", nil)
	t.Cleanup(func() { _ = os.RemoveAll(s.Dir()) })

	content := strings.Repeat("x", 1024)
	path, ok := s.Save("fs.grep", "call-002", content)
	if !ok {
		t.Fatal("expected content to be persisted")
	}
	if !strings.HasSuffix(path, "fs_grep-call-002.txt") {
		t.Errorf("unexpected path: %q", path)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("file not written: %v", err)
	}
	if string(got) != content {
		t.Error("file content mismatch")
	}
}

func TestStore_ListFilters(t *testing.T) {
	s := persist.New("test-session-003", nil)
	t.Cleanup(func() { _ = os.RemoveAll(s.Dir()) })

	bigContent := strings.Repeat("y", 1024)
	s.Save("bash", "c1", bigContent)
	s.Save("fs.read", "c2", bigContent)
	s.Save("bash", "c3", bigContent)

	all, err := s.List("")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Errorf("want 3 results, got %d", len(all))
	}

	filtered, err := s.List("bash")
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 2 {
		t.Errorf("want 2 bash results, got %d", len(filtered))
	}
}

func TestStore_ReadValidatesPath(t *testing.T) {
	s := persist.New("test-session-004", nil)
	t.Cleanup(func() { _ = os.RemoveAll(s.Dir()) })

	// Path outside session dir must be rejected
	_, err := s.Read("/etc/passwd")
	if err == nil {
		t.Error("expected error for path outside session dir")
	}

	// Valid path (even if file doesn't exist) should pass validation
	_, err = s.Read(filepath.Join(s.Dir(), "bash-call.txt"))
	// os.ErrNotExist is fine — path was valid
	if err != nil && !os.IsNotExist(err) {
		t.Errorf("unexpected error for valid path: %v", err)
	}
}
