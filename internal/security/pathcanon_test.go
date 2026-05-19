package security

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCanonicalizePath_ExistingPath(t *testing.T) {
	dir := t.TempDir()
	got, err := CanonicalizePath(dir)
	if err != nil {
		t.Fatalf("CanonicalizePath: %v", err)
	}
	want, _ := filepath.EvalSymlinks(dir)
	if got != filepath.Clean(want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestCanonicalizePath_NonExistentLeaf(t *testing.T) {
	dir := t.TempDir()
	leaf := filepath.Join(dir, "does-not-exist.txt")
	got, err := CanonicalizePath(leaf)
	if err != nil {
		t.Fatalf("CanonicalizePath: %v", err)
	}
	canonicalDir, _ := filepath.EvalSymlinks(dir)
	want := filepath.Join(canonicalDir, "does-not-exist.txt")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestCanonicalizePath_NonExistentMidComponent(t *testing.T) {
	dir := t.TempDir()
	deep := filepath.Join(dir, "missing-mid", "and-leaf.txt")
	got, err := CanonicalizePath(deep)
	if err != nil {
		t.Fatalf("CanonicalizePath: %v", err)
	}
	canonicalDir, _ := filepath.EvalSymlinks(dir)
	want := filepath.Join(canonicalDir, "missing-mid", "and-leaf.txt")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestCanonicalizePath_ResolvesSymlinkAncestor(t *testing.T) {
	// real/<linked> where /real is a symlink to a sibling tempdir; the
	// canonical form must point through the resolved target.
	parent := t.TempDir()
	target := filepath.Join(parent, "target")
	link := filepath.Join(parent, "link")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	got, err := CanonicalizePath(filepath.Join(link, "new-file.txt"))
	if err != nil {
		t.Fatalf("CanonicalizePath: %v", err)
	}
	canonicalTarget, _ := filepath.EvalSymlinks(target)
	want := filepath.Join(canonicalTarget, "new-file.txt")
	if got != want {
		t.Errorf("got %q, want %q (symlinked parent should be resolved)", got, want)
	}
}

func TestCanonicalizePath_RejectsRelativePath(t *testing.T) {
	if _, err := CanonicalizePath("relative/path"); err == nil {
		t.Error("expected error for relative path, got nil")
	}
}
