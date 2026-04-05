package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"somegit.dev/Owlibou/gnoma/internal/tool"
	"somegit.dev/Owlibou/gnoma/internal/tool/persist"
)

var listResultsSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"filter": {
			"type": "string",
			"description": "Optional tool name prefix to filter results (e.g. 'bash' shows only bash results). Note: dots in tool names become underscores (e.g. 'fs_grep' for 'fs.grep')."
		}
	}
}`)

type ListResultsTool struct {
	store *persist.Store
}

func NewListResultsTool(s *persist.Store) *ListResultsTool {
	return &ListResultsTool{store: s}
}

func (t *ListResultsTool) Name() string { return "list_results" }
func (t *ListResultsTool) Description() string {
	return "List tool result files saved in this session. Use this to discover outputs from previous tool calls that can be passed to elfs or read with read_result."
}
func (t *ListResultsTool) Parameters() json.RawMessage { return listResultsSchema }
func (t *ListResultsTool) IsReadOnly() bool            { return true }
func (t *ListResultsTool) IsDestructive() bool         { return false }

type listResultsArgs struct {
	Filter string `json:"filter,omitempty"`
}

func (t *ListResultsTool) Execute(_ context.Context, args json.RawMessage) (tool.Result, error) {
	var a listResultsArgs
	json.Unmarshal(args, &a) //nolint:errcheck

	files, err := t.store.List(a.Filter)
	if err != nil {
		return tool.Result{Output: fmt.Sprintf("error listing results: %v", err)}, nil
	}
	if len(files) == 0 {
		return tool.Result{Output: "no results persisted in this session yet"}, nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%d result(s) in session:\n\n", len(files))
	for _, f := range files {
		fmt.Fprintf(&b, "%s  [%s, %s, %s]\n",
			f.Path,
			f.ToolName,
			formatSize(f.Size),
			f.ModTime.Format("15:04:05"),
		)
	}
	return tool.Result{Output: b.String()}, nil
}

func formatSize(bytes int64) string {
	if bytes >= 1024*1024 {
		return fmt.Sprintf("%.1fMB", float64(bytes)/1024/1024)
	}
	if bytes >= 1024 {
		return fmt.Sprintf("%.1fKB", float64(bytes)/1024)
	}
	return fmt.Sprintf("%dB", bytes)
}
