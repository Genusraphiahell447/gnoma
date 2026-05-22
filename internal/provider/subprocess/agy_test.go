package subprocess

import (
	"encoding/json"
	"strings"
	"testing"

	"somegit.dev/Owlibou/gnoma/internal/message"
	"somegit.dev/Owlibou/gnoma/internal/provider"
	"somegit.dev/Owlibou/gnoma/internal/stream"
)

// TestAgyParser_EmitsLineDeltas verifies the plain-text parser emits each
// stdout line as an EventTextDelta with a trailing newline. The parser's
// behavior does not depend on ResponseFormat — JSON-mode augmentation lives
// in buildPrompt, not the parser.
func TestAgyParser_EmitsLineDeltas(t *testing.T) {
	parser := newParser(FormatAgyText, nil)
	if parser == nil {
		t.Fatal("newParser(FormatAgyText) returned nil")
	}
	lines := [][]byte{
		[]byte("Thinking..."),
		[]byte(`{"foo": "bar"}`),
	}

	var sb strings.Builder
	for _, line := range lines {
		evts, err := parser.ParseLine(line)
		if err != nil {
			t.Fatalf("ParseLine failed: %v", err)
		}
		for _, ev := range evts {
			if ev.Type == stream.EventTextDelta {
				sb.WriteString(ev.Text)
			}
		}
	}

	want := "Thinking...\n{\"foo\": \"bar\"}\n"
	if sb.String() != want {
		t.Errorf("output = %q, want %q", sb.String(), want)
	}
}

func TestAgyProvider_BuildPrompt_AugmentsWithSchema(t *testing.T) {
	agent := CLIAgent{Name: "agy", PromptResponseFormat: true}
	p := New(DiscoveredAgent{CLIAgent: agent})

	schema := json.RawMessage(`{"type": "object"}`)
	req := provider.Request{
		Messages: []message.Message{message.NewUserText("Hello")},
		ResponseFormat: &provider.ResponseFormat{
			Type:       provider.ResponseJSON,
			JSONSchema: &provider.JSONSchema{Schema: schema},
		},
	}

	prompt := p.buildPrompt(req)
	if !strings.Contains(prompt, "IMPORTANT: You MUST respond with a valid JSON object") {
		t.Error("prompt missing JSON instructions")
	}
	if !strings.Contains(prompt, `{"type": "object"}`) {
		t.Error("prompt missing schema")
	}
}

// TestAgyProvider_BuildPrompt_NilSchema covers the case where ResponseJSON is
// requested without a schema attached. Previously this dereferenced
// JSONSchema.Schema and panicked.
func TestAgyProvider_BuildPrompt_NilSchema(t *testing.T) {
	agent := CLIAgent{Name: "agy", PromptResponseFormat: true}
	p := New(DiscoveredAgent{CLIAgent: agent})

	cases := []struct {
		name string
		rf   *provider.ResponseFormat
	}{
		{
			name: "nil JSONSchema",
			rf:   &provider.ResponseFormat{Type: provider.ResponseJSON},
		},
		{
			name: "empty Schema bytes",
			rf: &provider.ResponseFormat{
				Type:       provider.ResponseJSON,
				JSONSchema: &provider.JSONSchema{},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := provider.Request{
				Messages:       []message.Message{message.NewUserText("Hello")},
				ResponseFormat: tc.rf,
			}
			prompt := p.buildPrompt(req)
			if !strings.Contains(prompt, "IMPORTANT: You MUST respond with a valid JSON object") {
				t.Error("prompt missing JSON instructions")
			}
			if !strings.Contains(prompt, "Respond with JSON only.") {
				t.Error("prompt missing trailing JSON-only instruction")
			}
		})
	}
}

// TestProvider_BuildPrompt_NoAugmentationWithoutFlag verifies that agents
// without PromptResponseFormat (e.g. claude, gemini, vibe) are not augmented
// when the caller asks for ResponseJSON. Those agents either have their own
// structured-output path or genuinely don't support JSON mode.
func TestProvider_BuildPrompt_NoAugmentationWithoutFlag(t *testing.T) {
	agent := CLIAgent{Name: "claude"} // PromptResponseFormat zero value: false
	p := New(DiscoveredAgent{CLIAgent: agent})

	req := provider.Request{
		Messages: []message.Message{message.NewUserText("Hello")},
		ResponseFormat: &provider.ResponseFormat{
			Type:       provider.ResponseJSON,
			JSONSchema: &provider.JSONSchema{Schema: json.RawMessage(`{}`)},
		},
	}
	prompt := p.buildPrompt(req)
	if prompt != "Hello" {
		t.Errorf("prompt = %q, want %q (no augmentation)", prompt, "Hello")
	}
}
