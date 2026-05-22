package subprocess

import (
	"slices"
	"testing"

	"somegit.dev/Owlibou/gnoma/internal/message"
	"somegit.dev/Owlibou/gnoma/internal/stream"
)

func TestCodexPromptArgs_BypassDefaultsOn(t *testing.T) {
	t.Setenv("GNOMA_CODEX_BYPASS_SANDBOX", "")
	args := codexPromptArgs("hi")
	if !slices.Contains(args, "--dangerously-bypass-approvals-and-sandbox") {
		t.Errorf("default args should include sandbox bypass; got %v", args)
	}
}

func TestCodexPromptArgs_BypassOptOut(t *testing.T) {
	for _, val := range []string{"0", "false", "no", "off", "FALSE"} {
		t.Run(val, func(t *testing.T) {
			t.Setenv("GNOMA_CODEX_BYPASS_SANDBOX", val)
			args := codexPromptArgs("hi")
			if slices.Contains(args, "--dangerously-bypass-approvals-and-sandbox") {
				t.Errorf("env=%q should drop bypass flag; got %v", val, args)
			}
			if !slices.Contains(args, "exec") || !slices.Contains(args, "--json") {
				t.Errorf("required base args missing; got %v", args)
			}
		})
	}
}

func TestCodexPromptArgs_UnknownValueDefaultsOn(t *testing.T) {
	t.Setenv("GNOMA_CODEX_BYPASS_SANDBOX", "maybe")
	args := codexPromptArgs("hi")
	if !slices.Contains(args, "--dangerously-bypass-approvals-and-sandbox") {
		t.Errorf("non-falsy value should keep bypass on; got %v", args)
	}
}

func TestCodexParser_ExtractsTextDelta(t *testing.T) {
	p := newCodexParser()
	line := []byte(`{"type":"item.completed","item":{"type":"agent_message","text":"hello world"}}`)

	evts, err := p.ParseLine(line)
	if err != nil {
		t.Fatal(err)
	}
	if len(evts) == 0 {
		t.Fatal("expected at least one event")
	}
	if evts[0].Type != stream.EventTextDelta {
		t.Errorf("got type %v, want EventTextDelta", evts[0].Type)
	}
	if evts[0].Text != "hello world" {
		t.Errorf("got text %q, want %q", evts[0].Text, "hello world")
	}
}

func TestCodexParser_ExtractsUsageFromTurnCompleted(t *testing.T) {
	p := newCodexParser()
	line := []byte(`{"type":"turn.completed","usage":{"input_tokens":123,"output_tokens":45}}`)

	evts, err := p.ParseLine(line)
	if err != nil {
		t.Fatal(err)
	}
	var usageEvt *stream.Event
	for i := range evts {
		if evts[i].Type == stream.EventUsage {
			usageEvt = &evts[i]
		}
	}
	if usageEvt == nil {
		t.Fatal("no EventUsage emitted")
	}
	if usageEvt.Usage.InputTokens != 123 {
		t.Errorf("input_tokens: got %d, want 123", usageEvt.Usage.InputTokens)
	}
	if usageEvt.Usage.OutputTokens != 45 {
		t.Errorf("output_tokens: got %d, want 45", usageEvt.Usage.OutputTokens)
	}
	if usageEvt.StopReason != message.StopEndTurn {
		t.Errorf("stop_reason: got %v, want StopEndTurn", usageEvt.StopReason)
	}
}

func TestCodexParser_ExtractsUsageFromPromptCompletionTokens(t *testing.T) {
	p := newCodexParser()
	line := []byte(`{"type":"turn.completed","usage":{"prompt_tokens":123,"completion_tokens":45}}`)

	evts, err := p.ParseLine(line)
	if err != nil {
		t.Fatal(err)
	}
	var usageEvt *stream.Event
	for i := range evts {
		if evts[i].Type == stream.EventUsage {
			usageEvt = &evts[i]
		}
	}
	if usageEvt == nil {
		t.Fatal("no EventUsage emitted")
	}
	if usageEvt.Usage.InputTokens != 123 {
		t.Errorf("input_tokens: got %d, want 123", usageEvt.Usage.InputTokens)
	}
	if usageEvt.Usage.OutputTokens != 45 {
		t.Errorf("output_tokens: got %d, want 45", usageEvt.Usage.OutputTokens)
	}
}

func TestCodexParser_IgnoresOtherItemsAndTypes(t *testing.T) {
	p := newCodexParser()
	lines := [][]byte{
		[]byte(`{"type":"item.completed","item":{"type":"tool_call","text":"something"}}`),
		[]byte(`{"type":"other_type"}`),
	}

	for _, line := range lines {
		evts, err := p.ParseLine(line)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if len(evts) != 0 {
			t.Errorf("expected 0 events, got %d", len(evts))
		}
	}
}

func TestCodexParser_SkipsNonJSONBanners(t *testing.T) {
	p := newCodexParser()
	// Real codex output interleaves banner lines, blank lines, and
	// human-readable warnings with the JSON event stream. None of
	// these may abort the turn — only the JSON events matter.
	lines := [][]byte{
		[]byte(""),
		[]byte("  "),
		[]byte("codex v1.2.3 starting"),
		[]byte(`WARNING: sandbox bypass enabled`),
		[]byte(`{"type":"item.completed","item":{"type":"agent_message","text":"ok"}}`),
		[]byte("trailing diagnostics: 42ms"),
	}
	var sawText bool
	for _, line := range lines {
		evts, err := p.ParseLine(line)
		if err != nil {
			t.Errorf("non-JSON line %q caused error: %v", string(line), err)
			continue
		}
		for _, e := range evts {
			if e.Type == stream.EventTextDelta {
				sawText = true
			}
		}
	}
	if !sawText {
		t.Error("legitimate JSON line was swallowed by banner-skip logic")
	}
}

func TestCodexParser_MalformedJSONSkippedNotFatal(t *testing.T) {
	p := newCodexParser()
	// Starts with `{` so the banner-skip heuristic doesn't filter it,
	// but is not valid JSON — must skip silently, not return an error.
	bad := []byte(`{"type":"item.completed",`)
	evts, err := p.ParseLine(bad)
	if err != nil {
		t.Errorf("malformed JSON should be skipped, got error: %v", err)
	}
	if len(evts) != 0 {
		t.Errorf("expected 0 events from malformed JSON, got %d", len(evts))
	}
}

func TestCodexParser_UsageMaxOfPaths(t *testing.T) {
	// Both input_tokens and prompt_tokens present with different values
	// — accounting must not silently undercount by always preferring
	// one field.
	p := newCodexParser()
	line := []byte(`{"type":"turn.completed","usage":{"input_tokens":100,"prompt_tokens":120,"output_tokens":30,"completion_tokens":35}}`)
	evts, err := p.ParseLine(line)
	if err != nil {
		t.Fatal(err)
	}
	if len(evts) != 1 || evts[0].Type != stream.EventUsage {
		t.Fatalf("expected single EventUsage, got %+v", evts)
	}
	if evts[0].Usage.InputTokens != 120 {
		t.Errorf("input tokens = %d, want max(100, 120) = 120", evts[0].Usage.InputTokens)
	}
	if evts[0].Usage.OutputTokens != 35 {
		t.Errorf("output tokens = %d, want max(30, 35) = 35", evts[0].Usage.OutputTokens)
	}
}

func TestCodexParser_FixtureFile(t *testing.T) {
	lines := loadFixture(t, "codex")
	p := newCodexParser()
	evts := collectEvents(t, p, lines)

	var textEvts, usageEvts int
	for _, e := range evts {
		switch e.Type {
		case stream.EventTextDelta:
			textEvts++
			if e.Text != "hello" {
				t.Errorf("expected text 'hello', got %q", e.Text)
			}
		case stream.EventUsage:
			usageEvts++
			if e.Usage.InputTokens != 10 || e.Usage.OutputTokens != 5 {
				t.Errorf("expected 10/5 tokens, got %d/%d", e.Usage.InputTokens, e.Usage.OutputTokens)
			}
		}
	}
	if textEvts != 1 {
		t.Errorf("expected 1 EventTextDelta, got %d", textEvts)
	}
	if usageEvts != 1 {
		t.Errorf("expected 1 EventUsage, got %d", usageEvts)
	}
}
