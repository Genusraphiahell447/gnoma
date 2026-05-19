package subprocess

import (
	"encoding/json"
	"strings"
	"testing"

	"somegit.dev/Owlibou/gnoma/internal/message"
	"somegit.dev/Owlibou/gnoma/internal/provider"
	"somegit.dev/Owlibou/gnoma/internal/stream"
)

func TestAgyProvider_StreamAugmentation(t *testing.T) {
	agent := CLIAgent{
		Name: "agy",
		PromptArgs: func(p string) []string {
			return []string{"-p", p}
		},
		Format: FormatAgyText,
	}
	_ = New(DiscoveredAgent{CLIAgent: agent, Path: "agy"})

	schema := json.RawMessage(`{"type": "object", "properties": {"foo": {"type": "string"}}}`)
	req := provider.Request{
		Messages: []message.Message{
			message.NewUserText("Hello"),
		},
		ResponseFormat: &provider.ResponseFormat{
			Type: provider.ResponseJSON,
			JSONSchema: &provider.JSONSchema{
				Schema: schema,
			},
		},
	}

	// We can't easily run the subprocess in a unit test without mocking exec.Command.
	// But we can check the prompt augmentation logic if we refactor Stream or test it indirectly.
	// For now, let's just verify the agyParser emits text deltas.
	
	parser := newParser(FormatAgyText, req.ResponseFormat)
	lines := [][]byte{
		[]byte("Thinking..."),
		[]byte(`{"foo": "bar"}`),
	}
	
	var allEvents []stream.Event
	for _, line := range lines {
		evts, err := parser.ParseLine(line)
		if err != nil {
			t.Fatalf("ParseLine failed: %v", err)
		}
		allEvents = append(allEvents, evts...)
	}
	
	var sb strings.Builder
	for _, ev := range allEvents {
		if ev.Type == stream.EventTextDelta {
			sb.WriteString(ev.Text)
		}
	}
	
	want := "Thinking...\n{\"foo\": \"bar\"}\n"
	if sb.String() != want {
		t.Errorf("output = %q, want %q", sb.String(), want)
	}
}

func TestAgyProvider_BuildPrompt(t *testing.T) {
	agent := CLIAgent{Name: "agy"}
	p := New(DiscoveredAgent{CLIAgent: agent})

	schema := json.RawMessage(`{"type": "object"}`)
	req := provider.Request{
		Messages: []message.Message{
			message.NewUserText("Hello"),
		},
		ResponseFormat: &provider.ResponseFormat{
			Type: provider.ResponseJSON,
			JSONSchema: &provider.JSONSchema{
				Schema: schema,
			},
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
