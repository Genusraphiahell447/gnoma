package context

import (
	"context"
	"fmt"
	"strings"

	"somegit.dev/Owlibou/gnoma/internal/message"
	"somegit.dev/Owlibou/gnoma/internal/provider"
	"somegit.dev/Owlibou/gnoma/internal/stream"
)

const summarySystemPrompt = `You are a conversation summarizer. Condense the following conversation into a brief summary that preserves:
- Key decisions made
- Important file paths and code changes
- Tool outputs and their results
- Current task context and progress
- Any errors or blockers encountered

Be concise but preserve all critical details the assistant needs to continue the conversation coherently.
Output only the summary, no preamble.`

// SummarizeStrategy uses the LLM to create a summary of older messages.
// More expensive than truncation but preserves context better.
type SummarizeStrategy struct {
	Provider  provider.Provider
	Model     string // model to use for summarization (empty = provider default)
	MaxTokens int64  // max tokens for summary output
}

func NewSummarizeStrategy(prov provider.Provider) *SummarizeStrategy {
	return &SummarizeStrategy{
		Provider:  prov,
		MaxTokens: 2048,
	}
}

func (s *SummarizeStrategy) Compact(messages []message.Message, budget int64) ([]message.Message, error) {
	if len(messages) <= 4 {
		return messages, nil
	}

	// Separate system prompt from history
	var systemMsgs []message.Message
	var history []message.Message

	for i, m := range messages {
		if i == 0 && m.Role == message.RoleSystem {
			systemMsgs = append(systemMsgs, m)
		} else {
			history = append(history, m)
		}
	}

	if len(history) <= 4 {
		return messages, nil
	}

	// Split: old messages to summarize, recent to keep.
	// Adjust split to never orphan tool results — the assistant message with
	// matching tool calls must stay in the recent window with its results.
	keepRecent := 6
	if keepRecent > len(history) {
		keepRecent = len(history)
	}
	splitAt := safeSplitPoint(history, len(history)-keepRecent)
	oldMessages := history[:splitAt]
	recentMessages := history[splitAt:]

	// Build conversation text for summarization
	var convText strings.Builder
	for _, m := range oldMessages {
		convText.WriteString(fmt.Sprintf("[%s]: %s\n\n", m.Role, m.TextContent()))
	}

	// Call LLM to summarize
	summary, err := s.callSummarize(convText.String())
	if err != nil {
		// Fall back to truncation on failure
		trunc := &TruncateStrategy{KeepRecent: keepRecent}
		return trunc.Compact(messages, budget)
	}

	// Build new history: system + summary marker + recent
	summaryMsg := message.NewUserText(fmt.Sprintf("[Conversation summary — %d earlier messages condensed]\n\n%s", len(oldMessages), summary))
	ackMsg := message.NewAssistantText("Understood, I have the context from the summary. Continuing from here.")

	result := append(systemMsgs, summaryMsg, ackMsg)
	result = append(result, recentMessages...)

	return result, nil
}

func (s *SummarizeStrategy) callSummarize(conversationText string) (string, error) {
	if s.Provider == nil {
		return "", fmt.Errorf("no provider for summarization")
	}

	req := provider.Request{
		Model:        s.Model,
		SystemPrompt: summarySystemPrompt,
		Messages: []message.Message{
			message.NewUserText("Summarize this conversation:\n\n" + conversationText),
		},
		MaxTokens: s.MaxTokens,
	}

	ctx := context.Background()
	str, err := s.Provider.Stream(ctx, req)
	if err != nil {
		return "", fmt.Errorf("summarization stream: %w", err)
	}
	defer func() { _ = str.Close() }()

	// Consume stream, collect text
	var result strings.Builder
	for str.Next() {
		evt := str.Current()
		if evt.Type == stream.EventTextDelta {
			result.WriteString(evt.Text)
		}
	}
	if err := str.Err(); err != nil {
		return "", fmt.Errorf("summarization stream error: %w", err)
	}

	summary := strings.TrimSpace(result.String())
	if summary == "" {
		return "", fmt.Errorf("empty summary returned")
	}

	return summary, nil
}
