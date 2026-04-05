package engine

import (
	"context"
	"fmt"
	"log/slog"

	gnomactx "somegit.dev/Owlibou/gnoma/internal/context"
	"somegit.dev/Owlibou/gnoma/internal/message"
	"somegit.dev/Owlibou/gnoma/internal/permission"
	"somegit.dev/Owlibou/gnoma/internal/provider"
	"somegit.dev/Owlibou/gnoma/internal/router"
	"somegit.dev/Owlibou/gnoma/internal/security"
	"somegit.dev/Owlibou/gnoma/internal/tool"
)

// Config holds engine configuration.
type Config struct {
	Provider provider.Provider  // direct provider (used if Router is nil)
	Router   *router.Router     // nil = use Provider directly
	Tools    *tool.Registry
	Firewall    *security.Firewall    // nil = no scanning
	Permissions *permission.Checker  // nil = allow all
	Context     *gnomactx.Window     // nil = no compaction
	System      string               // system prompt
	Model    string             // override model (empty = provider default)
	MaxTurns int                // safety limit on tool loops (0 = unlimited)
	Logger   *slog.Logger
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
	ToolChoice provider.ToolChoiceMode // "" = use provider default
}

// Engine orchestrates the conversation.
type Engine struct {
	cfg     Config
	history []message.Message
	usage   message.Usage
	logger  *slog.Logger

	// Cached model capabilities, resolved lazily
	modelCaps    *provider.Capabilities
	modelCapsFor string // model ID the cached caps are for

	// Deferred tool loading: tools with ShouldDefer() are excluded until
	// the model requests them. Activated on first use.
	activatedTools map[string]bool

	// Per-turn options, set for the duration of SubmitWithOptions.
	turnOpts TurnOptions
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
	model := e.cfg.Model
	if model == "" {
		model = e.cfg.Provider.DefaultModel()
	}

	// Return cached if same model
	if e.modelCaps != nil && e.modelCapsFor == model {
		return e.modelCaps
	}

	// Query provider for model list
	models, err := e.cfg.Provider.Models(ctx)
	if err != nil {
		e.logger.Debug("failed to fetch model capabilities", "error", err)
		return nil
	}

	for _, m := range models {
		if m.ID == model {
			e.modelCaps = &m.Capabilities
			e.modelCapsFor = model
			return e.modelCaps
		}
	}

	e.logger.Debug("model not found in provider model list", "model", model)
	return nil
}

// History returns the full conversation.
func (e *Engine) History() []message.Message {
	return e.history
}

// ContextWindow returns the context window (may be nil).
func (e *Engine) ContextWindow() *gnomactx.Window {
	return e.cfg.Context
}

// InjectMessage appends a message to conversation history without triggering a turn.
// Used for system notifications (permission mode changes, incognito toggles) that
// the model should see as context in subsequent turns.
func (e *Engine) InjectMessage(msg message.Message) {
	e.history = append(e.history, msg)
	if e.cfg.Context != nil {
		e.cfg.Context.AppendMessage(msg)
	}
}

// Usage returns cumulative token usage.
func (e *Engine) Usage() message.Usage {
	return e.usage
}

// SetProvider swaps the active provider (for dynamic switching).
func (e *Engine) SetProvider(p provider.Provider) {
	e.cfg.Provider = p
}

// SetModel changes the model within the current provider.
func (e *Engine) SetModel(model string) {
	e.cfg.Model = model
}

// Reset clears conversation history and usage.
func (e *Engine) Reset() {
	e.history = nil
	e.usage = message.Usage{}
	if e.cfg.Context != nil {
		e.cfg.Context.Reset()
	}
	e.activatedTools = make(map[string]bool)
}
