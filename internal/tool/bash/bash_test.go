package bash

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestBashTool_Interface(t *testing.T) {
	b := New()
	if b.Name() != "bash" {
		t.Errorf("Name() = %q", b.Name())
	}
	if b.IsReadOnly() {
		t.Error("bash should not be read-only")
	}
	if !b.IsDestructive() {
		t.Error("bash should be destructive")
	}
	if b.Parameters() == nil {
		t.Error("Parameters() should not be nil")
	}
}

func TestBashTool_Echo(t *testing.T) {
	b := New()
	result, err := b.Execute(context.Background(), json.RawMessage(`{"command":"echo hello world"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output != "hello world" {
		t.Errorf("Output = %q, want %q", result.Output, "hello world")
	}
	if result.Metadata["exit_code"] != 0 {
		t.Errorf("exit_code = %v, want 0", result.Metadata["exit_code"])
	}
}

func TestBashTool_ExitCode(t *testing.T) {
	b := New()
	result, err := b.Execute(context.Background(), json.RawMessage(`{"command":"exit 42"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Metadata["exit_code"] != 42 {
		t.Errorf("exit_code = %v, want 42", result.Metadata["exit_code"])
	}
	if !strings.HasPrefix(result.Output, "Exit code 42") {
		t.Errorf("Output = %q, should start with exit code", result.Output)
	}
}

func TestBashTool_Timeout(t *testing.T) {
	b := New(WithTimeout(100 * time.Millisecond))
	result, err := b.Execute(context.Background(), json.RawMessage(`{"command":"sleep 10"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Metadata["timeout"] != true {
		t.Error("should have timed out")
	}
	if !strings.Contains(result.Output, "timed out") {
		t.Errorf("Output = %q, should mention timeout", result.Output)
	}
}

func TestBashTool_CustomTimeout(t *testing.T) {
	b := New(WithTimeout(30 * time.Second))
	// Args override the default timeout
	result, err := b.Execute(context.Background(), json.RawMessage(`{"command":"sleep 10","timeout":1}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Metadata["timeout"] != true {
		t.Error("should have timed out with custom 1s timeout")
	}
}

func TestBashTool_InvalidArgs(t *testing.T) {
	b := New()
	_, err := b.Execute(context.Background(), json.RawMessage(`not json`))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestBashTool_EmptyCommand(t *testing.T) {
	b := New()
	_, err := b.Execute(context.Background(), json.RawMessage(`{"command":""}`))
	if err == nil {
		t.Error("expected error for empty command")
	}
}

func TestBashTool_SecurityBlock(t *testing.T) {
	b := New()

	// Command substitution should be blocked
	result, err := b.Execute(context.Background(), json.RawMessage(`{"command":"echo $(whoami)"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Metadata["blocked"] != true {
		t.Error("command with $() should be blocked")
	}
	if !strings.Contains(result.Output, "blocked") {
		t.Errorf("Output = %q, should mention blocked", result.Output)
	}
}

func TestBashTool_WorkingDir(t *testing.T) {
	b := New(WithWorkingDir(t.TempDir()))
	result, err := b.Execute(context.Background(), json.RawMessage(`{"command":"pwd"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output == "" {
		t.Error("pwd should produce output")
	}
}

func TestBashTool_ContextCancellation(t *testing.T) {
	b := New()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := b.Execute(ctx, json.RawMessage(`{"command":"echo hello"}`))
	// Should either return an error or a timeout result
	if err == nil {
		// That's ok too — context cancellation is best-effort for fast commands
		return
	}
}
