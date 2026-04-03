package stream

import (
	"encoding/json"
	"testing"

	"somegit.dev/Owlibou/gnoma/internal/message"
)

func TestAccumulator_TextOnly(t *testing.T) {
	acc := NewAccumulator()

	acc.Apply(Event{Type: EventTextDelta, Text: "Hello "})
	acc.Apply(Event{Type: EventTextDelta, Text: "world!"})
	acc.Apply(Event{Type: EventUsage, Usage: &message.Usage{InputTokens: 10, OutputTokens: 5}})

	resp := acc.Response(message.StopEndTurn, "mistral-large")

	if resp.StopReason != message.StopEndTurn {
		t.Errorf("StopReason = %q, want %q", resp.StopReason, message.StopEndTurn)
	}
	if resp.Model != "mistral-large" {
		t.Errorf("Model = %q, want %q", resp.Model, "mistral-large")
	}
	if resp.Message.Role != message.RoleAssistant {
		t.Errorf("Role = %q, want %q", resp.Message.Role, message.RoleAssistant)
	}
	if resp.Message.TextContent() != "Hello world!" {
		t.Errorf("TextContent() = %q, want %q", resp.Message.TextContent(), "Hello world!")
	}
	if resp.Usage.InputTokens != 10 {
		t.Errorf("Usage.InputTokens = %d, want 10", resp.Usage.InputTokens)
	}
}

func TestAccumulator_SingleToolCall(t *testing.T) {
	acc := NewAccumulator()

	acc.Apply(Event{Type: EventTextDelta, Text: "I'll run that."})
	acc.Apply(Event{
		Type:         EventToolCallStart,
		ToolCallID:   "tc_1",
		ToolCallName: "bash",
	})
	acc.Apply(Event{
		Type:       EventToolCallDelta,
		ToolCallID: "tc_1",
		ArgDelta:   `{"comma`,
	})
	acc.Apply(Event{
		Type:       EventToolCallDelta,
		ToolCallID: "tc_1",
		ArgDelta:   `nd":"ls"}`,
	})
	acc.Apply(Event{
		Type:       EventToolCallDone,
		ToolCallID: "tc_1",
		Args:       json.RawMessage(`{"command":"ls"}`),
	})

	resp := acc.Response(message.StopToolUse, "mistral-large")

	if resp.StopReason != message.StopToolUse {
		t.Errorf("StopReason = %q, want %q", resp.StopReason, message.StopToolUse)
	}

	content := resp.Message.Content
	if len(content) != 2 {
		t.Fatalf("len(Content) = %d, want 2", len(content))
	}

	// First block: text
	if content[0].Type != message.ContentText {
		t.Errorf("Content[0].Type = %v, want text", content[0].Type)
	}
	if content[0].Text != "I'll run that." {
		t.Errorf("Content[0].Text = %q", content[0].Text)
	}

	// Second block: tool call
	if content[1].Type != message.ContentToolCall {
		t.Errorf("Content[1].Type = %v, want tool_call", content[1].Type)
	}
	tc := content[1].ToolCall
	if tc.ID != "tc_1" {
		t.Errorf("ToolCall.ID = %q, want tc_1", tc.ID)
	}
	if tc.Name != "bash" {
		t.Errorf("ToolCall.Name = %q, want bash", tc.Name)
	}
	if string(tc.Arguments) != `{"command":"ls"}` {
		t.Errorf("ToolCall.Arguments = %s", tc.Arguments)
	}
}

func TestAccumulator_MultipleToolCalls(t *testing.T) {
	acc := NewAccumulator()

	// Tool call 1
	acc.Apply(Event{Type: EventToolCallStart, ToolCallID: "tc_1", ToolCallName: "bash"})
	acc.Apply(Event{Type: EventToolCallDone, ToolCallID: "tc_1", Args: json.RawMessage(`{"command":"ls"}`)})

	// Tool call 2
	acc.Apply(Event{Type: EventToolCallStart, ToolCallID: "tc_2", ToolCallName: "fs.read"})
	acc.Apply(Event{Type: EventToolCallDone, ToolCallID: "tc_2", Args: json.RawMessage(`{"path":"go.mod"}`)})

	resp := acc.Response(message.StopToolUse, "")
	calls := resp.Message.ToolCalls()
	if len(calls) != 2 {
		t.Fatalf("len(ToolCalls()) = %d, want 2", len(calls))
	}
	if calls[0].Name != "bash" {
		t.Errorf("calls[0].Name = %q", calls[0].Name)
	}
	if calls[1].Name != "fs.read" {
		t.Errorf("calls[1].Name = %q", calls[1].Name)
	}
}

func TestAccumulator_ThinkingBlocks(t *testing.T) {
	acc := NewAccumulator()

	acc.Apply(Event{Type: EventThinkingDelta, Text: "Let me think"})
	acc.Apply(Event{Type: EventThinkingDelta, Text: " about this..."})
	acc.Apply(Event{Type: EventTextDelta, Text: "Here's my answer."})

	resp := acc.Response(message.StopEndTurn, "")
	content := resp.Message.Content

	if len(content) != 2 {
		t.Fatalf("len(Content) = %d, want 2", len(content))
	}

	// Thinking block first
	if content[0].Type != message.ContentThinking {
		t.Errorf("Content[0].Type = %v, want thinking", content[0].Type)
	}
	if content[0].Thinking.Text != "Let me think about this..." {
		t.Errorf("Thinking.Text = %q", content[0].Thinking.Text)
	}

	// Then text
	if content[1].Type != message.ContentText {
		t.Errorf("Content[1].Type = %v, want text", content[1].Type)
	}
}

func TestAccumulator_ToolCallDelta_Assembly(t *testing.T) {
	acc := NewAccumulator()

	acc.Apply(Event{Type: EventToolCallStart, ToolCallID: "tc_1", ToolCallName: "bash"})
	// Simulate fragmented JSON
	acc.Apply(Event{Type: EventToolCallDelta, ToolCallID: "tc_1", ArgDelta: `{"`})
	acc.Apply(Event{Type: EventToolCallDelta, ToolCallID: "tc_1", ArgDelta: `comman`})
	acc.Apply(Event{Type: EventToolCallDelta, ToolCallID: "tc_1", ArgDelta: `d":"`})
	acc.Apply(Event{Type: EventToolCallDelta, ToolCallID: "tc_1", ArgDelta: `echo hi`})
	acc.Apply(Event{Type: EventToolCallDelta, ToolCallID: "tc_1", ArgDelta: `"}`})

	// Done event carries the complete args (authoritative)
	acc.Apply(Event{
		Type:       EventToolCallDone,
		ToolCallID: "tc_1",
		Args:       json.RawMessage(`{"command":"echo hi"}`),
	})

	resp := acc.Response(message.StopToolUse, "")
	calls := resp.Message.ToolCalls()
	if len(calls) != 1 {
		t.Fatalf("len(ToolCalls()) = %d, want 1", len(calls))
	}
	if string(calls[0].Arguments) != `{"command":"echo hi"}` {
		t.Errorf("Arguments = %s", calls[0].Arguments)
	}
}

func TestAccumulator_EmptyStream(t *testing.T) {
	acc := NewAccumulator()
	resp := acc.Response(message.StopEndTurn, "model-x")

	if resp.Model != "model-x" {
		t.Errorf("Model = %q", resp.Model)
	}
	if len(resp.Message.Content) != 0 {
		t.Errorf("len(Content) = %d, want 0", len(resp.Message.Content))
	}
}

func TestAccumulator_UsageCumulative(t *testing.T) {
	acc := NewAccumulator()

	acc.Apply(Event{Type: EventUsage, Usage: &message.Usage{InputTokens: 100, OutputTokens: 30}})
	acc.Apply(Event{Type: EventTextDelta, Text: "hi"})
	acc.Apply(Event{Type: EventUsage, Usage: &message.Usage{InputTokens: 0, OutputTokens: 20}})

	resp := acc.Response(message.StopEndTurn, "")
	// Last usage wins (providers typically send cumulative, not incremental)
	// But our Add() is additive — so 100+0=100 input, 30+20=50 output
	if resp.Usage.InputTokens != 100 {
		t.Errorf("InputTokens = %d, want 100", resp.Usage.InputTokens)
	}
	if resp.Usage.OutputTokens != 50 {
		t.Errorf("OutputTokens = %d, want 50", resp.Usage.OutputTokens)
	}
}

func TestEventType_String(t *testing.T) {
	tests := []struct {
		et   EventType
		want string
	}{
		{EventTextDelta, "text_delta"},
		{EventThinkingDelta, "thinking_delta"},
		{EventToolCallStart, "tool_call_start"},
		{EventToolCallDelta, "tool_call_delta"},
		{EventToolCallDone, "tool_call_done"},
		{EventUsage, "usage"},
		{EventError, "error"},
		{EventType(99), "unknown(99)"},
	}
	for _, tt := range tests {
		if got := tt.et.String(); got != tt.want {
			t.Errorf("EventType(%d).String() = %q, want %q", tt.et, got, tt.want)
		}
	}
}
