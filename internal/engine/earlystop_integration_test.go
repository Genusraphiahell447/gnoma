package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"somegit.dev/Owlibou/gnoma/internal/message"
	"somegit.dev/Owlibou/gnoma/internal/stream"
	"somegit.dev/Owlibou/gnoma/internal/tool"
)

// failingEditStream emits a single fs.edit tool call against the given path.
func failingEditStream(callID, path string) stream.Stream {
	args := []byte(`{"path":"` + path + `","old_string":"foo","new_string":"bar"}`)
	return newEventStream(message.StopToolUse, "test-model",
		stream.Event{Type: stream.EventTextDelta, Text: "Patching the file."},
		stream.Event{Type: stream.EventToolCallStart, ToolCallID: callID, ToolCallName: "fs.edit"},
		stream.Event{Type: stream.EventToolCallDone, ToolCallID: callID, ToolCallName: "fs.edit", Args: json.RawMessage(args)},
	)
}

func TestEarlyStop_PatchSpiral_InjectsCorrection(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Register(&mockTool{
		name: "fs.edit",
		execFn: func(_ context.Context, _ json.RawMessage) (tool.Result, error) {
			return tool.Result{}, errors.New("old_string not found")
		},
	})

	// Four failed edits on the same path, then a final acknowledgement.
	mp := &mockProvider{
		name: "test",
		streams: []stream.Stream{
			failingEditStream("tc_1", "/work/main.go"),
			failingEditStream("tc_2", "/work/main.go"),
			failingEditStream("tc_3", "/work/main.go"),
			failingEditStream("tc_4", "/work/main.go"),
			newEventStream(message.StopEndTurn, "test-model",
				stream.Event{Type: stream.EventTextDelta, Text: "Understood, switching to a full rewrite."},
			),
		},
	}

	e, _ := New(Config{Provider: mp, Tools: reg})
	_, err := e.Submit(context.Background(), "fix the bug", nil)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}

	// Walk history for the corrective injection.
	var foundSpiral bool
	for _, m := range e.History() {
		if m.Role != message.RoleUser {
			continue
		}
		if strings.Contains(m.TextContent(), "/work/main.go") &&
			strings.Contains(strings.ToLower(m.TextContent()), "fs.write") {
			foundSpiral = true
			break
		}
	}
	if !foundSpiral {
		t.Fatal("expected patch-spiral corrective message in history, not found")
	}

	if mp.calls != 5 {
		t.Errorf("provider calls = %d, want 5 (4 failing edits + 1 ack)", mp.calls)
	}
}

func TestEarlyStop_PatchSpiral_PerPathIsolation(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Register(&mockTool{
		name: "fs.edit",
		execFn: func(_ context.Context, _ json.RawMessage) (tool.Result, error) {
			return tool.Result{}, errors.New("old_string not found")
		},
	})

	// Failures alternate between two paths; neither reaches the threshold.
	mp := &mockProvider{
		name: "test",
		streams: []stream.Stream{
			failingEditStream("tc_1", "/work/a.go"),
			failingEditStream("tc_2", "/work/b.go"),
			failingEditStream("tc_3", "/work/a.go"),
			failingEditStream("tc_4", "/work/b.go"),
			newEventStream(message.StopEndTurn, "test-model",
				stream.Event{Type: stream.EventTextDelta, Text: "Giving up."},
			),
		},
	}

	e, _ := New(Config{Provider: mp, Tools: reg})
	_, err := e.Submit(context.Background(), "edit two files", nil)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}

	for _, m := range e.History() {
		if m.Role == message.RoleUser && strings.Contains(strings.ToLower(m.TextContent()), "fs.write") {
			t.Fatal("patch-spiral injection fired despite per-path failures below threshold")
		}
	}
}

func TestEarlyStop_GreetingRegression_InjectsCorrection(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Register(&mockTool{
		name:     "fs.read",
		readOnly: true,
		execFn: func(_ context.Context, _ json.RawMessage) (tool.Result, error) {
			return tool.Result{Output: "package main"}, nil
		},
	})

	mp := &mockProvider{
		name: "test",
		streams: []stream.Stream{
			// Round 1: legitimate tool call
			newEventStream(message.StopToolUse, "test-model",
				stream.Event{Type: stream.EventTextDelta, Text: "Reading file."},
				stream.Event{Type: stream.EventToolCallStart, ToolCallID: "tc_1", ToolCallName: "fs.read"},
				stream.Event{Type: stream.EventToolCallDone, ToolCallID: "tc_1", ToolCallName: "fs.read", Args: json.RawMessage(`{"path":"/x.go"}`)},
			),
			// Round 2: model loses context, emits a greeting
			newEventStream(message.StopEndTurn, "test-model",
				stream.Event{Type: stream.EventTextDelta, Text: "Hello! How can I help you today?"},
			),
			// Round 3: model resumes after correction
			newEventStream(message.StopEndTurn, "test-model",
				stream.Event{Type: stream.EventTextDelta, Text: "Sorry — continuing. The file is a Go package."},
			),
		},
	}

	e, _ := New(Config{Provider: mp, Tools: reg})
	_, err := e.Submit(context.Background(), "inspect /x.go", nil)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}

	var foundGreeting bool
	for _, m := range e.History() {
		if m.Role == message.RoleUser && strings.Contains(m.TextContent(), "greeting instead of continuing") {
			foundGreeting = true
			break
		}
	}
	if !foundGreeting {
		t.Fatal("expected greeting-regression corrective message in history")
	}
	if mp.calls != 3 {
		t.Errorf("provider calls = %d, want 3", mp.calls)
	}
}

func TestEarlyStop_NoFalsePositive_GreetingOnFirstTurn(t *testing.T) {
	// A greeting on the very first round (no prior tool calls) is fine — the
	// detector should only fire after a round that used tools.
	mp := &mockProvider{
		name: "test",
		streams: []stream.Stream{
			newEventStream(message.StopEndTurn, "test-model",
				stream.Event{Type: stream.EventTextDelta, Text: "Hi there! How can I help you?"},
			),
		},
	}

	e, _ := New(Config{Provider: mp, Tools: tool.NewRegistry()})
	_, err := e.Submit(context.Background(), "hello", nil)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}

	for _, m := range e.History() {
		if m.Role == message.RoleUser && strings.Contains(m.TextContent(), "greeting instead of continuing") {
			t.Fatal("greeting detector fired on first-round greeting")
		}
	}
	if mp.calls != 1 {
		t.Errorf("provider calls = %d, want 1", mp.calls)
	}
}

func TestEarlyStop_Repetition_BreaksAndCorrects(t *testing.T) {
	// Round 1: a stream that repeats a phrase enough to trip the detector.
	phrase := "I will read the file and then carefully apply the edit. "
	repeatEvents := make([]stream.Event, 0, 8)
	for range 8 {
		repeatEvents = append(repeatEvents, stream.Event{Type: stream.EventTextDelta, Text: phrase})
	}
	round1 := newEventStream(message.StopEndTurn, "test-model", repeatEvents...)

	round2 := newEventStream(message.StopEndTurn, "test-model",
		stream.Event{Type: stream.EventTextDelta, Text: "Acknowledged — taking a different approach."},
	)

	mp := &mockProvider{
		name:    "test",
		streams: []stream.Stream{round1, round2},
	}

	e, _ := New(Config{Provider: mp, Tools: tool.NewRegistry()})
	_, err := e.Submit(context.Background(), "do something", nil)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}

	var foundRep bool
	for _, m := range e.History() {
		if m.Role == message.RoleUser && strings.Contains(m.TextContent(), "repeating itself in a loop") {
			foundRep = true
			break
		}
	}
	if !foundRep {
		t.Fatal("expected repetition corrective message in history")
	}
	if mp.calls != 2 {
		t.Errorf("provider calls = %d, want 2", mp.calls)
	}
}
