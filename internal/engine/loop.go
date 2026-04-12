package engine

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	gnomactx "somegit.dev/Owlibou/gnoma/internal/context"
	"somegit.dev/Owlibou/gnoma/internal/hook"
	"somegit.dev/Owlibou/gnoma/internal/message"
	"somegit.dev/Owlibou/gnoma/internal/permission"
	"somegit.dev/Owlibou/gnoma/internal/provider"
	"somegit.dev/Owlibou/gnoma/internal/router"
	"somegit.dev/Owlibou/gnoma/internal/stream"
	"somegit.dev/Owlibou/gnoma/internal/tool"
	"somegit.dev/Owlibou/gnoma/internal/tool/persist"
)

// Submit sends a user message and runs the agentic loop to completion.
// The callback receives real-time streaming events.
func (e *Engine) Submit(ctx context.Context, input string, cb Callback) (*Turn, error) {
	return e.SubmitWithOptions(ctx, input, TurnOptions{}, cb)
}

// SubmitWithOptions is like Submit but applies per-turn overrides (e.g. ToolChoice).
func (e *Engine) SubmitWithOptions(ctx context.Context, input string, opts TurnOptions, cb Callback) (*Turn, error) {
	e.turnOpts = opts
	defer func() { e.turnOpts = TurnOptions{} }()

	userMsg := message.NewUserText(input)
	e.history = append(e.history, userMsg)
	if e.cfg.Context != nil {
		e.cfg.Context.AppendMessage(userMsg)
	}

	return e.runLoop(ctx, cb)
}

// SubmitMessages is like Submit but accepts pre-built messages.
func (e *Engine) SubmitMessages(ctx context.Context, msgs []message.Message, cb Callback) (*Turn, error) {
	e.history = append(e.history, msgs...)
	if e.cfg.Context != nil {
		for _, m := range msgs {
			e.cfg.Context.AppendMessage(m)
		}
	}

	return e.runLoop(ctx, cb)
}

func (e *Engine) runLoop(ctx context.Context, cb Callback) (*Turn, error) {
	turn := &Turn{}

	for {
		turn.Rounds++
		if e.cfg.MaxTurns > 0 && turn.Rounds > e.cfg.MaxTurns {
			e.cfg.Hooks.Fire(hook.Stop, hook.MarshalStopPayload("max_turns")) //nolint:errcheck
			return turn, fmt.Errorf("safety limit: %d rounds exceeded", e.cfg.MaxTurns)
		}

		// Build provider request (gates tools on model capabilities)
		req := e.buildRequest(ctx)

		// Route and stream
		var s stream.Stream
		var err error
		var decision router.RoutingDecision

		if e.cfg.Router != nil {
			// Classify task from the latest user message
			prompt := ""
			for i := len(e.history) - 1; i >= 0; i-- {
				if e.history[i].Role == message.RoleUser {
					prompt = e.history[i].TextContent()
					break
				}
			}
			task := router.ClassifyTask(prompt)
			if e.cfg.Context != nil {
				task.EstimatedTokens = int(e.cfg.Context.Tracker().CountTokens(prompt))
			} else {
				task.EstimatedTokens = int(gnomactx.EstimateTokens(prompt))
			}

			e.logger.Debug("routing request",
				"task_type", task.Type,
				"complexity", task.ComplexityScore,
				"round", turn.Rounds,
			)

			s, decision, err = e.cfg.Router.Stream(ctx, task, req)
			if decision.Arm != nil {
				e.logger.Debug("streaming request",
					"provider", decision.Arm.Provider.Name(),
					"model", decision.Arm.ModelName,
					"arm", decision.Arm.ID,
					"messages", len(req.Messages),
					"tools", len(req.Tools),
					"round", turn.Rounds,
				)
				if turn.Rounds == 1 {
					cb(stream.Event{
						Type:         stream.EventRouting,
						RoutingModel: string(decision.Arm.ID),
						RoutingTask:  task.Type.String(),
					})
				}
			}
		} else {
			e.logger.Debug("streaming request",
				"provider", e.cfg.Provider.Name(),
				"model", req.Model,
				"messages", len(req.Messages),
				"tools", len(req.Tools),
				"round", turn.Rounds,
			)
			s, err = e.cfg.Provider.Stream(ctx, req)
		}
		if err != nil {
			// Retry on transient errors (429, 5xx) with exponential backoff
			s, err = e.retryOnTransient(ctx, err, func() (stream.Stream, error) {
				if e.cfg.Router != nil {
					prompt := ""
					for i := len(e.history) - 1; i >= 0; i-- {
						if e.history[i].Role == message.RoleUser {
							prompt = e.history[i].TextContent()
							break
						}
					}
					task := router.ClassifyTask(prompt)
					if e.cfg.Context != nil {
						task.EstimatedTokens = int(e.cfg.Context.Tracker().CountTokens(prompt))
					} else {
						task.EstimatedTokens = int(gnomactx.EstimateTokens(prompt))
					}
					var retryDecision router.RoutingDecision
					s, retryDecision, err = e.cfg.Router.Stream(ctx, task, req)
					decision = retryDecision // adopt new reservation on retry
					return s, err
				}
				return e.cfg.Provider.Stream(ctx, req)
			})
			if err != nil {
				// Try reactive compaction on 413 (request too large)
				s, err = e.handleRequestTooLarge(ctx, err, req)
				if err != nil {
					decision.Rollback()
					return nil, fmt.Errorf("provider stream: %w", err)
				}
			}
		}

		// Consume stream, forwarding events to callback.
		// Track TTFT and stream duration for arm performance metrics.
		acc := stream.NewAccumulator()
		var stopReason message.StopReason
		var model string

		streamStart := time.Now()
		var firstTokenAt time.Time

		for s.Next() {
			evt := s.Current()
			acc.Apply(evt)

			// Record time of first text token for TTFT metric
			if firstTokenAt.IsZero() && evt.Type == stream.EventTextDelta && evt.Text != "" {
				firstTokenAt = time.Now()
			}

			// Capture stop reason and model from events
			if evt.StopReason != "" {
				stopReason = evt.StopReason
			}
			if evt.Model != "" {
				model = evt.Model
			}

			if cb != nil {
				cb(evt)
			}
		}
		streamEnd := time.Now()
		if err := s.Err(); err != nil {
			s.Close()
			decision.Rollback()
			return nil, fmt.Errorf("stream error: %w", err)
		}
		s.Close()

		// Build response
		resp := acc.Response(stopReason, model)

		// Commit pool reservation and record perf metrics for this round.
		actualTokens := int(resp.Usage.InputTokens + resp.Usage.OutputTokens)
		decision.Commit(actualTokens)
		if decision.Arm != nil && !firstTokenAt.IsZero() {
			decision.Arm.Perf.Update(
				firstTokenAt.Sub(streamStart),
				int(resp.Usage.OutputTokens),
				streamEnd.Sub(streamStart),
			)
		}

		turn.Usage.Add(resp.Usage)
		turn.Messages = append(turn.Messages, resp.Message)
		e.history = append(e.history, resp.Message)
		e.usage.Add(resp.Usage)

		// Track in context window and check for compaction
		if e.cfg.Context != nil {
			e.cfg.Context.AppendMessage(resp.Message)
			// Set tracker to the provider-reported context size (InputTokens = full context
			// as sent this round). This avoids double-counting InputTokens across rounds.
			if resp.Usage.InputTokens > 0 {
				e.cfg.Context.Tracker().Set(resp.Usage.InputTokens + resp.Usage.OutputTokens)
			} else {
				e.cfg.Context.Tracker().Add(message.Usage{OutputTokens: resp.Usage.OutputTokens})
			}
			if compacted, err := e.cfg.Context.CompactIfNeeded(); err != nil {
				e.logger.Error("context compaction failed", "error", err)
			} else if compacted {
				e.history = e.cfg.Context.Messages()
				e.logger.Info("context compacted", "messages", len(e.history))
			}
		}

		e.logger.Debug("turn response",
			"stop_reason", resp.StopReason,
			"tool_calls", len(resp.Message.ToolCalls()),
			"round", turn.Rounds,
		)

		// Decide next action
		switch resp.StopReason {
		case message.StopEndTurn, message.StopSequence:
			e.cfg.Hooks.Fire(hook.Stop, hook.MarshalStopPayload("end_turn")) //nolint:errcheck
			return turn, nil

		case message.StopMaxTokens:
			// Model hit its output token budget mid-response. Inject a continue prompt
			// and re-query so the response is completed rather than silently truncated.
			contMsg := message.NewUserText("Continue from where you left off.")
			e.history = append(e.history, contMsg)
			if e.cfg.Context != nil {
				e.cfg.Context.AppendMessage(contMsg)
			}
			// Continue loop — next round will resume generation

		case message.StopToolUse:
			results, err := e.executeTools(ctx, resp.Message.ToolCalls(), cb)
			if err != nil {
				return nil, fmt.Errorf("tool execution: %w", err)
			}
			toolMsg := message.NewToolResults(results...)
			turn.Messages = append(turn.Messages, toolMsg)
			e.history = append(e.history, toolMsg)
			if e.cfg.Context != nil {
				e.cfg.Context.AppendMessage(toolMsg)
			}
			// Continue loop — re-query provider with tool results

		default:
			// Unknown stop reason or empty — treat as end of turn
			e.cfg.Hooks.Fire(hook.Stop, hook.MarshalStopPayload("unknown")) //nolint:errcheck
			return turn, nil
		}
	}
}

func (e *Engine) buildRequest(ctx context.Context) provider.Request {
	// Use AllMessages (prefix + history) if context window manages prefix docs
	messages := e.history
	if e.cfg.Context != nil {
		messages = e.cfg.Context.AllMessages()
	}
	systemPrompt := e.cfg.System
	if e.cfg.Firewall != nil {
		messages = e.cfg.Firewall.ScanOutgoingMessages(messages)
		systemPrompt = e.cfg.Firewall.ScanSystemPrompt(systemPrompt)
	}

	req := provider.Request{
		Model:        e.cfg.Model,
		SystemPrompt: systemPrompt,
		Messages:     messages,
		ToolChoice:   e.turnOpts.ToolChoice,
	}

	// Only include tools if the model supports them.
	// When Router is active, skip capability gating — the router selects the arm
	// and already knows its capabilities. Gating here would use the wrong provider.
	caps := e.resolveCapabilities(ctx)
	if e.cfg.Router != nil || caps == nil || caps.ToolUse {
		// Router active, nil caps (unknown model), or model supports tools
		for _, t := range e.cfg.Tools.All() {
			// Skip deferred tools until the model requests them
			if dt, ok := t.(tool.DeferrableTool); ok && dt.ShouldDefer() && !e.activatedTools[t.Name()] {
				continue
			}
			req.Tools = append(req.Tools, provider.ToolDefinition{
				Name:        t.Name(),
				Description: t.Description(),
				Parameters:  t.Parameters(),
			})
		}
	} else {
		e.logger.Debug("tools omitted — model does not support tool use",
			"model", req.Model,
		)
	}

	// Inject coordinator guidance for orchestration tasks
	if e.cfg.Router != nil {
		prompt := ""
		for i := len(e.history) - 1; i >= 0; i-- {
			if e.history[i].Role == message.RoleUser {
				prompt = e.history[i].TextContent()
				break
			}
		}
		if router.ClassifyTask(prompt).Type == router.TaskOrchestration {
			req.SystemPrompt = coordinatorPrompt() + "\n\n" + req.SystemPrompt
		}
	}

	return req
}

// coordinatorPrompt returns the system prompt block injected for orchestration tasks.
func coordinatorPrompt() string {
	return `You are operating in coordinator mode. Your role is to decompose complex work into parallel tasks and orchestrate elfs.

Fan-out heuristics:
- Assess task independence before spawning. Ask: do these tasks read/write the same files?
- Read-only tasks on disjoint file sets can always run in parallel.
- Write tasks targeting the same file must be serial — group them into a single elf.
- Prefer wider fan-out (more elfs, smaller scope per elf) over deep sequential chains.

Concurrency rules:
- Call spawn_elfs with ALL independent tasks in one call — never spawn one elf at a time.
- Limit batch size to 5-7 tasks for optimal throughput. Split larger work into waves.
- Pass explicit file paths in each elf prompt — don't rely on the elf to discover them.
- Use list_results to discover outputs from prior calls before spawning dependent tasks.
- Pass result file paths to elfs so they can read prior outputs with read_result or fs.read.

Synthesis:
- After all elfs complete, synthesize their outputs into a single coherent response.
- If any elf failed, report the failure with context and suggest a focused retry.
- Do not repeat raw elf output verbatim — summarize, deduplicate, and integrate.`
}

func (e *Engine) executeTools(ctx context.Context, calls []message.ToolCall, cb Callback) ([]message.ToolResult, error) {
	// Partition into read-only (parallel) and write (serial) batches
	type toolCallWithTool struct {
		call message.ToolCall
		tool tool.Tool
	}

	var readOnly []toolCallWithTool
	var readWrite []toolCallWithTool
	var unknownResults []message.ToolResult

	for _, call := range calls {
		t, ok := e.cfg.Tools.Get(call.Name)
		if ok {
			// Activate deferred tools on first use
			if dt, isDeferrable := t.(tool.DeferrableTool); isDeferrable && dt.ShouldDefer() {
				e.activatedTools[call.Name] = true
			}
		}
		if !ok {
			e.logger.Warn("unknown tool", "name", call.Name)
			unknownResults = append(unknownResults, message.ToolResult{
				ToolCallID: call.ID,
				Content:    fmt.Sprintf("unknown tool: %s", call.Name),
				IsError:    true,
			})
			continue
		}
		tc := toolCallWithTool{call: call, tool: t}
		if t.IsReadOnly() {
			readOnly = append(readOnly, tc)
		} else {
			readWrite = append(readWrite, tc)
		}
	}

	results := make([]message.ToolResult, 0, len(calls))
	results = append(results, unknownResults...)

	// Execute read-only tools in parallel
	if len(readOnly) > 0 {
		e.logger.Debug("executing read-only tools in parallel", "count", len(readOnly))
		parallelResults := make([]message.ToolResult, len(readOnly))
		var wg sync.WaitGroup
		for i, tc := range readOnly {
			wg.Add(1)
			go func(idx int, tc toolCallWithTool) {
				defer wg.Done()
				parallelResults[idx] = e.executeSingleTool(ctx, tc.call, tc.tool, cb)
			}(i, tc)
		}
		wg.Wait()
		results = append(results, parallelResults...)
	}

	// Execute write tools sequentially
	for _, tc := range readWrite {
		results = append(results, e.executeSingleTool(ctx, tc.call, tc.tool, cb))
	}

	return results, nil
}

func (e *Engine) executeSingleTool(ctx context.Context, call message.ToolCall, t tool.Tool, cb Callback) message.ToolResult {
	// Permission check
	if e.cfg.Permissions != nil {
		info := permission.ToolInfo{
			Name:          call.Name,
			IsReadOnly:    t.IsReadOnly(),
			IsDestructive: t.IsDestructive(),
		}
		if err := e.cfg.Permissions.Check(ctx, info, call.Arguments); err != nil {
			e.logger.Info("tool permission denied", "name", call.Name, "error", err)
			return message.ToolResult{
				ToolCallID: call.ID,
				Content:    fmt.Sprintf("permission denied: %v", err),
				IsError:    true,
			}
		}
	}

	// PreToolUse hook: can deny execution or transform args.
	args := call.Arguments
	if e.cfg.Hooks != nil {
		payload := hook.MarshalPreToolPayload(call.Name, args)
		transformed, action, _ := e.cfg.Hooks.Fire(hook.PreToolUse, payload)
		if action == hook.Deny {
			return message.ToolResult{
				ToolCallID: call.ID,
				Content:    "denied by hook",
				IsError:    true,
			}
		}
		if newArgs := hook.ExtractTransformedArgs(transformed); newArgs != nil {
			args = newArgs
		}
	}

	e.logger.Debug("executing tool", "name", call.Name, "id", call.ID)

	result, err := t.Execute(ctx, args)
	if err != nil {
		e.logger.Error("tool execution failed", "name", call.Name, "error", err)
		return message.ToolResult{
			ToolCallID: call.ID,
			Content:    err.Error(),
			IsError:    true,
		}
	}

	// PostToolUse hook: can transform result (Deny treated as Skip).
	output := result.Output
	if e.cfg.Hooks != nil {
		payload := hook.MarshalPostToolPayload(call.Name, args, output, result.Metadata)
		transformed, _, _ := e.cfg.Hooks.Fire(hook.PostToolUse, payload)
		if s := hook.ExtractTransformedOutput(transformed); s != "" {
			output = s
		}
	}

	// Scan tool result through firewall
	if e.cfg.Firewall != nil {
		output = e.cfg.Firewall.ScanToolResult(output)
	}

	// Persist results to /tmp for cross-tool session sharing
	if e.cfg.Store != nil {
		if path, ok := e.cfg.Store.Save(call.Name, call.ID, output); ok {
			e.logger.Debug("tool result persisted", "name", call.Name, "path", path)
			output = persist.InlineReplacement(path, output)
		}
	}

	// Emit tool result event for the UI
	if cb != nil {
		cb(stream.Event{
			Type:       stream.EventToolResult,
			ToolName:   call.Name,
			ToolOutput: truncate(output, 2000),
		})
	}

	return message.ToolResult{
		ToolCallID: call.ID,
		Content:    output,
	}
}

func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}

// handleRequestTooLarge attempts compaction on 413 and retries once.
func (e *Engine) handleRequestTooLarge(ctx context.Context, origErr error, req provider.Request) (stream.Stream, error) {
	var provErr *provider.ProviderError
	if !errors.As(origErr, &provErr) || provErr.StatusCode != 413 {
		return nil, origErr
	}

	if e.cfg.Context == nil {
		return nil, origErr
	}

	e.logger.Warn("413 received, forcing emergency compaction")
	compacted, compactErr := e.cfg.Context.ForceCompact()
	if compactErr != nil || !compacted {
		return nil, origErr
	}

	e.history = e.cfg.Context.Messages()
	req = e.buildRequest(ctx)

	if e.cfg.Router != nil {
		prompt := ""
		for i := len(e.history) - 1; i >= 0; i-- {
			if e.history[i].Role == message.RoleUser {
				prompt = e.history[i].TextContent()
				break
			}
		}
		task := router.ClassifyTask(prompt)
		if e.cfg.Context != nil {
			task.EstimatedTokens = int(e.cfg.Context.Tracker().CountTokens(prompt))
		} else {
			task.EstimatedTokens = int(gnomactx.EstimateTokens(prompt))
		}
		s, _, err := e.cfg.Router.Stream(ctx, task, req)
		return s, err
	}
	return e.cfg.Provider.Stream(ctx, req)
}

// retryOnTransient retries the stream call on 429/5xx with exponential backoff.
// Returns the original error if not retryable or all retries exhausted.
func (e *Engine) retryOnTransient(ctx context.Context, firstErr error, fn func() (stream.Stream, error)) (stream.Stream, error) {
	var provErr *provider.ProviderError
	if !errors.As(firstErr, &provErr) || !provErr.Retryable {
		return nil, firstErr
	}

	const maxRetries = 4
	delays := [maxRetries]time.Duration{
		1 * time.Second,
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
	}

	// Use Retry-After if the provider told us
	if provErr.RetryAfter > 0 && provErr.RetryAfter < 30*time.Second {
		delays[0] = provErr.RetryAfter
	}

	for attempt := range maxRetries {
		e.logger.Debug("retrying after transient error",
			"attempt", attempt+1,
			"delay", delays[attempt],
			"status", provErr.StatusCode,
		)

		select {
		case <-time.After(delays[attempt]):
		case <-ctx.Done():
			return nil, ctx.Err()
		}

		s, err := fn()
		if err == nil {
			return s, nil
		}

		if !errors.As(err, &provErr) || !provErr.Retryable {
			return nil, err
		}
	}

	return nil, firstErr
}

