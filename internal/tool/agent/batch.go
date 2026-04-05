package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"somegit.dev/Owlibou/gnoma/internal/elf"
	"somegit.dev/Owlibou/gnoma/internal/stream"
	"somegit.dev/Owlibou/gnoma/internal/tool"
)

var batchSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"tasks": {
			"type": "array",
			"description": "List of tasks to execute in parallel. Each elf gets its own conversation and tools.",
			"items": {
				"type": "object",
				"properties": {
					"prompt": {
						"type": "string",
						"description": "The task prompt for the elf"
					},
					"task_type": {
						"type": "string",
						"description": "Task type hint for provider routing",
						"enum": ["generation", "review", "refactor", "debug", "explain", "planning"]
					}
				},
				"required": ["prompt"]
			},
			"minItems": 1,
			"maxItems": 10
		},
		"max_turns": {
			"type": "integer",
			"description": "Maximum tool-calling rounds per elf (0 or omit = unlimited)"
		}
	},
	"required": ["tasks"]
}`)

// BatchTool spawns multiple elfs in parallel from a single tool call.
type BatchTool struct {
	manager    *elf.Manager
	progressCh chan<- elf.Progress
}

func NewBatch(mgr *elf.Manager) *BatchTool {
	return &BatchTool{manager: mgr}
}

func (t *BatchTool) SetProgressCh(ch chan<- elf.Progress) {
	t.progressCh = ch
}

func (t *BatchTool) Name() string               { return "spawn_elfs" }
func (t *BatchTool) Description() string         { return "Spawn multiple elfs (sub-agents) in parallel. Use this when you need to run 2+ independent tasks concurrently. Each elf gets its own conversation and tools. All elfs run simultaneously and results are collected when all complete." }
func (t *BatchTool) Parameters() json.RawMessage { return batchSchema }
func (t *BatchTool) IsReadOnly() bool  { return true }
func (t *BatchTool) IsDestructive() bool { return false }

type batchArgs struct {
	Tasks    []batchTask `json:"tasks"`
	MaxTurns int         `json:"max_turns,omitempty"`
}

type batchTask struct {
	Prompt   string `json:"prompt"`
	TaskType string `json:"task_type,omitempty"`
}

func (t *BatchTool) Execute(ctx context.Context, args json.RawMessage) (tool.Result, error) {
	var a batchArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return tool.Result{}, fmt.Errorf("spawn_elfs: invalid args: %w", err)
	}
	if len(a.Tasks) == 0 {
		return tool.Result{}, fmt.Errorf("spawn_elfs: at least one task required")
	}
	if len(a.Tasks) > 10 {
		return tool.Result{}, fmt.Errorf("spawn_elfs: max 10 tasks per batch")
	}

	maxTurns := a.MaxTurns

	systemPrompt := "You are an elf — a focused sub-agent of gnoma. Complete the given task thoroughly and concisely. Use tools as needed."

	// Spawn all elfs with slight stagger to avoid rate limit bursts
	type elfEntry struct {
		elf  elf.Elf
		desc string
		task batchTask
	}
	var elfs []elfEntry

	for i, task := range a.Tasks {
		// Stagger spawns to avoid hitting rate limits (e.g., Mistral's 1 req/s)
		if i > 0 {
			select {
			case <-time.After(300 * time.Millisecond):
			case <-ctx.Done():
				for _, entry := range elfs {
					entry.elf.Cancel()
				}
				return tool.Result{Output: "cancelled during spawn"}, nil
			}
		}

		taskType := parseTaskType(task.TaskType, task.Prompt)
		e, err := t.manager.Spawn(ctx, taskType, task.Prompt, systemPrompt, maxTurns)
		if err != nil {
			for _, entry := range elfs {
				entry.elf.Cancel()
			}
			return tool.Result{Output: fmt.Sprintf("Failed to spawn elf: %v", err)}, nil
		}

		desc := task.Prompt
		if len(desc) > 60 {
			desc = desc[:60] + "…"
		}

		elfs = append(elfs, elfEntry{elf: e, desc: desc, task: task})

		t.sendProgress(elf.Progress{
			ElfID:       e.ID(),
			Description: desc,
			Activity:    "starting…",
		})
	}

	// Wait for all elfs in parallel, forwarding progress
	results := make([]elf.Result, len(elfs))
	var wg sync.WaitGroup

	for i, entry := range elfs {
		wg.Add(1)
		go func(idx int, e elfEntry) {
			defer wg.Done()

			// Forward progress events
			go t.drainEvents(e.elf, e.desc)

			// Wait with timeout
			done := make(chan elf.Result, 1)
			go func() { done <- e.elf.Wait() }()

			select {
			case r := <-done:
				results[idx] = r
			case <-ctx.Done():
				e.elf.Cancel()
				results[idx] = elf.Result{
					ID:     e.elf.ID(),
					Status: elf.StatusCancelled,
					Error:  ctx.Err(),
				}
			case <-time.After(5 * time.Minute):
				e.elf.Cancel()
				results[idx] = elf.Result{
					ID:     e.elf.ID(),
					Status: elf.StatusFailed,
					Error:  fmt.Errorf("timed out after 5 minutes"),
				}
			}

			// Report outcome to router
			t.manager.ReportResult(results[idx])

			// Send done progress
			r := results[idx]
			t.sendProgress(elf.Progress{
				ElfID:       r.ID,
				Description: e.desc,
				Tokens:      int(r.Usage.TotalTokens()),
				Done:        true,
				Duration:    r.Duration,
				Error:       errString(r.Error),
			})
		}(i, entry)
	}

	wg.Wait()

	// Build combined result
	var b strings.Builder
	fmt.Fprintf(&b, "%d elfs completed\n\n", len(results))

	for i, r := range results {
		fmt.Fprintf(&b, "--- Elf %d: %s (%s, %s) ---\n",
			i+1, elfs[i].desc,
			r.Status, r.Duration.Round(time.Millisecond),
		)
		if r.Error != nil {
			fmt.Fprintf(&b, "Error: %v\n", r.Error)
		}
		if r.Output != "" {
			output := r.Output
			const maxOutputChars = 2000
			if len(output) > maxOutputChars {
				output = output[:maxOutputChars] + fmt.Sprintf("\n\n[truncated — full output was %d chars]", len(r.Output))
			}
			b.WriteString(output)
		}
		b.WriteString("\n\n")
	}

	return tool.Result{
		Output: b.String(),
		Metadata: map[string]any{
			"elf_count": len(results),
			"total_ms":  totalDuration(results).Milliseconds(),
		},
	}, nil
}

func (t *BatchTool) drainEvents(e elf.Elf, desc string) {
	toolUses := 0
	tokens := 0
	lastSend := time.Now()
	textChars := 0

	for evt := range e.Events() {
		if t.progressCh == nil {
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
			continue
		default:
			continue
		}

		lastSend = time.Now()
		t.sendProgress(p)
	}
}

func (t *BatchTool) sendProgress(p elf.Progress) {
	if t.progressCh == nil {
		return
	}
	select {
	case t.progressCh <- p:
	default:
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func totalDuration(results []elf.Result) time.Duration {
	var max time.Duration
	for _, r := range results {
		if r.Duration > max {
			max = r.Duration
		}
	}
	return max
}
