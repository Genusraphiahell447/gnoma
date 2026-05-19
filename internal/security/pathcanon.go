package security

import (
	"fmt"
	"os"
	"path/filepath"
)

// CanonicalizePath resolves absPath to its absolute, symlink-evaluated form,
// tolerating non-existent leaf and intermediate components.
//
// The algorithm walks back from absPath toward "/" until it finds a path that
// exists (via Lstat), runs filepath.EvalSymlinks on that ancestor, then
// rejoins the non-existent tail. This defends against the TOCTOU sandbox
// escape "leaf does not exist yet, so EvalSymlinks errors → caller skips the
// symlink check → write proceeds outside the workspace through a symlinked
// parent."
//
// absPath must be absolute; callers pre-anchor relative paths against the
// appropriate root (process cwd for general use, workspace root for
// sandboxed tools). Returns an error if no existing ancestor is reachable
// (would imply a broken filesystem; "/" always Lstats fine on a sane host).
func CanonicalizePath(absPath string) (string, error) {
	if !filepath.IsAbs(absPath) {
		return "", fmt.Errorf("canonicalize: %q is not absolute", absPath)
	}

	ancestor := filepath.Clean(absPath)
	tail := ""
	for {
		if _, err := os.Lstat(ancestor); err == nil {
			break
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return "", fmt.Errorf("canonicalize %q: no existing ancestor", absPath)
		}
		tail = filepath.Join(filepath.Base(ancestor), tail)
		ancestor = parent
	}

	canonicalAncestor, err := filepath.EvalSymlinks(ancestor)
	if err != nil {
		return "", fmt.Errorf("canonicalize ancestor of %q: %w", absPath, err)
	}
	if tail == "" {
		return filepath.Clean(canonicalAncestor), nil
	}
	return filepath.Clean(filepath.Join(canonicalAncestor, tail)), nil
}
