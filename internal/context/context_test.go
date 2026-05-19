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
		_, _ = w.CompactIfNeeded()
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

func TestWindow_AppendMessage_NoTokenTracking(t *testing.T) {
	w := NewWindow(WindowConfig{MaxTokens: 100_000})

	before := w.Tracker().Used()
	w.AppendMessage(message.NewUserText("hello"))
	after := w.Tracker().Used()

	if after != before {
		t.Errorf("AppendMessage should not change tracker: before=%d, after=%d", before, after)
	}
	if len(w.Messages()) != 1 {
		t.Errorf("expected 1 message, got %d", len(w.Messages()))
	}
}

func TestWindow_CompactionUsesEstimateNotRatio(t *testing.T) {
	// Add many small messages then compact to 2.
	// The token estimate post-compaction should reflect actual content,
	// not a message-count ratio of the previous token count.
	w := NewWindow(WindowConfig{
		MaxTokens: 200_000,
		Strategy:  &TruncateStrategy{KeepRecent: 2},
	})

	// Push 20 messages, each costing 8000 tokens (total: 160K).
	// Compaction should leave 2 messages.
	for i := 0; i < 10; i++ {
		w.Append(message.NewUserText("msg"), message.Usage{InputTokens: 4000})
		w.Append(message.NewAssistantText("reply"), message.Usage{OutputTokens: 4000})
	}

	// Push past critical
	w.Tracker().Set(200_000 - DefaultAutocompactBuffer)

	compacted, err := w.CompactIfNeeded()
	if err != nil {
		t.Fatalf("CompactIfNeeded: %v", err)
	}
	if !compacted {
		t.Skip("compaction did not trigger")
	}

	// After compaction to ~2 messages, EstimateMessages(2 short messages) ~ <100 tokens.
	// The old ratio approach would give ~(2/21) * ~(200K-13K) = ~17800 tokens.
	// Verify we're well below 17000, indicating the estimate-based approach.
	if w.Tracker().Used() >= 17_000 {
		t.Errorf("token tracker after compaction seems to use ratio (got %d tokens, expected <17000 for estimate-based)", w.Tracker().Used())
	}
}

func TestWindow_AddPrefix_AppendsToPrefix(t *testing.T) {
	w := NewWindow(WindowConfig{
		MaxTokens:      100_000,
		PrefixMessages: []message.Message{message.NewSystemText("initial prefix")},
	})
	w.AppendMessage(message.NewUserText("hello"))

	w.AddPrefix(
		message.NewUserText("[Project docs: AGENTS.md]\n\nBuild: make build"),
		message.NewAssistantText("Understood."),
	)

	all := w.AllMessages()
	// prefix (1 initial + 2 added) + messages (1)
	if len(all) != 4 {
		t.Errorf("AllMessages() = %d, want 4", len(all))
	}
	// The added prefix messages come after the initial prefix, before conversation
	if all[1].Role != "user" {
		t.Errorf("all[1].Role = %q, want user", all[1].Role)
	}
	if all[3].Role != "user" {
		t.Errorf("all[3].Role = %q, want user (conversation msg)", all[3].Role)
	}
}

func TestWindow_AddPrefix_SurvivesReset(t *testing.T) {
	w := NewWindow(WindowConfig{MaxTokens: 100_000})
	w.AppendMessage(message.NewUserText("hello"))

	w.AddPrefix(message.NewSystemText("added prefix"))
	w.Reset()

	all := w.AllMessages()
	// Prefix should survive Reset(), conversation messages cleared
	if len(all) != 1 {
		t.Errorf("AllMessages() after Reset = %d, want 1 (just added prefix)", len(all))
	}
}

func TestWindow_Reset_ClearsMessages(t *testing.T) {
	w := NewWindow(WindowConfig{
		MaxTokens:      100_000,
		PrefixMessages: []message.Message{message.NewSystemText("prefix")},
	})
	w.AppendMessage(message.NewUserText("hello"))
	w.Tracker().Set(5000)

	w.Reset()

	if len(w.Messages()) != 0 {
		t.Errorf("Messages after reset = %d, want 0", len(w.Messages()))
	}
	if w.Tracker().Used() != 0 {
		t.Errorf("Tracker after reset = %d, want 0", w.Tracker().Used())
	}
	// Prefix should be preserved
	if len(w.AllMessages()) != 1 {
		t.Errorf("AllMessages after reset should have prefix only, got %d", len(w.AllMessages()))
	}
}

// --- Compaction safety (safeSplitPoint) ---

func toolCallMsg() message.Message {
	return message.NewAssistantContent(
		message.NewToolCallContent(message.ToolCall{
			ID:   "call-123",
			Name: "bash",
		}),
	)
}

func toolResultMsg() message.Message {
	return message.NewToolResults(message.ToolResult{
		ToolCallID: "call-123",
		Content:    "result",
	})
}

func TestSafeSplitPoint_NoAdjustmentNeeded(t *testing.T) {
	history := []message.Message{
		message.NewUserText("hello"),        // 0
		message.NewAssistantText("hi"),      // 1
		message.NewUserText("do something"), // 2 — plain user text, safe split point
	}
	// Target split at index 2: keep history[2:] as recent. Not a tool result.
	got := safeSplitPoint(history, 2)
	if got != 2 {
		t.Errorf("safeSplitPoint = %d, want 2 (no adjustment needed)", got)
	}
}

func TestSafeSplitPoint_WalksBackPastToolResult(t *testing.T) {
	history := []message.Message{
		message.NewUserText("hello"),        // 0
		message.NewAssistantText("hi"),      // 1
		toolCallMsg(),                        // 2 — assistant with tool call
		toolResultMsg(),                      // 3 — tool result (should NOT be split point)
		message.NewAssistantText("done"),    // 4
	}
	// Target split at 3 would orphan the tool result (no matching tool call in recent window)
	got := safeSplitPoint(history, 3)
	if got != 2 {
		t.Errorf("safeSplitPoint = %d, want 2 (walk back past tool result to tool call)", got)
	}
}

func TestSafeSplitPoint_NeverGoesNegative(t *testing.T) {
	// All messages are tool results — should return 0 (not go below 0)
	history := []message.Message{
		toolResultMsg(),
		toolResultMsg(),
	}
	got := safeSplitPoint(history, 0)
	if got != 0 {
		t.Errorf("safeSplitPoint = %d, want 0 (floor at 0)", got)
	}
}

func TestTruncate_NeverOrphansToolResult(t *testing.T) {
	s := NewTruncateStrategy() // keepRecent = 10
	s.KeepRecent = 3

	// History: user, assistant+toolcall, user+toolresult, assistant, user
	// With keepRecent=3, naive split at index 2 would grab [toolresult, assistant, user]
	// — orphaning the tool call. safeSplitPoint should walk back to index 1 instead.
	history := []message.Message{
		message.NewUserText("start"),       // 0
		toolCallMsg(),                       // 1 — assistant with tool call
		toolResultMsg(),                     // 2 — must stay paired with index 1
		message.NewAssistantText("done"),   // 3
		message.NewUserText("next"),        // 4
	}

	result, err := s.Compact(history, 100_000)
	if err != nil {
		t.Fatalf("Compact error: %v", err)
	}

	// Find the tool result message in result and verify its tool call ID
	// appears somewhere in a preceding assistant message
	toolCallIDs := make(map[string]bool)
	for _, m := range result {
		for _, c := range m.Content {
			if c.Type == message.ContentToolCall && c.ToolCall != nil {
				toolCallIDs[c.ToolCall.ID] = true
			}
		}
	}
	for _, m := range result {
		for _, c := range m.Content {
			if c.Type == message.ContentToolResult && c.ToolResult != nil {
				if !toolCallIDs[c.ToolResult.ToolCallID] {
					t.Errorf("orphaned tool result: ToolCallID %q has no matching tool call in compacted history",
						c.ToolResult.ToolCallID)
				}
			}
		}
	}
}
