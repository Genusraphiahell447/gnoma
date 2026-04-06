package hook

import (
	"testing"
	"time"

	"somegit.dev/Owlibou/gnoma/internal/config"
)

func TestParseHookDefs_ValidConfig(t *testing.T) {
	cfgs := []config.HookConfig{
		{
			Name:        "log-tools",
			Event:       "post_tool_use",
			Type:        "command",
			Exec:        "tee -a /tmp/log.jsonl",
			Timeout:     "5s",
			FailOpen:    true,
			ToolPattern: "bash*",
		},
	}
	defs, err := ParseHookDefs(cfgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(defs) != 1 {
		t.Fatalf("len(defs) = %d, want 1", len(defs))
	}
	d := defs[0]
	if d.Name != "log-tools" {
		t.Errorf("Name = %q", d.Name)
	}
	if d.Event != PostToolUse {
		t.Errorf("Event = %v, want PostToolUse", d.Event)
	}
	if d.Command != CommandTypeShell {
		t.Errorf("Command = %v, want CommandTypeShell", d.Command)
	}
	if d.Exec != "tee -a /tmp/log.jsonl" {
		t.Errorf("Exec = %q", d.Exec)
	}
	if d.Timeout != 5*time.Second {
		t.Errorf("Timeout = %v, want 5s", d.Timeout)
	}
	if !d.FailOpen {
		t.Error("FailOpen should be true")
	}
	if d.ToolPattern != "bash*" {
		t.Errorf("ToolPattern = %q", d.ToolPattern)
	}
}

func TestParseHookDefs_DefaultTimeout(t *testing.T) {
	cfgs := []config.HookConfig{
		{Name: "h", Event: "pre_tool_use", Type: "command", Exec: "echo ok"},
		// Timeout empty → default 30s
	}
	defs, err := ParseHookDefs(cfgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// timeout() method returns 30s when Timeout field is zero
	if defs[0].timeout() != 30*time.Second {
		t.Errorf("default timeout = %v, want 30s", defs[0].timeout())
	}
}

func TestParseHookDefs_InvalidEvent(t *testing.T) {
	cfgs := []config.HookConfig{
		{Name: "h", Event: "bogus_event", Type: "command", Exec: "echo ok"},
	}
	_, err := ParseHookDefs(cfgs)
	if err == nil {
		t.Error("expected error for invalid event")
	}
}

func TestParseHookDefs_InvalidType(t *testing.T) {
	cfgs := []config.HookConfig{
		{Name: "h", Event: "pre_tool_use", Type: "webhook", Exec: "echo ok"},
	}
	_, err := ParseHookDefs(cfgs)
	if err == nil {
		t.Error("expected error for invalid type")
	}
}

func TestParseHookDefs_InvalidTimeout(t *testing.T) {
	cfgs := []config.HookConfig{
		{Name: "h", Event: "pre_tool_use", Type: "command", Exec: "echo ok", Timeout: "notaduration"},
	}
	_, err := ParseHookDefs(cfgs)
	if err == nil {
		t.Error("expected error for invalid timeout")
	}
}

func TestParseHookDefs_EmptyName(t *testing.T) {
	cfgs := []config.HookConfig{
		{Name: "", Event: "pre_tool_use", Type: "command", Exec: "echo ok"},
	}
	_, err := ParseHookDefs(cfgs)
	if err == nil {
		t.Error("expected error for empty name")
	}
}

func TestParseHookDefs_EmptyExec(t *testing.T) {
	cfgs := []config.HookConfig{
		{Name: "h", Event: "pre_tool_use", Type: "command", Exec: ""},
	}
	_, err := ParseHookDefs(cfgs)
	if err == nil {
		t.Error("expected error for empty exec")
	}
}

func TestParseHookDefs_ToolPatternOnNonToolEvent_Ignored(t *testing.T) {
	// ToolPattern on SessionStart is silently ignored (cleared).
	cfgs := []config.HookConfig{
		{Name: "h", Event: "session_start", Type: "command", Exec: "echo ok", ToolPattern: "bash*"},
	}
	defs, err := ParseHookDefs(cfgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if defs[0].ToolPattern != "" {
		t.Errorf("ToolPattern should be cleared for non-tool events, got %q", defs[0].ToolPattern)
	}
}

func TestParseHookDefs_Empty(t *testing.T) {
	defs, err := ParseHookDefs(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(defs) != 0 {
		t.Errorf("expected empty slice, got %d", len(defs))
	}
}

func TestParseHookDefs_MultipleTypes(t *testing.T) {
	cfgs := []config.HookConfig{
		{Name: "cmd-hook", Event: "pre_tool_use", Type: "command", Exec: "echo ok"},
		{Name: "prompt-hook", Event: "pre_tool_use", Type: "prompt", Exec: "Is this safe? ALLOW or DENY."},
		{Name: "agent-hook", Event: "pre_tool_use", Type: "agent", Exec: "Review this tool call."},
	}
	defs, err := ParseHookDefs(cfgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if defs[0].Command != CommandTypeShell {
		t.Errorf("defs[0].Command = %v, want CommandTypeShell", defs[0].Command)
	}
	if defs[1].Command != CommandTypePrompt {
		t.Errorf("defs[1].Command = %v, want CommandTypePrompt", defs[1].Command)
	}
	if defs[2].Command != CommandTypeAgent {
		t.Errorf("defs[2].Command = %v, want CommandTypeAgent", defs[2].Command)
	}
}
