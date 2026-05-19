package engine

import (
	"context"
	"encoding/json"
	"testing"

	gnomactx "somegit.dev/Owlibou/gnoma/internal/context"
	"somegit.dev/Owlibou/gnoma/internal/message"
	"somegit.dev/Owlibou/gnoma/internal/stream"
	"somegit.dev/Owlibou/gnoma/internal/tool"
)

// deferredMockTool implements tool.Tool and tool.DeferrableTool.
type deferredMockTool struct {
	name string
}

func (d *deferredMockTool) Name() string               { return d.name }
func (d *deferredMockTool) Description() string         { return "deferred mock" }
func (d *deferredMockTool) Parameters() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (d *deferredMockTool) IsReadOnly() bool            { return true }
func (d *deferredMockTool) IsDestructive() bool         { return false }
func (d *deferredMockTool) ShouldDefer() bool           { return true }
func (d *deferredMockTool) Execute(_ context.Context, _ json.RawMessage) (tool.Result, error) {
	return tool.Result{Output: "deferred output"}, nil
}

func TestSetHistory_ReplacesHistory(t *testing.T) {
	e, _ := New(Config{
		Provider: &mockProvider{name: "test"},
		Tools:    tool.NewRegistry(),
	})

	msgs := []message.Message{
		message.NewUserText("hello"),
		message.NewAssistantText("hi there"),
	}
	e.SetHistory(msgs)

	got := e.History()
	if len(got) != 2 {
		t.Fatalf("History() len = %d, want 2", len(got))
	}
	if got[0].Role != message.RoleUser {
		t.Errorf("History()[0].Role = %q, want user", got[0].Role)
	}
	if got[1].Role != message.RoleAssistant {
		t.Errorf("History()[1].Role = %q, want assistant", got[1].Role)
	}
}

func TestSetHistory_OverwritesPreviousHistory(t *testing.T) {
	mp := &mockProvider{
		name: "test",
		streams: []stream.Stream{
			newEventStream(message.StopEndTurn, "",
				stream.Event{Type: stream.EventTextDelta, Text: "original"},
			),
		},
	}
	e, _ := New(Config{Provider: mp, Tools: tool.NewRegistry()})
	_, _ = e.Submit(context.Background(), "first message", nil)

	if len(e.History()) == 0 {
		t.Fatal("history should not be empty after Submit")
	}

	replacement := []message.Message{
		message.NewUserText("restored message"),
	}
	e.SetHistory(replacement)

	got := e.History()
	if len(got) != 1 {
		t.Fatalf("History() len = %d, want 1 after restore", len(got))
	}
	if got[0].TextContent() != "restored message" {
		t.Errorf("History()[0].TextContent() = %q, want %q", got[0].TextContent(), "restored message")
	}
}

func TestSetHistory_SyncsContextWindow(t *testing.T) {
	ctxWindow := gnomactx.NewWindow(gnomactx.WindowConfig{MaxTokens: 200_000})
	e, _ := New(Config{
		Provider: &mockProvider{name: "test"},
		Tools:    tool.NewRegistry(),
		Context:  ctxWindow,
	})

	msgs := []message.Message{
		message.NewUserText("user turn"),
		message.NewAssistantText("assistant turn"),
	}
	e.SetHistory(msgs)

	all := e.ContextWindow().AllMessages()
	if len(all) != 2 {
		t.Fatalf("ContextWindow().AllMessages() len = %d, want 2", len(all))
	}
	if all[0].TextContent() != "user turn" {
		t.Errorf("AllMessages()[0].TextContent() = %q, want %q", all[0].TextContent(), "user turn")
	}
}

func TestSetHistory_SyncsTrackerTokenCount(t *testing.T) {
	ctxWindow := gnomactx.NewWindow(gnomactx.WindowConfig{MaxTokens: 200_000})
	e, _ := New(Config{
		Provider: &mockProvider{name: "test"},
		Tools:    tool.NewRegistry(),
		Context:  ctxWindow,
	})

	// Start with zero tracker usage.
	if ctxWindow.Tracker().Used() != 0 {
		t.Fatal("tracker should start at zero")
	}

	msgs := []message.Message{
		message.NewUserText("hello world"),
	}
	e.SetHistory(msgs)

	// After SetHistory, tracker should reflect a non-zero estimate.
	used := ctxWindow.Tracker().Used()
	if used == 0 {
		t.Error("tracker should be non-zero after SetHistory with messages")
	}
}

func TestSetHistory_NilContextWindow_NoPanic(t *testing.T) {
	e, _ := New(Config{
		Provider: &mockProvider{name: "test"},
		Tools:    tool.NewRegistry(),
		// Context intentionally nil
	})

	msgs := []message.Message{message.NewUserText("safe")}

	// Should not panic when no context window is configured.
	e.SetHistory(msgs)

	if len(e.History()) != 1 {
		t.Errorf("History() len = %d, want 1", len(e.History()))
	}
}

func TestSetUsage_ReplacesUsage(t *testing.T) {
	e, _ := New(Config{
		Provider: &mockProvider{name: "test"},
		Tools:    tool.NewRegistry(),
	})

	u := message.Usage{InputTokens: 500, OutputTokens: 250}
	e.SetUsage(u)

	got := e.Usage()
	if got.InputTokens != 500 {
		t.Errorf("Usage().InputTokens = %d, want 500", got.InputTokens)
	}
	if got.OutputTokens != 250 {
		t.Errorf("Usage().OutputTokens = %d, want 250", got.OutputTokens)
	}
}

func TestSetUsage_OverwritesPreviousUsage(t *testing.T) {
	mp := &mockProvider{
		name: "test",
		streams: []stream.Stream{
			newEventStream(message.StopEndTurn, "",
				stream.Event{Type: stream.EventUsage, Usage: &message.Usage{InputTokens: 100, OutputTokens: 50}},
				stream.Event{Type: stream.EventTextDelta, Text: "hi"},
			),
		},
	}
	e, _ := New(Config{Provider: mp, Tools: tool.NewRegistry()})
	_, _ = e.Submit(context.Background(), "hello", nil)

	if e.Usage().InputTokens == 0 {
		t.Fatal("usage should be non-zero after Submit")
	}

	restored := message.Usage{InputTokens: 999, OutputTokens: 111}
	e.SetUsage(restored)

	got := e.Usage()
	if got.InputTokens != 999 {
		t.Errorf("Usage().InputTokens = %d, want 999", got.InputTokens)
	}
	if got.OutputTokens != 111 {
		t.Errorf("Usage().OutputTokens = %d, want 111", got.OutputTokens)
	}
}

func TestSetActivatedTools_DeferredToolIncludedInRequest(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Register(&deferredMockTool{name: "bash"})

	mp := &mockProvider{
		name: "test",
		streams: []stream.Stream{
			newEventStream(message.StopEndTurn, "mock-model",
				stream.Event{Type: stream.EventTextDelta, Text: "done"},
			),
		},
	}

	e, _ := New(Config{Provider: mp, Tools: reg})

	// Before activation: buildRequest should omit "bash" (deferred).
	reqBefore := e.buildRequest(context.Background())
	for _, td := range reqBefore.Tools {
		if td.Name == "bash" {
			t.Fatal("deferred tool 'bash' should not appear in request before activation")
		}
	}

	// Restore activated tools.
	e.SetActivatedTools(map[string]bool{"bash": true})

	// After activation: buildRequest should include "bash".
	reqAfter := e.buildRequest(context.Background())
	found := false
	for _, td := range reqAfter.Tools {
		if td.Name == "bash" {
			found = true
			break
		}
	}
	if !found {
		t.Error("deferred tool 'bash' should appear in request after SetActivatedTools")
	}
}

func TestSetActivatedTools_EmptyMap_DeactivatesAll(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Register(&deferredMockTool{name: "bash"})

	mp := &mockProvider{name: "test"}
	e, _ := New(Config{Provider: mp, Tools: reg})

	// Manually activate, then restore to empty.
	e.activatedTools["bash"] = true
	e.SetActivatedTools(map[string]bool{})

	req := e.buildRequest(context.Background())
	for _, td := range req.Tools {
		if td.Name == "bash" {
			t.Error("deferred tool 'bash' should not appear after SetActivatedTools(empty)")
		}
	}
}
