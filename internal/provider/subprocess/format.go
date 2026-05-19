package subprocess

import (
	"encoding/json"
	"fmt"

	"somegit.dev/Owlibou/gnoma/internal/message"
	"somegit.dev/Owlibou/gnoma/internal/stream"
)

// FormatParser converts raw stdout lines from a CLI subprocess into stream.Events.
// Each CLI agent emits its own line-delimited JSON format.
//
// Design note: These agents are full agentic loops, not LLM endpoints. The provider.Request
// fields Tools, Messages (history), SystemPrompt, Temperature, Thinking, etc. are NOT honored —
// they are opaque black boxes. Only the latest user message is passed as a prompt. Internal
// tool calls executed by the CLI are surfaced as EventTextDelta (opaque text) in v1.
type FormatParser interface {
	// ParseLine parses one newline-stripped stdout line. Returns 0 or more events.
	ParseLine(line []byte) ([]stream.Event, error)
	// Done is called when the process exits cleanly. May emit final events.
	Done() []stream.Event
}

// --- claude-stream-json ---
// Format emitted by: claude -p "..." --output-format stream-json --verbose
//
// Relevant event types:
//   type=assistant → message.content[].type=text → EventTextDelta
//   type=result    → usage.input_tokens/output_tokens, stop_reason → EventUsage; is_error → EventError

type claudeParser struct{}

func newClaudeParser() FormatParser { return &claudeParser{} }

type claudeEvent struct {
	Type    string         `json:"type"`
	Subtype string         `json:"subtype"`
	Message *claudeMessage `json:"message,omitempty"`
	IsError bool           `json:"is_error,omitempty"`
	// result fields
	StopReason string        `json:"stop_reason,omitempty"`
	Usage      *claudeUsage  `json:"usage,omitempty"`
}

type claudeMessage struct {
	Content []claudeContent `json:"content"`
	Usage   *claudeUsage    `json:"usage,omitempty"`
}

type claudeContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type claudeUsage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}

func (p *claudeParser) ParseLine(line []byte) ([]stream.Event, error) {
	var ev claudeEvent
	if err := json.Unmarshal(line, &ev); err != nil {
		return nil, fmt.Errorf("claude: parse line: %w", err)
	}

	switch ev.Type {
	case "assistant":
		if ev.Message == nil {
			return nil, nil
		}
		var evts []stream.Event
		for _, c := range ev.Message.Content {
			if c.Type == "text" && c.Text != "" {
				evts = append(evts, stream.Event{Type: stream.EventTextDelta, Text: c.Text})
			}
		}
		return evts, nil

	case "result":
		if ev.IsError {
			return []stream.Event{{
				Type: stream.EventError,
				Err:  fmt.Errorf("claude CLI: %s", ev.Subtype),
			}}, nil
		}
		var evts []stream.Event
		if ev.Usage != nil {
			evts = append(evts, stream.Event{
				Type: stream.EventUsage,
				Usage: &message.Usage{
					InputTokens:  ev.Usage.InputTokens,
					OutputTokens: ev.Usage.OutputTokens,
				},
				StopReason: claudeStopReason(ev.StopReason),
			})
		}
		return evts, nil
	}

	// system, rate_limit_event, tool, etc. — intentionally ignored
	return nil, nil
}

func (p *claudeParser) Done() []stream.Event { return nil }

func claudeStopReason(s string) message.StopReason {
	switch s {
	case "end_turn":
		return message.StopEndTurn
	case "max_tokens":
		return message.StopMaxTokens
	default:
		return message.StopEndTurn
	}
}

// --- gemini-stream-json ---
// Format emitted by: gemini -p "..." --output-format stream-json
//
// Relevant event types:
//   type=message, role=assistant, delta=true → EventTextDelta
//   type=result, status=success              → EventUsage

type geminiParser struct{}

func newGeminiParser() FormatParser { return &geminiParser{} }

type geminiEvent struct {
	Type   string       `json:"type"`
	Role   string       `json:"role,omitempty"`
	Content string      `json:"content,omitempty"`
	Delta  bool         `json:"delta,omitempty"`
	Status string       `json:"status,omitempty"`
	Stats  *geminiStats `json:"stats,omitempty"`
}

type geminiStats struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}

func (p *geminiParser) ParseLine(line []byte) ([]stream.Event, error) {
	var ev geminiEvent
	if err := json.Unmarshal(line, &ev); err != nil {
		return nil, fmt.Errorf("gemini: parse line: %w", err)
	}

	switch ev.Type {
	case "message":
		if ev.Role == "assistant" && ev.Content != "" {
			return []stream.Event{{Type: stream.EventTextDelta, Text: ev.Content}}, nil
		}
		// user messages and empty assistant messages are ignored
		return nil, nil

	case "result":
		if ev.Stats == nil {
			return nil, nil
		}
		stopReason := message.StopEndTurn
		if ev.Status != "success" {
			return []stream.Event{{
				Type: stream.EventError,
				Err:  fmt.Errorf("gemini CLI: result status %q", ev.Status),
			}}, nil
		}
		return []stream.Event{{
			Type: stream.EventUsage,
			Usage: &message.Usage{
				InputTokens:  ev.Stats.InputTokens,
				OutputTokens: ev.Stats.OutputTokens,
			},
			StopReason: stopReason,
		}}, nil
	}

	// init, other types — ignored
	return nil, nil
}

func (p *geminiParser) Done() []stream.Event { return nil }

// --- vibe-streaming (mistral) ---
// Format emitted by: vibe -p "..." --output streaming --trust
//
// Each line is a JSON message object with a "role" field.
// role=assistant: content → EventTextDelta; reasoning_content → EventThinkingDelta
// role=system, role=user: ignored
// No explicit "done" event — stream ends when process exits.

type vibeParser struct{}

func newVibeParser() FormatParser { return &vibeParser{} }

type vibeMessage struct {
	Role             string  `json:"role"`
	Content          string  `json:"content"`
	ReasoningContent *string `json:"reasoning_content"`
	MessageID        *string `json:"message_id"`
}

func (p *vibeParser) ParseLine(line []byte) ([]stream.Event, error) {
	var msg vibeMessage
	if err := json.Unmarshal(line, &msg); err != nil {
		return nil, fmt.Errorf("vibe: parse line: %w", err)
	}

	if msg.Role != "assistant" {
		return nil, nil
	}

	var evts []stream.Event
	if msg.ReasoningContent != nil && *msg.ReasoningContent != "" {
		evts = append(evts, stream.Event{
			Type: stream.EventThinkingDelta,
			Text: *msg.ReasoningContent,
		})
	}
	if msg.Content != "" {
		evts = append(evts, stream.Event{Type: stream.EventTextDelta, Text: msg.Content})
	}
	return evts, nil
}

func (p *vibeParser) Done() []stream.Event { return nil }
