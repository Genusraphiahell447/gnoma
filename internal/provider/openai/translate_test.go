package openai

import (
	"encoding/json"
	"testing"

	"somegit.dev/Owlibou/gnoma/internal/message"
	"somegit.dev/Owlibou/gnoma/internal/provider"

	"github.com/openai/openai-go/packages/param"
)

func TestTranslateMessage_AssistantToolCallNames_Sanitized(t *testing.T) {
	msg := message.Message{
		Role: message.RoleAssistant,
		Content: []message.Content{
			message.NewTextContent("calling tools"),
			message.NewToolCallContent(message.ToolCall{
				ID:        "call_1",
				Name:      "fs.ls", // internal gnoma name (dot)
				Arguments: json.RawMessage(`{"path":"/"}`),
			}),
			message.NewToolCallContent(message.ToolCall{
				ID:        "call_2",
				Name:      "fs.read", // internal gnoma name (dot)
				Arguments: json.RawMessage(`{"path":"/tmp/x"}`),
			}),
		},
	}

	out := translateMessage(msg)
	if len(out) != 1 {
		t.Fatalf("translateMessage returned %d messages, want 1", len(out))
	}

	calls := out[0].OfAssistant.ToolCalls
	if len(calls) != 2 {
		t.Fatalf("got %d tool calls, want 2", len(calls))
	}
	if calls[0].Function.Name != "fs_ls" {
		t.Errorf("tool call 0 name = %q, want %q", calls[0].Function.Name, "fs_ls")
	}
	if calls[1].Function.Name != "fs_read" {
		t.Errorf("tool call 1 name = %q, want %q", calls[1].Function.Name, "fs_read")
	}
}

func TestTranslateRequest_ToolChoiceDefault(t *testing.T) {
	tests := []struct {
		name       string
		tools      []provider.ToolDefinition
		toolChoice provider.ToolChoiceMode
		wantChoice string // "" means omitted
	}{
		{
			name:       "no tools, no choice — omitted",
			tools:      nil,
			toolChoice: "",
			wantChoice: "",
		},
		{
			name: "tools present, no explicit choice — defaults to auto",
			tools: []provider.ToolDefinition{
				{Name: "fs_ls", Description: "list dir", Parameters: json.RawMessage(`{"type":"object"}`)},
			},
			toolChoice: "",
			wantChoice: "auto",
		},
		{
			name: "tools present, explicit required",
			tools: []provider.ToolDefinition{
				{Name: "fs_ls", Description: "list dir", Parameters: json.RawMessage(`{"type":"object"}`)},
			},
			toolChoice: provider.ToolChoiceRequired,
			wantChoice: "required",
		},
		{
			name: "tools present, explicit none",
			tools: []provider.ToolDefinition{
				{Name: "fs_ls", Description: "list dir", Parameters: json.RawMessage(`{"type":"object"}`)},
			},
			toolChoice: provider.ToolChoiceNone,
			wantChoice: "none",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := provider.Request{
				Model:      "test-model",
				Tools:      tt.tools,
				ToolChoice: tt.toolChoice,
			}

			params := translateRequest(req)

			if tt.wantChoice == "" {
				if !param.IsOmitted(params.ToolChoice.OfAuto) {
					t.Errorf("tool_choice should be omitted, got OfAuto=%q", params.ToolChoice.OfAuto.Value)
				}
			} else {
				if param.IsOmitted(params.ToolChoice.OfAuto) {
					t.Errorf("tool_choice should be %q, but was omitted", tt.wantChoice)
				} else if params.ToolChoice.OfAuto.Value != tt.wantChoice {
					t.Errorf("tool_choice = %q, want %q", params.ToolChoice.OfAuto.Value, tt.wantChoice)
				}
			}
		})
	}
}
