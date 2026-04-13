package engine

import (
	"fmt"

	"somegit.dev/Owlibou/gnoma/internal/message"
)

// compactPreviousToolResults replaces the content of tool results from
// already-processed rounds with a short size marker. The most recent tool
// results (after the last assistant message) are kept intact because the
// model hasn't responded to them yet.
//
// This dramatically reduces context usage in multi-round agentic loops,
// which is critical for local models with small context windows.
func compactPreviousToolResults(msgs []message.Message) []message.Message {
	// Find the last assistant message — tool results before it have been
	// processed; those after it are pending.
	lastAssistant := -1
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == message.RoleAssistant {
			lastAssistant = i
			break
		}
	}
	if lastAssistant <= 0 {
		return msgs
	}

	out := make([]message.Message, len(msgs))
	copy(out, msgs)
	for i := range out {
		if i >= lastAssistant {
			break
		}
		if isToolResultMessage(out[i]) {
			out[i] = compactToolResultMessage(out[i])
		}
	}
	return out
}

func isToolResultMessage(m message.Message) bool {
	return m.Role == message.RoleUser &&
		len(m.Content) > 0 &&
		m.Content[0].Type == message.ContentToolResult
}

func compactToolResultMessage(m message.Message) message.Message {
	compacted := message.Message{
		Role:    m.Role,
		Content: make([]message.Content, len(m.Content)),
	}
	for i, c := range m.Content {
		if c.Type == message.ContentToolResult && c.ToolResult != nil {
			summary := fmt.Sprintf("[prior result: %d chars]", len(c.ToolResult.Content))
			compacted.Content[i] = message.Content{
				Type: message.ContentToolResult,
				ToolResult: &message.ToolResult{
					ToolCallID: c.ToolResult.ToolCallID,
					Content:    summary,
					IsError:    c.ToolResult.IsError,
				},
			}
		} else {
			compacted.Content[i] = c
		}
	}
	return compacted
}
