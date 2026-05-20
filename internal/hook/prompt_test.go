package hook

import (
	"context"
	"errors"
	"testing"

	"somegit.dev/Owlibou/gnoma/internal/message"
	"somegit.dev/Owlibou/gnoma/internal/stream"
)

// mockStreamer implements Streamer by returning a pre-built stream or an error.
type mockStreamer struct {
	s   stream.Stream
	err error
}

func (m *mockStreamer) Stream(_ context.Context, prompt string) (stream.Stream, error) {
	return m.s, m.err
}

// textStream returns a single-event stream with the given text.
func textStream(text string) stream.Stream {
	events := []stream.Event{
		{Type: stream.EventTextDelta, Text: text},
		{Type: stream.EventTextDelta, StopReason: message.StopEndTurn},
	}
	return &sliceStream{events: events}
}

type sliceStream struct {
	events []stream.Event
	idx    int
}

func (s *sliceStream) Next() bool            { s.idx++; return s.idx <= len(s.events) }
func (s *sliceStream) Current() stream.Event { return s.events[s.idx-1] }
func (s *sliceStream) Err() error            { return nil }
func (s *sliceStream) Close() error          { return nil }

// --- Template rendering tests ---

func TestRenderTemplate_EventVar(t *testing.T) {
	got, err := renderTemplate("event is {{.Event}}", TemplateData{Event: "pre_tool_use"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "event is pre_tool_use" {
		t.Errorf("got %q", got)
	}
}

func TestRenderTemplate_AllVars(t *testing.T) {
	tmpl := "{{.Event}} {{.Tool}} {{.Args}} {{.Result}}"
	data := TemplateData{Event: "pre_tool_use", Tool: "bash", Args: `{"cmd":"ls"}`, Result: ""}
	got, err := renderTemplate(tmpl, data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != `pre_tool_use bash {"cmd":"ls"} ` {
		t.Errorf("got %q", got)
	}
}

func TestRenderTemplate_NonToolEvent_EmptyToolFields(t *testing.T) {
	tmpl := "[{{.Tool}}][{{.Args}}][{{.Result}}]"
	data := TemplateData{Event: "session_start"} // Tool/Args/Result are zero values
	got, err := renderTemplate(tmpl, data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "[][][]" {
		t.Errorf("got %q", got)
	}
}

func TestRenderTemplate_InvalidTemplate(t *testing.T) {
	_, err := renderTemplate("{{.Unknown field", TemplateData{})
	if err == nil {
		t.Error("expected error for invalid template")
	}
}

// --- parseDecision tests ---

func TestParseDecision_ALLOW(t *testing.T) {
	if got := parseDecision("The action is ALLOW."); got != Allow {
		t.Errorf("got %v, want Allow", got)
	}
}

func TestParseDecision_DENY(t *testing.T) {
	if got := parseDecision("I must DENY this request."); got != Deny {
		t.Errorf("got %v, want Deny", got)
	}
}

func TestParseDecision_NoMatch(t *testing.T) {
	if got := parseDecision("I don't know."); got != Skip {
		t.Errorf("got %v, want Skip", got)
	}
}

func TestParseDecision_CaseInsensitive(t *testing.T) {
	cases := []struct {
		text string
		want Action
	}{
		{"allow", Allow},
		{"Allow", Allow},
		{"ALLOW", Allow},
		{"deny", Deny},
		{"Deny", Deny},
		{"DENY", Deny},
	}
	for _, tt := range cases {
		if got := parseDecision(tt.text); got != tt.want {
			t.Errorf("parseDecision(%q) = %v, want %v", tt.text, got, tt.want)
		}
	}
}

func TestParseDecision_FirstMatchWins(t *testing.T) {
	// "DENY" appears before "ALLOW" → Deny
	if got := parseDecision("I will DENY this, not ALLOW."); got != Deny {
		t.Errorf("got %v, want Deny (first match)", got)
	}
}

// --- PromptExecutor tests ---

func TestPromptExecutor_ResponseALLOW(t *testing.T) {
	def := HookDef{Name: "test", Event: PreToolUse, Command: CommandTypePrompt, Exec: "Is this safe? ALLOW or DENY."}
	ex := NewPromptExecutor(def, &mockStreamer{s: textStream("This is safe. ALLOW.")})
	result, err := ex.Execute(context.Background(), MarshalPreToolPayload("bash", nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != Allow {
		t.Errorf("action = %v, want Allow", result.Action)
	}
}

func TestPromptExecutor_ResponseDENY(t *testing.T) {
	def := HookDef{Name: "test", Event: PreToolUse, Command: CommandTypePrompt, Exec: "Is this safe? ALLOW or DENY."}
	ex := NewPromptExecutor(def, &mockStreamer{s: textStream("This is dangerous. DENY.")})
	result, err := ex.Execute(context.Background(), MarshalPreToolPayload("bash", nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != Deny {
		t.Errorf("action = %v, want Deny", result.Action)
	}
}

func TestPromptExecutor_ResponseNoMatch_Skip(t *testing.T) {
	def := HookDef{Name: "test", Event: PreToolUse, Command: CommandTypePrompt, Exec: "Review this."}
	ex := NewPromptExecutor(def, &mockStreamer{s: textStream("I'm not sure what to do.")})
	result, err := ex.Execute(context.Background(), MarshalPreToolPayload("bash", nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != Skip {
		t.Errorf("action = %v, want Skip", result.Action)
	}
}

func TestPromptExecutor_TemplateRendered(t *testing.T) {
	// Verify template vars are substituted — use a streamer that captures the prompt.
	var capturedPrompt string
	capturingStreamer := &capturingStreamer{response: "ALLOW"}
	def := HookDef{
		Name:    "test",
		Event:   PreToolUse,
		Command: CommandTypePrompt,
		Exec:    "Tool={{.Tool}} Event={{.Event}}",
	}
	ex := NewPromptExecutor(def, capturingStreamer)
	_, _ = ex.Execute(context.Background(), MarshalPreToolPayload("bash", nil))
	capturedPrompt = capturingStreamer.prompt
	if capturedPrompt == "" {
		t.Fatal("prompt not captured")
	}
	if capturedPrompt != "Tool=bash Event=pre_tool_use" {
		t.Errorf("prompt = %q", capturedPrompt)
	}
}

func TestPromptExecutor_OutputIsFullResponse(t *testing.T) {
	def := HookDef{Name: "test", Event: PreToolUse, Command: CommandTypePrompt, Exec: "Review."}
	response := "After analysis, ALLOW this operation."
	ex := NewPromptExecutor(def, &mockStreamer{s: textStream(response)})
	result, _ := ex.Execute(context.Background(), MarshalPreToolPayload("bash", nil))
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	// Output field carries the full LLM response text (for observability)
	if string(result.Output) != response {
		t.Errorf("Output = %q, want %q", result.Output, response)
	}
}

func TestPromptExecutor_StreamerError(t *testing.T) {
	def := HookDef{Name: "test", Event: PreToolUse, Command: CommandTypePrompt, Exec: "Review."}
	ex := NewPromptExecutor(def, &mockStreamer{err: errors.New("provider unavailable")})
	result, err := ex.Execute(context.Background(), MarshalPreToolPayload("bash", nil))
	if err == nil {
		t.Fatal("expected error")
	}
	// fail_open=false (default) → Deny on error; but error is returned, caller (Dispatcher) applies policy
	_ = result
}

// capturingStreamer records the prompt it was called with.
type capturingStreamer struct {
	prompt   string
	response string
}

func (c *capturingStreamer) Stream(_ context.Context, prompt string) (stream.Stream, error) {
	c.prompt = prompt
	return textStream(c.response), nil
}
