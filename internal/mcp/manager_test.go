package mcp

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"testing"
	"time"

	"somegit.dev/Owlibou/gnoma/internal/tool"
)

func TestManager_StartAll_RegistersTools(t *testing.T) {
	tools := []MCPTool{
		{Name: "status", Description: "Get status", InputSchema: json.RawMessage(`{"type":"object"}`)},
		{Name: "commit", Description: "Create commit", InputSchema: json.RawMessage(`{"type":"object"}`)},
	}
	callResult := `{"content":[{"type":"text","text":"ok"}]}`
	script := writeMCPServer(t, tools, callResult)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	reg := tool.NewRegistry()

	mgr := NewManager(logger)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := mgr.StartAll(ctx, []ServerConfig{
		{
			Name:    "git",
			Command: "bash",
			Args:    []string{script},
			Timeout: 5 * time.Second,
		},
	}, reg)
	if err != nil {
		t.Fatalf("StartAll: %v", err)
	}
	defer func() { _ = mgr.Shutdown() }()

	// Tools should be registered with mcp__ prefix.
	if _, ok := reg.Get("mcp__git__status"); !ok {
		t.Error("mcp__git__status not found in registry")
	}
	if _, ok := reg.Get("mcp__git__commit"); !ok {
		t.Error("mcp__git__commit not found in registry")
	}
}

func TestManager_StartAll_ReplaceDefault(t *testing.T) {
	tools := []MCPTool{
		{Name: "exec", Description: "Custom bash", InputSchema: json.RawMessage(`{"type":"object"}`)},
	}
	callResult := `{"content":[{"type":"text","text":"replaced"}]}`
	script := writeMCPServer(t, tools, callResult)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	reg := tool.NewRegistry()

	// Register a mock built-in "bash" tool first.
	reg.Register(&mockTool{name: "bash"})

	mgr := NewManager(logger)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := mgr.StartAll(ctx, []ServerConfig{
		{
			Name:           "custom",
			Command:        "bash",
			Args:           []string{script},
			Timeout:        5 * time.Second,
			ReplaceDefault: map[string]string{"exec": "bash"},
		},
	}, reg)
	if err != nil {
		t.Fatalf("StartAll: %v", err)
	}
	defer func() { _ = mgr.Shutdown() }()

	// The "bash" tool should now be the MCP adapter, not the mock.
	bashTool, ok := reg.Get("bash")
	if !ok {
		t.Fatal("bash tool not found after replace")
	}

	adapter, ok := bashTool.(*Adapter)
	if !ok {
		t.Fatalf("bash tool is %T, want *Adapter", bashTool)
	}
	if adapter.mcpTool.Name != "exec" {
		t.Errorf("replaced tool's MCP name = %q, want %q", adapter.mcpTool.Name, "exec")
	}
}

func TestManager_StartAll_BadCommand(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	reg := tool.NewRegistry()

	mgr := NewManager(logger)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := mgr.StartAll(ctx, []ServerConfig{
		{
			Name:    "bad",
			Command: "/nonexistent/binary/that/does/not/exist",
			Timeout: 2 * time.Second,
		},
	}, reg)
	if err == nil {
		t.Error("expected error for bad command")
		_ = mgr.Shutdown()
	}
}

func TestManager_Shutdown(t *testing.T) {
	tools := []MCPTool{
		{Name: "ping", Description: "Ping", InputSchema: json.RawMessage(`{"type":"object"}`)},
	}
	script := writeMCPServer(t, tools, `{"content":[]}`)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	reg := tool.NewRegistry()

	mgr := NewManager(logger)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := mgr.StartAll(ctx, []ServerConfig{
		{
			Name:    "test",
			Command: "bash",
			Args:    []string{script},
			Timeout: 5 * time.Second,
		},
	}, reg)
	if err != nil {
		t.Fatalf("StartAll: %v", err)
	}

	// Shutdown should not error.
	if err := mgr.Shutdown(); err != nil {
		t.Errorf("Shutdown: %v", err)
	}
}

func TestManager_StartAll_ReplaceDefault_PicksMatchingTool(t *testing.T) {
	// Server has multiple tools, only one replaces a built-in.
	tools := []MCPTool{
		{Name: "read", Description: "Read file", InputSchema: json.RawMessage(`{"type":"object"}`)},
		{Name: "write", Description: "Write file", InputSchema: json.RawMessage(`{"type":"object"}`)},
		{Name: "extra", Description: "Extra tool", InputSchema: json.RawMessage(`{"type":"object"}`)},
	}
	script := writeMCPServer(t, tools, `{"content":[]}`)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	reg := tool.NewRegistry()
	reg.Register(&mockTool{name: "fs.read"})
	reg.Register(&mockTool{name: "fs.write"})

	mgr := NewManager(logger)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := mgr.StartAll(ctx, []ServerConfig{
		{
			Name:           "custom-fs",
			Command:        "bash",
			Args:           []string{script},
			Timeout:        5 * time.Second,
			ReplaceDefault: map[string]string{"read": "fs.read", "write": "fs.write"},
		},
	}, reg)
	if err != nil {
		t.Fatalf("StartAll: %v", err)
	}
	defer func() { _ = mgr.Shutdown() }()

	// fs.read and fs.write should be replaced.
	if fsRead, ok := reg.Get("fs.read"); !ok {
		t.Error("fs.read not found")
	} else if _, ok := fsRead.(*Adapter); !ok {
		t.Error("fs.read should be replaced by MCP adapter")
	}
	if fsWrite, ok := reg.Get("fs.write"); !ok {
		t.Error("fs.write not found")
	} else if _, ok := fsWrite.(*Adapter); !ok {
		t.Error("fs.write should be replaced by MCP adapter")
	}
	// "extra" should be registered with mcp__ prefix.
	if _, ok := reg.Get("mcp__custom-fs__extra"); !ok {
		t.Error("mcp__custom-fs__extra not found in registry")
	}
}

// mockTool is a minimal tool.Tool for testing registry replacement.
type mockTool struct {
	name string
}

func (m *mockTool) Name() string                { return m.name }
func (m *mockTool) Description() string         { return "mock" }
func (m *mockTool) Parameters() json.RawMessage { return json.RawMessage(`{}`) }
func (m *mockTool) Execute(_ context.Context, _ json.RawMessage) (tool.Result, error) {
	return tool.Result{}, nil
}
func (m *mockTool) IsReadOnly() bool    { return false }
func (m *mockTool) IsDestructive() bool { return false }
