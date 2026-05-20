package hook

import (
	"context"
	"fmt"
	"time"
)

// HookDef is a parsed hook definition from config.
type HookDef struct {
	Name        string
	Event       EventType
	Command     CommandType
	Exec        string
	Timeout     time.Duration // default 30s if zero
	FailOpen    bool          // true = allow on error/timeout; false = deny
	ToolPattern string        // glob for tool name filtering (PreToolUse/PostToolUse only)
}

// Validate reports an error if the definition is unusable.
func (d HookDef) Validate() error {
	if d.Name == "" {
		return fmt.Errorf("hook: name is required")
	}
	if d.Exec == "" {
		return fmt.Errorf("hook %q: exec is required", d.Name)
	}
	return nil
}

// timeout returns the effective timeout, defaulting to 30s.
func (d HookDef) timeout() time.Duration {
	if d.Timeout > 0 {
		return d.Timeout
	}
	return 30 * time.Second
}

// HookResult is returned by an Executor after running a hook.
type HookResult struct {
	Action   Action
	Output   []byte // transformed payload (nil = no transform)
	Error    error
	Duration time.Duration
}

// Executor runs a single hook.
type Executor interface {
	Execute(ctx context.Context, payload []byte) (HookResult, error)
}

// Handler pairs a definition with its executor.
type Handler struct {
	def      HookDef
	executor Executor
}
