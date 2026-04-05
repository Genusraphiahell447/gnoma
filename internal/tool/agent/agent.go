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
	"somegit.dev/Owlibou/gnoma/internal/tool/persist"
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
		"max_turns": {
			"type": "integer",
			"description": "Maximum tool-calling rounds for the elf (0 or omit = unlimited)"
		}
	},
	"required": ["prompt"]
}`)

// Tool allows the LLM to spawn sub-agents (elfs).
type Tool struct {
	manager    *elf.Manager
	ProgressCh chan<- elf.Progress // optional: sends structured progress to TUI
	store      *persist.Store
}

func New(mgr *elf.Manager, store *persist.Store) *Tool {
	return &Tool{manager: mgr, store: store}
}

// SetProgressCh sets the channel for forwarding elf progress to the TUI.
func (t *Tool) SetProgressCh(ch chan<- elf.Progress) {
	t.ProgressCh = ch
}

func (t *Tool) Name() string               { return "agent" }
func (t *Tool) Description() string         { return "Spawn a sub-agent (elf) to handle a task independently. The elf gets its own conversation and tools. IMPORTANT: To spawn multiple elfs in parallel, call this tool multiple times in the SAME response — do not wait for one to finish before spawning the next." }
func (t *Tool) Parameters() json.RawMessage { return paramSchema }
func (t *Tool) IsReadOnly() bool  { return true }
func (t *Tool) IsDestructive() bool { return false }

type agentArgs struct {
	Prompt   string `json:"prompt"`
	TaskType string `json:"task_type,omitempty"`
	MaxTurns int    `json:"max_turns,omitempty"`
}

func (t *Tool) Execute(ctx context.Context, args json.RawMessage) (tool.Result, error) {
	var a agentArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return tool.Result{}, fmt.Errorf("agent: invalid args: %w", err)
	}
	if a.Prompt == "" {
		return tool.Result{}, fmt.Errorf("agent: prompt required")
	}

	taskType := parseTaskType(a.TaskType, a.Prompt)
	maxTurns := a.MaxTurns

	// Truncate description for tree display
	desc := a.Prompt
	if len(desc) > 60 {
		desc = desc[:60] + "…"
	}

	systemPrompt := "You are an elf — a focused sub-agent of gnoma. Complete the given task thoroughly and concisely. Use tools as needed."

	var preSave []persist.ResultFile
	if t.store != nil {
		preSave, _ = t.store.List("")
	}

	e, err := t.manager.Spawn(ctx, taskType, a.Prompt, systemPrompt, maxTurns)
	if err != nil {
		return tool.Result{Output: fmt.Sprintf("Failed to spawn elf: %v", err)}, nil
	}

	// Send initial progress
	t.sendProgress(elf.Progress{
		ElfID:       e.ID(),
		Description: desc,
		Activity:    "starting…",
	})

	// Drain elf events while waiting, forward progress to TUI
	done := make(chan elf.Result, 1)
	go func() { done <- e.Wait() }()

	// Forward elf streaming events as structured progress
	go func() {
		toolUses := 0
		tokens := 0
		lastSend := time.Now()
		textChars := 0

		for evt := range e.Events() {
			if t.ProgressCh == nil {
				continue
			}

			p := elf.Progress{
				ElfID:       e.ID(),
				Description: desc,
				ToolUses:    toolUses,
				Tokens:      tokens,
			}

			switch evt.Type {
			case stream.EventTextDelta:
				textChars += len(evt.Text)
				// Throttle text progress to every 500ms
				if time.Since(lastSend) < 500*time.Millisecond {
					continue
				}
				p.Activity = fmt.Sprintf("generating… (%d chars)", textChars)
			case stream.EventToolCallDone:
				name := evt.ToolCallName
				if name == "" {
					name = "tool"
				}
				p.Activity = fmt.Sprintf("⚙ [%s] running…", name)
			case stream.EventToolResult:
				toolUses++
				p.ToolUses = toolUses
				out := evt.ToolOutput
				if len(out) > 60 {
					out = out[:60] + "…"
				}
				out = strings.ReplaceAll(out, "\n", " ")
				p.Activity = fmt.Sprintf("→ %s", out)
			case stream.EventUsage:
				if evt.Usage != nil {
					tokens = int(evt.Usage.TotalTokens())
					p.Tokens = tokens
				}
				p.Activity = "" // no activity change on usage alone
			default:
				continue
			}

			lastSend = time.Now()
			t.sendProgress(p)
		}
	}()

	var result elf.Result
	select {
	case result = <-done:
	case <-ctx.Done():
		e.Cancel()
		t.sendProgress(elf.Progress{ElfID: e.ID(), Description: desc, Done: true, Error: "cancelled"})
		return tool.Result{Output: "Elf cancelled"}, nil
	case <-time.After(5 * time.Minute):
		e.Cancel()
		t.sendProgress(elf.Progress{ElfID: e.ID(), Description: desc, Done: true, Error: "timed out"})
		return tool.Result{Output: "Elf timed out after 5 minutes"}, nil
	}

	// Attribute /tmp result files produced during this elf's run
	if t.store != nil {
		postSave, _ := t.store.List("")
		preSet := make(map[string]bool, len(preSave))
		for _, f := range preSave {
			preSet[f.Path] = true
		}
		for _, f := range postSave {
			if !preSet[f.Path] {
				result.ResultFilePaths = append(result.ResultFilePaths, f.Path)
			}
		}
	}
	t.manager.ReportResult(result)

	// Send done signal — stays in tree until turn completes
	doneProgress := elf.Progress{
		ElfID:       result.ID,
		Description: desc,
		Tokens:      int(result.Usage.TotalTokens()),
		Done:        true,
		Duration:    result.Duration,
	}
	if result.Error != nil {
		doneProgress.Error = result.Error.Error()
	}
	t.sendProgress(doneProgress)

	var b strings.Builder
	fmt.Fprintf(&b, "Elf %s completed (%s, %s, %s)\n\n",
		result.ID, result.Status,
		result.Duration.Round(time.Millisecond),
		formatTokens(int(result.Usage.TotalTokens())),
	)
	if result.Error != nil {
		fmt.Fprintf(&b, "Error: %v\n", result.Error)
	}
	if result.Output != "" {
		// Truncate elf output to avoid flooding parent context.
		// The parent LLM gets enough to summarize; full text stays in the elf.
		b.WriteString(truncateOutput(result.Output, maxOutputChars))
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

func (t *Tool) sendProgress(p elf.Progress) {
	if t.ProgressCh == nil {
		return
	}
	select {
	case t.ProgressCh <- p:
	default:
	}
}

func formatTokens(tokens int) string {
	if tokens >= 1_000_000 {
		return fmt.Sprintf("%.1fM tokens", float64(tokens)/1_000_000)
	}
	if tokens >= 1_000 {
		return fmt.Sprintf("%.1fk tokens", float64(tokens)/1_000)
	}
	return fmt.Sprintf("%d tokens", tokens)
}

// parseTaskType maps explicit task_type hints to router TaskType.
// When no hint is provided (empty string), auto-classifies from the prompt.
func parseTaskType(s string, prompt string) router.TaskType {
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
		return router.ClassifyTask(prompt).Type
	}
}
