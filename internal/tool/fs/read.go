package fs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"somegit.dev/Owlibou/gnoma/internal/tool"
)

const (
	readToolName    = "fs.read"
	defaultMaxLines = 2000
)

var readParams = json.RawMessage(`{
	"type": "object",
	"properties": {
		"path": {
			"type": "string",
			"description": "Absolute path to the file to read"
		},
		"offset": {
			"type": "integer",
			"description": "Line number to start reading from (0-based)"
		},
		"limit": {
			"type": "integer",
			"description": "Maximum number of lines to read"
		}
	},
	"required": ["path"]
}`)

type ReadTool struct {
	maxLines int
}

type ReadOption func(*ReadTool)

func WithMaxLines(n int) ReadOption {
	return func(t *ReadTool) { t.maxLines = n }
}

func NewReadTool(opts ...ReadOption) *ReadTool {
	t := &ReadTool{maxLines: defaultMaxLines}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

func (t *ReadTool) Name() string               { return readToolName }
func (t *ReadTool) Description() string         { return "Read a file from the filesystem with optional offset and line limit" }
func (t *ReadTool) Parameters() json.RawMessage { return readParams }
func (t *ReadTool) IsReadOnly() bool            { return true }
func (t *ReadTool) IsDestructive() bool         { return false }

func (t *ReadTool) ExtractPaths(args json.RawMessage) []string {
	var a readArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return nil
	}
	return []string{a.Path}
}

type readArgs struct {
	Path   string `json:"path"`
	Offset int    `json:"offset,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

func (t *ReadTool) Execute(_ context.Context, args json.RawMessage) (tool.Result, error) {
	var a readArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return tool.Result{}, fmt.Errorf("fs.read: invalid args: %w", err)
	}
	if a.Path == "" {
		return tool.Result{}, fmt.Errorf("fs.read: path required")
	}

	data, err := os.ReadFile(a.Path)
	if err != nil {
		return tool.Result{Output: fmt.Sprintf("Error: %v", err)}, nil
	}

	lines := strings.Split(string(data), "\n")
	totalLines := len(lines)

	// Apply offset
	offset := a.Offset
	if offset < 0 {
		offset = 0
	}
	if offset >= totalLines {
		return tool.Result{
			Output:   fmt.Sprintf("(file has %d lines, offset %d is past end)", totalLines, offset),
			Metadata: map[string]any{"total_lines": totalLines},
		}, nil
	}
	lines = lines[offset:]

	// Apply limit
	limit := a.Limit
	if limit <= 0 {
		limit = t.maxLines
	}
	truncated := false
	if len(lines) > limit {
		lines = lines[:limit]
		truncated = true
	}

	// Format with line numbers (1-based, matching cat -n)
	var b strings.Builder
	for i, line := range lines {
		fmt.Fprintf(&b, "%d\t%s\n", offset+i+1, line)
	}

	output := strings.TrimRight(b.String(), "\n")

	meta := map[string]any{"total_lines": totalLines}
	if truncated {
		meta["truncated"] = true
		meta["showing"] = fmt.Sprintf("lines %d-%d of %d", offset+1, offset+len(lines), totalLines)
	}

	return tool.Result{Output: output, Metadata: meta}, nil
}
