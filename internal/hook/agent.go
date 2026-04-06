package hook

import (
	"context"
	"fmt"
	"time"
)

// ElfSpawnFn spawns an elf with the given prompt and returns its output text.
// This is satisfied by a closure wrapping elf.Manager.Spawn in main.go.
type ElfSpawnFn func(ctx context.Context, prompt string) (output string, err error)

// AgentExecutor spawns an elf and parses ALLOW/DENY from its output.
type AgentExecutor struct {
	def     HookDef
	spawnFn ElfSpawnFn
}

// NewAgentExecutor constructs an AgentExecutor.
func NewAgentExecutor(def HookDef, spawnFn ElfSpawnFn) *AgentExecutor {
	return &AgentExecutor{def: def, spawnFn: spawnFn}
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
	output, err := a.spawnFn(ctx, prompt)
	duration := time.Since(start)
	if err != nil {
		return HookResult{Duration: duration}, fmt.Errorf("hook %q: elf failed: %w", a.def.Name, err)
	}

	action := parseDecision(output)
	return HookResult{
		Action:   action,
		Duration: duration,
	}, nil
}
