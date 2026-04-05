package session

import (
	"somegit.dev/Owlibou/gnoma/internal/engine"
	"somegit.dev/Owlibou/gnoma/internal/stream"
)

// SessionState tracks the current state of a session.
type SessionState int

const (
	StateIdle SessionState = iota
	StateStreaming
	StateToolExec
	StateCancelled
	StateError
	StateClosed
)

func (s SessionState) String() string {
	switch s {
	case StateIdle:
		return "idle"
	case StateStreaming:
		return "streaming"
	case StateToolExec:
		return "tool_exec"
	case StateCancelled:
		return "cancelled"
	case StateError:
		return "error"
	case StateClosed:
		return "closed"
	default:
		return "unknown"
	}
}

// Status holds observable session state.
type Status struct {
	State        SessionState
	Provider     string
	Model        string
	TokensUsed   int64
	TokensMax    int64
	TokenPercent int // 0-100
	TokenState   string // "ok", "warning", "critical"
	TurnCount    int
}

// Session is the boundary between UI and engine.
// All communication is via channels. No shared mutable state.
type Session interface {
	// Send submits user input and begins an agentic turn.
	Send(input string) error
	// SendWithOptions is like Send but applies per-turn engine options.
	SendWithOptions(input string, opts engine.TurnOptions) error
	// Events returns the channel that receives streaming events.
	// A new channel is created per Send(). Closed when the turn completes.
	Events() <-chan stream.Event
	// TurnResult returns the completed Turn after Events() is drained.
	TurnResult() (*engine.Turn, error)
	// Cancel aborts the current turn.
	Cancel()
	// Close shuts down the session.
	Close() error
	// Status returns current session state.
	Status() Status
	// SessionID returns the persistent identifier for this session.
	SessionID() string
}
