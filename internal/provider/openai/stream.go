package openai

import (
	"encoding/json"

	"somegit.dev/Owlibou/gnoma/internal/message"
	"somegit.dev/Owlibou/gnoma/internal/stream"

	oai "github.com/openai/openai-go"
	"github.com/openai/openai-go/packages/ssestream"
)

// openaiStream adapts OpenAI's ssestream to gnoma's stream.Stream.
type openaiStream struct {
	raw         *ssestream.Stream[oai.ChatCompletionChunk]
	cur         stream.Event
	err         error
	model       string
	stopReason  message.StopReason
	emittedStop bool

	// Tool call tracking (OpenAI uses index-based accumulation)
	toolCalls    map[int64]*toolCallState
	hadToolCalls bool
}

type toolCallState struct {
	id   string
	name string
	args string
}

func newOpenAIStream(raw *ssestream.Stream[oai.ChatCompletionChunk]) *openaiStream {
	return &openaiStream{
		raw:       raw,
		toolCalls: make(map[int64]*toolCallState),
	}
}

func (s *openaiStream) Next() bool {
	for s.raw.Next() {
		chunk := s.raw.Current()

		if s.model == "" && chunk.Model != "" {
			s.model = chunk.Model
		}

		// Usage (only present when StreamOptions.IncludeUsage is true)
		if chunk.Usage.PromptTokens > 0 || chunk.Usage.CompletionTokens > 0 {
			usage := translateUsage(chunk.Usage)
			s.cur = stream.Event{
				Type:  stream.EventUsage,
				Usage: usage,
			}
			return true
		}

		if len(chunk.Choices) == 0 {
			continue
		}

		choice := chunk.Choices[0]
		delta := choice.Delta

		// Finish reason
		if choice.FinishReason != "" {
			s.stopReason = translateFinishReason(string(choice.FinishReason))
		}

		// Tool calls (index-based)
		if len(delta.ToolCalls) > 0 {
			for _, tc := range delta.ToolCalls {
				existing, ok := s.toolCalls[tc.Index]
				if !ok {
					// New tool call — capture initial arguments too
					existing = &toolCallState{
						id:   tc.ID,
						name: tc.Function.Name,
						args: tc.Function.Arguments,
					}
					s.toolCalls[tc.Index] = existing
					s.hadToolCalls = true

					if tc.Function.Name != "" {
						s.cur = stream.Event{
							Type:         stream.EventToolCallStart,
							ToolCallID:   tc.ID,
							ToolCallName: unsanitizeToolName(tc.Function.Name),
						}
						return true
					}
				}

				// Accumulate arguments (subsequent chunks)
				if tc.Function.Arguments != "" && ok {
					existing.args += tc.Function.Arguments
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

		// Text content
		if delta.Content != "" {
			s.cur = stream.Event{
				Type: stream.EventTextDelta,
				Text: delta.Content,
			}
			return true
		}
	}

	// Stream ended — flush tool call Done events, then emit stop
	for idx, tc := range s.toolCalls {
		s.cur = stream.Event{
			Type:         stream.EventToolCallDone,
			ToolCallID:   tc.id,
			ToolCallName: unsanitizeToolName(tc.name),
			Args:         json.RawMessage(tc.args),
		}
		delete(s.toolCalls, idx)
		return true
	}

	if !s.emittedStop {
		s.emittedStop = true
		if s.stopReason == "" {
			if s.hadToolCalls {
				s.stopReason = message.StopToolUse
			} else {
				s.stopReason = message.StopEndTurn
			}
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

func (s *openaiStream) Current() stream.Event { return s.cur }
func (s *openaiStream) Err() error            { return s.err }
func (s *openaiStream) Close() error           { return s.raw.Close() }
