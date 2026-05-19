package openai

import (
	"encoding/json"
	"errors"
	"log/slog"

	"somegit.dev/Owlibou/gnoma/internal/message"
	"somegit.dev/Owlibou/gnoma/internal/provider"
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
	id           string
	name         string
	args         string
	argsComplete bool // true when args arrived in the initial chunk; skip subsequent deltas
}

func newOpenAIStream(raw *ssestream.Stream[oai.ChatCompletionChunk]) *openaiStream {
	return &openaiStream{
		raw:       raw,
		toolCalls: make(map[int64]*toolCallState),
	}
}

// flushNextToolCall returns the next pending tool-call Done event, applying
// repairArgs to recover from small lexical mistakes that local-model servers
// (Ollama, llama.cpp, llamafile) routinely emit: markdown fences, trailing
// commas, prose-wrapped objects. The bool return is false once the queue is
// empty.
func (s *openaiStream) flushNextToolCall() (stream.Event, bool) {
	for idx, tc := range s.toolCalls {
		args, repaired := repairArgs(tc.args)
		if repaired {
			slog.Debug("openai: repaired malformed tool-call arguments",
				"tool", tc.name,
				"raw_len", len(tc.args),
				"repaired_len", len(args),
			)
		}
		ev := stream.Event{
			Type:         stream.EventToolCallDone,
			ToolCallID:   tc.id,
			ToolCallName: unsanitizeToolName(tc.name),
			Args:         args,
		}
		delete(s.toolCalls, idx)
		return ev, true
	}
	return stream.Event{}, false
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
						id:           tc.ID,
						name:         tc.Function.Name,
						args:         tc.Function.Arguments,
						argsComplete: tc.Function.Arguments != "" && json.Valid([]byte(tc.Function.Arguments)),
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

				// Accumulate arguments (subsequent chunks).
				// Skip if args were already provided in the initial chunk — some providers
				// (e.g. Ollama) send complete args in the name chunk and then repeat them
				// as a delta, which would cause doubled JSON and unmarshal failures.
				if tc.Function.Arguments != "" && ok && !existing.argsComplete {
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

		// Ollama thinking content — non-standard "thinking" or "reasoning" field on the delta.
		// Ollama uses "reasoning"; some other servers use "thinking".
		// The openai-go struct drops unknown fields, so we read the raw JSON directly.
		if raw := delta.RawJSON(); raw != "" {
			var extra struct {
				Thinking  string `json:"thinking"`
				Reasoning string `json:"reasoning"`
			}
			if json.Unmarshal([]byte(raw), &extra) == nil {
				text := extra.Thinking
				if text == "" {
					text = extra.Reasoning
				}
				if text != "" {
					s.cur = stream.Event{
						Type: stream.EventThinkingDelta,
						Text: text,
					}
					return true
				}
			}
		}
	}

	// Stream ended — flush tool call Done events, then emit stop.
	if ev, ok := s.flushNextToolCall(); ok {
		s.cur = ev
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

	s.err = wrapSDKError(s.raw.Err())
	return false
}

func (s *openaiStream) Current() stream.Event { return s.cur }
func (s *openaiStream) Err() error            { return s.err }
func (s *openaiStream) Close() error           { return s.raw.Close() }

// wrapSDKError converts an OpenAI SDK apierror.Error into a ProviderError
// so the engine's retry logic can classify it properly.
func wrapSDKError(err error) error {
	if err == nil {
		return nil
	}
	var apiErr *oai.Error
	if !errors.As(err, &apiErr) {
		return err
	}
	kind, retryable := provider.ClassifyHTTPError(apiErr.StatusCode, apiErr.Message)
	slog.Debug("openai SDK error wrapped",
		"status", apiErr.StatusCode,
		"kind", kind,
		"retryable", retryable,
		"message", apiErr.Message,
	)
	return &provider.ProviderError{
		Kind:       kind,
		Provider:   "openai",
		StatusCode: apiErr.StatusCode,
		Message:    apiErr.Message,
		Retryable:  retryable,
		Err:        err,
	}
}
