package anthropic

import (
	"encoding/json"
	"testing"

	"somegit.dev/Owlibou/gnoma/internal/message"
	"somegit.dev/Owlibou/gnoma/internal/provider"

	anthropic "github.com/anthropics/anthropic-sdk-go"
)

func TestTranslateMessages_UserText(t *testing.T) {
	msgs := []message.Message{message.NewUserText("hello")}
	result := translateMessages(msgs)

	if len(result) != 1 {
		t.Fatalf("len = %d, want 1", len(result))
	}
	if result[0].Role != anthropic.MessageParamRoleUser {
		t.Errorf("Role = %q, want user", result[0].Role)
	}
}

func TestTranslateMessages_SkipsSystem(t *testing.T) {
	msgs := []message.Message{
		message.NewSystemText("system prompt"),
		message.NewUserText("hello"),
	}
	result := translateMessages(msgs)

	// System messages are skipped — they go to the System param
	if len(result) != 1 {
		t.Fatalf("len = %d, want 1 (system skipped)", len(result))
	}
}

func TestTranslateMessages_AssistantWithToolCalls(t *testing.T) {
	msgs := []message.Message{
		message.NewAssistantContent(
			message.NewTextContent("running"),
			message.NewToolCallContent(message.ToolCall{
				ID:        "tc_1",
				Name:      "bash",
				Arguments: json.RawMessage(`{"command":"ls"}`),
			}),
		),
	}
	result := translateMessages(msgs)

	if len(result) != 1 {
		t.Fatalf("len = %d, want 1", len(result))
	}
	if result[0].Role != anthropic.MessageParamRoleAssistant {
		t.Errorf("Role = %q, want assistant", result[0].Role)
	}
}

func TestTranslateMessages_ToolResults(t *testing.T) {
	msgs := []message.Message{
		message.NewToolResults(
			message.ToolResult{ToolCallID: "tc_1", Content: "output", IsError: false},
			message.ToolResult{ToolCallID: "tc_2", Content: "error", IsError: true},
		),
	}
	result := translateMessages(msgs)

	if len(result) != 1 {
		t.Fatalf("len = %d, want 1 (single user message with tool results)", len(result))
	}
	if result[0].Role != anthropic.MessageParamRoleUser {
		t.Errorf("Role = %q, want user", result[0].Role)
	}
}

func TestTranslateTools(t *testing.T) {
	defs := []provider.ToolDefinition{
		{
			Name:        "bash",
			Description: "Run a command",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"}},"required":["command"]}`),
		},
	}
	tools := translateTools(defs)

	if len(tools) != 1 {
		t.Fatalf("len = %d, want 1", len(tools))
	}
	if tools[0].OfTool == nil {
		t.Fatal("OfTool should not be nil")
	}
	if tools[0].OfTool.Name != "bash" {
		t.Errorf("Name = %q", tools[0].OfTool.Name)
	}
}

func TestTranslateTools_Empty(t *testing.T) {
	tools := translateTools(nil)
	if tools != nil {
		t.Errorf("expected nil for empty defs")
	}
}

func TestTranslateStopReason(t *testing.T) {
	tests := []struct {
		input anthropic.StopReason
		want  message.StopReason
	}{
		{anthropic.StopReasonEndTurn, message.StopEndTurn},
		{anthropic.StopReasonMaxTokens, message.StopMaxTokens},
		{anthropic.StopReasonToolUse, message.StopToolUse},
		{anthropic.StopReasonStopSequence, message.StopSequence},
	}
	for _, tt := range tests {
		got := translateStopReason(tt.input)
		if got != tt.want {
			t.Errorf("translateStopReason(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestTranslateUsage(t *testing.T) {
	u := anthropic.Usage{
		InputTokens:              100,
		OutputTokens:             50,
		CacheReadInputTokens:     20,
		CacheCreationInputTokens: 10,
	}
	result := translateUsage(u)

	if result.InputTokens != 100 {
		t.Errorf("InputTokens = %d", result.InputTokens)
	}
	if result.OutputTokens != 50 {
		t.Errorf("OutputTokens = %d", result.OutputTokens)
	}
	if result.CacheReadTokens != 20 {
		t.Errorf("CacheReadTokens = %d", result.CacheReadTokens)
	}
	if result.CacheCreationTokens != 10 {
		t.Errorf("CacheCreationTokens = %d", result.CacheCreationTokens)
	}
}

func TestTranslateRequest(t *testing.T) {
	temp := 0.7
	req := provider.Request{
		Model:        "claude-sonnet-4-20250514",
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

	params := translateRequest(req)

	if params.Model != "claude-sonnet-4-20250514" {
		t.Errorf("Model = %q", params.Model)
	}
	if params.MaxTokens != 4096 {
		t.Errorf("MaxTokens = %d", params.MaxTokens)
	}
	// System messages in Messages should be skipped (1 system + 1 user → 1 message)
	if len(params.Messages) != 1 {
		t.Errorf("len(Messages) = %d, want 1", len(params.Messages))
	}
	if len(params.System) != 1 {
		t.Errorf("len(System) = %d, want 1", len(params.System))
	}
	if len(params.Tools) != 1 {
		t.Errorf("len(Tools) = %d, want 1", len(params.Tools))
	}
}
