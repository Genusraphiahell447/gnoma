package message

import (
	"encoding/json"
	"fmt"
)

// MarshalJSON encodes Content as {"type":"<name>","<field>":...}.
func (c Content) MarshalJSON() ([]byte, error) {
	switch c.Type {
	case ContentText:
		return json.Marshal(struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}{Type: "text", Text: c.Text})
	case ContentToolCall:
		return json.Marshal(struct {
			Type     string    `json:"type"`
			ToolCall *ToolCall `json:"tool_call"`
		}{Type: "tool_call", ToolCall: c.ToolCall})
	case ContentToolResult:
		return json.Marshal(struct {
			Type       string      `json:"type"`
			ToolResult *ToolResult `json:"tool_result"`
		}{Type: "tool_result", ToolResult: c.ToolResult})
	case ContentThinking:
		return json.Marshal(struct {
			Type     string    `json:"type"`
			Thinking *Thinking `json:"thinking"`
		}{Type: "thinking", Thinking: c.Thinking})
	default:
		return nil, fmt.Errorf("message: unknown ContentType %d", c.Type)
	}
}

// UnmarshalJSON decodes Content from {"type":"<name>","<field>":...}.
func (c *Content) UnmarshalJSON(data []byte) error {
	// First pass: extract the type discriminant.
	var disc struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &disc); err != nil {
		return fmt.Errorf("message: unmarshal content type: %w", err)
	}

	// Second pass: decode the payload for the known type.
	switch disc.Type {
	case "text":
		var v struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(data, &v); err != nil {
			return err
		}
		c.Type = ContentText
		c.Text = v.Text
	case "tool_call":
		var v struct {
			ToolCall *ToolCall `json:"tool_call"`
		}
		if err := json.Unmarshal(data, &v); err != nil {
			return err
		}
		c.Type = ContentToolCall
		c.ToolCall = v.ToolCall
	case "tool_result":
		var v struct {
			ToolResult *ToolResult `json:"tool_result"`
		}
		if err := json.Unmarshal(data, &v); err != nil {
			return err
		}
		c.Type = ContentToolResult
		c.ToolResult = v.ToolResult
	case "thinking":
		var v struct {
			Thinking *Thinking `json:"thinking"`
		}
		if err := json.Unmarshal(data, &v); err != nil {
			return err
		}
		c.Type = ContentThinking
		c.Thinking = v.Thinking
	default:
		return fmt.Errorf("message: unknown content type %q", disc.Type)
	}
	return nil
}
