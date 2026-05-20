package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sync"
	"testing"

	"somegit.dev/Owlibou/gnoma/internal/message"
	"somegit.dev/Owlibou/gnoma/internal/provider"
	"somegit.dev/Owlibou/gnoma/internal/stream"
	"somegit.dev/Owlibou/gnoma/internal/tool"
)

// recordingProvider captures every Request it receives so tests can assert
// on the tool catalogue per round.
type recordingProvider struct {
	mu       sync.Mutex
	requests []provider.Request
	streams  []stream.Stream
	calls    int
}

func (m *recordingProvider) Name() string         { return "recording" }
func (m *recordingProvider) DefaultModel() string { return "mock-model" }
func (m *recordingProvider) Models(_ context.Context) ([]provider.ModelInfo, error) {
	return []provider.ModelInfo{{
		ID: "mock-model", Name: "mock-model", Provider: "recording",
		Capabilities: provider.Capabilities{ToolUse: true, ContextWindow: 8192},
	}}, nil
}
func (m *recordingProvider) Stream(_ context.Context, req provider.Request) (stream.Stream, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Clone request so subsequent rounds don't mutate captured state.
	clone := req
	clone.Tools = slices.Clone(req.Tools)
	m.requests = append(m.requests, clone)
	if m.calls >= len(m.streams) {
		return nil, fmt.Errorf("recording: no more streams (called %d times)", m.calls+1)
	}
	s := m.streams[m.calls]
	m.calls++
	return s, nil
}

// singleToolCallStream returns a stream that emits a single tool call.
func singleToolCallStream(callID, name, args string) stream.Stream {
	return newEventStream(message.StopToolUse, "mock-model",
		stream.Event{Type: stream.EventToolCallStart, ToolCallID: callID, ToolCallName: name},
		stream.Event{Type: stream.EventToolCallDone, ToolCallID: callID, ToolCallName: name, Args: json.RawMessage(args)},
	)
}

// endTurnTextStream returns a stream that emits text and ends the turn.
func endTurnTextStream(text string) stream.Stream {
	return newEventStream(message.StopEndTurn, "mock-model",
		stream.Event{Type: stream.EventTextDelta, Text: text},
	)
}

func TestTwoStage_FullRoundTrip(t *testing.T) {
	// Tool registry: mixed categories so we can verify round 2 filtering.
	writeCalled := false
	reg := tool.NewRegistry()
	reg.Register(&categorizedMockTool{mockTool: mockTool{name: "fs.read", readOnly: true}, cat: tool.CategoryRead})
	reg.Register(&categorizedMockTool{
		mockTool: mockTool{
			name: "fs.write",
			execFn: func(_ context.Context, _ json.RawMessage) (tool.Result, error) {
				writeCalled = true
				return tool.Result{Output: "wrote file"}, nil
			},
		},
		cat: tool.CategoryWrite,
	})
	reg.Register(&categorizedMockTool{mockTool: mockTool{name: "bash"}, cat: tool.CategoryExec})

	// Three rounds: select_category → fs.write → end turn.
	mp := &recordingProvider{
		streams: []stream.Stream{
			singleToolCallStream("c1", SyntheticSelectCategoryName, `{"category":"write"}`),
			singleToolCallStream("c2", "fs.write", `{"path":"/tmp/x","content":"hi"}`),
			endTurnTextStream("done."),
		},
	}

	e, err := New(Config{
		Provider:           secureMock(mp),
		Tools:              reg,
		ForceTwoStageTools: true, // no router needed; just force the path
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	turn, err := e.Submit(context.Background(), "write a file", nil)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}

	if turn.Rounds != 3 {
		t.Errorf("Rounds = %d, want 3", turn.Rounds)
	}
	if !writeCalled {
		t.Error("fs.write tool was not executed")
	}

	if len(mp.requests) != 3 {
		t.Fatalf("captured %d requests, want 3", len(mp.requests))
	}

	// Round 1: only synthetic select_category, ToolChoice = Required.
	r1 := mp.requests[0]
	if len(r1.Tools) != 1 {
		t.Errorf("round 1 tool count = %d, want 1; tools=%v", len(r1.Tools), toolNamesIn(r1.Tools))
	}
	if len(r1.Tools) >= 1 && r1.Tools[0].Name != SyntheticSelectCategoryName {
		t.Errorf("round 1 tool[0] = %q, want %q", r1.Tools[0].Name, SyntheticSelectCategoryName)
	}
	if r1.ToolChoice != provider.ToolChoiceRequired {
		t.Errorf("round 1 ToolChoice = %q, want %q", r1.ToolChoice, provider.ToolChoiceRequired)
	}

	// Round 2: write tools + select_category, no read/exec.
	r2names := toolNamesIn(mp.requests[1].Tools)
	if !slices.Contains(r2names, "fs.write") {
		t.Errorf("round 2 missing fs.write: %v", r2names)
	}
	if !slices.Contains(r2names, SyntheticSelectCategoryName) {
		t.Errorf("round 2 missing select_category (re-selection should remain available): %v", r2names)
	}
	if slices.Contains(r2names, "fs.read") || slices.Contains(r2names, "bash") {
		t.Errorf("round 2 leaked non-write tools: %v", r2names)
	}

	// Round 3: same filter still applied (selection persists for the turn).
	r3names := toolNamesIn(mp.requests[2].Tools)
	if slices.Contains(r3names, "fs.read") || slices.Contains(r3names, "bash") {
		t.Errorf("round 3 leaked non-write tools: %v", r3names)
	}
}

func TestTwoStage_InvalidCategoryFallsBackToRoundOne(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Register(&categorizedMockTool{mockTool: mockTool{name: "fs.write"}, cat: tool.CategoryWrite})

	mp := &recordingProvider{
		streams: []stream.Stream{
			// Round 1: model picks an invalid category.
			singleToolCallStream("c1", SyntheticSelectCategoryName, `{"category":"bogus"}`),
			// Round 2: should see select_category-only again (round 1 mode).
			singleToolCallStream("c2", SyntheticSelectCategoryName, `{"category":"write"}`),
			// Round 3: filtered to write.
			endTurnTextStream("done."),
		},
	}

	e, err := New(Config{
		Provider:           secureMock(mp),
		Tools:              reg,
		ForceTwoStageTools: true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := e.Submit(context.Background(), "do stuff", nil); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	if len(mp.requests) < 2 {
		t.Fatalf("expected at least 2 requests, got %d", len(mp.requests))
	}

	// After invalid category, round 2 should be back to round-1 mode (only synthetic).
	r2 := mp.requests[1]
	if len(r2.Tools) != 1 || r2.Tools[0].Name != SyntheticSelectCategoryName {
		t.Errorf("after invalid category, expected round-1 mode again; got tools=%v", toolNamesIn(r2.Tools))
	}
	if r2.ToolChoice != provider.ToolChoiceRequired {
		t.Errorf("after invalid category, expected ToolChoiceRequired; got %q", r2.ToolChoice)
	}
}
