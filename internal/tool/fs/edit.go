package fs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"somegit.dev/Owlibou/gnoma/internal/tool"
)

const editToolName = "fs.edit"

var editParams = json.RawMessage(`{
	"type": "object",
	"properties": {
		"path": {
			"type": "string",
			"description": "Absolute path to the file to edit"
		},
		"old_string": {
			"type": "string",
			"description": "The exact text to find and replace"
		},
		"new_string": {
			"type": "string",
			"description": "The replacement text"
		},
		"replace_all": {
			"type": "boolean",
			"description": "Replace all occurrences (default false)"
		}
	},
	"required": ["path", "old_string", "new_string"]
}`)

type EditTool struct{}

func NewEditTool() *EditTool { return &EditTool{} }

func (t *EditTool) Name() string               { return editToolName }
func (t *EditTool) Description() string         { return "Perform exact string replacement in a file" }
func (t *EditTool) Parameters() json.RawMessage { return editParams }
func (t *EditTool) IsReadOnly() bool            { return false }
func (t *EditTool) IsDestructive() bool         { return false }

type editArgs struct {
	Path       string `json:"path"`
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all,omitempty"`
}

func (t *EditTool) Execute(_ context.Context, args json.RawMessage) (tool.Result, error) {
	var a editArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return tool.Result{}, fmt.Errorf("fs.edit: invalid args: %w", err)
	}
	if a.Path == "" {
		return tool.Result{}, fmt.Errorf("fs.edit: path required")
	}
	if a.OldString == a.NewString {
		return tool.Result{}, fmt.Errorf("fs.edit: old_string and new_string must differ")
	}

	data, err := os.ReadFile(a.Path)
	if err != nil {
		return tool.Result{Output: fmt.Sprintf("Error: %v", err)}, nil
	}

	content := string(data)

	count := strings.Count(content, a.OldString)
	if count == 0 {
		return tool.Result{
			Output:   "Error: old_string not found in file",
			Metadata: map[string]any{"matches": 0},
		}, nil
	}

	if !a.ReplaceAll && count > 1 {
		return tool.Result{
			Output:   fmt.Sprintf("Error: old_string has %d matches (must be unique, or use replace_all)", count),
			Metadata: map[string]any{"matches": count},
		}, nil
	}

	var newContent string
	if a.ReplaceAll {
		newContent = strings.ReplaceAll(content, a.OldString, a.NewString)
	} else {
		newContent = strings.Replace(content, a.OldString, a.NewString, 1)
	}

	if err := os.WriteFile(a.Path, []byte(newContent), 0o644); err != nil {
		return tool.Result{Output: fmt.Sprintf("Error writing file: %v", err)}, nil
	}

	replacements := 1
	if a.ReplaceAll {
		replacements = count
	}

	return tool.Result{
		Output:   fmt.Sprintf("Replaced %d occurrence(s) in %s", replacements, a.Path),
		Metadata: map[string]any{"replacements": replacements, "path": a.Path},
	}, nil
}
