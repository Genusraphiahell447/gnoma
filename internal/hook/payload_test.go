package hook

import (
	"encoding/json"
	"testing"
)

func TestMarshalPreToolPayload(t *testing.T) {
	args := json.RawMessage(`{"command":"ls -la"}`)
	payload := MarshalPreToolPayload("bash", args)

	var got map[string]any
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if got["event"] != "pre_tool_use" {
		t.Errorf("event = %q, want %q", got["event"], "pre_tool_use")
	}
	if got["tool"] != "bash" {
		t.Errorf("tool = %q, want %q", got["tool"], "bash")
	}
	if got["args"] == nil {
		t.Error("args field missing")
	}
}

func TestMarshalPostToolPayload(t *testing.T) {
	args := json.RawMessage(`{"command":"ls"}`)
	payload := MarshalPostToolPayload("bash", args, "file1\nfile2", nil)

	var got map[string]any
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if got["event"] != "post_tool_use" {
		t.Errorf("event = %q, want %q", got["event"], "post_tool_use")
	}
	if got["tool"] != "bash" {
		t.Errorf("tool = %q, want %q", got["tool"], "bash")
	}
	result, ok := got["result"].(map[string]any)
	if !ok {
		t.Fatal("result field missing or wrong type")
	}
	if result["output"] != "file1\nfile2" {
		t.Errorf("result.output = %q, want %q", result["output"], "file1\nfile2")
	}
}

func TestMarshalSessionStartPayload(t *testing.T) {
	payload := MarshalSessionStartPayload("abc-123", "tui")

	var got map[string]any
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if got["event"] != "session_start" {
		t.Errorf("event = %q, want %q", got["event"], "session_start")
	}
	if got["session_id"] != "abc-123" {
		t.Errorf("session_id = %q, want %q", got["session_id"], "abc-123")
	}
	if got["mode"] != "tui" {
		t.Errorf("mode = %q, want %q", got["mode"], "tui")
	}
}

func TestMarshalSessionEndPayload(t *testing.T) {
	payload := MarshalSessionEndPayload("abc-123", 42)

	var got map[string]any
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if got["event"] != "session_end" {
		t.Errorf("event = %q, want %q", got["event"], "session_end")
	}
	if got["session_id"] != "abc-123" {
		t.Errorf("session_id = %q, want %q", got["session_id"], "abc-123")
	}
	if int(got["turns"].(float64)) != 42 {
		t.Errorf("turns = %v, want 42", got["turns"])
	}
}

func TestMarshalPreCompactPayload(t *testing.T) {
	payload := MarshalPreCompactPayload(87, 120000)

	var got map[string]any
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if got["event"] != "pre_compact" {
		t.Errorf("event = %q, want %q", got["event"], "pre_compact")
	}
	if int(got["message_count"].(float64)) != 87 {
		t.Errorf("message_count = %v, want 87", got["message_count"])
	}
	if int(got["token_estimate"].(float64)) != 120000 {
		t.Errorf("token_estimate = %v, want 120000", got["token_estimate"])
	}
}

func TestMarshalStopPayload(t *testing.T) {
	payload := MarshalStopPayload("max_turns")

	var got map[string]any
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if got["event"] != "stop" {
		t.Errorf("event = %q, want %q", got["event"], "stop")
	}
	if got["reason"] != "max_turns" {
		t.Errorf("reason = %q, want %q", got["reason"], "max_turns")
	}
}

func TestExtractToolName(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
		want    string
	}{
		{
			"pre_tool_use payload",
			[]byte(`{"event":"pre_tool_use","tool":"bash","args":{}}`),
			"bash",
		},
		{
			"post_tool_use payload",
			[]byte(`{"event":"post_tool_use","tool":"fs.read","args":{}}`),
			"fs.read",
		},
		{
			"session_start has no tool",
			[]byte(`{"event":"session_start","session_id":"x","mode":"tui"}`),
			"",
		},
		{
			"empty payload",
			[]byte(`{}`),
			"",
		},
		{
			"malformed JSON",
			[]byte(`not json`),
			"",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExtractToolName(tt.payload); got != tt.want {
				t.Errorf("ExtractToolName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseHookOutput_JSONActionOverridesExitCode(t *testing.T) {
	// stdout says deny, exit code 0 — JSON wins
	stdout := []byte(`{"action":"deny","transformed":{"command":"safe"}}`)
	action, transformed, err := ParseHookOutput(stdout, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != Deny {
		t.Errorf("action = %v, want Deny", action)
	}
	if transformed == nil {
		t.Error("transformed should not be nil")
	}
}

func TestParseHookOutput_EmptyStdoutFallsBackToExitCode(t *testing.T) {
	tests := []struct {
		exitCode int
		want     Action
	}{
		{0, Allow},
		{1, Skip},
		{2, Deny},
	}
	for _, tt := range tests {
		action, transformed, err := ParseHookOutput(nil, tt.exitCode)
		if err != nil {
			t.Errorf("exit %d: unexpected error: %v", tt.exitCode, err)
			continue
		}
		if action != tt.want {
			t.Errorf("exit %d: action = %v, want %v", tt.exitCode, action, tt.want)
		}
		if transformed != nil {
			t.Errorf("exit %d: expected nil transformed", tt.exitCode)
		}
	}
}

func TestParseHookOutput_MalformedJSON(t *testing.T) {
	// non-empty stdout that isn't valid JSON falls back to exit code
	_, _, err := ParseHookOutput([]byte("not json"), 0)
	if err == nil {
		t.Error("expected error for malformed JSON stdout")
	}
}

func TestParseHookOutput_AllowString(t *testing.T) {
	stdout := []byte(`{"action":"allow"}`)
	action, _, err := ParseHookOutput(stdout, 2) // exit 2 but JSON says allow
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != Allow {
		t.Errorf("action = %v, want Allow", action)
	}
}

func TestExtractTransformedOutput(t *testing.T) {
	transformed := json.RawMessage(`{"output":"rewritten result","metadata":{"key":"val"}}`)
	got := ExtractTransformedOutput(transformed)
	if got != "rewritten result" {
		t.Errorf("ExtractTransformedOutput() = %q, want %q", got, "rewritten result")
	}
}

func TestExtractTransformedOutput_Empty(t *testing.T) {
	got := ExtractTransformedOutput(nil)
	if got != "" {
		t.Errorf("ExtractTransformedOutput(nil) = %q, want %q", got, "")
	}
}
