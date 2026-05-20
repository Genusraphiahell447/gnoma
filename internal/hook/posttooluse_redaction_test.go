package hook_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"somegit.dev/Owlibou/gnoma/internal/hook"
	"somegit.dev/Owlibou/gnoma/internal/message"
	"somegit.dev/Owlibou/gnoma/internal/provider"
	"somegit.dev/Owlibou/gnoma/internal/router"
	"somegit.dev/Owlibou/gnoma/internal/security"
	"somegit.dev/Owlibou/gnoma/internal/stream"
)

// This regression test locks in ADR-004's transitive guarantee:
// PostToolUse hooks of type "prompt" do not leak raw tool output to a
// remote LLM, because the LLM call routes through SafeProvider, which
// scans outgoing messages.
//
// If this test fails after a refactor, either:
//   - The hook prompt path no longer goes through the router/SafeProvider
//     (re-open ADR-004 — Position A is broken; switch to Position C/D), or
//   - SafeProvider was removed/relaxed (re-open Wave 1).

// recordingProvider captures the last request it received.
type recordingProvider struct {
	name    string
	lastReq provider.Request
}

func (p *recordingProvider) Name() string         { return p.name }
func (p *recordingProvider) DefaultModel() string { return "rec-model" }
func (p *recordingProvider) Models(_ context.Context) ([]provider.ModelInfo, error) {
	return []provider.ModelInfo{{ID: "rec-model", Name: "rec-model", Provider: p.name}}, nil
}
func (p *recordingProvider) Stream(_ context.Context, req provider.Request) (stream.Stream, error) {
	p.lastReq = req
	return &finalEventStream{
		events: []stream.Event{
			{Type: stream.EventTextDelta, Text: "ALLOW"},
			{Type: stream.EventTextDelta, StopReason: message.StopEndTurn, Model: "rec-model"},
		},
	}, nil
}
type finalEventStream struct {
	events []stream.Event
	idx    int
}

func (s *finalEventStream) Next() bool {
	if s.idx >= len(s.events) {
		return false
	}
	s.idx++
	return true
}
func (s *finalEventStream) Current() stream.Event { return s.events[s.idx-1] }
func (s *finalEventStream) Err() error            { return nil }
func (s *finalEventStream) Close() error          { return nil }

// streamerThroughRouter mirrors cmd/gnoma/main.go's unexported
// routerStreamer adapter. PromptExecutor needs only Stream(ctx, prompt);
// the router selects an arm and that arm's Provider does the work.
type streamerThroughRouter struct {
	rtr *router.Router
}

func (s *streamerThroughRouter) Stream(ctx context.Context, prompt string) (stream.Stream, error) {
	req := provider.Request{
		Messages: []message.Message{message.NewUserText(prompt)},
	}
	strm, decision, err := s.rtr.Stream(ctx, router.Task{Type: router.TaskReview}, req)
	if err != nil {
		return nil, err
	}
	decision.Commit(0)
	return strm, nil
}

func TestPostToolUsePromptHook_RedactsSecretViaSafeProvider(t *testing.T) {
	// Wire the same boundary main.go uses: SafeProvider wraps the
	// inner provider, router dispatches through arm.Provider.Stream.
	rec := &recordingProvider{name: "rec"}
	fwRef := new(security.FirewallRef)
	fwRef.Set(security.NewFirewall(security.FirewallConfig{
		ScanOutgoing:     true,
		EntropyThreshold: 4.5,
	}))

	rtr := router.New(router.Config{})
	rtr.RegisterArm(&router.Arm{
		ID:           router.NewArmID("rec", "rec-model"),
		Provider:     security.WrapProvider(rec, fwRef),
		ModelName:    "rec-model",
		IsLocal:      true,
		Capabilities: provider.Capabilities{ToolUse: false},
	})

	streamer := &streamerThroughRouter{rtr: rtr}

	// Prompt hook template that drops the raw tool result straight into
	// the LLM prompt. This is the worst-case user config.
	def := hook.HookDef{
		Name:    "leaky-prompt-hook",
		Event:   hook.PostToolUse,
		Command: hook.CommandTypePrompt,
		Exec:    `The bash tool ran. Output was:\n{{.Result}}\n\nDoes this contain a secret? Answer ALLOW or DENY.`,
	}
	exec := hook.NewPromptExecutor(def, streamer)

	// Build a PostToolUse payload whose result.output contains a
	// detectable secret.
	const secret = "sk-ant-api03-abcdefghijklmnopqrstuvwxyz"
	rawOutput := "command completed.\nleaked secret: " + secret
	payload := hook.MarshalPostToolPayload("bash", json.RawMessage(`{"cmd":"echo $K"}`), rawOutput, nil)

	if _, err := exec.Execute(context.Background(), payload); err != nil {
		t.Fatalf("PromptExecutor.Execute: %v", err)
	}

	// Assert: the recorded request reaching the inner provider does NOT
	// contain the raw secret. SafeProvider should have scrubbed it.
	if len(rec.lastReq.Messages) == 0 {
		t.Fatal("recordingProvider saw no request")
	}
	text := rec.lastReq.Messages[0].TextContent()
	if strings.Contains(text, secret) {
		t.Errorf("ADR-004 invariant broken: secret %q reached inner provider verbatim.\n"+
			"Recorded prompt: %q\n"+
			"Either the hook prompt path no longer routes through SafeProvider, or SafeProvider's "+
			"redaction was disabled. Re-open ADR-004.",
			secret, text)
	}
	if !strings.Contains(text, "[REDACTED]") {
		t.Errorf("expected [REDACTED] marker in recorded prompt, got %q", text)
	}
}
