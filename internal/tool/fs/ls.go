package fs

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"somegit.dev/Owlibou/gnoma/internal/tool"
)

const lsToolName = "fs.ls"

var lsParams = json.RawMessage(`{
	"type": "object",
	"properties": {
		"path": {
			"type": "string",
			"description": "Directory path to list (defaults to current directory)"
		}
	}
}`)

type LSTool struct{}

func NewLSTool() *LSTool { return &LSTool{} }

func (t *LSTool) Name() string               { return lsToolName }
func (t *LSTool) Description() string         { return "List directory contents with file types and sizes" }
func (t *LSTool) Parameters() json.RawMessage { return lsParams }
func (t *LSTool) IsReadOnly() bool            { return true }
func (t *LSTool) IsDestructive() bool         { return false }

func (t *LSTool) ExtractPaths(args json.RawMessage) []string {
	var a lsArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return nil
	}
	return []string{a.Path} // empty string = caller resolves to cwd
}

type lsArgs struct {
	Path string `json:"path,omitempty"`
}

func (t *LSTool) Execute(_ context.Context, args json.RawMessage) (tool.Result, error) {
	var a lsArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return tool.Result{}, fmt.Errorf("fs.ls: invalid args: %w", err)
	}

	dir := a.Path
	if dir == "" {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			return tool.Result{}, fmt.Errorf("fs.ls: %w", err)
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return tool.Result{Output: fmt.Sprintf("Error: %v", err)}, nil
	}

	var b strings.Builder
	dirCount, fileCount := 0, 0

	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}

		prefix := " "
		if entry.IsDir() {
			prefix = "d"
			dirCount++
		} else {
			fileCount++
		}

		size := formatSize(info.Size())
		if entry.IsDir() {
			size = "-"
		}

		// Check for symlink
		if entry.Type()&fs.ModeSymlink != 0 {
			prefix = "l"
			target, err := os.Readlink(filepath.Join(dir, entry.Name()))
			if err == nil {
				fmt.Fprintf(&b, "%s %8s %s -> %s\n", prefix, size, entry.Name(), target)
				continue
			}
		}

		fmt.Fprintf(&b, "%s %8s %s\n", prefix, size, entry.Name())
	}

	output := strings.TrimRight(b.String(), "\n")
	if output == "" {
		output = "(empty directory)"
	}

	return tool.Result{
		Output: output,
		Metadata: map[string]any{
			"directory": dir,
			"files":     fileCount,
			"dirs":      dirCount,
			"total":     fileCount + dirCount,
		},
	}, nil
}

func formatSize(bytes int64) string {
	switch {
	case bytes >= 1<<30:
		return fmt.Sprintf("%.1fG", float64(bytes)/(1<<30))
	case bytes >= 1<<20:
		return fmt.Sprintf("%.1fM", float64(bytes)/(1<<20))
	case bytes >= 1<<10:
		return fmt.Sprintf("%.1fK", float64(bytes)/(1<<10))
	default:
		return fmt.Sprintf("%dB", bytes)
	}
}
