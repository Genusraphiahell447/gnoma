package mcp

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeMockServer creates a bash script that reads a JSON-RPC request from stdin
// and writes a canned response to stdout. Returns the script path.
func writeMockServer(t *testing.T, responseJSON string) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "mock-server.sh")
	content := `#!/bin/bash
read -r line
echo '` + responseJSON + `'
`
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatalf("write mock server: %v", err)
	}
	return script
}

// writeMockServerMulti creates a script that responds to each line of input
// with a corresponding line of output from the provided responses.
func writeMockServerMulti(t *testing.T, responses []string) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "mock-server.sh")

	var body string
	for _, r := range responses {
		body += `read -r line
echo '` + r + `'
`
	}

	content := "#!/bin/bash\n" + body
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatalf("write mock server: %v", err)
	}
	return script
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestTransport_StartAndClose(t *testing.T) {
	script := writeMockServer(t, `{"jsonrpc":"2.0","id":1,"result":{}}`)
	tr := NewTransport("bash", []string{script}, nil, testLogger())

	ctx := context.Background()
	if err := tr.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if err := tr.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestTransport_Call_Success(t *testing.T) {
	resp := `{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}`
	script := writeMockServer(t, resp)
	tr := NewTransport("bash", []string{script}, nil, testLogger())

	ctx := context.Background()
	if err := tr.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer tr.Close()

	result, err := tr.Call(ctx, "tools/list", nil)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}

	var parsed struct {
		Tools []json.RawMessage `json:"tools"`
	}
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if len(parsed.Tools) != 0 {
		t.Errorf("expected empty tools, got %d", len(parsed.Tools))
	}
}

func TestTransport_Call_RPCError(t *testing.T) {
	resp := `{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"method not found"}}`
	script := writeMockServer(t, resp)
	tr := NewTransport("bash", []string{script}, nil, testLogger())

	ctx := context.Background()
	if err := tr.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer tr.Close()

	_, err := tr.Call(ctx, "nonexistent", nil)
	if err == nil {
		t.Fatal("expected error for RPC error response")
	}

	var rpcErr *RPCError
	if !errorAs(err, &rpcErr) {
		t.Fatalf("expected *RPCError, got %T: %v", err, err)
	}
	if rpcErr.Code != -32601 {
		t.Errorf("error code = %d, want -32601", rpcErr.Code)
	}
}

func TestTransport_Call_Timeout(t *testing.T) {
	// Script that hangs forever.
	dir := t.TempDir()
	script := filepath.Join(dir, "hang.sh")
	if err := os.WriteFile(script, []byte("#!/bin/bash\nsleep 60\n"), 0o755); err != nil {
		t.Fatalf("write: %v", err)
	}

	tr := NewTransport("bash", []string{script}, nil, testLogger())

	ctx := context.Background()
	if err := tr.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer tr.Close()

	ctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()

	_, err := tr.Call(ctx, "tools/list", nil)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestTransport_Call_EnvPassed(t *testing.T) {
	// Script that echoes an env var as the result.
	dir := t.TempDir()
	script := filepath.Join(dir, "env-echo.sh")
	content := `#!/bin/bash
read -r line
echo "{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"val\":\"$TEST_MCP_VAR\"}}"
`
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatalf("write: %v", err)
	}

	env := map[string]string{"TEST_MCP_VAR": "hello_mcp"}
	tr := NewTransport("bash", []string{script}, env, testLogger())

	ctx := context.Background()
	if err := tr.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer tr.Close()

	result, err := tr.Call(ctx, "test", nil)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}

	var parsed struct {
		Val string `json:"val"`
	}
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed.Val != "hello_mcp" {
		t.Errorf("val = %q, want %q", parsed.Val, "hello_mcp")
	}
}

func TestTransport_Notify(t *testing.T) {
	// Notification doesn't expect a response, so the script just reads and exits.
	dir := t.TempDir()
	script := filepath.Join(dir, "notify.sh")
	content := `#!/bin/bash
read -r line
# Write the received line to a file so we can verify it was sent.
echo "$line" > "` + filepath.Join(dir, "received.json") + `"
`
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatalf("write: %v", err)
	}

	tr := NewTransport("bash", []string{script}, nil, testLogger())

	ctx := context.Background()
	if err := tr.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if err := tr.Notify(ctx, "initialized", nil); err != nil {
		t.Fatalf("Notify: %v", err)
	}

	// Give the script a moment to write the file.
	time.Sleep(50 * time.Millisecond)
	tr.Close()

	data, err := os.ReadFile(filepath.Join(dir, "received.json"))
	if err != nil {
		t.Fatalf("read received: %v", err)
	}

	var notif Notification
	if err := json.Unmarshal(data, &notif); err != nil {
		t.Fatalf("unmarshal notification: %v", err)
	}
	if notif.Method != "initialized" {
		t.Errorf("method = %q, want %q", notif.Method, "initialized")
	}
}

func TestTransport_MultipleCalls(t *testing.T) {
	responses := []string{
		`{"jsonrpc":"2.0","id":1,"result":{"step":"first"}}`,
		`{"jsonrpc":"2.0","id":2,"result":{"step":"second"}}`,
	}
	script := writeMockServerMulti(t, responses)
	tr := NewTransport("bash", []string{script}, nil, testLogger())

	ctx := context.Background()
	if err := tr.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer tr.Close()

	// First call.
	r1, err := tr.Call(ctx, "first", nil)
	if err != nil {
		t.Fatalf("Call 1: %v", err)
	}

	var p1 struct{ Step string }
	json.Unmarshal(r1, &p1)
	if p1.Step != "first" {
		t.Errorf("call 1 step = %q, want %q", p1.Step, "first")
	}

	// Second call.
	r2, err := tr.Call(ctx, "second", nil)
	if err != nil {
		t.Fatalf("Call 2: %v", err)
	}

	var p2 struct{ Step string }
	json.Unmarshal(r2, &p2)
	if p2.Step != "second" {
		t.Errorf("call 2 step = %q, want %q", p2.Step, "second")
	}
}

// errorAs is a local helper since errors.As requires a pointer to interface.
func errorAs[T error](err error, target *T) bool {
	for err != nil {
		if t, ok := err.(T); ok {
			*target = t
			return true
		}
		if u, ok := err.(interface{ Unwrap() error }); ok {
			err = u.Unwrap()
		} else {
			return false
		}
	}
	return false
}
