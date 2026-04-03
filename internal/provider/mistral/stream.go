package mistral

import (
	"encoding/json"

	"somegit.dev/Owlibou/gnoma/internal/message"
	"somegit.dev/Owlibou/gnoma/internal/stream"
	mistralgo "github.com/VikingOwl91/mistral-go-sdk"
	"github.com/VikingOwl91/mistral-go-sdk/chat"
)

// mistralStream adapts mistral's Stream[CompletionChunk] to gnoma's stream.Stream.
type mistralStream struct {
	raw   *mistralgo.Stream[chat.CompletionChunk]
	cur   stream.Event
	err   error
	model string

	// Track active tool calls for delta assembly
	activeToolCalls map[int]*toolCallState // keyed by ToolCall.Index

	// Deferred finish reason (when finish arrives on the same chunk as content)
	pendingFinish *chat.FinishReason
	pendingUsage  *message.Usage  // usage from a chunk that also had other data
	emittedStop   bool            // true after we've emitted the synthetic stop event
	hadToolCalls  bool            // true if any tool calls were emitted
}

type toolCallState struct {
	id   string
	name string
	args string // accumulated argument fragments
}

func newMistralStream(raw *mistralgo.Stream[chat.CompletionChunk]) *mistralStream {
	return &mistralStream{
		raw:             raw,
		activeToolCalls: make(map[int]*toolCallState),
	}
}

func (s *mistralStream) Next() bool {
	for s.raw.Next() {
		chunk := s.raw.Current()

		// Capture model from first chunk
		if s.model == "" && chunk.Model != "" {
			s.model = chunk.Model
		}

		// Store usage if present (may be on same chunk as tool calls or finish)
		if chunk.Usage != nil {
			s.pendingUsage = translateUsage(chunk.Usage)
		}

		if len(chunk.Choices) == 0 {
			// Chunk with only usage and no choices — emit usage
			if s.pendingUsage != nil {
				s.cur = stream.Event{Type: stream.EventUsage, Usage: s.pendingUsage}
				s.pendingUsage = nil
				return true
			}
			continue
		}

		choice := chunk.Choices[0]
		delta := choice.Delta

		// Process text content first (even on chunks with finish reason)
		text := delta.Content.String()
		if text != "" {
			s.cur = stream.Event{
				Type: stream.EventTextDelta,
				Text: text,
			}
			// If this chunk also has a finish reason, store it for next iteration
			if choice.FinishReason != nil {
				s.pendingFinish = choice.FinishReason
			}
			return true
		}

		// Tool call deltas
		if len(delta.ToolCalls) > 0 {
			// Store finish reason if present on same chunk as tool calls
			if choice.FinishReason != nil {
				s.pendingFinish = choice.FinishReason
			}

			for _, tc := range delta.ToolCalls {
				existing, ok := s.activeToolCalls[tc.Index]
				if !ok {
					// New tool call
					s.activeToolCalls[tc.Index] = &toolCallState{
						id:   tc.ID,
						name: tc.Function.Name,
						args: tc.Function.Arguments,
					}
					s.hadToolCalls = true

					// If arguments are already complete (Mistral sends full args in one chunk),
					// emit ToolCallDone directly instead of Start
					if tc.Function.Arguments != "" && s.pendingFinish != nil {
						s.cur = stream.Event{
							Type:         stream.EventToolCallDone,
							ToolCallID:   tc.ID,
							ToolCallName: tc.Function.Name,
							Args:         json.RawMessage(tc.Function.Arguments),
						}
						// Remove from active — it's already done
						delete(s.activeToolCalls, tc.Index)
						return true
					}

					// Otherwise emit Start, accumulate deltas later
					s.cur = stream.Event{
						Type:         stream.EventToolCallStart,
						ToolCallID:   tc.ID,
						ToolCallName: tc.Function.Name,
					}
					return true
				}
				// Existing tool call — accumulate arguments, emit Delta
				existing.args += tc.Function.Arguments
				if tc.Function.Arguments != "" {
					s.cur = stream.Event{
						Type:       stream.EventToolCallDelta,
						ToolCallID: existing.id,
						ArgDelta:   tc.Function.Arguments,
					}
					return true
				}
			}
			continue
		}

		// Check finish reason (from this chunk or pending from previous)
		fr := choice.FinishReason
		if fr == nil {
			fr = s.pendingFinish
			s.pendingFinish = nil
		}

		if fr != nil {
			// Flush any pending tool calls as Done events
			if *fr == chat.FinishReasonToolCalls {
				for idx, tc := range s.activeToolCalls {
					s.cur = stream.Event{
						Type:       stream.EventToolCallDone,
						ToolCallID: tc.id,
						Args:       json.RawMessage(tc.args),
					}
					delete(s.activeToolCalls, idx)
					s.pendingFinish = fr // re-store to flush remaining on next call
					return true
				}
			}

			// Final event with stop reason
			s.cur = stream.Event{
				Type:       stream.EventTextDelta,
				StopReason: translateFinishReason(fr),
				Model:      s.model,
			}
			return true
		}
	}

	// Drain any pending finish reason that was stored with the last content chunk
	if s.pendingFinish != nil {
		fr := s.pendingFinish
		s.pendingFinish = nil

		// Flush pending tool calls
		if *fr == chat.FinishReasonToolCalls {
			for idx, tc := range s.activeToolCalls {
				s.cur = stream.Event{
					Type:       stream.EventToolCallDone,
					ToolCallID: tc.id,
					Args:       json.RawMessage(tc.args),
				}
				delete(s.activeToolCalls, idx)
				s.pendingFinish = fr
				return true
			}
		}

		s.cur = stream.Event{
			Type:       stream.EventTextDelta,
			StopReason: translateFinishReason(fr),
			Model:      s.model,
		}
		return true
	}

	// Emit any pending usage before the stop event
	if s.pendingUsage != nil {
		s.cur = stream.Event{Type: stream.EventUsage, Usage: s.pendingUsage}
		s.pendingUsage = nil
		return true
	}

	// Stream ended — emit inferred stop reason.
	if !s.emittedStop {
		s.emittedStop = true

		// If we have pending tool calls, they ended with the stream
		if len(s.activeToolCalls) > 0 {
			for idx, tc := range s.activeToolCalls {
				s.cur = stream.Event{
					Type:       stream.EventToolCallDone,
					ToolCallID: tc.id,
					Args:       json.RawMessage(tc.args),
				}
				delete(s.activeToolCalls, idx)
				return true
			}
		}

		// Infer stop reason: if tool calls were emitted, it's ToolUse; otherwise EndTurn
		stopReason := message.StopEndTurn
		if s.hadToolCalls {
			stopReason = message.StopToolUse
		}

		s.cur = stream.Event{
			Type:       stream.EventTextDelta,
			StopReason: stopReason,
			Model:      s.model,
		}
		return true
	}

	s.err = s.raw.Err()
	return false
}

func (s *mistralStream) Current() stream.Event {
	return s.cur
}

func (s *mistralStream) Err() error {
	return s.err
}

func (s *mistralStream) Close() error {
	return s.raw.Close()
}
