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
			m.Shutdown() // clean up already-started servers
			return fmt.Errorf("mcp server %q: %w", srv.Name, err)
		}

		tools, err := client.ListTools(ctx)
		if err != nil {
			m.Shutdown()
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
		tr.Close()
		return nil, err
	}

	return client, nil
}

func (m *Manager) registerTools(srv ServerConfig, tools []MCPTool, client *Client, registry *tool.Registry) {
	replaceSet := make(map[string]bool, len(srv.ReplaceDefault))
	for _, name := range srv.ReplaceDefault {
		replaceSet[name] = true
	}

	for _, mt := range tools {
		adapter := NewAdapter(srv.Name, mt, client)

		// Check if any replace_default entry matches this MCP tool.
		// Match by checking if the MCP tool name appears in a replace target,
		// or assign replacements in order.
		for _, replaceName := range srv.ReplaceDefault {
			if replaceSet[replaceName] {
				adapter.SetOverrideName(replaceName)
				delete(replaceSet, replaceName)
				break
			}
		}

		registry.Register(adapter)
		m.logger.Debug("mcp tool registered",
			"name", adapter.Name(),
			"server", srv.Name,
			"mcp_name", mt.Name,
		)
	}
}
