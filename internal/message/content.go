package message

import (
	"encoding/json"
	"fmt"
)

// ContentType discriminates the content block union.
type ContentType int

const (
	ContentText       ContentType = iota + 1
	ContentToolCall
	ContentToolResult
	ContentThinking
)

func (ct ContentType) String() string {
	switch ct {
	case ContentText:
		return "text"
	case ContentToolCall:
		return "tool_call"
	case ContentToolResult:
		return "tool_result"
	case ContentThinking:
		return "thinking"
	default:
		return fmt.Sprintf("unknown(%d)", ct)
	}
}

// Content is a discriminated union. Exactly one payload field is set per Type.
type Content struct {
	Type       ContentType
	Text       string      // ContentText
	ToolCall   *ToolCall   // ContentToolCall
	ToolResult *ToolResult // ContentToolResult
	Thinking   *Thinking   // ContentThinking
}

// ToolCall represents the model's request to invoke a tool.
type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// ToolResult is the output of executing a tool, correlated by ToolCallID.
type ToolResult struct {
	ToolCallID string `json:"tool_call_id"`
	Content    string `json:"content"`
	IsError    bool   `json:"is_error"`
}

// Thinking represents a reasoning/thinking trace.
// Signature must round-trip unchanged (Anthropic requirement).
type Thinking struct {
	Text      string `json:"text,omitempty"`
	Signature string `json:"signature,omitempty"`
	Redacted  bool   `json:"redacted,omitempty"`
}

func NewTextContent(text string) Content {
	return Content{Type: ContentText, Text: text}
}

func NewToolCallContent(tc ToolCall) Content {
	return Content{Type: ContentToolCall, ToolCall: &tc}
}

func NewToolResultContent(tr ToolResult) Content {
	return Content{Type: ContentToolResult, ToolResult: &tr}
}

func NewThinkingContent(th Thinking) Content {
	return Content{Type: ContentThinking, Thinking: &th}
}
