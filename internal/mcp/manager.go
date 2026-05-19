package mcp

import (
	"context"
	"fmt"
	"log/slog"

	"somegit.dev/Owlibou/gnoma/internal/tool"
)

// Manager coordinates multiple MCP server lifecycles and tool registration.
type Manager struct {
	clients map[string]*Client
	logger  *slog.Logger
}

// NewManager creates an MCP manager.
func NewManager(logger *slog.Logger) *Manager {
	return &Manager{
		clients: make(map[string]*Client),
		logger:  logger,
	}
}

// StartAll starts all configured MCP servers, discovers tools, and registers
// them in the tool registry. Servers start sequentially to simplify error handling.
func (m *Manager) StartAll(ctx context.Context, servers []ServerConfig, registry *tool.Registry) error {
	for _, srv := range servers {
		client, err := m.startServer(ctx, srv)
		if err != nil {
			if shutdownErr := m.Shutdown(); shutdownErr != nil {
				m.logger.Warn("partial shutdown after server-start failure",
					"failed_server", srv.Name, "shutdown_error", shutdownErr)
			}
			return fmt.Errorf("mcp server %q: %w", srv.Name, err)
		}

		tools, err := client.ListTools(ctx)
		if err != nil {
			if shutdownErr := m.Shutdown(); shutdownErr != nil {
				m.logger.Warn("partial shutdown after list-tools failure",
					"failed_server", srv.Name, "shutdown_error", shutdownErr)
			}
			return fmt.Errorf("mcp server %q: list tools: %w", srv.Name, err)
		}

		m.registerTools(srv, tools, client, registry)
		m.clients[srv.Name] = client

		m.logger.Info("mcp server started",
			"name", srv.Name,
			"tools", len(tools),
			"replace", srv.ReplaceDefault,
		)
	}

	return nil
}

// Shutdown gracefully stops all MCP server processes.
func (m *Manager) Shutdown() error {
	var firstErr error
	for name, client := range m.clients {
		if err := client.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("mcp shutdown %q: %w", name, err)
		}
	}
	m.clients = make(map[string]*Client)
	return firstErr
}

func (m *Manager) startServer(ctx context.Context, srv ServerConfig) (*Client, error) {
	tr := NewTransport(srv.Command, srv.Args, srv.Env, m.logger)

	if err := tr.Start(ctx); err != nil {
		return nil, err
	}

	client := NewClient(tr, m.logger)

	initCtx, cancel := context.WithTimeout(ctx, srv.Timeout)
	defer cancel()

	if err := client.Initialize(initCtx); err != nil {
		if closeErr := tr.Close(); closeErr != nil {
			m.logger.Warn("transport close after init failure", "error", closeErr)
		}
		return nil, err
	}

	return client, nil
}

func (m *Manager) registerTools(srv ServerConfig, tools []MCPTool, client *Client, registry *tool.Registry) {
	for _, mt := range tools {
		policy := srv.ToolPolicy[mt.Name]
		adapter := NewAdapter(srv.Name, mt, client, policy)

		// Explicit mapping: if this MCP tool name has a replace_default entry,
		// register it under the built-in's name instead of mcp__{server}__{tool}.
		if builtinName, ok := srv.ReplaceDefault[mt.Name]; ok {
			adapter.SetOverrideName(builtinName)
		}

		registry.Register(adapter)
		m.logger.Debug("mcp tool registered",
			"name", adapter.Name(),
			"server", srv.Name,
			"mcp_name", mt.Name,
		)
	}
}
