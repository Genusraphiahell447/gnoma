package stream

import (
	"encoding/json"
	"fmt"

	"somegit.dev/Owlibou/gnoma/internal/message"
)

// EventType discriminates streaming events.
type EventType int

const (
	EventTextDelta     EventType = iota + 1
	EventThinkingDelta
	EventToolCallStart
	EventToolCallDelta
	EventToolCallDone
	EventToolResult // tool execution output
	EventUsage
	EventError
)

func (et EventType) String() string {
	switch et {
	case EventTextDelta:
		return "text_delta"
	case EventThinkingDelta:
		return "thinking_delta"
	case EventToolCallStart:
		return "tool_call_start"
	case EventToolCallDelta:
		return "tool_call_delta"
	case EventToolCallDone:
		return "tool_call_done"
	case EventToolResult:
		return "tool_result"
	case EventUsage:
		return "usage"
	case EventError:
		return "error"
	default:
		return fmt.Sprintf("unknown(%d)", et)
	}
}

// Event is a single streaming event from a provider.
type Event struct {
	Type EventType

	// TextDelta, ThinkingDelta
	Text string

	// ToolCallStart: ID + Name set
	// ToolCallDelta: ID + ArgDelta set
	// ToolCallDone:  ID + Args set (complete JSON)
	ToolCallID   string
	ToolCallName string
	ArgDelta     string          // partial JSON fragment
	Args         json.RawMessage // complete arguments (on Done)

	// ToolResult: tool name + output
	ToolName   string
	ToolOutput string

	// Usage
	Usage *message.Usage

	// Error
	Err error

	// StopReason — set on the final event of a stream
	StopReason message.StopReason

	// Model — set on first event if available
	Model string
}
