package hook

import (
	"context"
	"errors"
	"testing"
	"time"
)

func spawnFnOK(output string) ElfSpawnFn {
	return func(_ context.Context, _ string) (string, error) {
		return output, nil
	}
}

func spawnFnErr(err error) ElfSpawnFn {
	return func(_ context.Context, _ string) (string, error) {
		return "", err
	}
}

func capturingSpawnFn(output string) (ElfSpawnFn, *string) {
	captured := new(string)
	fn := func(_ context.Context, prompt string) (string, error) {
		*captured = prompt
		return output, nil
	}
	return fn, captured
}

func TestAgentExecutor_OutputALLOW(t *testing.T) {
	def := HookDef{Name: "test", Event: PreToolUse, Command: CommandTypeAgent, Exec: "Review this tool call."}
	ex := NewAgentExecutor(def, spawnFnOK("After analysis, ALLOW this."))
	result, err := ex.Execute(context.Background(), MarshalPreToolPayload("bash", nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != Allow {
		t.Errorf("action = %v, want Allow", result.Action)
	}
}

func TestAgentExecutor_OutputDENY(t *testing.T) {
	def := HookDef{Name: "test", Event: PreToolUse, Command: CommandTypeAgent, Exec: "Review this."}
	ex := NewAgentExecutor(def, spawnFnOK("This is dangerous. DENY."))
	result, err := ex.Execute(context.Background(), MarshalPreToolPayload("bash", nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != Deny {
		t.Errorf("action = %v, want Deny", result.Action)
	}
}

func TestAgentExecutor_OutputNoMatch_Skip(t *testing.T) {
	def := HookDef{Name: "test", Event: PreToolUse, Command: CommandTypeAgent, Exec: "Review this."}
	ex := NewAgentExecutor(def, spawnFnOK("I'm unsure."))
	result, err := ex.Execute(context.Background(), MarshalPreToolPayload("bash", nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != Skip {
		t.Errorf("action = %v, want Skip", result.Action)
	}
}

func TestAgentExecutor_SpawnError(t *testing.T) {
	def := HookDef{Name: "test", Event: PreToolUse, Command: CommandTypeAgent, Exec: "Review."}
	ex := NewAgentExecutor(def, spawnFnErr(errors.New("no arms available")))
	_, err := ex.Execute(context.Background(), MarshalPreToolPayload("bash", nil))
	if err == nil {
		t.Error("expected error when spawn fails")
	}
}

func TestAgentExecutor_TemplateRendered(t *testing.T) {
	def := HookDef{
		Name:    "test",
		Event:   PreToolUse,
		Command: CommandTypeAgent,
		Exec:    "Tool={{.Tool}} Event={{.Event}}",
	}
	fn, captured := capturingSpawnFn("ALLOW")
	ex := NewAgentExecutor(def, fn)
	ex.Execute(context.Background(), MarshalPreToolPayload("bash", nil))
	if *captured != "Tool=bash Event=pre_tool_use" {
		t.Errorf("prompt = %q", *captured)
	}
}

func TestAgentExecutor_Duration(t *testing.T) {
	def := HookDef{Name: "test", Event: PreToolUse, Command: CommandTypeAgent, Exec: "Review."}
	fn := func(_ context.Context, _ string) (string, error) {
		time.Sleep(1 * time.Millisecond)
		return "ALLOW", nil
	}
	ex := NewAgentExecutor(def, fn)
	result, _ := ex.Execute(context.Background(), MarshalPreToolPayload("bash", nil))
	if result.Duration <= 0 {
		t.Error("expected Duration > 0")
	}
}
