package engine

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	gnomactx "somegit.dev/Owlibou/gnoma/internal/context"
	"somegit.dev/Owlibou/gnoma/internal/hook"
	"somegit.dev/Owlibou/gnoma/internal/message"
	"somegit.dev/Owlibou/gnoma/internal/permission"
	"somegit.dev/Owlibou/gnoma/internal/provider"
	"somegit.dev/Owlibou/gnoma/internal/router"
	"somegit.dev/Owlibou/gnoma/internal/security"
	"somegit.dev/Owlibou/gnoma/internal/tool"
	"somegit.dev/Owlibou/gnoma/internal/tool/persist"
)

// Config holds engine configuration.
type Config struct {
	Provider    provider.Provider        // direct provider (used if Router is nil)
	Router      *router.Router           // nil = use Provider directly
	Classifier  router.TaskClassifier    // nil = HeuristicClassifier
	Tools       *tool.Registry
	Firewall    *security.Firewall       // nil = no scanning
	Permissions *permission.Checker      // nil = allow all
	Context     *gnomactx.Window         // nil = no compaction
	System      string                   // system prompt
	Model       string                   // override model (empty = provider default)
	Temperature *float64                 // nil = provider default
	MaxTurns    int                      // safety limit on tool loops (0 = unlimited)
	Store       *persist.Store           // nil = no result persistence
	Hooks       *hook.Dispatcher         // nil = no hooks
	Logger      *slog.Logger
}

func (c Config) validate() error {
	if c.Provider == nil {
		return fmt.Errorf("engine: provider required")
	}
	if c.Tools == nil {
		return fmt.Errorf("engine: tool registry required")
	}
	return nil
}

// Turn is the result of a complete agentic turn (may span multiple API calls).
type Turn struct {
	Messages []message.Message // all messages produced (assistant + tool results)
	Usage    message.Usage     // cumulative for all API calls in this turn
	Rounds   int               // number of API round-trips
}

// TurnOptions carries per-turn overrides that apply for a single Submit call.
type TurnOptions struct {
	ToolChoice   provider.ToolChoiceMode // "" = use provider default
	AllowedTools []string                // if non-nil, only these tools are sent (matched by name)
	AllowedPaths []string                // if non-nil, tool filesystem access is restricted to these paths
}

// Engine orchestrates the conversation.
//
// Mutable state (history, usage, activatedTools, modelCaps, turnOpts, and the
// hot fields of cfg — Provider/Model) is guarded by mu. The lock is released
// across blocking provider.Stream calls so external setters can interleave.
type Engine struct {
	mu      sync.Mutex
	cfg     Config
	history []message.Message
	usage   message.Usage
	logger  *slog.Logger

	modelCaps    *provider.Capabilities
	modelCapsFor string

	activatedTools map[string]bool

	turnOpts TurnOptions
}

// ToolsAvailable reports whether the current model supports tool calling.
func (e *Engine) ToolsAvailable() bool {
	return e.forcedArmSupportsTools()
}

// forcedArmSupportsTools returns true if tool definitions should be included
// in the request. When the router has a forced arm, checks its ToolUse
// capability. Returns true for multi-arm routing (feasibility filter handles it)
// or when no router is configured.
func (e *Engine) forcedArmSupportsTools() bool {
	if e.cfg.Router == nil {
		return true
	}
	id := e.cfg.Router.ForcedArm()
	if id == "" {
		return true // multi-arm routing: router handles feasibility
	}
	arm, ok := e.cfg.Router.LookupArm(id)
	if !ok {
		if e.logger != nil {
			e.logger.Debug("forced arm not found in router, assuming tool support", "arm", id)
		}
		return true
	}
	if e.logger != nil {
		e.logger.Debug("forced arm tool support check",
			"arm", id,
			"tool_use", arm.Capabilities.ToolUse,
		)
	}
	return arm.Capabilities.ToolUse
}

// isLocalArm returns true if the forced arm is a local provider (Ollama, llama.cpp).
func (e *Engine) isLocalArm() bool {
	if e.cfg.Router == nil {
		return false
	}
	id := e.cfg.Router.ForcedArm()
	if id == "" {
		return false
	}
	arm, ok := e.cfg.Router.LookupArm(id)
	if !ok {
		return false
	}
	return arm.IsLocal
}

// New creates an engine.
func New(cfg Config) (*Engine, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Engine{
		cfg:            cfg,
		logger:         logger,
		activatedTools: make(map[string]bool),
	}, nil
}

// resolveCapabilities returns the capabilities for the active model.
// Caches the result — re-resolves if the model changes.
func (e *Engine) resolveCapabilities(ctx context.Context) *provider.Capabilities {
	e.mu.Lock()
	model := e.cfg.Model
	if model == "" {
		model = e.cfg.Provider.DefaultModel()
	}
	if e.modelCaps != nil && e.modelCapsFor == model {
		caps := e.modelCaps
		e.mu.Unlock()
		return caps
	}
	prov := e.cfg.Provider
	e.mu.Unlock()

	models, err := prov.Models(ctx)
	if err != nil {
		e.logger.Debug("failed to fetch model capabilities", "error", err)
		return nil
	}

	for _, m := range models {
		if m.ID == model {
			e.mu.Lock()
			e.modelCaps = &m.Capabilities
			e.modelCapsFor = model
			caps := e.modelCaps
			e.mu.Unlock()
			return caps
		}
	}

	e.logger.Debug("model not found in provider model list", "model", model)
	return nil
}

// History returns a snapshot copy of the conversation.
func (e *Engine) History() []message.Message {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]message.Message, len(e.history))
	copy(out, e.history)
	return out
}

// ContextWindow returns the context window (may be nil).
func (e *Engine) ContextWindow() *gnomactx.Window {
	return e.cfg.Context
}

// InjectMessage appends a message to conversation history without triggering a turn.
// Used for system notifications (permission mode changes, incognito toggles) that
// the model should see as context in subsequent turns.
func (e *Engine) InjectMessage(msg message.Message) {
	e.mu.Lock()
	e.history = append(e.history, msg)
	e.mu.Unlock()
	if e.cfg.Context != nil {
		e.cfg.Context.AppendMessage(msg)
	}
}

// Usage returns cumulative token usage.
func (e *Engine) Usage() message.Usage {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.usage
}

// SetProvider swaps the active provider (for dynamic switching).
func (e *Engine) SetProvider(p provider.Provider) {
	e.mu.Lock()
	e.cfg.Provider = p
	e.mu.Unlock()
}

// SetModel changes the model within the current provider.
func (e *Engine) SetModel(model string) {
	e.mu.Lock()
	e.cfg.Model = model
	e.mu.Unlock()
}

// SetHistory replaces the conversation history (for session restore).
// Also syncs the context window and re-estimates the tracker's token count.
func (e *Engine) SetHistory(msgs []message.Message) {
	e.mu.Lock()
	e.history = msgs
	e.mu.Unlock()
	if e.cfg.Context != nil {
		e.cfg.Context.SetMessages(msgs)
		e.cfg.Context.Tracker().Set(e.cfg.Context.Tracker().CountMessages(msgs))
	}
}

// SetUsage sets cumulative token usage (for session restore).
func (e *Engine) SetUsage(u message.Usage) {
	e.mu.Lock()
	e.usage = u
	e.mu.Unlock()
}

// SetActivatedTools restores the set of activated deferred tools (for session restore).
func (e *Engine) SetActivatedTools(tools map[string]bool) {
	e.mu.Lock()
	e.activatedTools = tools
	e.mu.Unlock()
}

// classify returns a Task for the given prompt using the configured classifier.
// Falls back to HeuristicClassifier if none is configured or if classification fails.
func (e *Engine) classify(ctx context.Context, prompt string) router.Task {
	cls := e.cfg.Classifier
	if cls == nil {
		cls = router.HeuristicClassifier{}
	}
	e.mu.Lock()
	histSnap := make([]message.Message, len(e.history))
	copy(histSnap, e.history)
	e.mu.Unlock()
	task, err := cls.Classify(ctx, prompt, histSnap)
	if err != nil {
		e.logger.Debug("classifier error, falling back to heuristic", "error", err)
		return router.ClassifyTask(prompt)
	}
	return task
}

// latestUserPrompt returns the text of the most recent user message.
func (e *Engine) latestUserPrompt() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := len(e.history) - 1; i >= 0; i-- {
		if e.history[i].Role == message.RoleUser {
			return e.history[i].TextContent()
		}
	}
	return ""
}

// historySnapshot returns a copy of the current history slice.
func (e *Engine) historySnapshot() []message.Message {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]message.Message, len(e.history))
	copy(out, e.history)
	return out
}

// appendHistory appends a message under the lock.
func (e *Engine) appendHistory(msg message.Message) {
	e.mu.Lock()
	e.history = append(e.history, msg)
	e.mu.Unlock()
}

// replaceHistory swaps the history slice (used after context compaction).
func (e *Engine) replaceHistory(msgs []message.Message) {
	e.mu.Lock()
	e.history = msgs
	e.mu.Unlock()
}

// addUsage accumulates token usage.
func (e *Engine) addUsage(u message.Usage) {
	e.mu.Lock()
	e.usage.Add(u)
	e.mu.Unlock()
}

// activeProvider returns the current provider under lock.
func (e *Engine) activeProvider() provider.Provider {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.cfg.Provider
}

// activeModel returns the configured model name under lock.
func (e *Engine) activeModel() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.cfg.Model
}

// snapshotTurnOpts returns a copy of the current per-turn options.
func (e *Engine) snapshotTurnOpts() TurnOptions {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.turnOpts
}

// markToolActivated records that a deferred tool has been requested.
func (e *Engine) markToolActivated(name string) {
	e.mu.Lock()
	e.activatedTools[name] = true
	e.mu.Unlock()
}

// isToolActivated reports whether a deferred tool has been activated.
func (e *Engine) isToolActivated(name string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.activatedTools[name]
}

// Reset clears conversation history and usage.
func (e *Engine) Reset() {
	e.mu.Lock()
	e.history = nil
	e.usage = message.Usage{}
	e.activatedTools = make(map[string]bool)
	e.mu.Unlock()
	if e.cfg.Context != nil {
		e.cfg.Context.Reset()
	}
}
