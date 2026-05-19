package fs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These tests exercise each fs tool with a Guard installed, verifying that
// paths outside the workspace are rejected at the tool boundary.

func TestReadTool_GuardDeniesOutsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "secret")
	if err := os.WriteFile(outsideFile, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := NewReadTool()
	r.SetGuard(mustGuard(t, root))

	res, err := r.Execute(context.Background(), mustJSON(t, readArgs{Path: outsideFile}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Output, "path outside workspace") {
		t.Errorf("expected workspace error, got %q", res.Output)
	}
}

func TestReadTool_GuardAllowsInsideRoot(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "ok.txt")
	if err := os.WriteFile(inside, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := NewReadTool()
	r.SetGuard(mustGuard(t, root))

	res, err := r.Execute(context.Background(), mustJSON(t, readArgs{Path: inside}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Output, "hi") {
		t.Errorf("expected file content, got %q", res.Output)
	}
}

func TestWriteTool_GuardDeniesOutsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "evil.txt")

	w := NewWriteTool()
	w.SetGuard(mustGuard(t, root))

	res, err := w.Execute(context.Background(), mustJSON(t, writeArgs{Path: target, Content: "x"}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Output, "path outside workspace") {
		t.Errorf("expected workspace error, got %q", res.Output)
	}
	if _, err := os.Stat(target); err == nil {
		t.Errorf("file was written despite guard: %s", target)
	}
}

func TestWriteTool_GuardAllowsInsideRoot(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "sub", "ok.txt")

	w := NewWriteTool()
	w.SetGuard(mustGuard(t, root))

	res, err := w.Execute(context.Background(), mustJSON(t, writeArgs{Path: target, Content: "hi"}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Output, "Wrote") {
		t.Errorf("expected write confirmation, got %q", res.Output)
	}
	if _, err := os.Stat(target); err != nil {
		t.Errorf("file missing after write: %v", err)
	}
}

func TestEditTool_GuardDeniesOutsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "f.txt")
	if err := os.WriteFile(target, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	e := NewEditTool()
	e.SetGuard(mustGuard(t, root))

	res, err := e.Execute(context.Background(), mustJSON(t, editArgs{Path: target, OldString: "hello", NewString: "hi"}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Output, "path outside workspace") {
		t.Errorf("expected workspace error, got %q", res.Output)
	}
	// File must remain unchanged.
	data, _ := os.ReadFile(target)
	if string(data) != "hello" {
		t.Errorf("file mutated despite guard: %q", string(data))
	}
}

func TestLSTool_GuardDeniesOutsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	l := NewLSTool()
	l.SetGuard(mustGuard(t, root))

	res, err := l.Execute(context.Background(), mustJSON(t, lsArgs{Path: outside}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Output, "path outside workspace") {
		t.Errorf("expected workspace error, got %q", res.Output)
	}
}

func TestLSTool_GuardEmptyPathDefaultsToRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "marker.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	l := NewLSTool()
	l.SetGuard(mustGuard(t, root))

	res, err := l.Execute(context.Background(), mustJSON(t, lsArgs{}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Output, "marker.txt") {
		t.Errorf("expected to list root contents, got %q", res.Output)
	}
}

func TestGlobTool_GuardDeniesOutsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	g := NewGlobTool()
	g.SetGuard(mustGuard(t, root))

	res, err := g.Execute(context.Background(), mustJSON(t, globArgs{Pattern: "*", Path: outside}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Output, "path outside workspace") {
		t.Errorf("expected workspace error, got %q", res.Output)
	}
}

func TestGrepTool_GuardDeniesOutsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "f.txt"), []byte("needle"), 0o644); err != nil {
		t.Fatal(err)
	}

	g := NewGrepTool()
	g.SetGuard(mustGuard(t, root))

	res, err := g.Execute(context.Background(), mustJSON(t, grepArgs{Pattern: "needle", Path: outside}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Output, "path outside workspace") {
		t.Errorf("expected workspace error, got %q", res.Output)
	}
}
