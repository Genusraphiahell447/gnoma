package engine

import (
	"encoding/json"
	"testing"

	"somegit.dev/Owlibou/gnoma/internal/message"
)

func TestCompactPreviousToolResults_NoAssistant(t *testing.T) {
	msgs := []message.Message{
		{Role: message.RoleUser, Content: []message.Content{message.NewTextContent("hello")}},
	}
	got := compactPreviousToolResults(msgs)
	if len(got) != 1 || got[0].TextContent() != "hello" {
		t.Error("should return messages unchanged when no assistant message exists")
	}
}

func TestCompactPreviousToolResults_SingleRound(t *testing.T) {
	// user → assistant(tool_call) → tool_result
	// Only one round, tool result is the latest — should NOT be compacted.
	msgs := []message.Message{
		{Role: message.RoleUser, Content: []message.Content{message.NewTextContent("do /init")}},
		{Role: message.RoleAssistant, Content: []message.Content{
			message.NewToolCallContent(message.ToolCall{ID: "c1", Name: "fs.ls", Arguments: json.RawMessage(`{}`)}),
		}},
		toolResultMsg("c1distances", "file1.go\nfile2.go\nfile3.go\n"),
	}
	got := compactPreviousToolResults(msgs)
	// Tool result is after the last assistant message — should be intact.
	result := got[2].Content[0].ToolResult
	if result.Content == "" || len(result.Content) < 10 {
		t.Errorf("latest tool result should be intact, got %q", result.Content)
	}
}

func TestCompactPreviousToolResults_TwoRounds(t *testing.T) {
	bigContent := make([]byte, 2000)
	for i := range bigContent {
		bigContent[i] = 'x'
	}

	msgs := []message.Message{
		{Role: message.RoleUser, Content: []message.Content{message.NewTextContent("do /init")}},
		// Round 0
		{Role: message.RoleAssistant, Content: []message.Content{
			message.NewToolCallContent(message.ToolCall{ID: "c1", Name: "fs.read", Arguments: json.RawMessage(`{}`)}),
		}},
		toolResultMsg("c1", string(bigContent)), // 2000 chars — should be compacted
		// Round 1
		{Role: message.RoleAssistant, Content: []message.Content{
			message.NewToolCallContent(message.ToolCall{ID: "c2", Name: "fs.write", Arguments: json.RawMessage(`{}`)}),
		}},
		toolResultMsg("c2", "file written"), // latest — should be intact
	}

	got := compactPreviousToolResults(msgs)

	// Round 0 tool result (index 2) should be compacted
	r0 := got[2].Content[0].ToolResult
	if len(r0.Content) > 100 {
		t.Errorf("round 0 tool result should be compacted, got %d chars", len(r0.Content))
	}
	if r0.ToolCallID != "c1" {
		t.Errorf("compacted result should preserve ToolCallID, got %q", r0.ToolCallID)
	}

	// Round 1 tool result (index 4) should be intact
	r1 := got[4].Content[0].ToolResult
	if r1.Content != "file written" {
		t.Errorf("latest tool result should be intact, got %q", r1.Content)
	}
}

func TestCompactPreviousToolResults_PreservesNonToolMessages(t *testing.T) {
	msgs := []message.Message{
		{Role: message.RoleUser, Content: []message.Content{message.NewTextContent("hello")}},
		{Role: message.RoleAssistant, Content: []message.Content{
			message.NewTextContent("I'll read the file"),
			message.NewToolCallContent(message.ToolCall{ID: "c1", Name: "fs.read", Arguments: json.RawMessage(`{}`)}),
		}},
		toolResultMsg("c1", "file contents here..."),
		{Role: message.RoleAssistant, Content: []message.Content{message.NewTextContent("done")}},
	}
	got := compactPreviousToolResults(msgs)

	// User text message should be unchanged
	if got[0].TextContent() != "hello" {
		t.Errorf("user message should be unchanged, got %q", got[0].TextContent())
	}
	// Assistant text should be unchanged
	if got[1].TextContent() != "I'll read the file" {
		t.Errorf("assistant message should be unchanged, got %q", got[1].TextContent())
	}
}

func TestCompactPreviousToolResults_PreservesErrorFlag(t *testing.T) {
	msgs := []message.Message{
		{Role: message.RoleUser, Content: []message.Content{message.NewTextContent("hi")}},
		{Role: message.RoleAssistant, Content: []message.Content{
			message.NewToolCallContent(message.ToolCall{ID: "c1", Name: "fs.read", Arguments: json.RawMessage(`{}`)}),
		}},
		errorToolResultMsg("c1", "permission denied: /etc/shadow"),
		{Role: message.RoleAssistant, Content: []message.Content{message.NewTextContent("sorry")}},
	}
	got := compactPreviousToolResults(msgs)
	r := got[2].Content[0].ToolResult
	if !r.IsError {
		t.Error("compacted error result should preserve IsError=true")
	}
}

func TestCompactPreviousToolResults_DoesNotMutateOriginal(t *testing.T) {
	original := "a]long tool result content that should not be modified"
	msgs := []message.Message{
		{Role: message.RoleUser, Content: []message.Content{message.NewTextContent("hi")}},
		{Role: message.RoleAssistant, Content: []message.Content{
			message.NewToolCallContent(message.ToolCall{ID: "c1", Name: "fs.read", Arguments: json.RawMessage(`{}`)}),
		}},
		toolResultMsg("c1", original),
		{Role: message.RoleAssistant, Content: []message.Content{message.NewTextContent("ok")}},
	}
	_ = compactPreviousToolResults(msgs)
	// Original message should be unchanged
	if msgs[2].Content[0].ToolResult.Content != original {
		t.Error("compaction should not mutate the original messages")
	}
}

// helpers

func toolResultMsg(toolCallID, content string) message.Message {
	return message.Message{
		Role: message.RoleUser,
		Content: []message.Content{{
			Type: message.ContentToolResult,
			ToolResult: &message.ToolResult{
				ToolCallID: toolCallID,
				Content:    content,
			},
		}},
	}
}

func errorToolResultMsg(toolCallID, content string) message.Message {
	return message.Message{
		Role: message.RoleUser,
		Content: []message.Content{{
			Type: message.ContentToolResult,
			ToolResult: &message.ToolResult{
				ToolCallID: toolCallID,
				Content:    content,
				IsError:    true,
			},
		}},
	}
}
