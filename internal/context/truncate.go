package context

import (
	"somegit.dev/Owlibou/gnoma/internal/message"
)

// TruncateStrategy drops oldest messages while preserving:
// - System prompt (first message if role=system)
// - Recent messages (last N turns)
// Fast, cheap, no LLM call. Used as default and emergency fallback.
type TruncateStrategy struct {
	// KeepRecent is the number of recent messages to always preserve.
	// Default: 10 (5 user + 5 assistant turns)
	KeepRecent int
}

func NewTruncateStrategy() *TruncateStrategy {
	return &TruncateStrategy{KeepRecent: 10}
}

func (s *TruncateStrategy) Compact(messages []message.Message, budget int64) ([]message.Message, error) {
	if len(messages) <= s.KeepRecent {
		return messages, nil // nothing to compact
	}

	keepRecent := s.KeepRecent
	if keepRecent > len(messages) {
		keepRecent = len(messages)
	}

	// Separate system prompt (if present) from history
	var systemMsgs []message.Message
	var history []message.Message

	for i, m := range messages {
		if i == 0 && m.Role == message.RoleSystem {
			systemMsgs = append(systemMsgs, m)
		} else {
			history = append(history, m)
		}
	}

	// Keep only the most recent messages from history
	if len(history) > keepRecent {
		// Add a compaction boundary marker
		marker := message.NewUserText("[Earlier conversation was summarized to save context]")
		ack := message.NewAssistantText("Understood, I'll continue from here.")

		recent := history[len(history)-keepRecent:]
		result := append(systemMsgs, marker, ack)
		result = append(result, recent...)
		return result, nil
	}

	return messages, nil
}
