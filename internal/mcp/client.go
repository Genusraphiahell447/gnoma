package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
)

const protocolVersion = "2024-11-05"

// ServerInfo describes the MCP server identity.
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// MCPTool is a tool definition discovered from an MCP server.
type MCPTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// Client implements the MCP protocol lifecycle over a Transport.
type Client struct {
	transport  *Transport
	serverInfo ServerInfo
	logger     *slog.Logger
}

// NewClient creates an MCP client. Call Initialize before using other methods.
func NewClient(transport *Transport, logger *slog.Logger) *Client {
	return &Client{
		transport: transport,
		logger:    logger,
	}
}

// Initialize performs the MCP handshake: sends initialize request,
// receives server info, and sends initialized notification.
func (c *Client) Initialize(ctx context.Context) error {
	params := struct {
		ProtocolVersion string   `json:"protocolVersion"`
		Capabilities    struct{} `json:"capabilities"`
		ClientInfo      struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"clientInfo"`
	}{
		ProtocolVersion: protocolVersion,
	}
	params.ClientInfo.Name = "gnoma"
	params.ClientInfo.Version = "0.1.0"

	result, err := c.transport.Call(ctx, "initialize", params)
	if err != nil {
		return fmt.Errorf("mcp initialize: %w", err)
	}

	var initResult struct {
		ProtocolVersion string     `json:"protocolVersion"`
		ServerInfo      ServerInfo `json:"serverInfo"`
	}
	if err := json.Unmarshal(result, &initResult); err != nil {
		return fmt.Errorf("mcp initialize: decode result: %w", err)
	}

	c.serverInfo = initResult.ServerInfo
	c.logger.Debug("mcp initialized",
		"server", c.serverInfo.Name,
		"version", c.serverInfo.Version,
		"protocol", initResult.ProtocolVersion,
	)

	// Send initialized notification (no response expected).
	if err := c.transport.Notify(ctx, "initialized", nil); err != nil {
		return fmt.Errorf("mcp initialized notification: %w", err)
	}

	return nil
}

// ListTools calls tools/list and returns discovered tool definitions.
func (c *Client) ListTools(ctx context.Context) ([]MCPTool, error) {
	result, err := c.transport.Call(ctx, "tools/list", nil)
	if err != nil {
		return nil, fmt.Errorf("mcp tools/list: %w", err)
	}

	var toolsResult struct {
		Tools []MCPTool `json:"tools"`
	}
	if err := json.Unmarshal(result, &toolsResult); err != nil {
		return nil, fmt.Errorf("mcp tools/list: decode: %w", err)
	}

	c.logger.Debug("mcp tools discovered", "count", len(toolsResult.Tools))
	return toolsResult.Tools, nil
}

// CallTool invokes a tool on the MCP server and returns the raw result.
func (c *Client) CallTool(ctx context.Context, name string, args json.RawMessage) (json.RawMessage, error) {
	params := struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments,omitempty"`
	}{
		Name:      name,
		Arguments: args,
	}

	result, err := c.transport.Call(ctx, "tools/call", params)
	if err != nil {
		return nil, fmt.Errorf("mcp tools/call %q: %w", name, err)
	}

	return result, nil
}

// ServerName returns the server's reported name.
func (c *Client) ServerName() string {
	return c.serverInfo.Name
}

// Close shuts down the transport and server process.
func (c *Client) Close() error {
	return c.transport.Close()
}
