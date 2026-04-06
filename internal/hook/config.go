package hook

import (
	"fmt"
	"log/slog"
	"time"

	"somegit.dev/Owlibou/gnoma/internal/config"
)

// toolScopedEvents are the events where ToolPattern applies.
var toolScopedEvents = map[EventType]bool{
	PreToolUse:  true,
	PostToolUse: true,
}

// ParseHookDefs converts raw config.HookConfig entries to validated HookDefs.
func ParseHookDefs(cfgs []config.HookConfig) ([]HookDef, error) {
	defs := make([]HookDef, 0, len(cfgs))
	for i, c := range cfgs {
		event, err := ParseEventType(c.Event)
		if err != nil {
			return nil, fmt.Errorf("hook[%d] %q: %w", i, c.Name, err)
		}

		cmd, err := ParseCommandType(c.Type)
		if err != nil {
			return nil, fmt.Errorf("hook[%d] %q: %w", i, c.Name, err)
		}

		var timeout time.Duration
		if c.Timeout != "" {
			timeout, err = time.ParseDuration(c.Timeout)
			if err != nil {
				return nil, fmt.Errorf("hook[%d] %q: invalid timeout %q: %w", i, c.Name, c.Timeout, err)
			}
		}

		toolPattern := c.ToolPattern
		if toolPattern != "" && !toolScopedEvents[event] {
			slog.Warn("hook tool_pattern ignored for non-tool event",
				"hook", c.Name, "event", c.Event)
			toolPattern = ""
		}

		def := HookDef{
			Name:        c.Name,
			Event:       event,
			Command:     cmd,
			Exec:        c.Exec,
			Timeout:     timeout,
			FailOpen:    c.FailOpen,
			ToolPattern: toolPattern,
		}
		if err := def.Validate(); err != nil {
			return nil, fmt.Errorf("hook[%d]: %w", i, err)
		}

		defs = append(defs, def)
	}
	return defs, nil
}
