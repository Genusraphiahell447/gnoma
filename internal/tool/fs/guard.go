package fs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var ErrOutsideWorkspace = errors.New("path outside workspace")

type Guard struct {
	roots []string
}

func NewGuard(roots ...string) (*Guard, error) {
	if len(roots) == 0 {
		return nil, errors.New("guard requires at least one root")
	}
	resolved := make([]string, 0, len(roots))
	for _, r := range roots {
		if !filepath.IsAbs(r) {
			return nil, fmt.Errorf("guard root %q must be absolute", r)
		}
		canonical, err := filepath.EvalSymlinks(r)
		if err != nil {
			return nil, fmt.Errorf("guard root %q: %w", r, err)
		}
		info, err := os.Stat(canonical)
		if err != nil {
			return nil, fmt.Errorf("guard root %q: %w", r, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("guard root %q is not a directory", r)
		}
		resolved = append(resolved, filepath.Clean(canonical))
	}
	return &Guard{roots: resolved}, nil
}

func (g *Guard) Roots() []string {
	out := make([]string, len(g.roots))
	copy(out, g.roots)
	return out
}

func (g *Guard) ResolveRead(path string) (string, error) {
	abs, err := g.absolutise(path)
	if err != nil {
		return "", err
	}
	canonical, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("resolve %q: %w", path, err)
	}
	if !g.contains(canonical) {
		return "", fmt.Errorf("%w: %s", ErrOutsideWorkspace, path)
	}
	return canonical, nil
}

// ResolveWrite canonicalises the deepest existing ancestor so a symlinked
// parent escaping the workspace is rejected even when the leaf doesn't exist.
func (g *Guard) ResolveWrite(path string) (string, error) {
	abs, err := g.absolutise(path)
	if err != nil {
		return "", err
	}

	ancestor := abs
	tail := ""
	for {
		if _, err := os.Lstat(ancestor); err == nil {
			break
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return "", fmt.Errorf("resolve %q: no existing ancestor", path)
		}
		tail = filepath.Join(filepath.Base(ancestor), tail)
		ancestor = parent
	}

	canonicalAncestor, err := filepath.EvalSymlinks(ancestor)
	if err != nil {
		return "", fmt.Errorf("resolve ancestor of %q: %w", path, err)
	}
	resolved := canonicalAncestor
	if tail != "" {
		resolved = filepath.Join(canonicalAncestor, tail)
	}
	if !g.contains(resolved) {
		return "", fmt.Errorf("%w: %s", ErrOutsideWorkspace, path)
	}
	return resolved, nil
}

// absolutise anchors relative paths against the first root rather than process
// cwd, which may drift over the lifetime of the agent.
func (g *Guard) absolutise(path string) (string, error) {
	if path == "" {
		return "", errors.New("empty path")
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	return filepath.Clean(filepath.Join(g.roots[0], path)), nil
}

// contains uses a separator boundary so "/ws-evil" is not considered inside "/ws".
func (g *Guard) contains(canonical string) bool {
	for _, root := range g.roots {
		if canonical == root {
			return true
		}
		prefix := root + string(filepath.Separator)
		if strings.HasPrefix(canonical, prefix) {
			return true
		}
	}
	return false
}
