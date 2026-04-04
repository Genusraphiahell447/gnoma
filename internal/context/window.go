package context

import (
	"fmt"
	"log/slog"

	"somegit.dev/Owlibou/gnoma/internal/message"
)

// Strategy compacts a message history to fit within a token budget.
type Strategy interface {
	// Compact reduces the message slice to fit within budget tokens.
	// Must preserve the system prompt (first message if role=system).
	Compact(messages []message.Message, budget int64) ([]message.Message, error)
}

// Window manages the sliding context window with compaction.
type Window struct {
	tracker  *Tracker
	strategy Strategy
	prefix   []message.Message // immutable prefix (project docs), never compacted
	messages []message.Message // mutable conversation history
	logger   *slog.Logger

	// Compact hooks
	onPreCompact  func([]message.Message)
	onPostCompact func([]message.Message)

	// Circuit breaker: stop retrying after consecutive failures
	consecutiveFailures int
	maxFailures         int
}

type WindowConfig struct {
	MaxTokens      int64
	Strategy       Strategy
	PrefixMessages []message.Message // immutable prefix, survives compaction
	OnPreCompact   func([]message.Message)
	OnPostCompact  func([]message.Message)
	Logger         *slog.Logger
}

func NewWindow(cfg WindowConfig) *Window {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Window{
		tracker:       NewTracker(cfg.MaxTokens),
		strategy:      cfg.Strategy,
		prefix:        cfg.PrefixMessages,
		messages:      nil,
		logger:        logger,
		onPreCompact:  cfg.OnPreCompact,
		onPostCompact: cfg.OnPostCompact,
		maxFailures:   3,
	}
}

// Append adds a message and tracks usage.
func (w *Window) Append(msg message.Message, usage message.Usage) {
	w.messages = append(w.messages, msg)
	w.tracker.Add(usage)
}

// Messages returns the mutable conversation history (without prefix).
func (w *Window) Messages() []message.Message {
	return w.messages
}

// AllMessages returns prefix + mutable history. Use this for building provider requests.
func (w *Window) AllMessages() []message.Message {
	if len(w.prefix) == 0 {
		return w.messages
	}
	all := make([]message.Message, 0, len(w.prefix)+len(w.messages))
	all = append(all, w.prefix...)
	all = append(all, w.messages...)
	return all
}

// SetMessages replaces the mutable message history (used after compaction).
func (w *Window) SetMessages(msgs []message.Message) {
	w.messages = msgs
}

// Tracker returns the token tracker.
func (w *Window) Tracker() *Tracker {
	return w.tracker
}

// CompactIfNeeded checks if compaction should trigger and runs it.
// Returns true if compaction was performed.
func (w *Window) CompactIfNeeded() (bool, error) {
	if !w.tracker.ShouldCompact() {
		return false, nil
	}
	return w.doCompact(false)
}

// ForceCompact runs compaction regardless of the token threshold.
// Used for reactive compaction (e.g., after a 413 response).
func (w *Window) ForceCompact() (bool, error) {
	if len(w.messages) <= 2 {
		return false, nil
	}
	return w.doCompact(true)
}

func (w *Window) doCompact(force bool) (bool, error) {
	if w.strategy == nil {
		return false, fmt.Errorf("no compaction strategy configured")
	}

	// Circuit breaker (skip for forced)
	if !force && w.consecutiveFailures >= w.maxFailures {
		w.logger.Warn("compaction circuit breaker open",
			"failures", w.consecutiveFailures,
			"max", w.maxFailures,
		)
		return false, nil
	}

	var budget int64
	if force {
		budget = w.tracker.MaxTokens() / 2
	} else {
		budget = w.tracker.Remaining() + w.tracker.Used()/2
		if budget < 0 {
			budget = w.tracker.MaxTokens() / 2
		}
	}

	label := "compacting"
	if force {
		label = "forced compacting"
	}
	w.logger.Info(label+" context",
		"messages", len(w.messages),
		"prefix", len(w.prefix),
		"used", w.tracker.Used(),
		"budget", budget,
	)

	// Pre-compact hook
	if w.onPreCompact != nil {
		w.onPreCompact(w.messages)
	}

	// Compact only mutable messages — prefix is preserved separately
	compacted, err := w.strategy.Compact(w.messages, budget)
	if err != nil {
		w.consecutiveFailures++
		w.logger.Error("compaction failed",
			"error", err,
			"consecutive_failures", w.consecutiveFailures,
		)
		return false, err
	}

	w.consecutiveFailures = 0
	originalLen := len(w.messages)
	w.messages = compacted

	ratio := float64(len(compacted)) / float64(originalLen+1)
	w.tracker.Set(int64(float64(w.tracker.Used()) * ratio))

	w.logger.Info("compaction complete",
		"messages_before", originalLen,
		"messages_after", len(compacted),
		"tokens_after", w.tracker.Used(),
	)

	// Post-compact hook
	if w.onPostCompact != nil {
		w.onPostCompact(compacted)
	}

	return true, nil
}

// Reset clears all messages and usage (prefix is preserved).
func (w *Window) Reset() {
	w.messages = nil
	w.tracker.Reset()
	w.consecutiveFailures = 0
}
