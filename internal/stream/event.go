package stream

import (
	"encoding/json"
	"fmt"

	"somegit.dev/Owlibou/gnoma/internal/message"
)

// EventType discriminates streaming events.
type EventType int

const (
	EventTextDelta EventType = iota + 1
	EventThinkingDelta
	EventToolCallStart
	EventToolCallDelta
	EventToolCallDone
	EventToolResult    // tool execution output
	EventPermissionReq // permission prompt needed
	EventUsage
	EventRouting  // router arm selection
	EventFailover // arm swap after a stream error (no content was emitted)
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
	case EventPermissionReq:
		return "permission_req"
	case EventUsage:
		return "usage"
	case EventRouting:
		return "routing"
	case EventFailover:
		return "failover"
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

	// PermissionReq: tool requesting permission, response channel
	PermissionResponse chan bool

	// Usage
	Usage *message.Usage

	// Routing — arm selected by router
	RoutingModel      string // e.g. "anthropic/claude-sonnet-4-20250514"
	RoutingTask       string // classified task type
	RoutingClassifier string // classifier source: heuristic / slm / slm_fallback

	// Failover — set on EventFailover. FailedArm is the arm whose stream
	// errored out before producing content; FailedReason is a short message
	// suitable for TUI display (the underlying error is in Err). RoutingModel
	// holds the arm being tried next.
	FailedArm    string
	FailedReason string

	// Error
	Err error

	// StopReason — set on the final event of a stream
	StopReason message.StopReason

	// Model — set on first event if available
	Model string
}
