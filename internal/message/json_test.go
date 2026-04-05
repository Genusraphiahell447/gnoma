package message_test

import (
	"encoding/json"
	"testing"

	"somegit.dev/Owlibou/gnoma/internal/message"
)

func TestContent_MarshalJSON_Text(t *testing.T) {
	c := message.NewTextContent("hello world")
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var got message.Content
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Type != message.ContentText || got.Text != "hello world" {
		t.Errorf("round-trip failed: %+v", got)
	}
}

func TestContent_MarshalJSON_ToolCall(t *testing.T) {
	c := message.NewToolCallContent(message.ToolCall{
		ID:        "tc_1",
		Name:      "bash",
		Arguments: json.RawMessage(`{"command":"ls"}`),
	})
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var got message.Content
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Type != message.ContentToolCall || got.ToolCall == nil {
		t.Fatalf("round-trip failed: %+v", got)
	}
	if got.ToolCall.ID != "tc_1" || got.ToolCall.Name != "bash" {
		t.Errorf("tool call fields wrong: %+v", got.ToolCall)
	}
	if string(got.ToolCall.Arguments) != `{"command":"ls"}` {
		t.Errorf("arguments wrong: %s", got.ToolCall.Arguments)
	}
}

func TestContent_MarshalJSON_ToolResult(t *testing.T) {
	c := message.NewToolResultContent(message.ToolResult{
		ToolCallID: "tc_1",
		Content:    "output",
		IsError:    false,
	})
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var got message.Content
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Type != message.ContentToolResult || got.ToolResult == nil {
		t.Fatalf("round-trip failed: %+v", got)
	}
	if got.ToolResult.ToolCallID != "tc_1" || got.ToolResult.Content != "output" {
		t.Errorf("tool result fields wrong: %+v", got.ToolResult)
	}
}

func TestContent_MarshalJSON_ToolResult_IsError(t *testing.T) {
	c := message.NewToolResultContent(message.ToolResult{
		ToolCallID: "tc_2",
		Content:    "error msg",
		IsError:    true,
	})
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var got message.Content
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if !got.ToolResult.IsError {
		t.Errorf("IsError not preserved")
	}
}

func TestContent_MarshalJSON_Thinking(t *testing.T) {
	c := message.NewThinkingContent(message.Thinking{
		Text:      "let me think",
		Signature: "sig_abc",
		Redacted:  false,
	})
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var got message.Content
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Type != message.ContentThinking || got.Thinking == nil {
		t.Fatalf("round-trip failed: %+v", got)
	}
	if got.Thinking.Text != "let me think" || got.Thinking.Signature != "sig_abc" {
		t.Errorf("thinking fields wrong: %+v", got.Thinking)
	}
}

func TestContent_UnmarshalJSON_UnknownType(t *testing.T) {
	data := []byte(`{"type":"unknown_xyz","text":"hi"}`)
	var got message.Content
	err := json.Unmarshal(data, &got)
	if err == nil {
		t.Error("expected error for unknown type, got nil")
	}
}

func TestMessage_RoundTrip(t *testing.T) {
	msg := message.NewUserText("hello")
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	var got message.Message
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Role != message.RoleUser || len(got.Content) != 1 || got.Content[0].Text != "hello" {
		t.Errorf("message round-trip failed: %+v", got)
	}
}

func TestMessages_Slice_RoundTrip(t *testing.T) {
	msgs := []message.Message{
		message.NewUserText("question"),
		message.NewAssistantContent(
			message.NewTextContent("answer"),
			message.NewToolCallContent(message.ToolCall{
				ID:        "tc_1",
				Name:      "bash",
				Arguments: json.RawMessage(`{}`),
			}),
		),
		message.NewToolResults(message.ToolResult{
			ToolCallID: "tc_1",
			Content:    "result",
		}),
	}
	data, err := json.Marshal(msgs)
	if err != nil {
		t.Fatal(err)
	}
	var got []message.Message
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(got))
	}
	if got[1].Content[1].Type != message.ContentToolCall {
		t.Errorf("tool call content type wrong: %v", got[1].Content[1].Type)
	}
}
