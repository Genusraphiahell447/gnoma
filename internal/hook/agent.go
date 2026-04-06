package hook

import (
	"context"
	"fmt"
	"time"

	"somegit.dev/Owlibou/gnoma/internal/elf"
	"somegit.dev/Owlibou/gnoma/internal/router"
)

// ElfSpawner is the minimal interface AgentExecutor needs from elf.Manager.
type ElfSpawner interface {
	Spawn(ctx context.Context, taskType router.TaskType, prompt, systemPrompt string, maxTurns int) (elf.Elf, error)
}

// AgentExecutor spawns an elf and parses ALLOW/DENY from its output.
type AgentExecutor struct {
	def     HookDef
	spawner ElfSpawner
}

// NewAgentExecutor constructs an AgentExecutor.
func NewAgentExecutor(def HookDef, spawner ElfSpawner) *AgentExecutor {
	return &AgentExecutor{def: def, spawner: spawner}
}

// Execute renders the hook template, spawns an elf, waits for its result,
// and returns Allow/Deny/Skip based on the output.
func (a *AgentExecutor) Execute(ctx context.Context, payload []byte) (HookResult, error) {
	data := templateDataFromPayload(payload, a.def.Event)
	prompt, err := renderTemplate(a.def.Exec, data)
	if err != nil {
		return HookResult{}, fmt.Errorf("hook %q: %w", a.def.Name, err)
	}

	start := time.Now()
	e, err := a.spawner.Spawn(ctx, router.TaskReview, prompt, "", 5)
	if err != nil {
		return HookResult{}, fmt.Errorf("hook %q: spawn elf: %w", a.def.Name, err)
	}

	result := e.Wait()
	duration := time.Since(start)

	if result.Error != nil {
		return HookResult{Duration: duration}, fmt.Errorf("hook %q: elf failed: %w", a.def.Name, result.Error)
	}

	action := parseDecision(result.Output)
	return HookResult{
		Action:   action,
		Duration: duration,
	}, nil
}
