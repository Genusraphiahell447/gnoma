package slm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"somegit.dev/Owlibou/gnoma/internal/message"
	"somegit.dev/Owlibou/gnoma/internal/provider"
	"somegit.dev/Owlibou/gnoma/internal/router"
	"somegit.dev/Owlibou/gnoma/internal/stream"
)

const defaultClassifyTimeout = 2 * time.Second

const classifySystemPrompt = `Classify the following coding request. Respond with JSON only, no other text.
Format: {"task_type": "<type>", "complexity": <0.0-1.0>, "requires_tools": <true|false>}

Task types: Debug, Explain, Generation, Refactor, UnitTest, Boilerplate, Planning, Orchestration, SecurityReview, Review

Complexity guide:
0.0–0.3: boilerplate, trivial edits, simple lookups, short explanations
0.4–0.6: new functions, refactors, unit tests, moderate analysis
0.7–1.0: architectural changes, multi-file edits, security review, planning`

type classifyResponse struct {
	TaskType      string  `json:"task_type"`
	Complexity    float64 `json:"complexity"`
	RequiresTools bool    `json:"requires_tools"`
}

// Classifier implements router.TaskClassifier using a llamafile-hosted SLM.
// On timeout or parse failure it falls back to router.HeuristicClassifier.
type Classifier struct {
	provider provider.Provider
	model    string
	timeout  time.Duration
	logger   *slog.Logger
}

// NewClassifier creates a Classifier. model is the model name passed to the provider
// (llamafile ignores it but openaicompat requires a non-empty value).
func NewClassifier(p provider.Provider, model string, logger *slog.Logger) *Classifier {
	if logger == nil {
		logger = slog.Default()
	}
	return &Classifier{
		provider: p,
		model:    model,
		timeout:  defaultClassifyTimeout,
		logger:   logger,
	}
}

// Classify calls the SLM and overlays the three SLM-authoritative fields
// (Type, ComplexityScore, RequiresTools) onto a heuristic baseline Task.
// This ensures Priority, EstimatedTokens, and RequiredEffort are always set.
func (c *Classifier) Classify(ctx context.Context, prompt string, history []message.Message) (router.Task, error) {
	tctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	resp, err := c.callSLM(tctx, prompt)
	if err != nil {
		c.logger.Debug("slm classify fallback", "error", err)
		t, ferr := router.HeuristicClassifier{}.Classify(ctx, prompt, history)
		t.ClassifierSource = router.ClassifierSLMFallback
		return t, ferr
	}

	// Start from the heuristic baseline so Priority/EstimatedTokens/RequiredEffort are set.
	task := router.ClassifyTask(prompt)
	task.Type = router.ParseTaskType(resp.TaskType)
	task.ComplexityScore = resp.Complexity
	task.RequiresTools = resp.RequiresTools
	task.ClassifierSource = router.ClassifierSLM
	return task, nil
}

func (c *Classifier) callSLM(ctx context.Context, prompt string) (*classifyResponse, error) {
	req := provider.Request{
		Model:        c.model,
		SystemPrompt: classifySystemPrompt,
		Messages: []message.Message{
			{
				Role:    message.RoleUser,
				Content: []message.Content{{Type: message.ContentText, Text: prompt}},
			},
		},
	}

	strm, err := c.provider.Stream(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("stream: %w", err)
	}
	defer func() { _ = strm.Close() }()

	var sb strings.Builder
	for strm.Next() {
		ev := strm.Current()
		if ev.Type == stream.EventTextDelta {
			sb.WriteString(ev.Text)
		}
	}
	if err := strm.Err(); err != nil {
		return nil, fmt.Errorf("stream error: %w", err)
	}

	text := extractJSON(sb.String())
	var resp classifyResponse
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		return nil, fmt.Errorf("parse %q: %w", text, err)
	}
	return &resp, nil
}

// extractJSON pulls the first {...} substring from s, stripping markdown fences if present.
func extractJSON(s string) string {
	s = strings.TrimSpace(s)

	// Strip ```json ... ``` fences.
	if strings.HasPrefix(s, "```") {
		end := strings.LastIndex(s, "```")
		if end > 3 {
			inner := s[3:end]
			inner = strings.TrimPrefix(inner, "json")
			s = strings.TrimSpace(inner)
		}
	}

	// Extract first balanced {...} block.
	start := strings.IndexByte(s, '{')
	if start < 0 {
		return s
	}
	depth := 0
	for i := start; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return s[start:]
}
