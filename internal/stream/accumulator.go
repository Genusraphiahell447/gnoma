package stream

import (
	"strings"

	"somegit.dev/Owlibou/gnoma/internal/message"
)

// Accumulator assembles a message.Response from a sequence of Events.
// Provider adapters translate SDK events into stream.Events;
// the Accumulator — shared, tested once — builds the final Response.
type Accumulator struct {
	content []message.Content
	usage   message.Usage

	// Active text block being built
	textBuf *strings.Builder

	// Active thinking block being built
	thinkBuf *strings.Builder

	// Tool calls in progress, keyed by ToolCallID
	toolCalls map[string]*toolCallAccum
	// Ordered tool call IDs to preserve emission order
	toolCallOrder []string
}

type toolCallAccum struct {
	id      string
	name    string
	argsBuf strings.Builder
	args    []byte // final complete args (from Done event)
}

func NewAccumulator() *Accumulator {
	return &Accumulator{
		toolCalls: make(map[string]*toolCallAccum),
	}
}

// Apply processes a single event, updating the accumulator's state.
func (a *Accumulator) Apply(e Event) {
	switch e.Type {
	case EventTextDelta:
		a.flushThinking()
		if a.textBuf == nil {
			a.textBuf = &strings.Builder{}
		}
		a.textBuf.WriteString(e.Text)

	case EventThinkingDelta:
		a.flushText()
		if a.thinkBuf == nil {
			a.thinkBuf = &strings.Builder{}
		}
		a.thinkBuf.WriteString(e.Text)

	case EventToolCallStart:
		a.flushText()
		a.flushThinking()
		tc := &toolCallAccum{id: e.ToolCallID, name: e.ToolCallName}
		a.toolCalls[e.ToolCallID] = tc
		a.toolCallOrder = append(a.toolCallOrder, e.ToolCallID)

	case EventToolCallDelta:
		if tc, ok := a.toolCalls[e.ToolCallID]; ok {
			tc.argsBuf.WriteString(e.ArgDelta)
		}

	case EventToolCallDone:
		tc, ok := a.toolCalls[e.ToolCallID]
		if !ok {
			// Done without prior Start (e.g., Mistral sends complete tool calls in one chunk)
			tc = &toolCallAccum{id: e.ToolCallID, name: e.ToolCallName}
			a.toolCalls[e.ToolCallID] = tc
			a.toolCallOrder = append(a.toolCallOrder, e.ToolCallID)
		}
		if e.Args != nil {
			// Done event carries authoritative complete args
			tc.args = e.Args
		} else {
			// Fall back to accumulated deltas
			tc.args = []byte(tc.argsBuf.String())
		}

	case EventUsage:
		if e.Usage != nil {
			a.usage.Add(*e.Usage)
		}

	case EventError:
		// Errors are handled by the stream consumer, not accumulated
	}
}

// HasContent reports whether any user-visible content (text, thinking, or
// tool calls) has been accumulated. Used by the engine to decide whether a
// stream error is safe to fail over to another arm: a retry that would
// produce duplicate text after the user already saw partial output is
// worse than surfacing the error.
//
// Usage events alone do not count as content — they are bookkeeping.
func (a *Accumulator) HasContent() bool {
	if a.textBuf != nil && a.textBuf.Len() > 0 {
		return true
	}
	if a.thinkBuf != nil && a.thinkBuf.Len() > 0 {
		return true
	}
	if len(a.content) > 0 {
		return true
	}
	if len(a.toolCallOrder) > 0 {
		return true
	}
	return false
}

// Response builds the final message.Response from all accumulated events.
func (a *Accumulator) Response(stopReason message.StopReason, model string) message.Response {
	a.flushText()
	a.flushThinking()
	a.flushToolCalls()

	return message.Response{
		Message: message.Message{
			Role:    message.RoleAssistant,
			Content: a.content,
		},
		StopReason: stopReason,
		Usage:      a.usage,
		Model:      model,
	}
}

func (a *Accumulator) flushText() {
	if a.textBuf != nil && a.textBuf.Len() > 0 {
		a.content = append(a.content, message.NewTextContent(a.textBuf.String()))
		a.textBuf = nil
	}
}

func (a *Accumulator) flushThinking() {
	if a.thinkBuf != nil && a.thinkBuf.Len() > 0 {
		a.content = append(a.content, message.NewThinkingContent(message.Thinking{
			Text: a.thinkBuf.String(),
		}))
		a.thinkBuf = nil
	}
}

func (a *Accumulator) flushToolCalls() {
	for _, id := range a.toolCallOrder {
		tc, ok := a.toolCalls[id]
		if !ok {
			continue
		}
		args := tc.args
		if args == nil {
			// Fallback: use accumulated deltas even if Done never arrived
			args = []byte(tc.argsBuf.String())
		}
		a.content = append(a.content, message.NewToolCallContent(message.ToolCall{
			ID:        tc.id,
			Name:      tc.name,
			Arguments: args,
		}))
	}
	a.toolCalls = make(map[string]*toolCallAccum)
	a.toolCallOrder = nil
}
