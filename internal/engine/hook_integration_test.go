package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"somegit.dev/Owlibou/gnoma/internal/hook"
	"somegit.dev/Owlibou/gnoma/internal/message"
	"somegit.dev/Owlibou/gnoma/internal/stream"
	"somegit.dev/Owlibou/gnoma/internal/tool"
)

// --- test executors ---

type blockingExecutor struct{}

func (b *blockingExecutor) Execute(_ context.Context, _ []byte) (hook.HookResult, error) {
	return hook.HookResult{Action: hook.Deny}, nil
}

type allowingExecutor struct{}

func (a *allowingExecutor) Execute(_ context.Context, _ []byte) (hook.HookResult, error) {
	return hook.HookResult{Action: hook.Allow}, nil
}

// argTransformExecutor replaces the "args" field in the payload.
type argTransformExecutor struct{ newArgs json.RawMessage }

func (t *argTransformExecutor) Execute(_ context.Context, payload []byte) (hook.HookResult, error) {
	out, _ := json.Marshal(map[string]any{
		"tool": hook.ExtractToolName(payload),
		"args": t.newArgs,
	})
	return hook.HookResult{Action: hook.Allow, Output: out}, nil
}

// resultTransformExecutor replaces the tool output.
type resultTransformExecutor struct{ newOutput string }

func (r *resultTransformExecutor) Execute(_ context.Context, _ []byte) (hook.HookResult, error) {
	out, _ := json.Marshal(map[string]any{"output": r.newOutput})
	return hook.HookResult{Action: hook.Allow, Output: out}, nil
}

// recordingExecutor records whether it was called and the payload.
type recordingExecutor struct {
	called  bool
	payload []byte
}

func (r *recordingExecutor) Execute(_ context.Context, payload []byte) (hook.HookResult, error) {
	r.called = true
	r.payload = append([]byte(nil), payload...)
	return hook.HookResult{Action: hook.Allow}, nil
}

// --- helpers ---

func hookDispatcher(event hook.EventType, ex hook.Executor) *hook.Dispatcher {
	def := hook.HookDef{Name: "test", Event: event, Command: hook.CommandTypeShell, Exec: "x"}
	d := &hook.Dispatcher{}
	d.SetChain(event, []hook.Handler{hook.NewHandler(def, ex)})
	return d
}

// toolCallStream builds a stream that emits a single tool call then stops.
func toolCallStream(callID, toolName, args string, stopReason message.StopReason, model string) stream.Stream {
	events := []stream.Event{
		{Type: stream.EventToolCallDone, ToolCallID: callID, ToolCallName: toolName, Args: json.RawMessage(args)},
		{Type: stream.EventTextDelta, StopReason: stopReason, Model: model},
	}
	return &eventStream{events: events}
}

// --- tests ---

func TestHook_NilDispatcher_NoChange(t *testing.T) {
	mp := &mockProvider{
		streams: []stream.Stream{
			newEventStream(message.StopEndTurn, "m",
				stream.Event{Type: stream.EventTextDelta, Text: "hello"},
			),
		},
	}
	eng, err := New(Config{Provider: secureMock(mp), Tools: tool.NewRegistry()})
	if err != nil {
		t.Fatal(err)
	}
	turn, err := eng.Submit(context.Background(), "hi", nil)
	if err != nil {
		t.Fatal(err)
	}
	if turn.Rounds != 1 {
		t.Errorf("rounds = %d, want 1", turn.Rounds)
	}
}

func TestHook_PreToolUse_Deny(t *testing.T) {
	executed := false
	reg := tool.NewRegistry()
	reg.Register(&mockTool{
		name: "bash",
		execFn: func(_ context.Context, _ json.RawMessage) (tool.Result, error) {
			executed = true
			return tool.Result{Output: "should not run"}, nil
		},
	})

	mp := &mockProvider{
		streams: []stream.Stream{
			toolCallStream("c1", "bash", `{"command":"rm -rf /"}`, message.StopToolUse, "m"),
			newEventStream(message.StopEndTurn, "m",
				stream.Event{Type: stream.EventTextDelta, Text: "ok"},
			),
		},
	}

	eng, _ := New(Config{
		Provider: secureMock(mp),
		Tools:    reg,
		Hooks:    hookDispatcher(hook.PreToolUse, &blockingExecutor{}),
	})
	_, _ = eng.Submit(context.Background(), "run", nil)

	if executed {
		t.Error("tool was executed despite PreToolUse deny")
	}
}

func TestHook_PreToolUse_Allow(t *testing.T) {
	executed := false
	reg := tool.NewRegistry()
	reg.Register(&mockTool{
		name: "bash",
		execFn: func(_ context.Context, _ json.RawMessage) (tool.Result, error) {
			executed = true
			return tool.Result{Output: "ran"}, nil
		},
	})

	mp := &mockProvider{
		streams: []stream.Stream{
			toolCallStream("c1", "bash", `{}`, message.StopToolUse, "m"),
			newEventStream(message.StopEndTurn, "m",
				stream.Event{Type: stream.EventTextDelta, Text: "ok"},
			),
		},
	}

	eng, _ := New(Config{
		Provider: secureMock(mp),
		Tools:    reg,
		Hooks:    hookDispatcher(hook.PreToolUse, &allowingExecutor{}),
	})
	_, _ = eng.Submit(context.Background(), "run", nil)

	if !executed {
		t.Error("tool was not executed despite PreToolUse allow")
	}
}

func TestHook_PreToolUse_DenyMessage(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Register(&mockTool{
		name: "bash",
		execFn: func(_ context.Context, _ json.RawMessage) (tool.Result, error) {
			return tool.Result{Output: "should not run"}, nil
		},
	})

	mp := &mockProvider{
		streams: []stream.Stream{
			toolCallStream("c1", "bash", `{}`, message.StopToolUse, "m"),
			newEventStream(message.StopEndTurn, "m",
				stream.Event{Type: stream.EventTextDelta, Text: "ok"},
			),
		},
	}

	eng, _ := New(Config{
		Provider: secureMock(mp),
		Tools:    reg,
		Hooks:    hookDispatcher(hook.PreToolUse, &blockingExecutor{}),
	})
	_, _ = eng.Submit(context.Background(), "run", nil)

	for _, msg := range eng.History() {
		for _, c := range msg.Content {
			if c.Type == message.ContentToolResult && c.ToolResult != nil {
				if !strings.HasPrefix(c.ToolResult.Content, "denied by hook") {
					t.Errorf("denied result = %q, want prefix 'denied by hook'", c.ToolResult.Content)
				}
				return
			}
		}
	}
	t.Error("no tool result found in history")
}

func TestHook_PreToolUse_Transform(t *testing.T) {
	var receivedArgs json.RawMessage
	reg := tool.NewRegistry()
	reg.Register(&mockTool{
		name: "bash",
		execFn: func(_ context.Context, args json.RawMessage) (tool.Result, error) {
			receivedArgs = args
			return tool.Result{Output: "ok"}, nil
		},
	})

	mp := &mockProvider{
		streams: []stream.Stream{
			toolCallStream("c1", "bash", `{"command":"original"}`, message.StopToolUse, "m"),
			newEventStream(message.StopEndTurn, "m",
				stream.Event{Type: stream.EventTextDelta, Text: "done"},
			),
		},
	}

	eng, _ := New(Config{
		Provider: secureMock(mp),
		Tools:    reg,
		Hooks: hookDispatcher(hook.PreToolUse,
			&argTransformExecutor{newArgs: json.RawMessage(`{"command":"safe-replacement"}`)}),
	})
	_, _ = eng.Submit(context.Background(), "run", nil)

	var got map[string]string
	_ = json.Unmarshal(receivedArgs, &got)
	if got["command"] != "safe-replacement" {
		t.Errorf("tool args = %s, want safe-replacement", receivedArgs)
	}
}

func TestHook_PostToolUse_Transform(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Register(&mockTool{
		name: "bash",
		execFn: func(_ context.Context, _ json.RawMessage) (tool.Result, error) {
			return tool.Result{Output: "original output"}, nil
		},
	})

	mp := &mockProvider{
		streams: []stream.Stream{
			toolCallStream("c1", "bash", `{}`, message.StopToolUse, "m"),
			newEventStream(message.StopEndTurn, "m",
				stream.Event{Type: stream.EventTextDelta, Text: "done"},
			),
		},
	}

	eng, _ := New(Config{
		Provider: secureMock(mp),
		Tools:    reg,
		Hooks: hookDispatcher(hook.PostToolUse,
			&resultTransformExecutor{newOutput: "transformed output"}),
	})
	_, _ = eng.Submit(context.Background(), "run", nil)

	for _, msg := range eng.History() {
		for _, c := range msg.Content {
			if c.Type == message.ContentToolResult && c.ToolResult != nil {
				if c.ToolResult.Content != "transformed output" {
					t.Errorf("tool result = %q, want 'transformed output'", c.ToolResult.Content)
				}
				return
			}
		}
	}
	t.Error("no tool result found in history")
}

func TestHook_PostToolUse_DenyTreatedAsSkip(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Register(&mockTool{
		name: "bash",
		execFn: func(_ context.Context, _ json.RawMessage) (tool.Result, error) {
			return tool.Result{Output: "tool ran"}, nil
		},
	})

	mp := &mockProvider{
		streams: []stream.Stream{
			toolCallStream("c1", "bash", `{}`, message.StopToolUse, "m"),
			newEventStream(message.StopEndTurn, "m",
				stream.Event{Type: stream.EventTextDelta, Text: "done"},
			),
		},
	}

	eng, _ := New(Config{
		Provider: secureMock(mp),
		Tools:    reg,
		Hooks:    hookDispatcher(hook.PostToolUse, &blockingExecutor{}),
	})
	turn, err := eng.Submit(context.Background(), "run", nil)
	if err != nil {
		t.Fatal(err)
	}
	// 2 rounds = tool call + end turn, confirming the result reached the LLM.
	if turn.Rounds != 2 {
		t.Errorf("rounds = %d, want 2 (result reached LLM despite PostToolUse deny)", turn.Rounds)
	}
}

func TestHook_Stop_MaxTurns(t *testing.T) {
	// Stop hook fires when MaxTurns is exceeded.
	stopRecorder := &recordingExecutor{}
	reg := tool.NewRegistry()
	reg.Register(&mockTool{
		name: "bash",
		execFn: func(_ context.Context, _ json.RawMessage) (tool.Result, error) {
			return tool.Result{Output: "ok"}, nil
		},
	})

	mp := &mockProvider{
		streams: []stream.Stream{
			// Round 1: tool call → will loop to round 2
			toolCallStream("c1", "bash", `{}`, message.StopToolUse, "m"),
			// Round 2: MaxTurns=1 check triggers before this, so it's never consumed
		},
	}

	d := &hook.Dispatcher{}
	d.SetChain(hook.Stop, []hook.Handler{
		hook.NewHandler(
			hook.HookDef{Name: "stop-rec", Event: hook.Stop, Command: hook.CommandTypeShell, Exec: "x"},
			stopRecorder,
		),
	})

	eng, _ := New(Config{Provider: secureMock(mp), Tools: reg, Hooks: d, MaxTurns: 1})
	_, err := eng.Submit(context.Background(), "run", nil)
	// MaxTurns exceeded returns an error
	if err == nil {
		t.Fatal("expected error for MaxTurns exceeded")
	}
	if !stopRecorder.called {
		t.Error("Stop hook was not fired on MaxTurns exceeded")
	}
}
