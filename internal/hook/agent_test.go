package hook

import (
	"context"
	"errors"
	"testing"
	"time"

	"somegit.dev/Owlibou/gnoma/internal/elf"
	"somegit.dev/Owlibou/gnoma/internal/router"
	"somegit.dev/Owlibou/gnoma/internal/stream"
)

// mockElfSpawner satisfies ElfSpawner. Records calls and returns configurable results.
type mockElfSpawner struct {
	result elf.Result
	err    error
	// Captures
	lastPrompt string
	lastTask   router.TaskType
}

func (m *mockElfSpawner) Spawn(ctx context.Context, taskType router.TaskType, prompt, systemPrompt string, maxTurns int) (elf.Elf, error) {
	m.lastPrompt = prompt
	m.lastTask = taskType
	if m.err != nil {
		return nil, m.err
	}
	return &immediateElf{result: m.result}, nil
}

// immediateElf returns a pre-computed result immediately.
type immediateElf struct {
	result elf.Result
}

func (e *immediateElf) ID() string                        { return "test-elf" }
func (e *immediateElf) Status() elf.Status                { return e.result.Status }
func (e *immediateElf) Events() <-chan stream.Event        { return nil }
func (e *immediateElf) Wait() elf.Result                  { return e.result }
func (e *immediateElf) Cancel()                           {}

func TestAgentExecutor_OutputALLOW(t *testing.T) {
	def := HookDef{Name: "test", Event: PreToolUse, Command: CommandTypeAgent, Exec: "Review this tool call."}
	spawner := &mockElfSpawner{result: elf.Result{Output: "After analysis, ALLOW this.", Status: elf.StatusCompleted}}
	ex := NewAgentExecutor(def, spawner)
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
	spawner := &mockElfSpawner{result: elf.Result{Output: "This is dangerous. DENY.", Status: elf.StatusCompleted}}
	ex := NewAgentExecutor(def, spawner)
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
	spawner := &mockElfSpawner{result: elf.Result{Output: "I'm unsure.", Status: elf.StatusCompleted}}
	ex := NewAgentExecutor(def, spawner)
	result, err := ex.Execute(context.Background(), MarshalPreToolPayload("bash", nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != Skip {
		t.Errorf("action = %v, want Skip", result.Action)
	}
}

func TestAgentExecutor_ElfFailure_Error(t *testing.T) {
	def := HookDef{Name: "test", Event: PreToolUse, Command: CommandTypeAgent, Exec: "Review."}
	spawner := &mockElfSpawner{result: elf.Result{
		Output: "",
		Status: elf.StatusFailed,
		Error:  errors.New("elf crashed"),
	}}
	ex := NewAgentExecutor(def, spawner)
	_, err := ex.Execute(context.Background(), MarshalPreToolPayload("bash", nil))
	if err == nil {
		t.Error("expected error for failed elf")
	}
}

func TestAgentExecutor_SpawnError(t *testing.T) {
	def := HookDef{Name: "test", Event: PreToolUse, Command: CommandTypeAgent, Exec: "Review."}
	spawner := &mockElfSpawner{err: errors.New("no arms available")}
	ex := NewAgentExecutor(def, spawner)
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
	spawner := &mockElfSpawner{result: elf.Result{Output: "ALLOW", Status: elf.StatusCompleted}}
	ex := NewAgentExecutor(def, spawner)
	ex.Execute(context.Background(), MarshalPreToolPayload("bash", nil))
	if spawner.lastPrompt != "Tool=bash Event=pre_tool_use" {
		t.Errorf("prompt = %q", spawner.lastPrompt)
	}
}

func TestAgentExecutor_Duration(t *testing.T) {
	def := HookDef{Name: "test", Event: PreToolUse, Command: CommandTypeAgent, Exec: "Review."}
	spawner := &mockElfSpawner{result: elf.Result{Output: "ALLOW", Status: elf.StatusCompleted, Duration: 100 * time.Millisecond}}
	ex := NewAgentExecutor(def, spawner)
	result, _ := ex.Execute(context.Background(), MarshalPreToolPayload("bash", nil))
	if result.Duration <= 0 {
		t.Error("expected Duration > 0")
	}
}

func TestAgentExecutor_TaskTypeIsReview(t *testing.T) {
	def := HookDef{Name: "test", Event: PreToolUse, Command: CommandTypeAgent, Exec: "Review."}
	spawner := &mockElfSpawner{result: elf.Result{Output: "ALLOW", Status: elf.StatusCompleted}}
	ex := NewAgentExecutor(def, spawner)
	ex.Execute(context.Background(), MarshalPreToolPayload("bash", nil))
	if spawner.lastTask != router.TaskReview {
		t.Errorf("task type = %v, want TaskReview", spawner.lastTask)
	}
}
