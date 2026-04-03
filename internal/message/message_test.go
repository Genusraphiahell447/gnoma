package message

import (
	"encoding/json"
	"testing"
)

func TestNewUserText(t *testing.T) {
	m := NewUserText("hello")
	if m.Role != RoleUser {
		t.Errorf("Role = %q, want %q", m.Role, RoleUser)
	}
	if len(m.Content) != 1 {
		t.Fatalf("len(Content) = %d, want 1", len(m.Content))
	}
	if m.Content[0].Type != ContentText {
		t.Errorf("Content[0].Type = %v, want %v", m.Content[0].Type, ContentText)
	}
	if m.Content[0].Text != "hello" {
		t.Errorf("Content[0].Text = %q, want %q", m.Content[0].Text, "hello")
	}
}

func TestNewAssistantText(t *testing.T) {
	m := NewAssistantText("response")
	if m.Role != RoleAssistant {
		t.Errorf("Role = %q, want %q", m.Role, RoleAssistant)
	}
	if m.TextContent() != "response" {
		t.Errorf("TextContent() = %q, want %q", m.TextContent(), "response")
	}
}

func TestNewSystemText(t *testing.T) {
	m := NewSystemText("you are a helper")
	if m.Role != RoleSystem {
		t.Errorf("Role = %q, want %q", m.Role, RoleSystem)
	}
}

func TestNewAssistantContent_Mixed(t *testing.T) {
	m := NewAssistantContent(
		NewTextContent("I'll run that command."),
		NewToolCallContent(ToolCall{
			ID:        "tc_1",
			Name:      "bash",
			Arguments: json.RawMessage(`{"command":"ls"}`),
		}),
	)

	if m.Role != RoleAssistant {
		t.Errorf("Role = %q, want %q", m.Role, RoleAssistant)
	}
	if len(m.Content) != 2 {
		t.Fatalf("len(Content) = %d, want 2", len(m.Content))
	}
	if m.Content[0].Type != ContentText {
		t.Errorf("Content[0].Type = %v, want text", m.Content[0].Type)
	}
	if m.Content[1].Type != ContentToolCall {
		t.Errorf("Content[1].Type = %v, want tool_call", m.Content[1].Type)
	}
}

func TestNewToolResults(t *testing.T) {
	m := NewToolResults(
		ToolResult{ToolCallID: "tc_1", Content: "output1"},
		ToolResult{ToolCallID: "tc_2", Content: "output2", IsError: true},
	)

	if m.Role != RoleUser {
		t.Errorf("Role = %q, want %q", m.Role, RoleUser)
	}
	if len(m.Content) != 2 {
		t.Fatalf("len(Content) = %d, want 2", len(m.Content))
	}
	if m.Content[0].ToolResult.ToolCallID != "tc_1" {
		t.Errorf("Content[0].ToolResult.ToolCallID = %q", m.Content[0].ToolResult.ToolCallID)
	}
	if m.Content[1].ToolResult.IsError != true {
		t.Error("Content[1].ToolResult.IsError should be true")
	}
}

func TestMessage_HasToolCalls(t *testing.T) {
	tests := []struct {
		name string
		msg  Message
		want bool
	}{
		{
			name: "text only",
			msg:  NewUserText("hello"),
			want: false,
		},
		{
			name: "with tool call",
			msg: NewAssistantContent(
				NewTextContent("running..."),
				NewToolCallContent(ToolCall{ID: "tc_1", Name: "bash"}),
			),
			want: true,
		},
		{
			name: "tool results (not calls)",
			msg:  NewToolResults(ToolResult{ToolCallID: "tc_1", Content: "ok"}),
			want: false,
		},
		{
			name: "empty message",
			msg:  Message{Role: RoleAssistant},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.msg.HasToolCalls(); got != tt.want {
				t.Errorf("HasToolCalls() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMessage_ToolCalls(t *testing.T) {
	m := NewAssistantContent(
		NewTextContent("here are two commands"),
		NewToolCallContent(ToolCall{ID: "tc_1", Name: "bash", Arguments: json.RawMessage(`{"command":"ls"}`)}),
		NewTextContent("and another"),
		NewToolCallContent(ToolCall{ID: "tc_2", Name: "fs.read", Arguments: json.RawMessage(`{"path":"go.mod"}`)}),
	)

	calls := m.ToolCalls()
	if len(calls) != 2 {
		t.Fatalf("len(ToolCalls()) = %d, want 2", len(calls))
	}
	if calls[0].ID != "tc_1" {
		t.Errorf("calls[0].ID = %q, want tc_1", calls[0].ID)
	}
	if calls[1].Name != "fs.read" {
		t.Errorf("calls[1].Name = %q, want fs.read", calls[1].Name)
	}
}

func TestMessage_ToolCalls_Empty(t *testing.T) {
	m := NewUserText("no tools here")
	calls := m.ToolCalls()
	if len(calls) != 0 {
		t.Errorf("len(ToolCalls()) = %d, want 0", len(calls))
	}
}

func TestMessage_TextContent(t *testing.T) {
	tests := []struct {
		name string
		msg  Message
		want string
	}{
		{
			name: "single text",
			msg:  NewUserText("hello"),
			want: "hello",
		},
		{
			name: "multiple text blocks",
			msg: NewAssistantContent(
				NewTextContent("first "),
				NewToolCallContent(ToolCall{ID: "tc_1", Name: "bash"}),
				NewTextContent("second"),
			),
			want: "first second",
		},
		{
			name: "no text",
			msg:  NewToolResults(ToolResult{ToolCallID: "tc_1", Content: "output"}),
			want: "",
		},
		{
			name: "empty message",
			msg:  Message{Role: RoleAssistant},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.msg.TextContent(); got != tt.want {
				t.Errorf("TextContent() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResponse_Fields(t *testing.T) {
	r := Response{
		Message:    NewAssistantText("done"),
		StopReason: StopEndTurn,
		Usage:      Usage{InputTokens: 100, OutputTokens: 50},
		Model:      "mistral-large-latest",
	}

	if r.StopReason != StopEndTurn {
		t.Errorf("StopReason = %q, want %q", r.StopReason, StopEndTurn)
	}
	if r.Usage.TotalTokens() != 150 {
		t.Errorf("Usage.TotalTokens() = %d, want 150", r.Usage.TotalTokens())
	}
	if r.Model != "mistral-large-latest" {
		t.Errorf("Model = %q", r.Model)
	}
	if r.Message.TextContent() != "done" {
		t.Errorf("Message.TextContent() = %q", r.Message.TextContent())
	}
}
