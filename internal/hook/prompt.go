package hook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"text/template"
	"time"

	"somegit.dev/Owlibou/gnoma/internal/message"
	"somegit.dev/Owlibou/gnoma/internal/stream"
)

// TemplateData holds the variables available in hook prompt templates.
type TemplateData struct {
	Event  string
	Tool   string
	Args   string
	Result string
}

// renderTemplate executes a text/template with the given data.
func renderTemplate(tmpl string, data TemplateData) (string, error) {
	t, err := template.New("hook").Parse(tmpl)
	if err != nil {
		return "", fmt.Errorf("hook: template parse error: %w", err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("hook: template execute error: %w", err)
	}
	return buf.String(), nil
}

// parseDecision scans text for the first case-insensitive occurrence of
// "ALLOW" or "DENY". Returns Skip if neither is found.
func parseDecision(text string) Action {
	upper := strings.ToUpper(text)
	ai := strings.Index(upper, "ALLOW")
	di := strings.Index(upper, "DENY")
	switch {
	case ai >= 0 && (di < 0 || ai < di):
		return Allow
	case di >= 0:
		return Deny
	default:
		return Skip
	}
}

// Streamer is the minimal interface PromptExecutor needs from the router.
// *router.Router satisfies this interface via an adapter in main.go.
type Streamer interface {
	Stream(ctx context.Context, prompt string) (stream.Stream, error)
}

// PromptExecutor sends a templated prompt to an LLM and parses ALLOW/DENY
// from the response.
type PromptExecutor struct {
	def      HookDef
	streamer Streamer
}

// NewPromptExecutor constructs a PromptExecutor.
func NewPromptExecutor(def HookDef, streamer Streamer) *PromptExecutor {
	return &PromptExecutor{def: def, streamer: streamer}
}

// Execute renders the template, sends the prompt, and parses the response.
func (p *PromptExecutor) Execute(ctx context.Context, payload []byte) (HookResult, error) {
	data := templateDataFromPayload(payload, p.def.Event)
	prompt, err := renderTemplate(p.def.Exec, data)
	if err != nil {
		return HookResult{}, fmt.Errorf("hook %q: %w", p.def.Name, err)
	}

	start := time.Now()
	s, err := p.streamer.Stream(ctx, prompt)
	if err != nil {
		return HookResult{}, fmt.Errorf("hook %q: stream error: %w", p.def.Name, err)
	}
	defer func() { _ = s.Close() }()

	acc := stream.NewAccumulator()
	var stopReason message.StopReason
	var model string
	for s.Next() {
		evt := s.Current()
		acc.Apply(evt)
		if evt.StopReason != "" {
			stopReason = evt.StopReason
			model = evt.Model
		}
	}
	if err := s.Err(); err != nil {
		return HookResult{}, fmt.Errorf("hook %q: stream error: %w", p.def.Name, err)
	}

	resp := acc.Response(stopReason, model)
	text := resp.Message.TextContent()

	action := parseDecision(text)
	return HookResult{
		Action:   action,
		Output:   []byte(text),
		Duration: time.Since(start),
	}, nil
}

// templateDataFromPayload builds TemplateData from a hook payload.
func templateDataFromPayload(payload []byte, event EventType) TemplateData {
	data := TemplateData{Event: event.String()}
	if event == PreToolUse || event == PostToolUse {
		data.Tool = ExtractToolName(payload)
		data.Args = extractRawField(payload, "args")
		data.Result = extractRawField(payload, "result")
	}
	return data
}

// extractRawField returns the JSON-encoded value of a top-level field.
// Returns "" if absent or on error.
func extractRawField(payload []byte, field string) string {
	var v map[string]json.RawMessage
	if err := json.Unmarshal(payload, &v); err != nil {
		return ""
	}
	raw, ok := v[field]
	if !ok {
		return ""
	}
	return string(raw)
}
