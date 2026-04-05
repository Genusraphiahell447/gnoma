package session_test

import (
	"encoding/json"
	"testing"
	"time"

	"somegit.dev/Owlibou/gnoma/internal/message"
	"somegit.dev/Owlibou/gnoma/internal/session"
)

func TestSnapshot_RoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	snap := session.Snapshot{
		ID: "snap-test-001",
		Metadata: session.Metadata{
			ID:           "snap-test-001",
			Provider:     "anthropic",
			Model:        "claude-3-5-sonnet",
			TurnCount:    2,
			UpdatedAt:    now,
			CreatedAt:    now.Add(-5 * time.Minute),
			MessageCount: 5,
		},
		Messages: []message.Message{
			message.NewUserText("what files are in this dir?"),
			message.NewAssistantContent(
				message.NewTextContent("I'll check that for you."),
				message.NewToolCallContent(message.ToolCall{
					ID:        "toolu_01abc",
					Name:      "bash",
					Arguments: json.RawMessage(`{"command":"ls -la"}`),
				}),
			),
			message.NewToolResults(message.ToolResult{
				ToolCallID: "toolu_01abc",
				Content:    "total 42\ndrwxr-xr-x ...",
				IsError:    false,
			}),
			message.NewAssistantContent(
				message.NewThinkingContent(message.Thinking{
					Text:      "The directory contains source files.",
					Signature: "sig_abc123",
				}),
				message.NewTextContent("Here are the files."),
			),
			message.NewUserText("thanks"),
		},
	}

	data, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got session.Snapshot
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.ID != snap.ID {
		t.Errorf("ID: got %q, want %q", got.ID, snap.ID)
	}
	if got.Metadata.Provider != "anthropic" {
		t.Errorf("provider: got %q", got.Metadata.Provider)
	}
	if len(got.Messages) != 5 {
		t.Fatalf("messages: got %d, want 5", len(got.Messages))
	}
	// Verify tool call round-trip
	tc := got.Messages[1].Content[1]
	if tc.Type != message.ContentToolCall || tc.ToolCall == nil {
		t.Errorf("tool call content wrong: %+v", tc)
	}
	if tc.ToolCall.ID != "toolu_01abc" {
		t.Errorf("tool call ID: got %q", tc.ToolCall.ID)
	}
	// Verify thinking round-trip
	th := got.Messages[3].Content[0]
	if th.Type != message.ContentThinking || th.Thinking == nil {
		t.Errorf("thinking content wrong: %+v", th)
	}
	if th.Thinking.Signature != "sig_abc123" {
		t.Errorf("thinking signature: got %q", th.Thinking.Signature)
	}
}
