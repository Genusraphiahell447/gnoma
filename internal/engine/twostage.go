package engine

import (
	"encoding/json"
	"fmt"
	"strings"

	"somegit.dev/Owlibou/gnoma/internal/message"
	"somegit.dev/Owlibou/gnoma/internal/provider"
	"somegit.dev/Owlibou/gnoma/internal/tool"
)

// SyntheticSelectCategoryName is the tool name used by the two-stage routing
// path to let the model pick a category before real tool schemas are sent.
// The name is exported for tests that need to assert against it.
const SyntheticSelectCategoryName = "select_category"

// buildSelectCategoryDef constructs the synthetic select_category tool
// definition. Categories in the enum match tool.AllCategories() so the
// schema stays in sync if categories are added.
func buildSelectCategoryDef() provider.ToolDefinition {
	cats := tool.AllCategories()
	quoted := make([]string, len(cats))
	for i, c := range cats {
		quoted[i] = `"` + string(c) + `"`
	}
	params := json.RawMessage(`{
  "type": "object",
  "properties": {
    "category": {
      "type": "string",
      "enum": [` + strings.Join(quoted, ", ") + `],
      "description": "Tool category to load schemas for. Pick one based on what you intend to do next."
    }
  },
  "required": ["category"]
}`)
	return provider.ToolDefinition{
		Name: SyntheticSelectCategoryName,
		Description: "Select the category of tools to load for the next round. " +
			"Use 'read' for file reads, 'write' to modify files, 'search' to search the codebase, " +
			"'exec' to run commands, 'meta' for agent orchestration and introspection. " +
			"You can call this again later to switch categories.",
		Parameters: params,
	}
}

// snapshotSelectedCategory returns the category chosen by the model so far
// in this turn (or empty string if none).
func (e *Engine) snapshotSelectedCategory() tool.Category {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.selectedCategory
}

// setSelectedCategory records the model's category choice under lock.
func (e *Engine) setSelectedCategory(c tool.Category) {
	e.mu.Lock()
	e.selectedCategory = c
	e.mu.Unlock()
}

// resetTwoStageState clears any per-turn two-stage state. Called at the start
// of every runLoop so an aborted previous turn cannot leak state forward.
func (e *Engine) resetTwoStageState() {
	e.mu.Lock()
	e.selectedCategory = ""
	e.mu.Unlock()
}

// interceptSelectCategoryCalls splits the incoming tool calls into two
// buckets: real calls that need actual tool execution, and synthetic
// select_category calls that the engine handles internally. It updates
// e.selectedCategory as a side effect and returns synthetic tool results
// that satisfy the provider's "every tool_call needs a tool_result" contract.
func (e *Engine) interceptSelectCategoryCalls(calls []message.ToolCall) (realCalls []message.ToolCall, syntheticResults []message.ToolResult) {
	for _, call := range calls {
		if call.Name != SyntheticSelectCategoryName {
			realCalls = append(realCalls, call)
			continue
		}
		result := e.handleSelectCategory(call)
		syntheticResults = append(syntheticResults, result)
	}
	return realCalls, syntheticResults
}

// handleSelectCategory parses the synthetic tool call, updates engine state,
// and returns the tool result the model will see in the next round.
func (e *Engine) handleSelectCategory(call message.ToolCall) message.ToolResult {
	var args struct {
		Category string `json:"category"`
	}
	if err := json.Unmarshal(call.Arguments, &args); err != nil {
		e.setSelectedCategory("")
		return message.ToolResult{
			ToolCallID: call.ID,
			Content:    fmt.Sprintf("invalid arguments for select_category: %v. Please pick one of: read, write, search, exec, meta.", err),
			IsError:    true,
		}
	}

	cat := tool.Category(strings.ToLower(strings.TrimSpace(args.Category)))
	if !tool.IsValidCategory(cat) {
		e.setSelectedCategory("")
		return message.ToolResult{
			ToolCallID: call.ID,
			Content:    fmt.Sprintf("unknown category %q. Pick one of: read, write, search, exec, meta.", args.Category),
			IsError:    true,
		}
	}

	e.setSelectedCategory(cat)
	available := e.toolNamesForCategory(cat)
	content := fmt.Sprintf("Category %q selected. Tools now available: %s. Call them directly on the next turn.",
		cat, strings.Join(available, ", "))
	if e.logger != nil {
		e.logger.Debug("two-stage: category selected",
			"category", cat,
			"tools", available,
		)
	}
	return message.ToolResult{
		ToolCallID: call.ID,
		Content:    content,
	}
}

// toolNamesForCategory returns the registered tool names whose category
// matches the argument, in deterministic order.
func (e *Engine) toolNamesForCategory(cat tool.Category) []string {
	var names []string
	for _, t := range e.cfg.Tools.All() {
		if tool.CategoryOf(t) == cat {
			names = append(names, t.Name())
		}
	}
	return names
}
