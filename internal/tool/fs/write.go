package fs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"somegit.dev/Owlibou/gnoma/internal/tool"
)

const writeToolName = "fs.write"

var writeParams = json.RawMessage(`{
	"type": "object",
	"properties": {
		"path": {
			"type": "string",
			"description": "Absolute path to the file to write"
		},
		"content": {
			"type": "string",
			"description": "Content to write to the file"
		}
	},
	"required": ["path", "content"]
}`)

type WriteOption func(*WriteTool)

// WithMaxFileSize rejects writes where the content exceeds n bytes. 0 means no limit.
func WithMaxFileSize(n int64) WriteOption {
	return func(t *WriteTool) { t.maxFileSize = n }
}

type WriteTool struct {
	maxFileSize int64
	guard       *Guard
}

func (t *WriteTool) SetGuard(g *Guard) { t.guard = g }

func NewWriteTool(opts ...WriteOption) *WriteTool {
	t := &WriteTool{}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

func (t *WriteTool) Name() string { return writeToolName }
func (t *WriteTool) Description() string {
	return "Write content to a file, creating parent directories as needed"
}
func (t *WriteTool) Parameters() json.RawMessage { return writeParams }
func (t *WriteTool) IsReadOnly() bool            { return false }
func (t *WriteTool) IsDestructive() bool         { return false }
func (t *WriteTool) Category() tool.Category     { return tool.CategoryWrite }

func (t *WriteTool) ExtractPaths(args json.RawMessage) []string {
	var a writeArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return nil
	}
	return []string{a.Path}
}

type writeArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func (t *WriteTool) Execute(_ context.Context, args json.RawMessage) (tool.Result, error) {
	var a writeArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return tool.Result{}, fmt.Errorf("fs.write: invalid args: %w", err)
	}
	if a.Path == "" {
		return tool.Result{}, fmt.Errorf("fs.write: path required")
	}

	if t.maxFileSize > 0 && int64(len(a.Content)) > t.maxFileSize {
		return tool.Result{Output: fmt.Sprintf("Error: content too large (%d bytes, limit %d bytes)", len(a.Content), t.maxFileSize)}, nil
	}

	path := a.Path
	if t.guard != nil {
		resolved, err := t.guard.ResolveWrite(path)
		if err != nil {
			return tool.Result{Output: fmt.Sprintf("Error: %v", err)}, nil
		}
		path = resolved
	}

	// Create parent directories
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return tool.Result{Output: fmt.Sprintf("Error creating directory: %v", err)}, nil
	}

	if err := os.WriteFile(path, []byte(a.Content), 0o644); err != nil {
		return tool.Result{Output: fmt.Sprintf("Error writing file: %v", err)}, nil
	}

	return tool.Result{
		Output:   fmt.Sprintf("Wrote %d bytes to %s", len(a.Content), path),
		Metadata: map[string]any{"bytes_written": len(a.Content), "path": path},
	}, nil
}
