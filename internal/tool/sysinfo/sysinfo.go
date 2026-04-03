package sysinfo

import (
	"context"
	"encoding/json"

	"somegit.dev/Owlibou/gnoma/internal/tool"
	"somegit.dev/Owlibou/gnoma/internal/tool/bash"
)

var paramSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"section": {
			"type": "string",
			"description": "Section to query: runtimes, packages, tools, hardware, all",
			"enum": ["runtimes", "packages", "tools", "hardware", "all"]
		}
	}
}`)

// Tool provides queryable access to system inventory.
type Tool struct {
	inventory *bash.SystemInventory
}

func New(inv *bash.SystemInventory) *Tool {
	return &Tool{inventory: inv}
}

func (t *Tool) Name() string               { return "system_info" }
func (t *Tool) Description() string         { return "Query system information: installed runtimes, packages, tools, hardware" }
func (t *Tool) Parameters() json.RawMessage { return paramSchema }
func (t *Tool) IsReadOnly() bool            { return true }
func (t *Tool) IsDestructive() bool         { return false }

type sysInfoArgs struct {
	Section string `json:"section,omitempty"`
}

func (t *Tool) Execute(_ context.Context, args json.RawMessage) (tool.Result, error) {
	var a sysInfoArgs
	if args != nil {
		_ = json.Unmarshal(args, &a)
	}
	if a.Section == "" {
		a.Section = "all"
	}
	return tool.Result{Output: t.inventory.QuerySection(a.Section)}, nil
}
