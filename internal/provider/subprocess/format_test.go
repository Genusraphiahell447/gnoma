package subprocess

import (
	"os"
	"strings"
	"testing"

	"somegit.dev/Owlibou/gnoma/internal/message"
	"somegit.dev/Owlibou/gnoma/internal/stream"
)

// loadFixture reads testdata/<name>.jsonl and returns non-empty lines.
func loadFixture(t *testing.T, name string) [][]byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name + ".jsonl")
	if err != nil {
		t.Fatalf("load fixture %s: %v", name, err)
	}
	var lines [][]byte
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, []byte(line))
		}
	}
	return lines
}

// collectEvents runs all fixture lines through a parser and returns all emitted events.
func collectEvents(t *testing.T, p FormatParser, lines [][]byte) []stream.Event {
	t.Helper()
	var events []stream.Event
	for _, line := range lines {
		evts, err := p.ParseLine(line)
		if err != nil {
			t.Errorf("ParseLine(%s): %v", string(line), err)
			continue
		}
		events = append(events, evts...)
	}
	events = append(events, p.Done()...)
	return events
}

// --- claude-stream-json ---

func TestClaudeParser_ExtractsTextDelta(t *testing.T) {
	p := newClaudeParser()
	line := []byte(`{"type":"assistant","message":{"model":"claude-sonnet-4-6","content":[{"type":"text","text":"hello world"}],"usage":{"input_tokens":5,"output_tokens":2}}}`)

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

func TestClaudeParser_ExtractsUsageFromResult(t *testing.T) {
	p := newClaudeParser()
	line := []byte(`{"type":"result","subtype":"success","is_error":false,"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":5},"result":"hello"}`)

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
	if usageEvt.Usage == nil {
		t.Fatal("Usage is nil")
	}
	if usageEvt.Usage.InputTokens != 10 {
		t.Errorf("input_tokens: got %d, want 10", usageEvt.Usage.InputTokens)
	}
	if usageEvt.Usage.OutputTokens != 5 {
		t.Errorf("output_tokens: got %d, want 5", usageEvt.Usage.OutputTokens)
	}
	if usageEvt.StopReason != message.StopEndTurn {
		t.Errorf("stop_reason: got %v, want StopEndTurn", usageEvt.StopReason)
	}
}

func TestClaudeParser_IgnoresSystemAndRateLimit(t *testing.T) {
	p := newClaudeParser()
	system := []byte(`{"type":"system","subtype":"init","model":"claude-sonnet-4-6"}`)
	rateLimit := []byte(`{"type":"rate_limit_event","rate_limit_info":{}}`)

	for _, line := range [][]byte{system, rateLimit} {
		evts, err := p.ParseLine(line)
		if err != nil {
			t.Errorf("ParseLine(%s): unexpected error: %v", line, err)
		}
		if len(evts) != 0 {
			t.Errorf("expected no events for %s, got %d", line, len(evts))
		}
	}
}

func TestClaudeParser_ErrorResult(t *testing.T) {
	p := newClaudeParser()
	line := []byte(`{"type":"result","subtype":"error_during_run","is_error":true,"usage":{"input_tokens":5,"output_tokens":0}}`)

	evts, err := p.ParseLine(line)
	if err != nil {
		t.Fatal(err)
	}
	var hasError bool
	for _, e := range evts {
		if e.Type == stream.EventError {
			hasError = true
		}
	}
	if !hasError {
		t.Error("expected EventError for is_error=true result")
	}
}

func TestClaudeParser_FixtureFile(t *testing.T) {
	lines := loadFixture(t, "claude")
	p := newClaudeParser()
	evts := collectEvents(t, p, lines)

	var textEvts, usageEvts int
	for _, e := range evts {
		switch e.Type {
		case stream.EventTextDelta:
			textEvts++
			if e.Text == "" {
				t.Error("EventTextDelta with empty text")
			}
		case stream.EventUsage:
			usageEvts++
		}
	}
	if textEvts == 0 {
		t.Error("no EventTextDelta from claude fixture")
	}
	if usageEvts == 0 {
		t.Error("no EventUsage from claude fixture")
	}
}

// --- gemini-stream-json ---

func TestGeminiParser_ExtractsTextDelta(t *testing.T) {
	p := newGeminiParser()
	line := []byte(`{"type":"message","role":"assistant","content":"hello world","delta":true}`)

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

func TestGeminiParser_ExtractsUsageFromResult(t *testing.T) {
	p := newGeminiParser()
	line := []byte(`{"type":"result","status":"success","stats":{"input_tokens":100,"output_tokens":20}}`)

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
	if usageEvt.Usage.InputTokens != 100 {
		t.Errorf("input_tokens: got %d, want 100", usageEvt.Usage.InputTokens)
	}
	if usageEvt.Usage.OutputTokens != 20 {
		t.Errorf("output_tokens: got %d, want 20", usageEvt.Usage.OutputTokens)
	}
}

func TestGeminiParser_IgnoresUserMessages(t *testing.T) {
	p := newGeminiParser()
	line := []byte(`{"type":"message","role":"user","content":"say hi"}`)

	evts, err := p.ParseLine(line)
	if err != nil {
		t.Fatal(err)
	}
	if len(evts) != 0 {
		t.Errorf("expected no events for user message, got %d", len(evts))
	}
}

func TestGeminiParser_FixtureFile(t *testing.T) {
	lines := loadFixture(t, "gemini")
	p := newGeminiParser()
	evts := collectEvents(t, p, lines)

	var textEvts, usageEvts int
	for _, e := range evts {
		switch e.Type {
		case stream.EventTextDelta:
			textEvts++
		case stream.EventUsage:
			usageEvts++
		}
	}
	if textEvts == 0 {
		t.Error("no EventTextDelta from gemini fixture")
	}
	if usageEvts == 0 {
		t.Error("no EventUsage from gemini fixture")
	}
}

// --- vibe-streaming (mistral) ---

func TestVibeParser_ExtractsTextDelta(t *testing.T) {
	p := newVibeParser()
	line := []byte(`{"role":"assistant","content":"hello world","reasoning_content":null,"tool_calls":null,"message_id":"abc123"}`)

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

func TestVibeParser_ExtractsThinkingDelta(t *testing.T) {
	p := newVibeParser()
	line := []byte(`{"role":"assistant","content":"hello","reasoning_content":"I should say hi","tool_calls":null,"message_id":"abc123"}`)

	evts, err := p.ParseLine(line)
	if err != nil {
		t.Fatal(err)
	}
	var hasThinking bool
	for _, e := range evts {
		if e.Type == stream.EventThinkingDelta {
			hasThinking = true
			if e.Text == "" {
				t.Error("EventThinkingDelta with empty text")
			}
		}
	}
	if !hasThinking {
		t.Error("expected EventThinkingDelta when reasoning_content is set")
	}
}

func TestVibeParser_IgnoresSystemAndUser(t *testing.T) {
	p := newVibeParser()
	for _, line := range []string{
		`{"role":"system","content":"You are Vibe...","message_id":null}`,
		`{"role":"user","content":"say hi","message_id":"abc"}`,
	} {
		evts, err := p.ParseLine([]byte(line))
		if err != nil {
			t.Errorf("ParseLine(%s): unexpected error: %v", line, err)
		}
		if len(evts) != 0 {
			t.Errorf("expected no events for role=%s, got %d", line[:20], len(evts))
		}
	}
}

func TestVibeParser_FixtureFile(t *testing.T) {
	lines := loadFixture(t, "vibe")
	p := newVibeParser()
	evts := collectEvents(t, p, lines)

	var textEvts int
	for _, e := range evts {
		if e.Type == stream.EventTextDelta {
			textEvts++
		}
	}
	if textEvts == 0 {
		t.Error("no EventTextDelta from vibe fixture")
	}
}
