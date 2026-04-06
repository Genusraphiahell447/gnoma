package hook

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestCommandExecutor_ExitZero_Allow(t *testing.T) {
	def := HookDef{Name: "test", Event: PreToolUse, Command: CommandTypeShell, Exec: "exit 0"}
	ex := NewCommandExecutor(def)
	result, err := ex.Execute(context.Background(), []byte(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != Allow {
		t.Errorf("action = %v, want Allow", result.Action)
	}
}

func TestCommandExecutor_ExitOne_Skip(t *testing.T) {
	def := HookDef{Name: "test", Event: PreToolUse, Command: CommandTypeShell, Exec: "exit 1"}
	ex := NewCommandExecutor(def)
	result, err := ex.Execute(context.Background(), []byte(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != Skip {
		t.Errorf("action = %v, want Skip", result.Action)
	}
}

func TestCommandExecutor_ExitTwo_Deny(t *testing.T) {
	def := HookDef{Name: "test", Event: PreToolUse, Command: CommandTypeShell, Exec: "exit 2"}
	ex := NewCommandExecutor(def)
	result, err := ex.Execute(context.Background(), []byte(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != Deny {
		t.Errorf("action = %v, want Deny", result.Action)
	}
}

func TestCommandExecutor_StdinDelivered(t *testing.T) {
	// Hook reads stdin and echoes it back to stdout as JSON with action=allow
	def := HookDef{
		Name:    "test",
		Event:   PreToolUse,
		Command: CommandTypeShell,
		Exec:    `read -r line; echo '{"action":"allow","transformed":'"$line"'}'; exit 0`,
	}
	ex := NewCommandExecutor(def)
	payload := []byte(`{"tool":"bash"}`)
	result, err := ex.Execute(context.Background(), payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != Allow {
		t.Errorf("action = %v, want Allow", result.Action)
	}
}

func TestCommandExecutor_StdoutJSON_Parsed(t *testing.T) {
	// Hook outputs JSON with transformed payload
	def := HookDef{
		Name:    "test",
		Event:   PreToolUse,
		Command: CommandTypeShell,
		Exec:    `echo '{"action":"deny","transformed":{"command":"safe"}}'`,
	}
	ex := NewCommandExecutor(def)
	result, err := ex.Execute(context.Background(), []byte(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != Deny {
		t.Errorf("action = %v, want Deny", result.Action)
	}
	if result.Output == nil {
		t.Error("expected non-nil transformed output")
	}
}

func TestCommandExecutor_StdoutJSON_ActionOverridesExitCode(t *testing.T) {
	// exit 0, but stdout says deny
	def := HookDef{
		Name:    "test",
		Event:   PreToolUse,
		Command: CommandTypeShell,
		Exec:    `echo '{"action":"deny"}'; exit 0`,
	}
	ex := NewCommandExecutor(def)
	result, err := ex.Execute(context.Background(), []byte(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != Deny {
		t.Errorf("action = %v, want Deny", result.Action)
	}
}

func TestCommandExecutor_EmptyStdout_ExitCodeFallback(t *testing.T) {
	def := HookDef{
		Name:    "test",
		Event:   PreToolUse,
		Command: CommandTypeShell,
		Exec:    `exit 2`,
	}
	ex := NewCommandExecutor(def)
	result, err := ex.Execute(context.Background(), []byte(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != Deny {
		t.Errorf("action = %v, want Deny", result.Action)
	}
	if result.Output != nil {
		t.Error("expected nil output for empty stdout")
	}
}

func TestCommandExecutor_Timeout_FailOpenTrue(t *testing.T) {
	def := HookDef{
		Name:     "test",
		Event:    PreToolUse,
		Command:  CommandTypeShell,
		Exec:     "sleep 10",
		Timeout:  50 * time.Millisecond,
		FailOpen: true,
	}
	ex := NewCommandExecutor(def)
	result, err := ex.Execute(context.Background(), []byte(`{}`))
	if err == nil {
		t.Fatal("expected error on timeout")
	}
	if result.Action != Allow {
		t.Errorf("fail_open=true: action = %v, want Allow", result.Action)
	}
}

func TestCommandExecutor_Timeout_FailOpenFalse(t *testing.T) {
	def := HookDef{
		Name:     "test",
		Event:    PreToolUse,
		Command:  CommandTypeShell,
		Exec:     "sleep 10",
		Timeout:  50 * time.Millisecond,
		FailOpen: false,
	}
	ex := NewCommandExecutor(def)
	result, err := ex.Execute(context.Background(), []byte(`{}`))
	if err == nil {
		t.Fatal("expected error on timeout")
	}
	if result.Action != Deny {
		t.Errorf("fail_open=false: action = %v, want Deny", result.Action)
	}
}

func TestCommandExecutor_InvalidJSON_Stdout(t *testing.T) {
	// Hook writes garbage — should error
	def := HookDef{
		Name:    "test",
		Event:   PreToolUse,
		Command: CommandTypeShell,
		Exec:    `echo "not valid json"`,
	}
	ex := NewCommandExecutor(def)
	result, err := ex.Execute(context.Background(), []byte(`{}`))
	if err == nil {
		t.Fatal("expected error for invalid JSON stdout")
	}
	// fail_open=false (default) → Deny on error
	if result.Action != Deny {
		t.Errorf("action = %v, want Deny on error with fail_open=false", result.Action)
	}
}

func TestCommandExecutor_Duration_Recorded(t *testing.T) {
	def := HookDef{Name: "test", Event: PreToolUse, Command: CommandTypeShell, Exec: "exit 0"}
	ex := NewCommandExecutor(def)
	result, _ := ex.Execute(context.Background(), []byte(`{}`))
	if result.Duration <= 0 {
		t.Error("expected Duration > 0")
	}
}

func TestCommandExecutor_PreToolPayload_HasToolField(t *testing.T) {
	// Verify that a real pre_tool_use payload round-trips through the executor
	args := json.RawMessage(`{"command":"ls"}`)
	payload := MarshalPreToolPayload("bash", args)

	def := HookDef{
		Name:    "test",
		Event:   PreToolUse,
		Command: CommandTypeShell,
		// Read tool name from stdin, allow if it's "bash"
		Exec: `input=$(cat); tool=$(echo "$input" | grep -o '"tool":"[^"]*"' | cut -d'"' -f4); [ "$tool" = "bash" ] && exit 0 || exit 2`,
	}
	ex := NewCommandExecutor(def)
	result, err := ex.Execute(context.Background(), payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != Allow {
		t.Errorf("action = %v, want Allow for tool=bash", result.Action)
	}
}

// Verify the executor honours context cancellation in addition to its own timeout.
func TestCommandExecutor_ContextCancelled(t *testing.T) {
	def := HookDef{
		Name:     "test",
		Event:    PreToolUse,
		Command:  CommandTypeShell,
		Exec:     "sleep 10",
		FailOpen: true,
	}
	ex := NewCommandExecutor(def)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	result, err := ex.Execute(ctx, []byte(`{}`))
	if err == nil {
		t.Fatal("expected error on context cancellation")
	}
	if !strings.Contains(err.Error(), "hook") {
		t.Logf("error = %v", err) // just informational
	}
	if result.Action != Allow { // fail_open=true
		t.Errorf("action = %v, want Allow (fail_open=true)", result.Action)
	}
}
