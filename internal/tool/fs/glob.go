package fs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
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

type GlobTool struct{}

func NewGlobTool() *GlobTool { return &GlobTool{} }

func (t *GlobTool) Name() string               { return globToolName }
func (t *GlobTool) Description() string         { return "Find files matching a glob pattern, sorted by modification time" }
func (t *GlobTool) Parameters() json.RawMessage { return globParams }
func (t *GlobTool) IsReadOnly() bool            { return true }
func (t *GlobTool) IsDestructive() bool         { return false }

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
		var err error
		root, err = os.Getwd()
		if err != nil {
			return tool.Result{}, fmt.Errorf("fs.glob: %w", err)
		}
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

		matched, err := filepath.Match(a.Pattern, rel)
		if err != nil {
			// Try matching just the filename for simple patterns
			matched, _ = filepath.Match(a.Pattern, d.Name())
		}

		if matched {
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
