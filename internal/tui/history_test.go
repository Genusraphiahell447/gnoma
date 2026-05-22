package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stageHistoryDir redirects GlobalConfigDir() to t.TempDir() by overriding
// XDG_CONFIG_HOME. Returns the resolved ~/.config/gnoma path.
func stageHistoryDir(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	return filepath.Join(root, "gnoma")
}

func TestSavePromptHistory_WritesFileWithRestrictivePerms(t *testing.T) {
	dir := stageHistoryDir(t)

	savePromptHistory("first prompt")

	path := filepath.Join(dir, "history.txt")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("history file not created: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("history file mode = %o, want 0600", mode)
	}
}

func TestSavePromptHistory_RewritesExistingFileTo0600(t *testing.T) {
	dir := stageHistoryDir(t)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "history.txt")
	if err := os.WriteFile(path, []byte("old entry\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	savePromptHistory("new entry")

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("history file mode = %o, want 0600 after rewrite", mode)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "old entry") {
		t.Error("rewrite dropped previously stored entry")
	}
	if !strings.Contains(string(data), "new entry") {
		t.Error("rewrite missing newly appended entry")
	}
}

func TestSavePromptHistory_TruncatesToLast500Entries(t *testing.T) {
	dir := stageHistoryDir(t)

	// Save 600 entries.
	for i := 0; i < 600; i++ {
		savePromptHistory(fmt.Sprintf("entry-%d", i))
	}

	// On-disk file must also be capped (not just the loaded view).
	data, err := os.ReadFile(filepath.Join(dir, "history.txt"))
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	onDiskLines := strings.Count(strings.TrimRight(string(data), "\n"), "\n") + 1
	if onDiskLines > 500 {
		t.Errorf("on-disk history has %d lines, want ≤500", onDiskLines)
	}

	got := loadPromptHistory()
	if len(got) > 500 {
		t.Errorf("history length = %d, want ≤500 after 600 writes", len(got))
	}
	if len(got) == 0 {
		t.Fatal("history unexpectedly empty")
	}
	// Most recent entry should be the last one written.
	if got[len(got)-1] != "entry-599" {
		t.Errorf("last entry = %q, want entry-599", got[len(got)-1])
	}
	// Oldest retained entry should be entry-100 (600-500).
	if got[0] != "entry-100" {
		t.Errorf("first entry = %q, want entry-100", got[0])
	}
}

func TestSavePromptHistory_IgnoresBlankInput(t *testing.T) {
	dir := stageHistoryDir(t)

	savePromptHistory("")
	savePromptHistory("   \n\t ")

	path := filepath.Join(dir, "history.txt")
	if _, err := os.Stat(path); err == nil {
		t.Error("blank input should not create history file")
	}
}

func TestSavePromptHistory_NewlinesFlattenedToSpace(t *testing.T) {
	stageHistoryDir(t)

	savePromptHistory("line one\nline two")

	got := loadPromptHistory()
	if len(got) != 1 {
		t.Fatalf("history length = %d, want 1", len(got))
	}
	if got[0] != "line one line two" {
		t.Errorf("got %q, want 'line one line two'", got[0])
	}
}
