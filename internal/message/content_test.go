package message

import (
	"encoding/json"
	"testing"
)

func TestNewTextContent(t *testing.T) {
	c := NewTextContent("hello world")

	if c.Type != ContentText {
		t.Errorf("Type = %v, want %v", c.Type, ContentText)
	}
	if c.Text != "hello world" {
		t.Errorf("Text = %q, want %q", c.Text, "hello world")
	}
	if c.ToolCall != nil {
		t.Error("ToolCall should be nil for text content")
	}
	if c.ToolResult != nil {
		t.Error("ToolResult should be nil for text content")
	}
	if c.Thinking != nil {
		t.Error("Thinking should be nil for text content")
	}
}

func TestNewToolCallContent(t *testing.T) {
	args := json.RawMessage(`{"command":"ls -la"}`)
	tc := ToolCall{
		ID:        "tc_001",
		Name:      "bash",
		Arguments: args,
	}
	c := NewToolCallContent(tc)

	if c.Type != ContentToolCall {
		t.Errorf("Type = %v, want %v", c.Type, ContentToolCall)
	}
	if c.ToolCall == nil {
		t.Fatal("ToolCall should not be nil")
	}
	if c.ToolCall.ID != "tc_001" {
		t.Errorf("ToolCall.ID = %q, want %q", c.ToolCall.ID, "tc_001")
	}
	if c.ToolCall.Name != "bash" {
		t.Errorf("ToolCall.Name = %q, want %q", c.ToolCall.Name, "bash")
	}
	if string(c.ToolCall.Arguments) != `{"command":"ls -la"}` {
		t.Errorf("ToolCall.Arguments = %s, want %s", c.ToolCall.Arguments, args)
	}
	if c.Text != "" {
		t.Error("Text should be empty for tool call content")
	}
}

func TestNewToolResultContent(t *testing.T) {
	tr := ToolResult{
		ToolCallID: "tc_001",
		Content:    "file1.go\nfile2.go",
		IsError:    false,
	}
	c := NewToolResultContent(tr)

	if c.Type != ContentToolResult {
		t.Errorf("Type = %v, want %v", c.Type, ContentToolResult)
	}
	if c.ToolResult == nil {
		t.Fatal("ToolResult should not be nil")
	}
	if c.ToolResult.ToolCallID != "tc_001" {
		t.Errorf("ToolResult.ToolCallID = %q, want %q", c.ToolResult.ToolCallID, "tc_001")
	}
	if c.ToolResult.IsError {
		t.Error("ToolResult.IsError should be false")
	}
}

func TestNewToolResultContent_Error(t *testing.T) {
	tr := ToolResult{
		ToolCallID: "tc_002",
		Content:    "permission denied",
		IsError:    true,
	}
	c := NewToolResultContent(tr)

	if !c.ToolResult.IsError {
		t.Error("ToolResult.IsError should be true")
	}
	if c.ToolResult.Content != "permission denied" {
		t.Errorf("ToolResult.Content = %q, want %q", c.ToolResult.Content, "permission denied")
	}
}

func TestNewThinkingContent(t *testing.T) {
	th := Thinking{
		Text:      "Let me think about this...",
		Signature: "sig_abc123",
	}
	c := NewThinkingContent(th)

	if c.Type != ContentThinking {
		t.Errorf("Type = %v, want %v", c.Type, ContentThinking)
	}
	if c.Thinking == nil {
		t.Fatal("Thinking should not be nil")
	}
	if c.Thinking.Text != "Let me think about this..." {
		t.Errorf("Thinking.Text = %q", c.Thinking.Text)
	}
	if c.Thinking.Signature != "sig_abc123" {
		t.Errorf("Thinking.Signature = %q", c.Thinking.Signature)
	}
	if c.Thinking.Redacted {
		t.Error("Thinking.Redacted should be false")
	}
}

func TestNewRedactedThinkingContent(t *testing.T) {
	th := Thinking{
		Redacted: true,
	}
	c := NewThinkingContent(th)

	if !c.Thinking.Redacted {
		t.Error("Thinking.Redacted should be true")
	}
}

func TestContentType_String(t *testing.T) {
	tests := []struct {
		ct   ContentType
		want string
	}{
		{ContentText, "text"},
		{ContentToolCall, "tool_call"},
		{ContentToolResult, "tool_result"},
		{ContentThinking, "thinking"},
		{ContentType(99), "unknown(99)"},
	}
	for _, tt := range tests {
		if got := tt.ct.String(); got != tt.want {
			t.Errorf("ContentType(%d).String() = %q, want %q", tt.ct, got, tt.want)
		}
	}
}

func TestToolCall_JSON_RoundTrip(t *testing.T) {
	original := ToolCall{
		ID:        "tc_100",
		Name:      "fs.read",
		Arguments: json.RawMessage(`{"path":"/tmp/test.go","offset":0}`),
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded ToolCall
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if decoded.ID != original.ID {
		t.Errorf("ID = %q, want %q", decoded.ID, original.ID)
	}
	if decoded.Name != original.Name {
		t.Errorf("Name = %q, want %q", decoded.Name, original.Name)
	}
	if string(decoded.Arguments) != string(original.Arguments) {
		t.Errorf("Arguments = %s, want %s", decoded.Arguments, original.Arguments)
	}
}
