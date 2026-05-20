package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"somegit.dev/Owlibou/gnoma/internal/tool"
)

// Adapter wraps an MCPTool as a gnoma tool.Tool.
type Adapter struct {
	serverName   string
	mcpTool      MCPTool
	client       *Client
	overrideName string // non-empty when replacing a built-in
	policy       ToolPolicy
}

// Compile-time interface checks.
var (
	_ tool.Tool              = (*Adapter)(nil)
	_ tool.DeferrableTool    = (*Adapter)(nil)
	_ tool.PathSensitiveTool = (*Adapter)(nil)
)

// NewAdapter creates a tool adapter for the given MCP tool.
func NewAdapter(serverName string, mcpTool MCPTool, client *Client, policy ToolPolicy) *Adapter {
	return &Adapter{
		serverName: serverName,
		mcpTool:    mcpTool,
		client:     client,
		policy:     policy,
	}
}

// SetOverrideName sets a replacement name (used for replace_default).
func (a *Adapter) SetOverrideName(name string) {
	a.overrideName = name
}

// Name returns the tool name. Uses mcp__{server}__{tool} convention,
// or the override name when replacing a built-in.
func (a *Adapter) Name() string {
	if a.overrideName != "" {
		return a.overrideName
	}
	return fmt.Sprintf("mcp__%s__%s", a.serverName, a.mcpTool.Name)
}

// Description returns the MCP tool's description.
func (a *Adapter) Description() string {
	return a.mcpTool.Description
}

// Parameters returns the MCP tool's input schema (zero-copy passthrough).
func (a *Adapter) Parameters() json.RawMessage {
	return a.mcpTool.InputSchema
}

func (a *Adapter) ExtractPaths(args json.RawMessage) []string {
	var m map[string]any
	if err := json.Unmarshal(args, &m); err != nil {
		return nil
	}
	var paths []string
	for _, argName := range a.policy.PathArgs {
		if v, ok := m[argName]; ok {
			if s, ok := v.(string); ok {
				paths = append(paths, s)
			}
		}
	}
	return paths
}

// Execute calls the MCP server's tools/call method.
func (a *Adapter) Execute(ctx context.Context, args json.RawMessage) (tool.Result, error) {
	result, err := a.client.CallTool(ctx, a.mcpTool.Name, args)
	if err != nil {
		// RPC errors are surfaced as tool output so the LLM can see them.
		var rpcErr *RPCError
		if errors.As(err, &rpcErr) {
			return tool.Result{
				Output: fmt.Sprintf("MCP error: %s", rpcErr.Message),
			}, nil
		}
		// Transport-level errors are Go errors (broken pipe, timeout).
		return tool.Result{}, err
	}

	output, err := extractTextContent(result)
	if err != nil {
		return tool.Result{
			Output: fmt.Sprintf("MCP response parse error: %v\nRaw: %s", err, result),
		}, nil
	}

	return tool.Result{Output: output}, nil
}

// IsReadOnly returns false conservatively — MCP tools may have side effects.
func (a *Adapter) IsReadOnly() bool { return false }

// IsDestructive returns false — use permission rules for granular control.
func (a *Adapter) IsDestructive() bool { return false }

// ShouldDefer returns true — MCP tools start deferred to reduce token overhead.
func (a *Adapter) ShouldDefer() bool { return true }

// extractTextContent concatenates text blocks from an MCP tools/call result.
func extractTextContent(raw json.RawMessage) (string, error) {
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", err
	}

	var parts []string
	for _, c := range result.Content {
		if c.Type == "text" {
			parts = append(parts, c.Text)
		}
	}
	return strings.Join(parts, "\n"), nil
}
