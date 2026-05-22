// Package subprocess provides a provider.Provider that delegates to CLI agents
// (claude, gemini, vibe, codex) by spawning them as subprocesses.
//
// Impedance mismatch: these CLI agents are full agentic loops, not LLM endpoints.
// Only the latest user message is passed as a prompt. The following provider.Request
// fields are intentionally ignored: Tools, SystemPrompt, Messages (history),
// Temperature, TopP, TopK, Thinking, ToolChoice, MaxTokens.
// Internal tool calls executed by the CLI are surfaced as EventTextDelta (opaque).
//
// SECURITY WARNING: These CLI agents are external trust boundaries. They run
// their own agentic loops, execute their own tools (often with --yolo or --trust),
// and may bypass gnoma's tool permissions, system prompts, and history controls.
// gnoma's firewall only redacts the prompt passed to the CLI and the final text
// response; internal agent cycles are invisible to gnoma.
package subprocess

import (
	"context"
	"fmt"
	"os/exec"

	"somegit.dev/Owlibou/gnoma/internal/message"
	"somegit.dev/Owlibou/gnoma/internal/provider"
	"somegit.dev/Owlibou/gnoma/internal/stream"
)

// Provider wraps a single DiscoveredAgent and implements provider.Provider.
type Provider struct {
	agent DiscoveredAgent
}

// New creates a Provider for the given discovered CLI agent.
func New(agent DiscoveredAgent) *Provider {
	return &Provider{agent: agent}
}

// Name returns "subprocess" — all CLI agents share this provider namespace.
func (p *Provider) Name() string { return "subprocess" }

// DefaultModel returns the CLI binary name (e.g., "claude", "gemini", "vibe", "codex").
func (p *Provider) DefaultModel() string { return p.agent.Name }

// Models returns a single ModelInfo describing this CLI agent.
func (p *Provider) Models(_ context.Context) ([]provider.ModelInfo, error) {
	return []provider.ModelInfo{
		{
			ID:           p.agent.Name,
			Name:         p.agent.DisplayName,
			Provider:     "subprocess",
			Capabilities: p.agent.Capabilities,
		},
	}, nil
}

// Stream spawns the CLI agent with the latest user message as a prompt and
// returns an event stream. All fields in req except the last user message are
// ignored — see package doc for rationale.
func (p *Provider) Stream(ctx context.Context, req provider.Request) (stream.Stream, error) {
	prompt := p.buildPrompt(req)

	args := p.agent.PromptArgs(prompt)
	cmd := exec.CommandContext(ctx, p.agent.Path, args...)

	parser := newParser(p.agent.Format, req.ResponseFormat)
	if parser == nil {
		return nil, fmt.Errorf("subprocess: unknown format %q for agent %q", p.agent.Format, p.agent.Name)
	}

	s, err := newSubprocessStream(ctx, cmd, parser)
	if err != nil {
		return nil, fmt.Errorf("subprocess %q: %w", p.agent.Name, err)
	}
	return s, nil
}

// buildPrompt extracts the latest user message and, for agents flagged
// PromptResponseFormat, augments it with JSON-schema instructions when the
// caller asked for ResponseJSON. The augmentation is a best-effort fallback;
// compliance depends on the underlying model.
func (p *Provider) buildPrompt(req provider.Request) string {
	prompt := extractLastUserMessage(req.Messages)

	if !p.agent.PromptResponseFormat {
		return prompt
	}
	if req.ResponseFormat == nil || req.ResponseFormat.Type != provider.ResponseJSON {
		return prompt
	}

	prompt += "\n\nIMPORTANT: You MUST respond with a valid JSON object"
	if req.ResponseFormat.JSONSchema != nil && len(req.ResponseFormat.JSONSchema.Schema) > 0 {
		prompt += " matching this schema:\n"
		prompt += string(req.ResponseFormat.JSONSchema.Schema)
	} else {
		prompt += "."
	}
	prompt += "\nRespond with JSON only."
	return prompt
}

// extractLastUserMessage returns the content of the last user-role message in msgs.
// Returns an empty string if there are no user messages.
func extractLastUserMessage(msgs []message.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		if m.Role != message.RoleUser {
			continue
		}
		for _, c := range m.Content {
			if c.Type == message.ContentText && c.Text != "" {
				return c.Text
			}
		}
	}
	return ""
}
