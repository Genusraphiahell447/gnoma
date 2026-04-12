package mcp

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestAdapter_Name_Default(t *testing.T) {
	a := NewAdapter("git", MCPTool{Name: "status"}, nil)
	want := "mcp__git__status"
	if got := a.Name(); got != want {
		t.Errorf("Name() = %q, want %q", got, want)
	}
}

func TestAdapter_Name_Override(t *testing.T) {
	a := NewAdapter("custom-fs", MCPTool{Name: "read_file"}, nil)
	a.SetOverrideName("fs.read")
	want := "fs.read"
	if got := a.Name(); got != want {
		t.Errorf("Name() = %q, want %q", got, want)
	}
}

func TestAdapter_NameConvention(t *testing.T) {
	tests := []struct {
		server string
		tool   string
		want   string
	}{
		{"git", "status", "mcp__git__status"},
		{"docker", "ps", "mcp__docker__ps"},
		{"my-server", "my-tool", "mcp__my-server__my-tool"},
	}

	for _, tt := range tests {
		a := NewAdapter(tt.server, MCPTool{Name: tt.tool}, nil)
		if got := a.Name(); got != tt.want {
			t.Errorf("Name(%q, %q) = %q, want %q", tt.server, tt.tool, got, tt.want)
		}
	}
}

func TestAdapter_Description(t *testing.T) {
	a := NewAdapter("git", MCPTool{
		Name:        "status",
		Description: "Get git status",
	}, nil)
	if got := a.Description(); got != "Get git status" {
		t.Errorf("Description() = %q, want %q", got, "Get git status")
	}
}

func TestAdapter_Parameters(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`)
	a := NewAdapter("git", MCPTool{
		Name:        "status",
		InputSchema: schema,
	}, nil)

	got := a.Parameters()
	if string(got) != string(schema) {
		t.Errorf("Parameters() = %s, want %s", got, schema)
	}
}

func TestAdapter_IsReadOnly(t *testing.T) {
	a := NewAdapter("git", MCPTool{Name: "status"}, nil)
	if a.IsReadOnly() {
		t.Error("IsReadOnly() = true, want false (conservative default)")
	}
}

func TestAdapter_IsDestructive(t *testing.T) {
	a := NewAdapter("git", MCPTool{Name: "status"}, nil)
	if a.IsDestructive() {
		t.Error("IsDestructive() = true, want false")
	}
}

func TestAdapter_ShouldDefer(t *testing.T) {
	a := NewAdapter("git", MCPTool{Name: "status"}, nil)
	if !a.ShouldDefer() {
		t.Error("ShouldDefer() = false, want true (MCP tools start deferred)")
	}
}

func TestAdapter_Execute(t *testing.T) {
	callResult := `{"content":[{"type":"text","text":"On branch main\nnothing to commit"}]}`
	tools := []MCPTool{
		{Name: "status", Description: "Git status", InputSchema: json.RawMessage(`{"type":"object"}`)},
	}
	script := writeMCPServer(t, tools, callResult)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	tr := NewTransport("bash", []string{script}, nil, logger)

	ctx := context.Background()
	if err := tr.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	client := NewClient(tr, logger)
	defer client.Close()

	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	a := NewAdapter("git", tools[0], client)

	result, err := a.Execute(ctx, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	want := "On branch main\nnothing to commit"
	if result.Output != want {
		t.Errorf("Output = %q, want %q", result.Output, want)
	}
}

func TestAdapter_Execute_MultipleTextBlocks(t *testing.T) {
	callResult := `{"content":[{"type":"text","text":"line 1"},{"type":"text","text":"line 2"}]}`
	tools := []MCPTool{
		{Name: "multi", Description: "Multi", InputSchema: json.RawMessage(`{"type":"object"}`)},
	}
	script := writeMCPServer(t, tools, callResult)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	tr := NewTransport("bash", []string{script}, nil, logger)

	ctx := context.Background()
	if err := tr.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	client := NewClient(tr, logger)
	defer client.Close()

	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	a := NewAdapter("test", tools[0], client)
	result, err := a.Execute(ctx, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	want := "line 1\nline 2"
	if result.Output != want {
		t.Errorf("Output = %q, want %q", result.Output, want)
	}
}

func TestAdapter_Execute_RPCError(t *testing.T) {
	// Server that returns an error for tools/call.
	dir := t.TempDir()
	script := filepath.Join(dir, "err-server.sh")
	content := `#!/bin/bash
DIR="` + dir + `"
while IFS= read -r line; do
  method=$(echo "$line" | python3 -c "import sys,json; print(json.load(sys.stdin).get('method',''))" 2>/dev/null)
  id=$(echo "$line" | python3 -c "import sys,json; print(json.load(sys.stdin).get('id',0))" 2>/dev/null)

  case "$method" in
    initialize)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":"2024-11-05","serverInfo":{"name":"err","version":"1.0"}}}\n' "$id"
      ;;
    initialized)
      ;;
    tools/call)
      printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32000,"message":"tool failed"}}\n' "$id"
      ;;
  esac
done
`
	os.WriteFile(script, []byte(content), 0o755)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	tr := NewTransport("bash", []string{script}, nil, logger)

	ctx := context.Background()
	tr.Start(ctx)
	client := NewClient(tr, logger)
	defer client.Close()
	client.Initialize(ctx)

	a := NewAdapter("err", MCPTool{Name: "broken", InputSchema: json.RawMessage(`{}`)}, client)
	result, err := a.Execute(ctx, json.RawMessage(`{}`))

	// RPC errors should be returned as tool output (not Go errors)
	// so the LLM can see the failure and retry/explain.
	if err != nil {
		t.Fatalf("Execute returned Go error: %v", err)
	}
	if result.Output == "" {
		t.Error("expected non-empty error output")
	}
}
