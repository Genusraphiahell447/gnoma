package hook

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
)

// Dispatcher manages hook handler chains and fires them on events.
type Dispatcher struct {
	chains map[EventType][]Handler
	logger *slog.Logger
}

// SetChain replaces the handler chain for an event. Primarily for testing.
func (d *Dispatcher) SetChain(event EventType, handlers []Handler) {
	if d.chains == nil {
		d.chains = make(map[EventType][]Handler)
	}
	d.chains[event] = handlers
}

// NewHandler constructs a Handler from a definition and executor.
func NewHandler(def HookDef, ex Executor) Handler {
	return Handler{def: def, executor: ex}
}

// NewDispatcher validates defs, constructs the appropriate executor per
// CommandType, and groups handlers by EventType.
// streamer and spawnFn may be nil if no prompt/agent hooks are configured.
func NewDispatcher(defs []HookDef, streamer Streamer, spawnFn ElfSpawnFn, logger *slog.Logger) (*Dispatcher, error) {
	if logger == nil {
		logger = slog.Default()
	}
	d := &Dispatcher{
		chains: make(map[EventType][]Handler),
		logger: logger,
	}
	for _, def := range defs {
		if err := def.Validate(); err != nil {
			return nil, fmt.Errorf("hook.NewDispatcher: %w", err)
		}
		ex, err := buildExecutor(def, streamer, spawnFn)
		if err != nil {
			return nil, fmt.Errorf("hook.NewDispatcher: building executor for %q: %w", def.Name, err)
		}
		d.chains[def.Event] = append(d.chains[def.Event], Handler{def: def, executor: ex})
	}
	return d, nil
}

// buildExecutor constructs the right Executor for a HookDef.
func buildExecutor(def HookDef, streamer Streamer, spawnFn ElfSpawnFn) (Executor, error) {
	switch def.Command {
	case CommandTypeShell:
		return NewCommandExecutor(def), nil
	case CommandTypePrompt:
		if streamer == nil {
			return nil, fmt.Errorf("prompt hook %q requires a Streamer (no router configured)", def.Name)
		}
		return NewPromptExecutor(def, streamer), nil
	case CommandTypeAgent:
		if spawnFn == nil {
			return nil, fmt.Errorf("agent hook %q requires an ElfSpawnFn (no elf manager configured)", def.Name)
		}
		return NewAgentExecutor(def, spawnFn), nil
	default:
		return nil, fmt.Errorf("unknown command type %v", def.Command)
	}
}

// Fire runs all handlers registered for event, in order.
// Returns the (possibly transformed) payload, the aggregate Action, and the first error.
// Safe to call on a nil *Dispatcher — returns (payload, Allow, nil).
func (d *Dispatcher) Fire(event EventType, payload []byte) ([]byte, Action, error) {
	if d == nil {
		return payload, Allow, nil
	}

	handlers := d.chains[event]
	if len(handlers) == 0 {
		return payload, Allow, nil
	}

	results := make([]HookResult, 0, len(handlers))
	current := payload
	var firstErr error

	logger := d.logger
	if logger == nil {
		logger = slog.Default()
	}

	for _, h := range handlers {
		// For tool-scoped events, skip handlers whose ToolPattern doesn't match.
		if (event == PreToolUse || event == PostToolUse) && h.def.ToolPattern != "" {
			toolName := ExtractToolName(current)
			matched, _ := filepath.Match(h.def.ToolPattern, toolName)
			if !matched {
				continue
			}
		}

		ctx, cancel := context.WithTimeout(context.Background(), h.def.timeout())
		result, err := h.executor.Execute(ctx, current)
		cancel()

		if err != nil {
			logger.Warn("hook executor error", "hook", h.def.Name, "error", err)
			if firstErr == nil {
				firstErr = err
			}
			// Apply fail_open policy: treat as Deny or Allow.
			if h.def.FailOpen {
				result.Action = Allow
			} else {
				result.Action = Deny
			}
			result.Output = nil
		}

		// Chain transforms: if this handler produced output, pass it forward.
		if len(result.Output) > 0 {
			current = result.Output
		}

		results = append(results, result)
	}

	action := resolveAction(results, event)
	return current, action, firstErr
}

// resolveAction aggregates handler results into a final Action.
// Rules:
//   - PostToolUse Deny is treated as Skip (execution already happened).
//   - Any Deny → final Deny.
//   - Skip abstains (doesn't count as a vote either way).
//   - All remaining Allow (or empty / all-Skip) → Allow.
func resolveAction(results []HookResult, event EventType) Action {
	for _, r := range results {
		if r.Action == Deny && event == PostToolUse {
			continue // Deny on PostToolUse is meaningless — tool already ran
		}
		if r.Action == Deny {
			return Deny
		}
	}
	return Allow
}
