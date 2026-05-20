package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	gnomactx "somegit.dev/Owlibou/gnoma/internal/context"
	"somegit.dev/Owlibou/gnoma/internal/message"
	"somegit.dev/Owlibou/gnoma/internal/provider"
	"somegit.dev/Owlibou/gnoma/internal/router"
	"somegit.dev/Owlibou/gnoma/internal/security"
	"somegit.dev/Owlibou/gnoma/internal/stream"
	"somegit.dev/Owlibou/gnoma/internal/tool"
)

// --- Mock Provider ---

// mockProvider returns pre-configured streams for each call.
type mockProvider struct {
	name    string
	calls   int
	streams []stream.Stream // one per call, consumed in order
}

func (m *mockProvider) Name() string         { return m.name }
func (m *mockProvider) DefaultModel() string  { return "mock-model" }
func (m *mockProvider) Models(_ context.Context) ([]provider.ModelInfo, error) {
	return []provider.ModelInfo{{
		ID: "mock-model", Name: "mock-model", Provider: m.name,
		Capabilities: provider.Capabilities{ToolUse: true},
	}}, nil
}
func (m *mockProvider) Stream(_ context.Context, _ provider.Request) (stream.Stream, error) {
	if m.calls >= len(m.streams) {
		return nil, fmt.Errorf("mock: no more streams (called %d times)", m.calls+1)
	}
	s := m.streams[m.calls]
	m.calls++
	return s, nil
}

// secureMock wraps a test provider in *security.SafeProvider so it
// satisfies router.SecureProvider's sealed Marker. Tests use this at every
// Config.Provider site; the wrapper is firewall-less (pass-through), so
// behavior is unchanged from before the seal landed.
func secureMock(p provider.Provider) router.SecureProvider {
	return security.WrapProvider(p, nil)
}

// eventStream is a mock stream backed by a slice of events.
type eventStream struct {
	events     []stream.Event
	idx        int
	stopReason message.StopReason
	model      string
}

func newEventStream(stopReason message.StopReason, model string, events ...stream.Event) *eventStream {
	// Append a final event with stop reason
	events = append(events, stream.Event{
		Type:       stream.EventTextDelta,
		StopReason: stopReason,
		Model:      model,
	})
	return &eventStream{events: events, stopReason: stopReason, model: model}
}

func (s *eventStream) Next() bool {
	if s.idx >= len(s.events) {
		return false
	}
	s.idx++
	return true
}

func (s *eventStream) Current() stream.Event {
	return s.events[s.idx-1]
}

func (s *eventStream) Err() error   { return nil }
func (s *eventStream) Close() error { return nil }

// --- Mock Tool ---

type mockTool struct {
	name     string
	readOnly bool
	execFn   func(ctx context.Context, args json.RawMessage) (tool.Result, error)
}

func (m *mockTool) Name() string               { return m.name }
func (m *mockTool) Description() string         { return "mock tool" }
func (m *mockTool) Parameters() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (m *mockTool) IsReadOnly() bool            { return m.readOnly }
func (m *mockTool) IsDestructive() bool         { return false }
func (m *mockTool) Execute(ctx context.Context, args json.RawMessage) (tool.Result, error) {
	if m.execFn != nil {
		return m.execFn(ctx, args)
	}
	return tool.Result{Output: "mock output"}, nil
}

// --- Tests ---

func TestNew_ValidConfig(t *testing.T) {
	e, err := New(Config{
		Provider: secureMock(&mockProvider{name: "test"}),
		Tools:    tool.NewRegistry(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if e == nil {
		t.Fatal("engine should not be nil")
	}
}

func TestNew_MissingProvider(t *testing.T) {
	_, err := New(Config{Tools: tool.NewRegistry()})
	if err == nil {
		t.Fatal("expected error for missing provider")
	}
}

func TestNew_MissingTools(t *testing.T) {
	_, err := New(Config{Provider: secureMock(&mockProvider{name: "test"})})
	if err == nil {
		t.Fatal("expected error for missing tool registry")
	}
}

func TestSubmit_SimpleTextResponse(t *testing.T) {
	mp := &mockProvider{
		name: "test",
		streams: []stream.Stream{
			newEventStream(message.StopEndTurn, "test-model",
				stream.Event{Type: stream.EventTextDelta, Text: "Hello "},
				stream.Event{Type: stream.EventTextDelta, Text: "world!"},
				stream.Event{Type: stream.EventUsage, Usage: &message.Usage{InputTokens: 10, OutputTokens: 5}},
			),
		},
	}

	e, _ := New(Config{Provider: secureMock(mp), Tools: tool.NewRegistry()})

	var events []stream.Event
	turn, err := e.Submit(context.Background(), "hi", func(evt stream.Event) {
		events = append(events, evt)
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}

	// Check turn
	if turn.Rounds != 1 {
		t.Errorf("Rounds = %d, want 1", turn.Rounds)
	}
	if len(turn.Messages) != 1 {
		t.Fatalf("len(Messages) = %d, want 1", len(turn.Messages))
	}
	if turn.Messages[0].TextContent() != "Hello world!" {
		t.Errorf("TextContent = %q", turn.Messages[0].TextContent())
	}
	if turn.Usage.InputTokens != 10 {
		t.Errorf("Usage.InputTokens = %d", turn.Usage.InputTokens)
	}

	// Check history
	history := e.History()
	if len(history) != 2 {
		t.Fatalf("len(History) = %d, want 2 (user + assistant)", len(history))
	}
	if history[0].Role != message.RoleUser {
		t.Errorf("History[0].Role = %q", history[0].Role)
	}
	if history[1].Role != message.RoleAssistant {
		t.Errorf("History[1].Role = %q", history[1].Role)
	}

	// Check events were forwarded
	if len(events) == 0 {
		t.Error("callback should have received events")
	}
}

func TestSubmit_ToolCallLoop(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Register(&mockTool{
		name: "bash",
		execFn: func(_ context.Context, args json.RawMessage) (tool.Result, error) {
			return tool.Result{Output: "file1.go\nfile2.go"}, nil
		},
	})

	mp := &mockProvider{
		name: "test",
		streams: []stream.Stream{
			// Round 1: model calls a tool
			newEventStream(message.StopToolUse, "model-1",
				stream.Event{Type: stream.EventTextDelta, Text: "Let me list files."},
				stream.Event{Type: stream.EventToolCallStart, ToolCallID: "tc_1", ToolCallName: "bash"},
				stream.Event{Type: stream.EventToolCallDone, ToolCallID: "tc_1", Args: json.RawMessage(`{"command":"ls"}`)},
			),
			// Round 2: model responds with final answer
			newEventStream(message.StopEndTurn, "model-1",
				stream.Event{Type: stream.EventTextDelta, Text: "Found file1.go and file2.go."},
			),
		},
	}

	e, _ := New(Config{Provider: secureMock(mp), Tools: reg})

	turn, err := e.Submit(context.Background(), "list files", nil)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}

	if turn.Rounds != 2 {
		t.Errorf("Rounds = %d, want 2", turn.Rounds)
	}

	// Messages: assistant (tool call), tool results, assistant (final)
	if len(turn.Messages) != 3 {
		t.Fatalf("len(Messages) = %d, want 3", len(turn.Messages))
	}

	// First message has tool call
	if !turn.Messages[0].HasToolCalls() {
		t.Error("Messages[0] should have tool calls")
	}

	// Second message is tool results
	if turn.Messages[1].Role != message.RoleUser {
		t.Errorf("Messages[1].Role = %q, want user (tool results)", turn.Messages[1].Role)
	}

	// Third message is final text
	if turn.Messages[2].TextContent() != "Found file1.go and file2.go." {
		t.Errorf("Messages[2].TextContent = %q", turn.Messages[2].TextContent())
	}

	// History: user + assistant(tool call) + tool results + assistant(final)
	if len(e.History()) != 4 {
		t.Errorf("len(History) = %d, want 4", len(e.History()))
	}

	// Provider called twice
	if mp.calls != 2 {
		t.Errorf("provider called %d times, want 2", mp.calls)
	}
}

func TestSubmit_UnknownTool(t *testing.T) {
	reg := tool.NewRegistry()
	// Don't register any tools

	mp := &mockProvider{
		name: "test",
		streams: []stream.Stream{
			// Model calls a tool that doesn't exist
			newEventStream(message.StopToolUse, "",
				stream.Event{Type: stream.EventToolCallStart, ToolCallID: "tc_1", ToolCallName: "nonexistent"},
				stream.Event{Type: stream.EventToolCallDone, ToolCallID: "tc_1", Args: json.RawMessage(`{}`)},
			),
			// Model responds after seeing error
			newEventStream(message.StopEndTurn, "",
				stream.Event{Type: stream.EventTextDelta, Text: "Sorry, that tool doesn't exist."},
			),
		},
	}

	e, _ := New(Config{Provider: secureMock(mp), Tools: reg})

	turn, err := e.Submit(context.Background(), "do something", nil)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}

	// Should still complete — unknown tool returns error result, model sees it
	if turn.Rounds != 2 {
		t.Errorf("Rounds = %d, want 2", turn.Rounds)
	}
}

func TestSubmit_ToolExecutionError(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Register(&mockTool{
		name: "failing",
		execFn: func(_ context.Context, _ json.RawMessage) (tool.Result, error) {
			return tool.Result{}, errors.New("disk full")
		},
	})

	mp := &mockProvider{
		name: "test",
		streams: []stream.Stream{
			newEventStream(message.StopToolUse, "",
				stream.Event{Type: stream.EventToolCallStart, ToolCallID: "tc_1", ToolCallName: "failing"},
				stream.Event{Type: stream.EventToolCallDone, ToolCallID: "tc_1", Args: json.RawMessage(`{}`)},
			),
			newEventStream(message.StopEndTurn, "",
				stream.Event{Type: stream.EventTextDelta, Text: "The tool failed."},
			),
		},
	}

	e, _ := New(Config{Provider: secureMock(mp), Tools: reg})

	turn, err := e.Submit(context.Background(), "do it", nil)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}

	// Tool error is returned as error result, not a fatal error
	if turn.Rounds != 2 {
		t.Errorf("Rounds = %d, want 2", turn.Rounds)
	}
}

func TestSubmit_MaxTurnsLimit(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Register(&mockTool{name: "bash"})

	// Provider always returns tool calls — would loop forever
	mp := &mockProvider{
		name: "test",
		streams: []stream.Stream{
			newEventStream(message.StopToolUse, "",
				stream.Event{Type: stream.EventToolCallStart, ToolCallID: "tc_1", ToolCallName: "bash"},
				stream.Event{Type: stream.EventToolCallDone, ToolCallID: "tc_1", Args: json.RawMessage(`{}`)},
			),
			newEventStream(message.StopToolUse, "",
				stream.Event{Type: stream.EventToolCallStart, ToolCallID: "tc_2", ToolCallName: "bash"},
				stream.Event{Type: stream.EventToolCallDone, ToolCallID: "tc_2", Args: json.RawMessage(`{}`)},
			),
			newEventStream(message.StopToolUse, "",
				stream.Event{Type: stream.EventToolCallStart, ToolCallID: "tc_3", ToolCallName: "bash"},
				stream.Event{Type: stream.EventToolCallDone, ToolCallID: "tc_3", Args: json.RawMessage(`{}`)},
			),
		},
	}

	e, _ := New(Config{Provider: secureMock(mp), Tools: reg, MaxTurns: 2})

	_, err := e.Submit(context.Background(), "loop forever", nil)
	if err == nil {
		t.Fatal("expected error from max turns limit")
	}
	if mp.calls != 2 {
		t.Errorf("provider called %d times, want 2 (limited)", mp.calls)
	}
}

func TestSubmit_MultipleToolCalls(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Register(&mockTool{
		name: "bash",
		execFn: func(_ context.Context, _ json.RawMessage) (tool.Result, error) {
			return tool.Result{Output: "bash output"}, nil
		},
	})
	reg.Register(&mockTool{
		name:     "fs.read",
		readOnly: true,
		execFn: func(_ context.Context, _ json.RawMessage) (tool.Result, error) {
			return tool.Result{Output: "file content"}, nil
		},
	})

	mp := &mockProvider{
		name: "test",
		streams: []stream.Stream{
			// Model calls two tools at once
			newEventStream(message.StopToolUse, "",
				stream.Event{Type: stream.EventToolCallStart, ToolCallID: "tc_1", ToolCallName: "bash"},
				stream.Event{Type: stream.EventToolCallDone, ToolCallID: "tc_1", Args: json.RawMessage(`{"command":"ls"}`)},
				stream.Event{Type: stream.EventToolCallStart, ToolCallID: "tc_2", ToolCallName: "fs.read"},
				stream.Event{Type: stream.EventToolCallDone, ToolCallID: "tc_2", Args: json.RawMessage(`{"path":"go.mod"}`)},
			),
			newEventStream(message.StopEndTurn, "",
				stream.Event{Type: stream.EventTextDelta, Text: "Done."},
			),
		},
	}

	e, _ := New(Config{Provider: secureMock(mp), Tools: reg})

	turn, err := e.Submit(context.Background(), "run both", nil)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}

	if turn.Rounds != 2 {
		t.Errorf("Rounds = %d, want 2", turn.Rounds)
	}

	// Tool results message should have 2 results
	toolMsg := turn.Messages[1] // assistant, tool_results, assistant
	if len(toolMsg.Content) != 2 {
		t.Errorf("tool results has %d content blocks, want 2", len(toolMsg.Content))
	}
}

func TestSubmit_NilCallback(t *testing.T) {
	mp := &mockProvider{
		name: "test",
		streams: []stream.Stream{
			newEventStream(message.StopEndTurn, "",
				stream.Event{Type: stream.EventTextDelta, Text: "ok"},
			),
		},
	}

	e, _ := New(Config{Provider: secureMock(mp), Tools: tool.NewRegistry()})

	// nil callback should not panic
	turn, err := e.Submit(context.Background(), "test", nil)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if turn.Rounds != 1 {
		t.Errorf("Rounds = %d", turn.Rounds)
	}
}

func TestEngine_Reset(t *testing.T) {
	mp := &mockProvider{
		name: "test",
		streams: []stream.Stream{
			newEventStream(message.StopEndTurn, "",
				stream.Event{Type: stream.EventTextDelta, Text: "first"},
				stream.Event{Type: stream.EventUsage, Usage: &message.Usage{InputTokens: 100}},
			),
		},
	}

	e, _ := New(Config{Provider: secureMock(mp), Tools: tool.NewRegistry()})
	_, _ = e.Submit(context.Background(), "hello", nil)

	if len(e.History()) == 0 {
		t.Fatal("history should not be empty before reset")
	}
	if e.Usage().InputTokens == 0 {
		t.Fatal("usage should not be zero before reset")
	}

	e.Reset()

	if len(e.History()) != 0 {
		t.Errorf("history should be empty after reset, got %d", len(e.History()))
	}
	if e.Usage().InputTokens != 0 {
		t.Errorf("usage should be zero after reset, got %d", e.Usage().InputTokens)
	}
}

func TestEngine_Reset_ClearsContextWindow(t *testing.T) {
	ctxWindow := gnomactx.NewWindow(gnomactx.WindowConfig{MaxTokens: 200_000})
	mp := &mockProvider{
		name: "test",
		streams: []stream.Stream{
			newEventStream(message.StopEndTurn, "",
				stream.Event{Type: stream.EventTextDelta, Text: "hi"},
			),
		},
	}
	e, _ := New(Config{
		Provider: secureMock(mp),
		Tools:    tool.NewRegistry(),
		Context:  ctxWindow,
	})
	_, _ = e.Submit(context.Background(), "hello", nil)

	if len(ctxWindow.Messages()) == 0 {
		t.Fatal("context window should have messages before reset")
	}

	e.Reset()

	if len(ctxWindow.Messages()) != 0 {
		t.Errorf("context window should be empty after reset, got %d messages", len(ctxWindow.Messages()))
	}
}

func TestSubmit_ContextWindowTracksUserAndToolMessages(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Register(&mockTool{
		name: "bash",
		execFn: func(_ context.Context, _ json.RawMessage) (tool.Result, error) {
			return tool.Result{Output: "output"}, nil
		},
	})

	mp := &mockProvider{
		name: "test",
		streams: []stream.Stream{
			newEventStream(message.StopToolUse, "model",
				stream.Event{Type: stream.EventToolCallStart, ToolCallID: "tc1", ToolCallName: "bash"},
				stream.Event{Type: stream.EventToolCallDone, ToolCallID: "tc1", Args: json.RawMessage(`{"command":"ls"}`)},
				stream.Event{Type: stream.EventUsage, Usage: &message.Usage{InputTokens: 100, OutputTokens: 20}},
			),
			newEventStream(message.StopEndTurn, "model",
				stream.Event{Type: stream.EventTextDelta, Text: "Done."},
			),
		},
	}

	ctxWindow := gnomactx.NewWindow(gnomactx.WindowConfig{MaxTokens: 200_000})
	e, _ := New(Config{
		Provider: secureMock(mp),
		Tools:    reg,
		Context:  ctxWindow,
	})

	_, err := e.Submit(context.Background(), "list files", nil)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}

	allMsgs := ctxWindow.AllMessages()
	// Expect: user msg, assistant (tool call), tool results, assistant (final)
	if len(allMsgs) < 4 {
		t.Errorf("context window has %d messages, want at least 4 (user+assistant+tool_results+assistant)", len(allMsgs))
		for i, m := range allMsgs {
			t.Logf("  [%d] role=%s content=%s", i, m.Role, m.TextContent())
		}
	}
	// First message should be user
	if len(allMsgs) > 0 && allMsgs[0].Role != message.RoleUser {
		t.Errorf("allMsgs[0].Role = %q, want user", allMsgs[0].Role)
	}
}

func TestSubmit_TrackerReflectsInputTokens(t *testing.T) {
	// Verify the tracker is set from InputTokens (not accumulated).
	// After 3 rounds, tracker should equal last round's InputTokens+OutputTokens,
	// not the sum of all rounds.
	ctxWindow := gnomactx.NewWindow(gnomactx.WindowConfig{MaxTokens: 200_000})

	mp := &mockProvider{
		name: "test",
		streams: []stream.Stream{
			newEventStream(message.StopEndTurn, "",
				stream.Event{Type: stream.EventUsage, Usage: &message.Usage{InputTokens: 100, OutputTokens: 50}},
				stream.Event{Type: stream.EventTextDelta, Text: "a"},
			),
		},
	}
	e, _ := New(Config{Provider: secureMock(mp), Tools: tool.NewRegistry(), Context: ctxWindow})

	_, _ = e.Submit(context.Background(), "hi", nil)

	// Tracker should be InputTokens + OutputTokens = 150, not more
	used := ctxWindow.Tracker().Used()
	if used != 150 {
		t.Errorf("tracker = %d, want 150 (InputTokens+OutputTokens, not cumulative)", used)
	}
}

func TestSubmit_CumulativeUsage(t *testing.T) {
	mp := &mockProvider{
		name: "test",
		streams: []stream.Stream{
			newEventStream(message.StopEndTurn, "",
				stream.Event{Type: stream.EventUsage, Usage: &message.Usage{InputTokens: 100, OutputTokens: 50}},
				stream.Event{Type: stream.EventTextDelta, Text: "first"},
			),
			newEventStream(message.StopEndTurn, "",
				stream.Event{Type: stream.EventUsage, Usage: &message.Usage{InputTokens: 200, OutputTokens: 80}},
				stream.Event{Type: stream.EventTextDelta, Text: "second"},
			),
		},
	}

	e, _ := New(Config{Provider: secureMock(mp), Tools: tool.NewRegistry()})

	_, _ = e.Submit(context.Background(), "one", nil)
	_, _ = e.Submit(context.Background(), "two", nil)

	if e.Usage().InputTokens != 300 {
		t.Errorf("cumulative InputTokens = %d, want 300", e.Usage().InputTokens)
	}
	if e.Usage().OutputTokens != 130 {
		t.Errorf("cumulative OutputTokens = %d, want 130", e.Usage().OutputTokens)
	}
}

// spyClassifier records calls and delegates to HeuristicClassifier.
type spyClassifier struct {
	calls  int
	result *router.Task // when non-nil, return this instead of heuristic result
}

func (s *spyClassifier) Classify(ctx context.Context, prompt string, history []message.Message) (router.Task, error) {
	s.calls++
	if s.result != nil {
		return *s.result, nil
	}
	return router.HeuristicClassifier{}.Classify(ctx, prompt, history)
}

func TestSubmit_UsesInjectedClassifier(t *testing.T) {
	rtr := router.New(router.Config{})
	armID := router.NewArmID("test", "mock-model")
	mp := &mockProvider{
		name: "test",
		streams: []stream.Stream{
			newEventStream(message.StopEndTurn, "mock-model",
				stream.Event{Type: stream.EventTextDelta, Text: "ok"},
			),
		},
	}
	rtr.RegisterArm(&router.Arm{
		ID:           armID,
		Provider:     secureMock(mp),
		ModelName:    "mock-model",
		Capabilities: provider.Capabilities{ToolUse: true},
	})
	rtr.ForceArm(armID)

	spy := &spyClassifier{}
	e, err := New(Config{
		Provider:   secureMock(mp),
		Router:     rtr,
		Tools:      tool.NewRegistry(),
		Classifier: spy,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := e.Submit(context.Background(), "implement a parser", nil); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	if spy.calls == 0 {
		t.Error("expected Classify to be called at least once, got 0 calls")
	}
}

func TestSubmit_NilClassifierFallsBackToHeuristic(t *testing.T) {
	rtr := router.New(router.Config{})
	armID := router.NewArmID("test", "mock-model")
	mp := &mockProvider{
		name: "test",
		streams: []stream.Stream{
			newEventStream(message.StopEndTurn, "mock-model",
				stream.Event{Type: stream.EventTextDelta, Text: "ok"},
			),
		},
	}
	rtr.RegisterArm(&router.Arm{
		ID:           armID,
		Provider:     secureMock(mp),
		ModelName:    "mock-model",
		Capabilities: provider.Capabilities{ToolUse: true},
	})
	rtr.ForceArm(armID)

	// No Classifier set — should not panic, should use heuristic
	e, err := New(Config{
		Provider: secureMock(mp),
		Router:   rtr,
		Tools:    tool.NewRegistry(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = e.Submit(context.Background(), "debug the server crash", nil)
	if err != nil {
		t.Fatalf("Submit with nil Classifier: %v", err)
	}
}

func TestSubmit_ReportsOutcomeToRouter(t *testing.T) {
	rtr := router.New(router.Config{})
	armID := router.NewArmID("test", "mock-model")

	makeStream := func() stream.Stream {
		return newEventStream(message.StopEndTurn, "mock-model",
			stream.Event{Type: stream.EventTextDelta, Text: "hi"},
			stream.Event{Type: stream.EventUsage, Usage: &message.Usage{InputTokens: 10, OutputTokens: 5}},
		)
	}
	mp := &mockProvider{
		name:    "test",
		streams: []stream.Stream{makeStream(), makeStream(), makeStream()},
	}
	rtr.RegisterArm(&router.Arm{
		ID:           armID,
		Provider:     secureMock(mp),
		ModelName:    "mock-model",
		Capabilities: provider.Capabilities{ToolUse: true},
	})
	rtr.ForceArm(armID)

	e, err := New(Config{
		Provider: secureMock(mp),
		Router:   rtr,
		Tools:    tool.NewRegistry(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, err := e.Submit(ctx, "hello", nil); err != nil {
			t.Fatalf("Submit %d: %v", i, err)
		}
	}

	taskType := router.ClassifyTask("hello").Type
	score, hasData := rtr.QualityTracker().Quality(armID, taskType)
	if !hasData {
		t.Fatal("expected quality data after 3 successful turns — ReportOutcome may not be wired")
	}
	if score < 0.9 {
		t.Errorf("quality score = %f, want ≥0.9 for all successful turns", score)
	}
}

func TestSubmit_SuppressesOutcomeWhenIncognito(t *testing.T) {
	// Incognito sessions must not leave bandit signal behind.
	rtr := router.New(router.Config{})
	armID := router.NewArmID("test", "mock-model")

	makeStream := func() stream.Stream {
		return newEventStream(message.StopEndTurn, "mock-model",
			stream.Event{Type: stream.EventTextDelta, Text: "hi"},
			stream.Event{Type: stream.EventUsage, Usage: &message.Usage{InputTokens: 10, OutputTokens: 5}},
		)
	}
	mp := &mockProvider{name: "test", streams: []stream.Stream{makeStream(), makeStream()}}
	rtr.RegisterArm(&router.Arm{
		ID:           armID,
		Provider:     secureMock(mp),
		ModelName:    "mock-model",
		Capabilities: provider.Capabilities{ToolUse: true},
	})
	rtr.ForceArm(armID)

	fw := security.NewFirewall(security.FirewallConfig{ScanOutgoing: true})
	fw.Incognito().Activate()

	e, err := New(Config{
		Provider: secureMock(mp),
		Router:   rtr,
		Tools:    tool.NewRegistry(),
		Firewall: fw,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	for i := 0; i < 2; i++ {
		if _, err := e.Submit(context.Background(), "hello", nil); err != nil {
			t.Fatalf("Submit %d: %v", i, err)
		}
	}

	taskType := router.ClassifyTask("hello").Type
	if _, hasData := rtr.QualityTracker().Quality(armID, taskType); hasData {
		t.Error("quality tracker received outcomes despite incognito — gating not effective")
	}
}
