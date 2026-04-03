package anthropic

import (
	"encoding/json"

	"somegit.dev/Owlibou/gnoma/internal/message"
	"somegit.dev/Owlibou/gnoma/internal/stream"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/packages/ssestream"
)

// anthropicStream adapts Anthropic's ssestream to gnoma's stream.Stream.
type anthropicStream struct {
	raw         *ssestream.Stream[anthropic.MessageStreamEventUnion]
	cur         stream.Event
	err         error
	model       string
	stopReason  message.StopReason
	emittedStop bool

	// Track current content block for tool call assembly
	currentToolCallID   string
	currentToolCallName string
	toolCallArgs        string // accumulated JSON fragments
}

func newAnthropicStream(raw *ssestream.Stream[anthropic.MessageStreamEventUnion]) *anthropicStream {
	return &anthropicStream{raw: raw}
}

func (s *anthropicStream) Next() bool {
	for s.raw.Next() {
		event := s.raw.Current()

		switch variant := event.AsAny().(type) {
		case anthropic.MessageStartEvent:
			if variant.Message.Model != "" {
				s.model = string(variant.Message.Model)
			}
			usage := translateUsage(variant.Message.Usage)
			s.cur = stream.Event{
				Type:  stream.EventUsage,
				Usage: &usage,
				Model: s.model,
			}
			return true

		case anthropic.ContentBlockStartEvent:
			cb := variant.ContentBlock
			if toolUse := cb.AsToolUse(); toolUse.ID != "" {
				s.currentToolCallID = toolUse.ID
				s.currentToolCallName = unsanitizeToolName(toolUse.Name)
				s.toolCallArgs = ""
				s.cur = stream.Event{
					Type:         stream.EventToolCallStart,
					ToolCallID:   s.currentToolCallID,
					ToolCallName: s.currentToolCallName,
				}
				return true
			}
			// Text or thinking block start — no event needed, deltas follow

		case anthropic.ContentBlockDeltaEvent:
			delta := variant.Delta
			switch d := delta.AsAny().(type) {
			case anthropic.TextDelta:
				if d.Text != "" {
					s.cur = stream.Event{
						Type: stream.EventTextDelta,
						Text: d.Text,
					}
					return true
				}
			case anthropic.InputJSONDelta:
				s.toolCallArgs += d.PartialJSON
				if d.PartialJSON != "" {
					s.cur = stream.Event{
						Type:       stream.EventToolCallDelta,
						ToolCallID: s.currentToolCallID,
						ArgDelta:   d.PartialJSON,
					}
					return true
				}
			case anthropic.ThinkingDelta:
				if d.Thinking != "" {
					s.cur = stream.Event{
						Type: stream.EventThinkingDelta,
						Text: d.Thinking,
					}
					return true
				}
			}

		case anthropic.ContentBlockStopEvent:
			// Emit ToolCallDone if we were accumulating a tool call
			if s.currentToolCallID != "" {
				s.cur = stream.Event{
					Type:         stream.EventToolCallDone,
					ToolCallID:   s.currentToolCallID,
					ToolCallName: s.currentToolCallName,
					Args:         json.RawMessage(s.toolCallArgs),
				}
				s.currentToolCallID = ""
				s.currentToolCallName = ""
				s.toolCallArgs = ""
				return true
			}

		case anthropic.MessageDeltaEvent:
			s.stopReason = translateStopReason(anthropic.StopReason(variant.Delta.StopReason))
			if variant.Usage.OutputTokens > 0 {
				usage := message.Usage{OutputTokens: variant.Usage.OutputTokens}
				s.cur = stream.Event{
					Type:  stream.EventUsage,
					Usage: &usage,
				}
				return true
			}

		case anthropic.MessageStopEvent:
			// Stream complete
		}
	}

	// Stream ended — emit stop reason
	if !s.emittedStop {
		s.emittedStop = true
		if s.stopReason == "" {
			s.stopReason = message.StopEndTurn
		}
		s.cur = stream.Event{
			Type:       stream.EventTextDelta,
			StopReason: s.stopReason,
			Model:      s.model,
		}
		return true
	}

	s.err = s.raw.Err()
	return false
}

func (s *anthropicStream) Current() stream.Event {
	return s.cur
}

func (s *anthropicStream) Err() error {
	return s.err
}

func (s *anthropicStream) Close() error {
	return s.raw.Close()
}

// toolCallDoneFromAccum creates a ToolCallDone event.
// Called when ContentBlockStop arrives for a tool_use block.
func toolCallDoneEvent(id string, args json.RawMessage) stream.Event {
	return stream.Event{
		Type:       stream.EventToolCallDone,
		ToolCallID: id,
		Args:       args,
	}
}
