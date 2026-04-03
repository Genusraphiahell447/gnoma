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
	messages []message.Message
	logger   *slog.Logger

	// Circuit breaker: stop retrying after consecutive failures
	consecutiveFailures int
	maxFailures         int
}

type WindowConfig struct {
	MaxTokens int64
	Strategy  Strategy
	Logger    *slog.Logger
}

func NewWindow(cfg WindowConfig) *Window {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Window{
		tracker:     NewTracker(cfg.MaxTokens),
		strategy:    cfg.Strategy,
		messages:    nil,
		logger:      logger,
		maxFailures: 3,
	}
}

// Append adds a message and tracks usage.
func (w *Window) Append(msg message.Message, usage message.Usage) {
	w.messages = append(w.messages, msg)
	w.tracker.Add(usage)
}

// Messages returns the current message history.
func (w *Window) Messages() []message.Message {
	return w.messages
}

// SetMessages replaces the message history (used after compaction).
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

	if w.strategy == nil {
		return false, fmt.Errorf("no compaction strategy configured")
	}

	// Circuit breaker
	if w.consecutiveFailures >= w.maxFailures {
		w.logger.Warn("compaction circuit breaker open",
			"failures", w.consecutiveFailures,
			"max", w.maxFailures,
		)
		return false, nil
	}

	budget := w.tracker.Remaining() + w.tracker.Used()/2 // target: half of current usage
	if budget < 0 {
		budget = w.tracker.MaxTokens() / 2
	}

	w.logger.Info("compacting context",
		"messages", len(w.messages),
		"used", w.tracker.Used(),
		"budget", budget,
		"strategy", fmt.Sprintf("%T", w.strategy),
	)

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
	w.messages = compacted

	// Rough estimate: reduce tracked tokens proportionally
	ratio := float64(len(compacted)) / float64(len(w.messages)+1)
	w.tracker.Set(int64(float64(w.tracker.Used()) * ratio))

	w.logger.Info("compaction complete",
		"messages_before", len(w.messages),
		"messages_after", len(compacted),
		"tokens_after", w.tracker.Used(),
	)

	return true, nil
}

// Reset clears all messages and usage.
func (w *Window) Reset() {
	w.messages = nil
	w.tracker.Reset()
	w.consecutiveFailures = 0
}
