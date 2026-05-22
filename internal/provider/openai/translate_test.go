package openai

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"somegit.dev/Owlibou/gnoma/internal/message"
	"somegit.dev/Owlibou/gnoma/internal/provider"

	"github.com/openai/openai-go/packages/param"
)

func TestTranslateMessage_UserTextOnly_UsesStringContent(t *testing.T) {
	m := message.NewUserText("hello")
	out := translateMessage(m)
	if len(out) != 1 {
		t.Fatalf("got %d messages, want 1", len(out))
	}
	user := out[0].OfUser
	if user == nil {
		t.Fatal("expected OfUser to be set")
	}
	if user.Content.OfString.Value != "hello" {
		t.Errorf("OfString = %q, want %q", user.Content.OfString.Value, "hello")
	}
	if len(user.Content.OfArrayOfContentParts) != 0 {
		t.Errorf("OfArrayOfContentParts should be empty when no image, got %d parts", len(user.Content.OfArrayOfContentParts))
	}
}

func TestTranslateMessage_UserWithImage_EmitsContentParts(t *testing.T) {
	pngBytes := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	m := message.Message{
		Role: message.RoleUser,
		Content: []message.Content{
			message.NewTextContent("what is this?"),
			message.NewImageContent(message.Image{
				Data:      pngBytes,
				MediaType: "image/png",
				Path:      "/tmp/x.png",
			}),
		},
	}
	out := translateMessage(m)
	if len(out) != 1 {
		t.Fatalf("got %d messages, want 1", len(out))
	}
	user := out[0].OfUser
	if user == nil {
		t.Fatal("expected OfUser to be set")
	}
	parts := user.Content.OfArrayOfContentParts
	if len(parts) != 2 {
		t.Fatalf("got %d content parts, want 2 (text + image)", len(parts))
	}
	gotText := parts[0].GetText()
	if gotText == nil || *gotText != "what is this?" {
		t.Errorf("first part should be text %q, got %v", "what is this?", gotText)
	}
	gotImg := parts[1].GetImageURL()
	if gotImg == nil {
		t.Fatal("second part should be image")
	}
	wantPrefix := "data:image/png;base64,"
	if !strings.HasPrefix(gotImg.URL, wantPrefix) {
		t.Errorf("image URL %q should start with %q", gotImg.URL, wantPrefix)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(gotImg.URL, wantPrefix))
	if err != nil {
		t.Fatalf("base64 decode: %v", err)
	}
	if string(decoded) != string(pngBytes) {
		t.Error("decoded image bytes do not match original")
	}
}

func TestBuildUserContentParts_DropsEmptyImage(t *testing.T) {
	blocks := []message.Content{
		message.NewTextContent("a"),
		{Type: message.ContentImage, Image: nil},
		message.NewTextContent("b"),
	}
	parts := buildUserContentParts(blocks)
	if len(parts) != 1 {
		t.Fatalf("got %d parts, want 1 (adjacent text concatenated, nil image dropped)", len(parts))
	}
	if got := parts[0].GetText(); got == nil || *got != "ab" {
		t.Errorf("merged text = %v, want %q", got, "ab")
	}
}

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
