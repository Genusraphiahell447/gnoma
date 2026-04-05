package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"somegit.dev/Owlibou/gnoma/internal/tool"
	"somegit.dev/Owlibou/gnoma/internal/tool/persist"
)

var readResultSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"path": {
			"type": "string",
			"description": "Absolute path to the result file (from list_results output)"
		}
	},
	"required": ["path"]
}`)

type ReadResultTool struct {
	store *persist.Store
}

func NewReadResultTool(s *persist.Store) *ReadResultTool {
	return &ReadResultTool{store: s}
}

func (t *ReadResultTool) Name() string { return "read_result" }
func (t *ReadResultTool) Description() string {
	return "Read the full content of a persisted tool result file. Use paths from list_results. Only files within the current session directory are accessible."
}
func (t *ReadResultTool) Parameters() json.RawMessage { return readResultSchema }
func (t *ReadResultTool) IsReadOnly() bool            { return true }
func (t *ReadResultTool) IsDestructive() bool         { return false }

type readResultArgs struct {
	Path string `json:"path"`
}

func (t *ReadResultTool) Execute(_ context.Context, args json.RawMessage) (tool.Result, error) {
	var a readResultArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return tool.Result{}, fmt.Errorf("read_result: invalid args: %w", err)
	}
	if a.Path == "" {
		return tool.Result{}, fmt.Errorf("read_result: path required")
	}

	content, err := t.store.Read(a.Path)
	if err != nil {
		return tool.Result{Output: fmt.Sprintf("error reading result: %v", err)}, nil
	}
	return tool.Result{Output: content}, nil
}
