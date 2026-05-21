package subprocess

import (
	"testing"

	"somegit.dev/Owlibou/gnoma/internal/message"
	"somegit.dev/Owlibou/gnoma/internal/stream"
)

func TestCodexParser_ExtractsTextDelta(t *testing.T) {
	p := newCodexParser()
	line := []byte(`{"type":"item.completed","item":{"type":"agent_message","text":"hello world"}}`)

	evts, err := p.ParseLine(line)
	if err != nil {
		t.Fatal(err)
	}
	if len(evts) == 0 {
		t.Fatal("expected at least one event")
	}
	if evts[0].Type != stream.EventTextDelta {
		t.Errorf("got type %v, want EventTextDelta", evts[0].Type)
	}
	if evts[0].Text != "hello world" {
		t.Errorf("got text %q, want %q", evts[0].Text, "hello world")
	}
}

func TestCodexParser_ExtractsUsageFromTurnCompleted(t *testing.T) {
	p := newCodexParser()
	line := []byte(`{"type":"turn.completed","usage":{"input_tokens":123,"output_tokens":45}}`)

	evts, err := p.ParseLine(line)
	if err != nil {
		t.Fatal(err)
	}
	var usageEvt *stream.Event
	for i := range evts {
		if evts[i].Type == stream.EventUsage {
			usageEvt = &evts[i]
		}
	}
	if usageEvt == nil {
		t.Fatal("no EventUsage emitted")
	}
	if usageEvt.Usage.InputTokens != 123 {
		t.Errorf("input_tokens: got %d, want 123", usageEvt.Usage.InputTokens)
	}
	if usageEvt.Usage.OutputTokens != 45 {
		t.Errorf("output_tokens: got %d, want 45", usageEvt.Usage.OutputTokens)
	}
	if usageEvt.StopReason != message.StopEndTurn {
		t.Errorf("stop_reason: got %v, want StopEndTurn", usageEvt.StopReason)
	}
}

func TestCodexParser_ExtractsUsageFromPromptCompletionTokens(t *testing.T) {
	p := newCodexParser()
	line := []byte(`{"type":"turn.completed","usage":{"prompt_tokens":123,"completion_tokens":45}}`)

	evts, err := p.ParseLine(line)
	if err != nil {
		t.Fatal(err)
	}
	var usageEvt *stream.Event
	for i := range evts {
		if evts[i].Type == stream.EventUsage {
			usageEvt = &evts[i]
		}
	}
	if usageEvt == nil {
		t.Fatal("no EventUsage emitted")
	}
	if usageEvt.Usage.InputTokens != 123 {
		t.Errorf("input_tokens: got %d, want 123", usageEvt.Usage.InputTokens)
	}
	if usageEvt.Usage.OutputTokens != 45 {
		t.Errorf("output_tokens: got %d, want 45", usageEvt.Usage.OutputTokens)
	}
}

func TestCodexParser_IgnoresOtherItemsAndTypes(t *testing.T) {
	p := newCodexParser()
	lines := [][]byte{
		[]byte(`{"type":"item.completed","item":{"type":"tool_call","text":"something"}}`),
		[]byte(`{"type":"other_type"}`),
	}

	for _, line := range lines {
		evts, err := p.ParseLine(line)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if len(evts) != 0 {
			t.Errorf("expected 0 events, got %d", len(evts))
		}
	}
}

func TestCodexParser_FixtureFile(t *testing.T) {
	lines := loadFixture(t, "codex")
	p := newCodexParser()
	evts := collectEvents(t, p, lines)

	var textEvts, usageEvts int
	for _, e := range evts {
		switch e.Type {
		case stream.EventTextDelta:
			textEvts++
			if e.Text != "hello" {
				t.Errorf("expected text 'hello', got %q", e.Text)
			}
		case stream.EventUsage:
			usageEvts++
			if e.Usage.InputTokens != 10 || e.Usage.OutputTokens != 5 {
				t.Errorf("expected 10/5 tokens, got %d/%d", e.Usage.InputTokens, e.Usage.OutputTokens)
			}
		}
	}
	if textEvts != 1 {
		t.Errorf("expected 1 EventTextDelta, got %d", textEvts)
	}
	if usageEvts != 1 {
		t.Errorf("expected 1 EventUsage, got %d", usageEvts)
	}
}
