package fs

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNewGuard_RejectsEmptyRoots(t *testing.T) {
	if _, err := NewGuard(); err == nil {
		t.Fatal("NewGuard() with no roots should error")
	}
}

func TestNewGuard_RejectsRelativeRoot(t *testing.T) {
	if _, err := NewGuard("relative/path"); err == nil {
		t.Fatal("NewGuard with relative root should error")
	}
}

func TestNewGuard_RejectsNonexistentRoot(t *testing.T) {
	if _, err := NewGuard("/definitely/does/not/exist/anywhere"); err == nil {
		t.Fatal("NewGuard with nonexistent root should error")
	}
}

func TestGuard_ResolveInsideRoot(t *testing.T) {
	root := t.TempDir()
	g := mustGuard(t, root)

	path := filepath.Join(root, "file.txt")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := g.ResolveRead(path)
	if err != nil {
		t.Fatalf("ResolveRead inside root: %v", err)
	}
	if got != path {
		t.Errorf("got %q, want %q", got, path)
	}
}

func TestGuard_ResolveReadOutsideRootDenied(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	g := mustGuard(t, root)

	outsidePath := filepath.Join(outside, "secret")
	if err := os.WriteFile(outsidePath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := g.ResolveRead(outsidePath)
	if !errors.Is(err, ErrOutsideWorkspace) {
		t.Fatalf("want ErrOutsideWorkspace, got %v", err)
	}
}

func TestGuard_ResolveReadRelativeEscape(t *testing.T) {
	root := t.TempDir()
	g := mustGuard(t, root)

	// Relative path with ../../../ should resolve relative to first root and
	// escape it; guard must deny.
	_, err := g.ResolveRead("../../../etc/passwd")
	if !errors.Is(err, ErrOutsideWorkspace) {
		t.Fatalf("want ErrOutsideWorkspace, got %v", err)
	}
}

func TestGuard_ResolveReadSymlinkEscapeDenied(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows")
	}
	root := t.TempDir()
	outside := t.TempDir()
	g := mustGuard(t, root)

	target := filepath.Join(outside, "target")
	if err := os.WriteFile(target, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	_, err := g.ResolveRead(link)
	if !errors.Is(err, ErrOutsideWorkspace) {
		t.Fatalf("symlink escaping root should be denied; got %v", err)
	}
}

func TestGuard_ResolveReadSymlinkWithinRootAllowed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows")
	}
	root := t.TempDir()
	g := mustGuard(t, root)

	target := filepath.Join(root, "target")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	got, err := g.ResolveRead(link)
	if err != nil {
		t.Fatalf("symlink inside root: %v", err)
	}
	// Canonical form should be the target, not the link.
	if !strings.HasPrefix(got, root) {
		t.Errorf("canonical %q should be inside root %q", got, root)
	}
}

func TestGuard_ResolveWriteNewFileAllowed(t *testing.T) {
	root := t.TempDir()
	g := mustGuard(t, root)

	newFile := filepath.Join(root, "newdir", "newfile.txt")
	got, err := g.ResolveWrite(newFile)
	if err != nil {
		t.Fatalf("ResolveWrite to new path inside root: %v", err)
	}
	if !strings.HasPrefix(got, root) {
		t.Errorf("canonical %q should be inside root %q", got, root)
	}
}

func TestGuard_ResolveWriteOutsideRootDenied(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	g := mustGuard(t, root)

	_, err := g.ResolveWrite(filepath.Join(outside, "evil.txt"))
	if !errors.Is(err, ErrOutsideWorkspace) {
		t.Fatalf("want ErrOutsideWorkspace, got %v", err)
	}
}

func TestGuard_ResolveWriteViaSymlinkedParentDenied(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows")
	}
	root := t.TempDir()
	outside := t.TempDir()
	g := mustGuard(t, root)

	// Create a symlink inside root whose target is outside.
	linkedDir := filepath.Join(root, "escape")
	if err := os.Symlink(outside, linkedDir); err != nil {
		t.Fatal(err)
	}

	// Writing under the symlinked dir lands outside root.
	_, err := g.ResolveWrite(filepath.Join(linkedDir, "evil.txt"))
	if !errors.Is(err, ErrOutsideWorkspace) {
		t.Fatalf("write via symlinked parent should be denied; got %v", err)
	}
}

func TestGuard_MultipleRoots(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()
	g := mustGuard(t, rootA, rootB)

	a := filepath.Join(rootA, "file")
	if err := os.WriteFile(a, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	b := filepath.Join(rootB, "file")
	if err := os.WriteFile(b, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := g.ResolveRead(a); err != nil {
		t.Errorf("rootA: %v", err)
	}
	if _, err := g.ResolveRead(b); err != nil {
		t.Errorf("rootB: %v", err)
	}
}

func TestGuard_RootBoundaryNotPrefixMatch(t *testing.T) {
	// Catch the classic bug: /foo/bar must NOT be considered inside /foo/ba.
	parent := t.TempDir()
	rootShort := filepath.Join(parent, "ws")
	rootLongName := filepath.Join(parent, "ws-evil")
	for _, d := range []string{rootShort, rootLongName} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	g := mustGuard(t, rootShort)

	evil := filepath.Join(rootLongName, "secret")
	if err := os.WriteFile(evil, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := g.ResolveRead(evil)
	if !errors.Is(err, ErrOutsideWorkspace) {
		t.Fatalf("ws-evil/secret should not be considered inside ws; got %v", err)
	}
}

func TestGuard_RootItselfAllowed(t *testing.T) {
	root := t.TempDir()
	g := mustGuard(t, root)

	if _, err := g.ResolveRead(root); err != nil {
		t.Errorf("root path itself should be allowed: %v", err)
	}
}

func mustGuard(t *testing.T, roots ...string) *Guard {
	t.Helper()
	g, err := NewGuard(roots...)
	if err != nil {
		t.Fatalf("NewGuard: %v", err)
	}
	return g
}
