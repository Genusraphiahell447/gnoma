package message

import "strings"

// Role identifies the sender of a message.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleSystem    Role = "system"
)

// Message represents a single turn in the conversation.
type Message struct {
	Role    Role      `json:"role"`
	Content []Content `json:"content"`
}

func NewUserText(text string) Message {
	return Message{
		Role:    RoleUser,
		Content: []Content{NewTextContent(text)},
	}
}

func NewAssistantText(text string) Message {
	return Message{
		Role:    RoleAssistant,
		Content: []Content{NewTextContent(text)},
	}
}

func NewAssistantContent(blocks ...Content) Message {
	return Message{
		Role:    RoleAssistant,
		Content: blocks,
	}
}

func NewSystemText(text string) Message {
	return Message{
		Role:    RoleSystem,
		Content: []Content{NewTextContent(text)},
	}
}

func NewToolResults(results ...ToolResult) Message {
	content := make([]Content, len(results))
	for i, r := range results {
		content[i] = NewToolResultContent(r)
	}
	return Message{
		Role:    RoleUser,
		Content: content,
	}
}

// HasToolCalls returns true if any content block is a tool call.
func (m Message) HasToolCalls() bool {
	for _, c := range m.Content {
		if c.Type == ContentToolCall {
			return true
		}
	}
	return false
}

// ToolCalls extracts all tool call blocks.
func (m Message) ToolCalls() []ToolCall {
	var calls []ToolCall
	for _, c := range m.Content {
		if c.Type == ContentToolCall && c.ToolCall != nil {
			calls = append(calls, *c.ToolCall)
		}
	}
	return calls
}

// TextContent concatenates all text blocks.
func (m Message) TextContent() string {
	var b strings.Builder
	for _, c := range m.Content {
		if c.Type == ContentText {
			b.WriteString(c.Text)
		}
	}
	return b.String()
}
