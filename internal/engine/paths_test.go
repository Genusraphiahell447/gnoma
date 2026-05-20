package engine

import (
	"context"
	"encoding/json"
	"testing"

	"somegit.dev/Owlibou/gnoma/internal/message"
	"somegit.dev/Owlibou/gnoma/internal/provider"
	"somegit.dev/Owlibou/gnoma/internal/stream"
	"somegit.dev/Owlibou/gnoma/internal/tool"
)

func TestIsUnderAllowedPaths(t *testing.T) {
	tests := []struct {
		name    string
		target  string
		allowed []string
		want    bool
	}{
		{"exact match", "/tmp/foo", []string{"/tmp/foo"}, true},
		{"under allowed dir", "/tmp/foo/bar.go", []string{"/tmp"}, true},
		{"not under allowed", "/etc/passwd", []string{"/tmp"}, false},
		{"prevents prefix bypass", "/tmpx/foo", []string{"/tmp"}, false},
		{"matches second path", "/home/user/file", []string{"/tmp", "/home/user"}, true},
		{"empty allowed slice", "/tmp/foo", []string{}, false},
		{"nested dir", "/project/src/main.go", []string{"/project"}, true},
		{"sibling dir denied", "/project-evil/foo", []string{"/project"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isUnderAllowedPaths(tc.target, tc.allowed)
			if got != tc.want {
				t.Errorf("isUnderAllowedPaths(%q, %v) = %v, want %v",
					tc.target, tc.allowed, got, tc.want)
			}
		})
	}
}

// mockPathSensitiveTool is a mock that implements both Tool and PathSensitiveTool.
type mockPathSensitiveTool struct {
	mockTool
	extractedPaths []string
}

func (m *mockPathSensitiveTool) ExtractPaths(_ json.RawMessage) []string {
	return m.extractedPaths
}

func TestSubmitWithOptions_AllowedPaths_DeniesBash(t *testing.T) {
	called := false
	reg := tool.NewRegistry()
	reg.Register(&mockTool{
		name: "bash",
		execFn: func(_ context.Context, _ json.RawMessage) (tool.Result, error) {
			called = true
			return tool.Result{Output: "should not reach here"}, nil
		},
	})

	mp := &mockProvider{
		name: "test",
		streams: []stream.Stream{
			newEventStream(message.StopToolUse, "",
				stream.Event{Type: stream.EventToolCallStart, ToolCallID: "tc1", ToolCallName: "bash"},
				stream.Event{Type: stream.EventToolCallDone, ToolCallID: "tc1", Args: json.RawMessage(`{"command":"cat /etc/passwd"}`)},
			),
			newEventStream(message.StopEndTurn, "",
				stream.Event{Type: stream.EventTextDelta, Text: "blocked"},
			),
		},
	}

	e, _ := New(Config{Provider: secureMock(mp), Tools: reg})

	_, err := e.SubmitWithOptions(context.Background(), "run bash",
		TurnOptions{AllowedPaths: []string{"/tmp"}}, nil)
	if err != nil {
		t.Fatalf("SubmitWithOptions: %v", err)
	}
	if called {
		t.Error("bash tool should not be executed when AllowedPaths is set")
	}
}

func TestSubmitWithOptions_AllowedPaths_DeniesOutsidePath(t *testing.T) {
	called := false
	reg := tool.NewRegistry()
	reg.Register(&mockPathSensitiveTool{
		mockTool: mockTool{
			name: "fs.read",
			execFn: func(_ context.Context, _ json.RawMessage) (tool.Result, error) {
				called = true
				return tool.Result{Output: "secret content"}, nil
			},
		},
		extractedPaths: []string{"/etc/passwd"},
	})

	mp := &mockProvider{
		name: "test",
		streams: []stream.Stream{
			newEventStream(message.StopToolUse, "",
				stream.Event{Type: stream.EventToolCallStart, ToolCallID: "tc1", ToolCallName: "fs.read"},
				stream.Event{Type: stream.EventToolCallDone, ToolCallID: "tc1", Args: json.RawMessage(`{"path":"/etc/passwd"}`)},
			),
			newEventStream(message.StopEndTurn, "",
				stream.Event{Type: stream.EventTextDelta, Text: "blocked"},
			),
		},
	}

	e, _ := New(Config{Provider: secureMock(mp), Tools: reg})

	_, err := e.SubmitWithOptions(context.Background(), "read file",
		TurnOptions{AllowedPaths: []string{"/tmp"}}, nil)
	if err != nil {
		t.Fatalf("SubmitWithOptions: %v", err)
	}
	if called {
		t.Error("fs.read should not be executed when path is outside AllowedPaths")
	}
}

func TestSubmitWithOptions_AllowedPaths_AllowsInsidePath(t *testing.T) {
	called := false
	reg := tool.NewRegistry()
	reg.Register(&mockPathSensitiveTool{
		mockTool: mockTool{
			name: "fs.read",
			execFn: func(_ context.Context, _ json.RawMessage) (tool.Result, error) {
				called = true
				return tool.Result{Output: "ok"}, nil
			},
		},
		extractedPaths: []string{"/tmp/allowed/file.txt"},
	})

	mp := &mockProvider{
		name: "test",
		streams: []stream.Stream{
			newEventStream(message.StopToolUse, "",
				stream.Event{Type: stream.EventToolCallStart, ToolCallID: "tc1", ToolCallName: "fs.read"},
				stream.Event{Type: stream.EventToolCallDone, ToolCallID: "tc1", Args: json.RawMessage(`{"path":"/tmp/allowed/file.txt"}`)},
			),
			newEventStream(message.StopEndTurn, "",
				stream.Event{Type: stream.EventTextDelta, Text: "done"},
			),
		},
	}

	e, _ := New(Config{Provider: secureMock(mp), Tools: reg})

	_, err := e.SubmitWithOptions(context.Background(), "read file",
		TurnOptions{AllowedPaths: []string{"/tmp/allowed"}}, nil)
	if err != nil {
		t.Fatalf("SubmitWithOptions: %v", err)
	}
	if !called {
		t.Error("fs.read should be executed when path is inside AllowedPaths")
	}
}

func TestSubmitWithOptions_NilAllowedPaths_NoRestriction(t *testing.T) {
	called := false
	reg := tool.NewRegistry()
	reg.Register(&mockPathSensitiveTool{
		mockTool: mockTool{
			name: "fs.read",
			execFn: func(_ context.Context, _ json.RawMessage) (tool.Result, error) {
				called = true
				return tool.Result{Output: "ok"}, nil
			},
		},
		extractedPaths: []string{"/etc/passwd"},
	})

	mp := &mockProvider{
		name: "test",
		streams: []stream.Stream{
			newEventStream(message.StopToolUse, "",
				stream.Event{Type: stream.EventToolCallStart, ToolCallID: "tc1", ToolCallName: "fs.read"},
				stream.Event{Type: stream.EventToolCallDone, ToolCallID: "tc1", Args: json.RawMessage(`{"path":"/etc/passwd"}`)},
			),
			newEventStream(message.StopEndTurn, "",
				stream.Event{Type: stream.EventTextDelta, Text: "done"},
			),
		},
	}

	e, _ := New(Config{Provider: secureMock(mp), Tools: reg})

	// No AllowedPaths → no restriction
	_, err := e.SubmitWithOptions(context.Background(), "read file", TurnOptions{}, nil)
	if err != nil {
		t.Fatalf("SubmitWithOptions: %v", err)
	}
	if !called {
		t.Error("fs.read should be executed when AllowedPaths is not set")
	}
}

func TestSubmitWithOptions_AllowedPaths_NonPathSensitiveToolAllowed(t *testing.T) {
	// A tool that doesn't implement PathSensitiveTool should be permitted even
	// when AllowedPaths is set — it doesn't access the filesystem directly.
	called := false
	reg := tool.NewRegistry()
	reg.Register(&mockTool{ // plain mockTool, not PathSensitiveTool
		name: "sysinfo",
		execFn: func(_ context.Context, _ json.RawMessage) (tool.Result, error) {
			called = true
			return tool.Result{Output: "linux"}, nil
		},
	})

	mp := &mockProvider{
		name: "test",
		streams: []stream.Stream{
			newEventStream(message.StopToolUse, "",
				stream.Event{Type: stream.EventToolCallStart, ToolCallID: "tc1", ToolCallName: "sysinfo"},
				stream.Event{Type: stream.EventToolCallDone, ToolCallID: "tc1", Args: json.RawMessage(`{}`)},
			),
			newEventStream(message.StopEndTurn, "",
				stream.Event{Type: stream.EventTextDelta, Text: "done"},
			),
		},
	}

	// Register arm with tool support
	from := provider.Capabilities{ToolUse: true}
	_ = from
	e, _ := New(Config{Provider: secureMock(mp), Tools: reg})

	_, err := e.SubmitWithOptions(context.Background(), "get info",
		TurnOptions{AllowedPaths: []string{"/tmp"}}, nil)
	if err != nil {
		t.Fatalf("SubmitWithOptions: %v", err)
	}
	if !called {
		t.Error("non-path-sensitive tool should be permitted regardless of AllowedPaths")
	}
}
