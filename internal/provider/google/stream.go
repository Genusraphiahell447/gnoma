package google

import (
	"context"
	"encoding/json"
	"iter"

	"somegit.dev/Owlibou/gnoma/internal/message"
	"somegit.dev/Owlibou/gnoma/internal/stream"

	"google.golang.org/genai"
)

// googleStream bridges Google's range-based iterator to gnoma's pull-based Stream.
// Uses a goroutine + channel to convert iter.Seq2 → Next()/Current().
type googleStream struct {
	events chan stream.Event
	cancel context.CancelFunc
	cur    stream.Event
	err    error
	done   bool
}

func newGoogleStream(ctx context.Context, iter iter.Seq2[*genai.GenerateContentResponse, error], model string) *googleStream {
	ctx, cancel := context.WithCancel(ctx)
	s := &googleStream{
		events: make(chan stream.Event, 16),
		cancel: cancel,
	}

	go func() {
		defer close(s.events)
		var stopReason message.StopReason
		hadFunctionCalls := false

		for resp, err := range iter {
			if err != nil {
				select {
				case s.events <- stream.Event{Type: stream.EventError, Err: err}:
				case <-ctx.Done():
				}
				return
			}

			if len(resp.Candidates) == 0 {
				continue
			}

			candidate := resp.Candidates[0]
			if candidate.FinishReason != "" {
				stopReason = translateFinishReason(candidate.FinishReason)
			}

			if candidate.Content == nil {
				continue
			}

			for _, part := range candidate.Content.Parts {
				var evt stream.Event

				if part.FunctionCall != nil {
					// Google sends complete function calls, not deltas
					fc := part.FunctionCall
					args, _ := json.Marshal(fc.Args)
					hadFunctionCalls = true
					evt = stream.Event{
						Type:         stream.EventToolCallDone,
						ToolCallID:   fc.ID,
						ToolCallName: unsanitizeToolName(fc.Name),
						Args:         args,
					}
				} else if part.Thought {
					evt = stream.Event{
						Type: stream.EventThinkingDelta,
						Text: part.Text,
					}
				} else if part.Text != "" {
					evt = stream.Event{
						Type: stream.EventTextDelta,
						Text: part.Text,
					}
				} else {
					continue
				}

				select {
				case s.events <- evt:
				case <-ctx.Done():
					return
				}
			}
		}

		// Override stop reason if function calls were emitted
		// (Google uses STOP even when returning function calls)
		if hadFunctionCalls {
			stopReason = message.StopToolUse
		} else if stopReason == "" {
			stopReason = message.StopEndTurn
		}
		select {
		case s.events <- stream.Event{
			Type:       stream.EventTextDelta,
			StopReason: stopReason,
			Model:      model,
		}:
		case <-ctx.Done():
		}
	}()

	return s
}

func (s *googleStream) Next() bool {
	if s.done {
		return false
	}
	evt, ok := <-s.events
	if !ok {
		s.done = true
		return false
	}
	if evt.Type == stream.EventError {
		s.err = evt.Err
		s.done = true
		return false
	}
	s.cur = evt
	return true
}

func (s *googleStream) Current() stream.Event { return s.cur }
func (s *googleStream) Err() error            { return s.err }
func (s *googleStream) Close() error {
	s.cancel()
	// Drain channel to let goroutine exit
	for range s.events {
	}
	return nil
}
