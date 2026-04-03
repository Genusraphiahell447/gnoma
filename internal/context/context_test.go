package context

import (
	"fmt"
	"testing"

	"somegit.dev/Owlibou/gnoma/internal/message"
)

// --- Tracker ---

func TestTracker_States(t *testing.T) {
	tr := NewTracker(200_000) // 200K context window

	// Initially OK
	if tr.State() != TokensOK {
		t.Errorf("initial state = %s, want ok", tr.State())
	}

	// Add usage below warning threshold
	tr.Add(message.Usage{InputTokens: 100_000, OutputTokens: 50_000})
	if tr.State() != TokensOK {
		t.Errorf("150K of 200K = %s, want ok", tr.State())
	}

	// Add more to hit warning (200K - 20K = 180K threshold)
	tr.Add(message.Usage{InputTokens: 20_000, OutputTokens: 10_000})
	if tr.State() != TokensWarning {
		t.Errorf("180K of 200K = %s, want warning", tr.State())
	}

	// Add more to hit critical (200K - 13K = 187K threshold)
	tr.Add(message.Usage{InputTokens: 5_000, OutputTokens: 3_000})
	if tr.State() != TokensCritical {
		t.Errorf("188K of 200K = %s, want critical", tr.State())
	}

	if !tr.ShouldCompact() {
		t.Error("should compact at critical")
	}
}

func TestTracker_PercentUsed(t *testing.T) {
	tr := NewTracker(100_000)
	tr.Add(message.Usage{InputTokens: 25_000, OutputTokens: 25_000})

	if tr.PercentUsed() != 50 {
		t.Errorf("PercentUsed = %d, want 50", tr.PercentUsed())
	}
}

func TestTracker_Remaining(t *testing.T) {
	tr := NewTracker(100_000)
	tr.Add(message.Usage{InputTokens: 60_000})

	if tr.Remaining() != 40_000 {
		t.Errorf("Remaining = %d, want 40000", tr.Remaining())
	}
}

func TestTracker_Reset(t *testing.T) {
	tr := NewTracker(100_000)
	tr.Add(message.Usage{InputTokens: 50_000})
	tr.Reset()

	if tr.Used() != 0 {
		t.Errorf("Used after reset = %d", tr.Used())
	}
}

// --- TruncateStrategy ---

func TestTruncateStrategy_KeepsRecent(t *testing.T) {
	s := &TruncateStrategy{KeepRecent: 4}

	msgs := []message.Message{
		message.NewSystemText("system prompt"),
		message.NewUserText("old message 1"),
		message.NewAssistantText("old reply 1"),
		message.NewUserText("old message 2"),
		message.NewAssistantText("old reply 2"),
		message.NewUserText("recent 1"),
		message.NewAssistantText("recent reply 1"),
		message.NewUserText("recent 2"),
		message.NewAssistantText("recent reply 2"),
	}

	result, err := s.Compact(msgs, 50_000)
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}

	// System + marker + ack + 4 recent = 7
	if len(result) != 7 {
		t.Errorf("len = %d, want 7 (system + marker + ack + 4 recent)", len(result))
		for i, m := range result {
			t.Logf("  [%d] %s: %s", i, m.Role, m.TextContent())
		}
	}

	// First message should be system
	if result[0].Role != message.RoleSystem {
		t.Errorf("result[0].Role = %q, want system", result[0].Role)
	}

	// Marker message
	if result[1].Role != message.RoleUser {
		t.Errorf("result[1] should be compaction marker")
	}

	// Last message should be the most recent
	last := result[len(result)-1]
	if last.TextContent() != "recent reply 2" {
		t.Errorf("last message = %q, want 'recent reply 2'", last.TextContent())
	}
}

func TestTruncateStrategy_NoopWhenSmall(t *testing.T) {
	s := &TruncateStrategy{KeepRecent: 10}

	msgs := []message.Message{
		message.NewUserText("hello"),
		message.NewAssistantText("hi"),
	}

	result, err := s.Compact(msgs, 50_000)
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}

	if len(result) != 2 {
		t.Errorf("small history should not be compacted, got %d messages", len(result))
	}
}

// --- Window ---

func TestWindow_CompactIfNeeded(t *testing.T) {
	w := NewWindow(WindowConfig{
		MaxTokens: 100_000,
		Strategy:  &TruncateStrategy{KeepRecent: 2},
	})

	// Add enough messages and usage to trigger compaction
	for i := 0; i < 20; i++ {
		w.Append(message.NewUserText("message"), message.Usage{InputTokens: 5000})
		w.Append(message.NewAssistantText("reply"), message.Usage{OutputTokens: 5000})
	}

	// Should be at critical
	if w.Tracker().State() != TokensCritical {
		t.Skipf("not at critical (used: %d, max: %d), skipping", w.Tracker().Used(), w.Tracker().MaxTokens())
	}

	compacted, err := w.CompactIfNeeded()
	if err != nil {
		t.Fatalf("CompactIfNeeded: %v", err)
	}
	if !compacted {
		t.Error("should have compacted")
	}

	// Messages should be reduced
	if len(w.Messages()) >= 40 {
		t.Errorf("messages not reduced: %d", len(w.Messages()))
	}
}

func TestWindow_CircuitBreaker(t *testing.T) {
	// Strategy that always fails
	failStrategy := &failingStrategy{}
	w := NewWindow(WindowConfig{
		MaxTokens: 1000,
		Strategy:  failStrategy,
	})

	// Push past critical
	w.Append(message.NewUserText("x"), message.Usage{InputTokens: 990})

	// Try to compact — should fail 3 times then stop
	for i := 0; i < 5; i++ {
		w.CompactIfNeeded()
	}

	if failStrategy.calls > 3 {
		t.Errorf("circuit breaker should stop after 3 failures, got %d calls", failStrategy.calls)
	}
}

type failingStrategy struct {
	calls int
}

func (s *failingStrategy) Compact(msgs []message.Message, budget int64) ([]message.Message, error) {
	s.calls++
	return nil, fmt.Errorf("always fails")
}

var _ Strategy = (*failingStrategy)(nil)
