package mcp

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

// writeMCPServer creates a bash script that implements a minimal MCP server.
// Response payloads are written to files to avoid bash quoting issues.
func writeMCPServer(t *testing.T, tools []MCPTool, callResult string) string {
	t.Helper()
	dir := t.TempDir()

	// Write response payloads as files.
	initResult := `{"protocolVersion":"2024-11-05","capabilities":{"tools":{}},"serverInfo":{"name":"test-server","version":"1.0.0"}}`
	_ = os.WriteFile(filepath.Join(dir, "init.json"), []byte(initResult), 0o644)

	toolsJSON, err := json.Marshal(struct {
		Tools []MCPTool `json:"tools"`
	}{Tools: tools})
	if err != nil {
		t.Fatalf("marshal tools: %v", err)
	}
	_ = os.WriteFile(filepath.Join(dir, "tools.json"), toolsJSON, 0o644)
	_ = os.WriteFile(filepath.Join(dir, "call.json"), []byte(callResult), 0o644)

	// The script uses pure bash for JSON parsing — no python3 or jq dependency.
	// We extract "method" and "id" with grep since the JSON-RPC format is predictable.
	script := filepath.Join(dir, "mcp-server.sh")
	content := `#!/bin/bash
DIR="` + dir + `"
while IFS= read -r line; do
  method=$(echo "$line" | grep -o '"method":"[^"]*"' | head -1 | sed 's/"method":"//;s/"//')
  id=$(echo "$line" | grep -o '"id":[0-9]*' | head -1 | cut -d: -f2)

  case "$method" in
    initialize)
      result=$(cat "$DIR/init.json")
      printf '{"jsonrpc":"2.0","id":%s,"result":%s}\n' "$id" "$result"
      ;;
    initialized)
      ;;
    tools/list)
      result=$(cat "$DIR/tools.json")
      printf '{"jsonrpc":"2.0","id":%s,"result":%s}\n' "$id" "$result"
      ;;
    tools/call)
      result=$(cat "$DIR/call.json")
      printf '{"jsonrpc":"2.0","id":%s,"result":%s}\n' "$id" "$result"
      ;;
    *)
      printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32601,"message":"method not found"}}\n' "$id"
      ;;
  esac
done
`
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatalf("write mcp server: %v", err)
	}
	return script
}

func TestClient_Initialize(t *testing.T) {
	tools := []MCPTool{
		{Name: "echo", Description: "Echo input", InputSchema: json.RawMessage(`{"type":"object"}`)},
	}
	script := writeMCPServer(t, tools, `{}`)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	tr := NewTransport("bash", []string{script}, nil, logger)

	ctx := context.Background()
	if err := tr.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	client := NewClient(tr, logger)
	defer func() { _ = client.Close() }()

	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	if client.serverInfo.Name != "test-server" {
		t.Errorf("serverInfo.Name = %q, want %q", client.serverInfo.Name, "test-server")
	}
	if client.serverInfo.Version != "1.0.0" {
		t.Errorf("serverInfo.Version = %q, want %q", client.serverInfo.Version, "1.0.0")
	}
}

func TestClient_ListTools(t *testing.T) {
	tools := []MCPTool{
		{
			Name:        "get_status",
			Description: "Get git status",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`),
		},
		{
			Name:        "commit",
			Description: "Create commit",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"message":{"type":"string"}},"required":["message"]}`),
		},
	}
	script := writeMCPServer(t, tools, `{}`)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	tr := NewTransport("bash", []string{script}, nil, logger)

	ctx := context.Background()
	if err := tr.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	client := NewClient(tr, logger)
	defer func() { _ = client.Close() }()

	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	got, err := client.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("got %d tools, want 2", len(got))
	}
	if got[0].Name != "get_status" {
		t.Errorf("tool[0].Name = %q, want %q", got[0].Name, "get_status")
	}
	if got[1].Name != "commit" {
		t.Errorf("tool[1].Name = %q, want %q", got[1].Name, "commit")
	}
	// Verify InputSchema passes through as raw JSON.
	if string(got[0].InputSchema) == "" {
		t.Error("tool[0].InputSchema is empty")
	}
}

func TestClient_CallTool(t *testing.T) {
	tools := []MCPTool{
		{Name: "echo", Description: "Echo", InputSchema: json.RawMessage(`{"type":"object"}`)},
	}
	callResult := `{"content":[{"type":"text","text":"hello world"}]}`
	script := writeMCPServer(t, tools, callResult)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	tr := NewTransport("bash", []string{script}, nil, logger)

	ctx := context.Background()
	if err := tr.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	client := NewClient(tr, logger)
	defer func() { _ = client.Close() }()

	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	result, err := client.CallTool(ctx, "echo", json.RawMessage(`{"input":"test"}`))
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	// Result should be the raw content array.
	var parsed struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if len(parsed.Content) != 1 {
		t.Fatalf("got %d content blocks, want 1", len(parsed.Content))
	}
	if parsed.Content[0].Text != "hello world" {
		t.Errorf("content text = %q, want %q", parsed.Content[0].Text, "hello world")
	}
}

func TestClient_InitializeFailure(t *testing.T) {
	// Server that returns an error for initialize.
	dir := t.TempDir()
	script := filepath.Join(dir, "bad-server.sh")
	content := `#!/bin/bash
read -r line
id=$(echo "$line" | grep -o '"id":[0-9]*' | head -1 | cut -d: -f2)
echo "{\"jsonrpc\":\"2.0\",\"id\":$id,\"error\":{\"code\":-32000,\"message\":\"init failed\"}}"
`
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatalf("write: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	tr := NewTransport("bash", []string{script}, nil, logger)

	ctx := context.Background()
	if err := tr.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	client := NewClient(tr, logger)
	defer func() { _ = client.Close() }()

	err := client.Initialize(ctx)
	if err == nil {
		t.Fatal("expected Initialize to fail")
	}
}
