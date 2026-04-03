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

type WriteTool struct{}

func NewWriteTool() *WriteTool { return &WriteTool{} }

func (t *WriteTool) Name() string               { return writeToolName }
func (t *WriteTool) Description() string         { return "Write content to a file, creating parent directories as needed" }
func (t *WriteTool) Parameters() json.RawMessage { return writeParams }
func (t *WriteTool) IsReadOnly() bool            { return false }
func (t *WriteTool) IsDestructive() bool         { return false }

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

	// Create parent directories
	dir := filepath.Dir(a.Path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return tool.Result{Output: fmt.Sprintf("Error creating directory: %v", err)}, nil
	}

	if err := os.WriteFile(a.Path, []byte(a.Content), 0o644); err != nil {
		return tool.Result{Output: fmt.Sprintf("Error writing file: %v", err)}, nil
	}

	return tool.Result{
		Output:   fmt.Sprintf("Wrote %d bytes to %s", len(a.Content), a.Path),
		Metadata: map[string]any{"bytes_written": len(a.Content), "path": a.Path},
	}, nil
}
