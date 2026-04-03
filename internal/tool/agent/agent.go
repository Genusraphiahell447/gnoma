package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"somegit.dev/Owlibou/gnoma/internal/elf"
	"somegit.dev/Owlibou/gnoma/internal/router"
	"somegit.dev/Owlibou/gnoma/internal/stream"
	"somegit.dev/Owlibou/gnoma/internal/tool"
)

var paramSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"prompt": {
			"type": "string",
			"description": "The task prompt for the sub-agent (elf)"
		},
		"task_type": {
			"type": "string",
			"description": "Task type hint for provider routing",
			"enum": ["generation", "review", "refactor", "debug", "explain", "planning"]
		},
		"wait": {
			"type": "boolean",
			"description": "Wait for the elf to complete (default true)"
		}
	},
	"required": ["prompt"]
}`)

// Tool allows the LLM to spawn sub-agents (elfs).
type Tool struct {
	manager    *elf.Manager
	ProgressCh chan<- string // optional: sends 2-line progress to TUI
}

func New(mgr *elf.Manager) *Tool {
	return &Tool{manager: mgr}
}

// SetProgressCh sets the channel for forwarding elf progress to the TUI.
func (t *Tool) SetProgressCh(ch chan<- string) {
	t.ProgressCh = ch
}

func (t *Tool) Name() string               { return "agent" }
func (t *Tool) Description() string         { return "Spawn a sub-agent (elf) to handle a task independently. The elf gets its own conversation and tools." }
func (t *Tool) Parameters() json.RawMessage { return paramSchema }
func (t *Tool) IsReadOnly() bool            { return true }
func (t *Tool) IsDestructive() bool         { return false }

type agentArgs struct {
	Prompt   string `json:"prompt"`
	TaskType string `json:"task_type,omitempty"`
	Wait     *bool  `json:"wait,omitempty"`
}

func (t *Tool) Execute(ctx context.Context, args json.RawMessage) (tool.Result, error) {
	var a agentArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return tool.Result{}, fmt.Errorf("agent: invalid args: %w", err)
	}
	if a.Prompt == "" {
		return tool.Result{}, fmt.Errorf("agent: prompt required")
	}

	taskType := parseTaskType(a.TaskType)
	wait := true
	if a.Wait != nil {
		wait = *a.Wait
	}

	systemPrompt := "You are an elf — a focused sub-agent of gnoma. Complete the given task thoroughly and concisely. Use tools as needed."

	e, err := t.manager.Spawn(ctx, taskType, a.Prompt, systemPrompt)
	if err != nil {
		return tool.Result{Output: fmt.Sprintf("Failed to spawn elf: %v", err)}, nil
	}

	if !wait {
		return tool.Result{
			Output:   fmt.Sprintf("Elf %s spawned in background (task: %s)", e.ID(), taskType),
			Metadata: map[string]any{"elf_id": e.ID(), "background": true},
		}, nil
	}

	// Drain elf events while waiting, forward progress to TUI
	done := make(chan elf.Result, 1)
	go func() { done <- e.Wait() }()

	// Forward elf streaming events as progress
	go func() {
		var lastLines [2]string
		for evt := range e.Events() {
			if evt.Type == stream.EventTextDelta && evt.Text != "" {
				// Accumulate and keep last 2 lines
				text := lastLines[0] + lastLines[1] + evt.Text
				lines := strings.Split(text, "\n")
				if len(lines) >= 2 {
					lastLines[0] = lines[len(lines)-2]
					lastLines[1] = lines[len(lines)-1]
				} else if len(lines) == 1 {
					lastLines[0] = lastLines[1]
					lastLines[1] = lines[0]
				}
				if t.ProgressCh != nil {
					progress := strings.TrimSpace(lastLines[0])
					if l1 := strings.TrimSpace(lastLines[1]); l1 != "" {
						progress += "\n" + l1
					}
					select {
					case t.ProgressCh <- progress:
					default: // don't block
					}
				}
			}
		}
	}()

	var result elf.Result
	select {
	case result = <-done:
	case <-ctx.Done():
		e.Cancel()
		return tool.Result{Output: "Elf cancelled"}, nil
	case <-time.After(5 * time.Minute):
		e.Cancel()
		return tool.Result{Output: "Elf timed out after 5 minutes"}, nil
	}

	// Clear progress
	if t.ProgressCh != nil {
		select {
		case t.ProgressCh <- "":
		default:
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Elf %s completed (%s, %s)\n\n", result.ID, result.Status, result.Duration.Round(time.Millisecond))
	if result.Error != nil {
		fmt.Fprintf(&b, "Error: %v\n", result.Error)
	}
	if result.Output != "" {
		b.WriteString(result.Output)
	}

	return tool.Result{
		Output: b.String(),
		Metadata: map[string]any{
			"elf_id":   result.ID,
			"status":   result.Status.String(),
			"duration": result.Duration.String(),
		},
	}, nil
}

func parseTaskType(s string) router.TaskType {
	switch strings.ToLower(s) {
	case "generation":
		return router.TaskGeneration
	case "review":
		return router.TaskReview
	case "refactor":
		return router.TaskRefactor
	case "debug":
		return router.TaskDebug
	case "explain":
		return router.TaskExplain
	case "planning":
		return router.TaskPlanning
	default:
		return router.TaskGeneration
	}
}
