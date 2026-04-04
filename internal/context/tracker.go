package context

import (
	"somegit.dev/Owlibou/gnoma/internal/message"
)

// TokenState indicates how close to the context limit we are.
type TokenState int

const (
	TokensOK       TokenState = iota // well within budget
	TokensWarning                    // approaching limit
	TokensCritical                   // at or near limit, compaction needed
)

func (s TokenState) String() string {
	switch s {
	case TokensOK:
		return "ok"
	case TokensWarning:
		return "warning"
	case TokensCritical:
		return "critical"
	default:
		return "unknown"
	}
}

// Thresholds for compaction triggers (from CC autoCompact.ts).
const (
	DefaultAutocompactBuffer = 13_000 // tokens below context window to trigger
	DefaultWarningBuffer     = 20_000 // tokens below context window for warning
)

// Tracker monitors cumulative token usage against a context window budget.
type Tracker struct {
	maxTokens int64 // context window size
	current   int64 // cumulative tokens used

	// Configurable buffers
	autocompactBuffer int64
	warningBuffer     int64
}

func NewTracker(maxTokens int64) *Tracker {
	return &Tracker{
		maxTokens:         maxTokens,
		autocompactBuffer: DefaultAutocompactBuffer,
		warningBuffer:     DefaultWarningBuffer,
	}
}

// Add records token usage from a turn.
func (t *Tracker) Add(usage message.Usage) {
	t.current += usage.InputTokens + usage.OutputTokens
}

// Set overrides the current token count (e.g., after compaction).
func (t *Tracker) Set(tokens int64) {
	t.current = tokens
}

// Reset clears the tracked usage.
func (t *Tracker) Reset() {
	t.current = 0
}

// Used returns the current token count.
func (t *Tracker) Used() int64 {
	return t.current
}

// MaxTokens returns the context window size.
func (t *Tracker) MaxTokens() int64 {
	return t.maxTokens
}

// Remaining returns tokens left before the context window limit.
func (t *Tracker) Remaining() int64 {
	rem := t.maxTokens - t.current
	if rem < 0 {
		return 0
	}
	return rem
}

// PercentUsed returns 0-100 indicating usage level.
func (t *Tracker) PercentUsed() int {
	if t.maxTokens <= 0 {
		return 0
	}
	pct := int((t.current * 100) / t.maxTokens)
	if pct > 100 {
		return 100
	}
	return pct
}

// State returns the current token warning state.
func (t *Tracker) State() TokenState {
	if t.maxTokens <= 0 {
		return TokensOK
	}

	threshold := t.maxTokens - t.autocompactBuffer
	warningThreshold := t.maxTokens - t.warningBuffer

	if t.current >= threshold {
		return TokensCritical
	}
	if t.current >= warningThreshold {
		return TokensWarning
	}
	return TokensOK
}

// ShouldCompact returns true if auto-compaction should trigger.
func (t *Tracker) ShouldCompact() bool {
	return t.State() == TokensCritical
}

// PreEstimate adds an estimated token count before the provider reports actual usage.
// Used for proactive compaction triggering before sending a request.
func (t *Tracker) PreEstimate(tokens int64) {
	t.current += tokens
}

// EstimateTokens returns a rough token estimate for a text string.
// Heuristic: ~4 characters per token for English text.
func EstimateTokens(text string) int64 {
	return int64(len(text)+3) / 4
}

// EstimateMessages returns a rough token estimate for a slice of messages.
func EstimateMessages(msgs []message.Message) int64 {
	var total int64
	for _, msg := range msgs {
		for _, c := range msg.Content {
			switch c.Type {
			case message.ContentText:
				total += EstimateTokens(c.Text)
			case message.ContentToolCall:
				total += 50 // schema overhead per tool call
				if c.ToolCall != nil {
					total += EstimateTokens(string(c.ToolCall.Arguments))
				}
			case message.ContentToolResult:
				if c.ToolResult != nil {
					total += EstimateTokens(c.ToolResult.Content)
				}
			case message.ContentThinking:
				if c.Thinking != nil {
					total += EstimateTokens(c.Thinking.Text)
				}
			}
		}
		total += 4 // per-message overhead (role, separators)
	}
	return total
}
