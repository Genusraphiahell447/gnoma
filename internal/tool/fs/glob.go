package fs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"somegit.dev/Owlibou/gnoma/internal/tool"
)

const globToolName = "fs.glob"

var globParams = json.RawMessage(`{
	"type": "object",
	"properties": {
		"pattern": {
			"type": "string",
			"description": "Glob pattern to match files (e.g. **/*.go, src/**/*.ts)"
		},
		"path": {
			"type": "string",
			"description": "Directory to search in (defaults to current directory)"
		}
	},
	"required": ["pattern"]
}`)

type GlobTool struct {
	guard *Guard
}

func NewGlobTool() *GlobTool { return &GlobTool{} }

func (t *GlobTool) SetGuard(g *Guard) { t.guard = g }

func (t *GlobTool) Name() string { return globToolName }
func (t *GlobTool) Description() string {
	return "Find files matching a glob pattern, sorted by modification time"
}
func (t *GlobTool) Parameters() json.RawMessage { return globParams }
func (t *GlobTool) IsReadOnly() bool            { return true }
func (t *GlobTool) IsDestructive() bool         { return false }
func (t *GlobTool) Category() tool.Category     { return tool.CategorySearch }

func (t *GlobTool) ExtractPaths(args json.RawMessage) []string {
	var a globArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return nil
	}
	return []string{a.Path} // empty string = caller resolves to cwd
}

type globArgs struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path,omitempty"`
}

func (t *GlobTool) Execute(_ context.Context, args json.RawMessage) (tool.Result, error) {
	var a globArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return tool.Result{}, fmt.Errorf("fs.glob: invalid args: %w", err)
	}
	if a.Pattern == "" {
		return tool.Result{}, fmt.Errorf("fs.glob: pattern required")
	}

	root := a.Path
	if root == "" {
		if t.guard != nil {
			root = t.guard.Roots()[0]
		} else {
			var err error
			root, err = os.Getwd()
			if err != nil {
				return tool.Result{}, fmt.Errorf("fs.glob: %w", err)
			}
		}
	}
	if t.guard != nil {
		resolved, err := t.guard.ResolveRead(root)
		if err != nil {
			return tool.Result{Output: fmt.Sprintf("Error: %v", err)}, nil
		}
		root = resolved
	}

	var matches []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip inaccessible entries
		}
		if d.IsDir() {
			// Skip hidden directories
			if d.Name() != "." && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}

		if matchGlob(a.Pattern, rel) {
			matches = append(matches, rel)
		}
		return nil
	})
	if err != nil {
		return tool.Result{Output: fmt.Sprintf("Error walking directory: %v", err)}, nil
	}

	// Sort by modification time (most recent first)
	sort.Slice(matches, func(i, j int) bool {
		iInfo, _ := os.Stat(filepath.Join(root, matches[i]))
		jInfo, _ := os.Stat(filepath.Join(root, matches[j]))
		if iInfo == nil || jInfo == nil {
			return matches[i] < matches[j]
		}
		return iInfo.ModTime().After(jInfo.ModTime())
	})

	output := strings.Join(matches, "\n")
	if output == "" {
		output = "(no matches)"
	}

	return tool.Result{
		Output:   output,
		Metadata: map[string]any{"count": len(matches), "pattern": a.Pattern},
	}, nil
}

// matchGlob matches a relative path against a glob pattern.
// Unlike filepath.Match, it supports ** to match zero or more path components.
func matchGlob(pattern, name string) bool {
	// Normalize to forward slashes for consistent component splitting.
	pattern = filepath.ToSlash(pattern)
	name = filepath.ToSlash(name)

	if !strings.Contains(pattern, "**") {
		ok, _ := filepath.Match(pattern, filepath.FromSlash(name))
		return ok
	}
	return matchComponents(strings.Split(pattern, "/"), strings.Split(name, "/"))
}

// matchComponents recursively matches pattern segments against path segments.
// A "**" segment matches zero or more consecutive path components.
func matchComponents(pats, parts []string) bool {
	for len(pats) > 0 {
		if pats[0] == "**" {
			// Consume all leading ** segments.
			for len(pats) > 0 && pats[0] == "**" {
				pats = pats[1:]
			}
			if len(pats) == 0 {
				return true // trailing ** matches everything
			}
			// Try anchoring the remaining pattern at each position.
			for i := range parts {
				if matchComponents(pats, parts[i:]) {
					return true
				}
			}
			return false
		}
		if len(parts) == 0 {
			return false
		}
		ok, err := path.Match(pats[0], parts[0])
		if err != nil || !ok {
			return false
		}
		pats = pats[1:]
		parts = parts[1:]
	}
	return len(parts) == 0
}
