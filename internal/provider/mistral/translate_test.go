package mistral

import (
	"encoding/json"
	"testing"

	"somegit.dev/Owlibou/gnoma/internal/message"
	"somegit.dev/Owlibou/gnoma/internal/provider"
	"somegit.dev/vikingowl/mistral-go-sdk/chat"
)

func TestTranslateMessage_User(t *testing.T) {
	m := message.NewUserText("hello world")
	result := translateMessage(m)

	um, ok := result.(*chat.UserMessage)
	if !ok {
		t.Fatalf("expected *UserMessage, got %T", result)
	}
	if um.Content.String() != "hello world" {
		t.Errorf("Content = %q, want %q", um.Content.String(), "hello world")
	}
}

func TestTranslateMessage_System(t *testing.T) {
	m := message.NewSystemText("you are a helper")
	result := translateMessage(m)

	sm, ok := result.(*chat.SystemMessage)
	if !ok {
		t.Fatalf("expected *SystemMessage, got %T", result)
	}
	if sm.Content.String() != "you are a helper" {
		t.Errorf("Content = %q", sm.Content.String())
	}
}

func TestTranslateMessage_AssistantText(t *testing.T) {
	m := message.NewAssistantText("here's the answer")
	result := translateMessage(m)

	am, ok := result.(*chat.AssistantMessage)
	if !ok {
		t.Fatalf("expected *AssistantMessage, got %T", result)
	}
	if am.Content.String() != "here's the answer" {
		t.Errorf("Content = %q", am.Content.String())
	}
	if len(am.ToolCalls) != 0 {
		t.Errorf("ToolCalls should be empty, got %d", len(am.ToolCalls))
	}
}

func TestTranslateMessage_AssistantWithToolCalls(t *testing.T) {
	m := message.NewAssistantContent(
		message.NewTextContent("running command"),
		message.NewToolCallContent(message.ToolCall{
			ID:        "tc_1",
			Name:      "bash",
			Arguments: json.RawMessage(`{"command":"ls"}`),
		}),
	)
	result := translateMessage(m)

	am, ok := result.(*chat.AssistantMessage)
	if !ok {
		t.Fatalf("expected *AssistantMessage, got %T", result)
	}
	if len(am.ToolCalls) != 1 {
		t.Fatalf("len(ToolCalls) = %d, want 1", len(am.ToolCalls))
	}
	if am.ToolCalls[0].ID != "tc_1" {
		t.Errorf("ToolCalls[0].ID = %q", am.ToolCalls[0].ID)
	}
	if am.ToolCalls[0].Function.Name != "bash" {
		t.Errorf("ToolCalls[0].Function.Name = %q", am.ToolCalls[0].Function.Name)
	}
	if am.ToolCalls[0].Function.Arguments != `{"command":"ls"}` {
		t.Errorf("ToolCalls[0].Function.Arguments = %q", am.ToolCalls[0].Function.Arguments)
	}
}

func TestExpandToolResults(t *testing.T) {
	msgs := []message.Message{
		message.NewUserText("run two commands"),
		message.NewAssistantContent(
			message.NewToolCallContent(message.ToolCall{ID: "tc_1", Name: "bash"}),
			message.NewToolCallContent(message.ToolCall{ID: "tc_2", Name: "bash"}),
		),
		message.NewToolResults(
			message.ToolResult{ToolCallID: "tc_1", Content: "output1"},
			message.ToolResult{ToolCallID: "tc_2", Content: "output2"},
		),
	}

	expanded := expandToolResults(msgs)

	// UserMessage, AssistantMessage, ToolMessage, ToolMessage
	if len(expanded) != 4 {
		t.Fatalf("len(expanded) = %d, want 4", len(expanded))
	}

	// First: UserMessage
	if _, ok := expanded[0].(*chat.UserMessage); !ok {
		t.Errorf("expanded[0] = %T, want *UserMessage", expanded[0])
	}

	// Second: AssistantMessage
	if _, ok := expanded[1].(*chat.AssistantMessage); !ok {
		t.Errorf("expanded[1] = %T, want *AssistantMessage", expanded[1])
	}

	// Third and fourth: ToolMessages
	tm1, ok := expanded[2].(*chat.ToolMessage)
	if !ok {
		t.Fatalf("expanded[2] = %T, want *ToolMessage", expanded[2])
	}
	if tm1.ToolCallID != "tc_1" {
		t.Errorf("expanded[2].ToolCallID = %q, want tc_1", tm1.ToolCallID)
	}

	tm2, ok := expanded[3].(*chat.ToolMessage)
	if !ok {
		t.Fatalf("expanded[3] = %T, want *ToolMessage", expanded[3])
	}
	if tm2.ToolCallID != "tc_2" {
		t.Errorf("expanded[3].ToolCallID = %q, want tc_2", tm2.ToolCallID)
	}
}

func TestTranslateTools(t *testing.T) {
	defs := []provider.ToolDefinition{
		{
			Name:        "bash",
			Description: "Run a bash command",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"}},"required":["command"]}`),
		},
		{
			Name:        "fs.read",
			Description: "Read a file",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`),
		},
	}

	tools := translateTools(defs)
	if len(tools) != 2 {
		t.Fatalf("len(tools) = %d, want 2", len(tools))
	}

	if tools[0].Type != "function" {
		t.Errorf("tools[0].Type = %q, want function", tools[0].Type)
	}
	if tools[0].Function.Name != "bash" {
		t.Errorf("tools[0].Function.Name = %q", tools[0].Function.Name)
	}
	if tools[0].Function.Description != "Run a bash command" {
		t.Errorf("tools[0].Function.Description = %q", tools[0].Function.Description)
	}
	if tools[0].Function.Parameters == nil {
		t.Error("tools[0].Function.Parameters should not be nil")
	}
	// Verify the parameters were correctly unmarshaled
	if _, ok := tools[0].Function.Parameters["type"]; !ok {
		t.Error("tools[0].Function.Parameters missing 'type' key")
	}
}

func TestTranslateTools_Empty(t *testing.T) {
	tools := translateTools(nil)
	if tools != nil {
		t.Errorf("translateTools(nil) should return nil, got %v", tools)
	}
}

func TestTranslateFinishReason(t *testing.T) {
	tests := []struct {
		name   string
		reason *chat.FinishReason
		want   message.StopReason
	}{
		{"nil", nil, ""},
		{"stop", ptr(chat.FinishReasonStop), message.StopEndTurn},
		{"tool_calls", ptr(chat.FinishReasonToolCalls), message.StopToolUse},
		{"length", ptr(chat.FinishReasonLength), message.StopMaxTokens},
		{"model_length", ptr(chat.FinishReasonModelLength), message.StopMaxTokens},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := translateFinishReason(tt.reason)
			if got != tt.want {
				t.Errorf("translateFinishReason() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTranslateUsage(t *testing.T) {
	u := &chat.UsageInfo{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
	}
	result := translateUsage(u)
	if result.InputTokens != 100 {
		t.Errorf("InputTokens = %d, want 100", result.InputTokens)
	}
	if result.OutputTokens != 50 {
		t.Errorf("OutputTokens = %d, want 50", result.OutputTokens)
	}
}

func TestTranslateUsage_Nil(t *testing.T) {
	result := translateUsage(nil)
	if result != nil {
		t.Error("translateUsage(nil) should return nil")
	}
}

func TestTranslateRequest(t *testing.T) {
	temp := 0.7
	req := provider.Request{
		Model:        "mistral-large-latest",
		SystemPrompt: "you are helpful",
		Messages: []message.Message{
			message.NewSystemText("you are helpful"),
			message.NewUserText("hello"),
		},
		Tools: []provider.ToolDefinition{
			{Name: "bash", Description: "Run command", Parameters: json.RawMessage(`{"type":"object"}`)},
		},
		MaxTokens:   4096,
		Temperature: &temp,
	}

	cr := translateRequest(req)

	if cr.Model != "mistral-large-latest" {
		t.Errorf("Model = %q", cr.Model)
	}
	if len(cr.Messages) != 2 {
		t.Errorf("len(Messages) = %d, want 2", len(cr.Messages))
	}
	if len(cr.Tools) != 1 {
		t.Errorf("len(Tools) = %d, want 1", len(cr.Tools))
	}
	if cr.MaxTokens == nil || *cr.MaxTokens != 4096 {
		t.Errorf("MaxTokens = %v", cr.MaxTokens)
	}
	if cr.Temperature == nil || *cr.Temperature != 0.7 {
		t.Errorf("Temperature = %v", cr.Temperature)
	}
}

func ptr[T any](v T) *T {
	return &v
}
