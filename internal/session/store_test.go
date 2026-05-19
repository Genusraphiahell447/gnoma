package session_test

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"somegit.dev/Owlibou/gnoma/internal/message"
	"somegit.dev/Owlibou/gnoma/internal/session"
)

func makeSnap(id string, updated time.Time) session.Snapshot {
	return session.Snapshot{
		ID: id,
		Metadata: session.Metadata{
			ID:           id,
			Provider:     "anthropic",
			Model:        "claude",
			TurnCount:    1,
			UpdatedAt:    updated,
			CreatedAt:    updated,
			MessageCount: 2,
		},
		Messages: []message.Message{
			message.NewUserText("hello"),
			message.NewAssistantText("hi"),
		},
	}
}

func makeStore(t *testing.T) *session.SessionStore {
	t.Helper()
	root := t.TempDir()
	return session.NewSessionStore(root, 3, slog.Default())
}

func TestSessionStore_SaveLoad(t *testing.T) {
	store := makeStore(t)
	snap := makeSnap("sess-001", time.Now().UTC())

	if err := store.Save(snap); err != nil {
		t.Fatal(err)
	}

	got, err := store.Load("sess-001")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "sess-001" {
		t.Errorf("ID mismatch: %q", got.ID)
	}
	if len(got.Messages) != 2 {
		t.Errorf("messages: %d", len(got.Messages))
	}
	if got.Metadata.Provider != "anthropic" {
		t.Errorf("provider: %q", got.Metadata.Provider)
	}
}

func TestSessionStore_Load_Missing(t *testing.T) {
	store := makeStore(t)
	_, err := store.Load("nonexistent")
	if err == nil {
		t.Error("expected error for missing session")
	}
}

func TestSessionStore_Load_CorruptMetadata(t *testing.T) {
	root := t.TempDir()
	store := session.NewSessionStore(root, 3, slog.Default())

	dir := filepath.Join(root, ".gnoma", "sessions", "corrupt-sess")
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "metadata.json"), []byte("not json"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "messages.json"), []byte("[]"), 0o644)

	_, err := store.Load("corrupt-sess")
	if err == nil {
		t.Error("expected error for corrupt metadata")
	}
}

func TestSessionStore_List_SortedByUpdatedAt(t *testing.T) {
	store := makeStore(t)
	now := time.Now().UTC()

	_ = store.Save(makeSnap("sess-old", now.Add(-2*time.Hour)))
	_ = store.Save(makeSnap("sess-new", now))
	_ = store.Save(makeSnap("sess-mid", now.Add(-1*time.Hour)))

	list, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 sessions, got %d", len(list))
	}
	if list[0].ID != "sess-new" {
		t.Errorf("first should be newest: %q", list[0].ID)
	}
	if list[2].ID != "sess-old" {
		t.Errorf("last should be oldest: %q", list[2].ID)
	}
}

func TestSessionStore_Load_RejectsPathTraversal(t *testing.T) {
	store := makeStore(t)
	cases := []string{"../../etc/passwd", "../sibling", ""}
	for _, id := range cases {
		_, err := store.Load(id)
		if err == nil {
			t.Errorf("Load(%q): expected error for invalid ID", id)
		}
	}
}

func TestSessionStore_Save_RejectsPathTraversal(t *testing.T) {
	store := makeStore(t)
	snap := makeSnap("../../evil", time.Now().UTC())
	if err := store.Save(snap); err == nil {
		t.Error("Save with traversal ID: expected error")
	}
}

func TestSessionStore_Save_FilesArePrivate(t *testing.T) {
	// Session files contain conversation history including raw user
	// input — keep them 0o600 / 0o700 so other local users on shared
	// hosts can't read them.
	root := t.TempDir()
	store := session.NewSessionStore(root, 3, slog.Default())
	snap := makeSnap("sess-perms", time.Now().UTC())
	if err := store.Save(snap); err != nil {
		t.Fatal(err)
	}

	dir := filepath.Join(root, ".gnoma", "sessions", "sess-perms")
	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat session dir: %v", err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Errorf("session dir mode = %o, want 0700", dirInfo.Mode().Perm())
	}

	for _, name := range []string{"metadata.json", "messages.json"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("%s mode = %o, want 0600", name, info.Mode().Perm())
		}
	}
}

func TestSessionStore_Prune_RemovesOldest(t *testing.T) {
	store := makeStore(t) // maxKeep = 3
	now := time.Now().UTC()

	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("sess-%03d", i)
		_ = store.Save(makeSnap(id, now.Add(time.Duration(i)*time.Minute)))
	}

	list, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Errorf("expected 3 sessions after prune, got %d", len(list))
	}
	for _, m := range list {
		if m.ID == "sess-000" || m.ID == "sess-001" {
			t.Errorf("oldest session %q should have been pruned", m.ID)
		}
	}
}
