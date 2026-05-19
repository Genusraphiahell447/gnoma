package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
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
	e.mu.Lock()
	e.turnOpts = opts
	userMsg := message.NewUserText(input)
	e.history = append(e.history, userMsg)
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		e.turnOpts = TurnOptions{}
		e.mu.Unlock()
	}()

	if e.cfg.Context != nil {
		e.cfg.Context.AppendMessage(userMsg)
	}

	return e.runLoop(ctx, cb)
}

// SubmitMessages is like Submit but accepts pre-built messages.
func (e *Engine) SubmitMessages(ctx context.Context, msgs []message.Message, cb Callback) (*Turn, error) {
	e.mu.Lock()
	e.history = append(e.history, msgs...)
	e.mu.Unlock()
	if e.cfg.Context != nil {
		for _, m := range msgs {
			e.cfg.Context.AppendMessage(m)
		}
	}

	return e.runLoop(ctx, cb)
}

func (e *Engine) runLoop(ctx context.Context, cb Callback) (*Turn, error) {
	// Two-stage tool-routing state is per-turn; clear it so an aborted turn
	// can't leak its last category selection into the next one.
	e.resetTwoStageState()
	defer e.resetTwoStageState()

	turn := &Turn{}
	loopStart := time.Now()
	var lastArmID router.ArmID
	var lastTaskType router.TaskType
	var lastClassifierSource router.ClassifierSource

	// Early-stop detectors — per-turn scope, single-goroutine use.
	repetitionDet := NewRepetitionDetector()
	patchFails := NewPatchFailureTracker()
	priorRoundHadToolCalls := false

	reportOutcome := func(err error) {
		if e.cfg.Router == nil || lastArmID == "" {
			return
		}
		// Suppress quality feedback while incognito is active — bandit
		// learning would otherwise persist signal about the session.
		if e.cfg.Firewall != nil && !e.cfg.Firewall.Incognito().ShouldLearn() {
			return
		}
		e.cfg.Router.ReportOutcome(router.Outcome{
			ArmID:            lastArmID,
			TaskType:         lastTaskType,
			ClassifierSource: lastClassifierSource,
			Success:          err == nil,
			Tokens:           int(turn.Usage.InputTokens + turn.Usage.OutputTokens),
			Duration:         time.Since(loopStart),
		})
	}

	for {
		turn.Rounds++
		if e.cfg.MaxTurns > 0 && turn.Rounds > e.cfg.MaxTurns {
			e.cfg.Hooks.Fire(hook.Stop, hook.MarshalStopPayload("max_turns")) //nolint:errcheck
			err := fmt.Errorf("safety limit: %d rounds exceeded", e.cfg.MaxTurns)
			reportOutcome(err)
			return turn, err
		}

		// Build provider request (gates tools on model capabilities)
		req := e.buildRequest(ctx)

		// Route and stream
		var s stream.Stream
		var err error
		var decision router.RoutingDecision

		if e.cfg.Router != nil {
			prompt := e.latestUserPrompt()
			task := e.classify(ctx, prompt)
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
				lastArmID = decision.Arm.ID
				lastTaskType = task.Type
				lastClassifierSource = task.ClassifierSource
				e.logger.Debug("streaming request",
					"provider", decision.Arm.Provider.Name(),
					"model", decision.Arm.ModelName,
					"arm", decision.Arm.ID,
					"messages", len(req.Messages),
					"tools", len(req.Tools),
					"round", turn.Rounds,
				)
				if turn.Rounds == 1 && cb != nil {
					cb(stream.Event{
						Type:              stream.EventRouting,
						RoutingModel:      string(decision.Arm.ID),
						RoutingTask:       task.Type.String(),
						RoutingClassifier: task.ClassifierSource.String(),
					})
				}
			}
		} else {
			prov := e.activeProvider()
			e.logger.Debug("streaming request",
				"provider", prov.Name(),
				"model", req.Model,
				"messages", len(req.Messages),
				"tools", len(req.Tools),
				"round", turn.Rounds,
			)
			s, err = prov.Stream(ctx, req)
		}
		if err != nil {
			var failedArms []router.ArmID
			if e.cfg.Router != nil && decision.Arm != nil {
				failedArms = append(failedArms, decision.Arm.ID)
			}

			// If we have a router and no forced arm, we fall back to other models immediately.
			skipDelay := e.cfg.Router != nil && e.cfg.Router.ForcedArm() == ""

			// Apply temporary backoff to the failing arm if it was a 429
			if e.cfg.Router != nil && decision.Arm != nil {
				var provErr *provider.ProviderError
				if errors.As(err, &provErr) && (provErr.StatusCode == 429 || provErr.StatusCode == 529) {
					e.logger.Info("applying backoff to exhausted model", "arm", decision.Arm.ID)
					e.cfg.Router.Backoff(decision.Arm.ID, 5*time.Minute)
				}
			}

			// Retry on transient errors (429, 5xx) with exponential backoff
			s, err = e.retryOnTransient(ctx, err, skipDelay, func() (stream.Stream, error) {
				if e.cfg.Router != nil {
					prompt := e.latestUserPrompt()
					task := e.classify(ctx, prompt)
					if e.cfg.Context != nil {
						task.EstimatedTokens = int(e.cfg.Context.Tracker().CountTokens(prompt))
					} else {
						task.EstimatedTokens = int(gnomactx.EstimateTokens(prompt))
					}
					
					task.ExcludedArms = failedArms
					var retryDecision router.RoutingDecision
					s, retryDecision, err = e.cfg.Router.Stream(ctx, task, req)
					if err == nil {
						decision = retryDecision // adopt new reservation on retry
					} else if retryDecision.Arm != nil {
						failedArms = append(failedArms, retryDecision.Arm.ID)

						// Also apply backoff to arms that fail during the fallback retry loop
						var provErr *provider.ProviderError
						if errors.As(err, &provErr) && (provErr.StatusCode == 429 || provErr.StatusCode == 529) {
							e.logger.Info("applying backoff to exhausted model (during fallback)", "arm", retryDecision.Arm.ID)
							e.cfg.Router.Backoff(retryDecision.Arm.ID, 5*time.Minute)
						}
					}
					return s, err
				}
				return e.activeProvider().Stream(ctx, req)
			})
			if err != nil {
				// Try reactive compaction on 413 (request too large)
				s, err = e.handleRequestTooLarge(ctx, err)
				if err != nil {
					decision.Rollback()
					streamErr := fmt.Errorf("provider stream: %w", err)
					reportOutcome(streamErr)
					return nil, streamErr
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
		repetitionTripped := false

		for s.Next() {
			evt := s.Current()
			acc.Apply(evt)

			// Record time of first text token for TTFT metric
			if firstTokenAt.IsZero() && evt.Type == stream.EventTextDelta && evt.Text != "" {
				firstTokenAt = time.Now()
			}

			// Feed text deltas to the repetition detector. On trigger, stop
			// consuming further events — the partial response is committed
			// to history below and a corrective message is injected.
			if evt.Type == stream.EventTextDelta && evt.Text != "" {
				if repetitionDet.Feed(evt.Text) {
					repetitionTripped = true
					e.logger.Info("early-stop: repetition loop detected", "round", turn.Rounds)
					if cb != nil {
						cb(evt)
					}
					break
				}
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
			e.logger.Debug("stream terminated with error",
				"error", err,
				"rounds", turn.Rounds,
			)
			if closeErr := s.Close(); closeErr != nil {
				e.logger.Warn("stream close after error failed", "error", closeErr)
			}
			decision.Rollback()
			streamErr := e.annotateStreamError(err, len(req.Tools))
			reportOutcome(streamErr)
			return nil, streamErr
		}
		if err := s.Close(); err != nil {
			e.logger.Warn("stream close failed", "error", err)
		}

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
		e.appendHistory(resp.Message)
		e.addUsage(resp.Usage)

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
				compactedMsgs := e.cfg.Context.Messages()
				e.replaceHistory(compactedMsgs)
				e.logger.Info("context compacted", "messages", len(compactedMsgs))
			}
		}

		e.logger.Debug("turn response",
			"stop_reason", resp.StopReason,
			"tool_calls", len(resp.Message.ToolCalls()),
			"round", turn.Rounds,
		)

		// Repetition loop — inject correction and re-query.
		if repetitionTripped {
			e.injectCorrective(RepetitionInjection())
			continue
		}

		// Greeting regression — only meaningful after a round that used tools.
		if priorRoundHadToolCalls && !resp.Message.HasToolCalls() {
			if DetectGreeting(resp.Message.TextContent()) {
				e.logger.Info("early-stop: greeting regression detected", "round", turn.Rounds)
				e.injectCorrective(GreetingInjection())
				continue
			}
		}

		// Decide next action
		switch resp.StopReason {
		case message.StopEndTurn, message.StopSequence:
			e.cfg.Hooks.Fire(hook.Stop, hook.MarshalStopPayload("end_turn")) //nolint:errcheck
			reportOutcome(nil)
			return turn, nil

		case message.StopMaxTokens:
			// Model hit its output token budget mid-response. Inject a continue prompt
			// and re-query so the response is completed rather than silently truncated.
			contMsg := message.NewUserText("Continue from where you left off.")
			e.appendHistory(contMsg)
			if e.cfg.Context != nil {
				e.cfg.Context.AppendMessage(contMsg)
			}
			// Continue loop — next round will resume generation

		case message.StopToolUse:
			calls := resp.Message.ToolCalls()
			results, err := e.executeTools(ctx, calls, cb)
			if err != nil {
				toolErr := fmt.Errorf("tool execution: %w", err)
				reportOutcome(toolErr)
				return nil, toolErr
			}
			toolMsg := message.NewToolResults(results...)
			turn.Messages = append(turn.Messages, toolMsg)
			e.appendHistory(toolMsg)
			if e.cfg.Context != nil {
				e.cfg.Context.AppendMessage(toolMsg)
			}

			// Track patch failures per file; trigger an escalation if a
			// single path crosses the threshold.
			if spiralPath := e.recordPatchOutcomes(calls, results, patchFails); spiralPath != "" {
				e.logger.Info("early-stop: patch spiral detected", "path", spiralPath, "round", turn.Rounds)
				e.injectCorrective(PatchSpiralInjection(spiralPath))
			}

			priorRoundHadToolCalls = true
			// Continue loop — re-query provider with tool results

		default:
			// Unknown stop reason or empty — treat as end of turn
			e.cfg.Hooks.Fire(hook.Stop, hook.MarshalStopPayload("unknown")) //nolint:errcheck
			reportOutcome(nil)
			return turn, nil
		}
	}
}

// injectCorrective appends a user-role corrective message to history and the
// context window. Used by the early-stop detectors to steer the model on the
// next round.
func (e *Engine) injectCorrective(text string) {
	msg := message.NewUserText(text)
	e.appendHistory(msg)
	if e.cfg.Context != nil {
		e.cfg.Context.AppendMessage(msg)
	}
}

// recordPatchOutcomes walks fs.edit/fs.write tool calls and feeds their
// success/failure into the tracker. Returns the first path that crossed the
// patch-spiral threshold on this round, or "" if none did.
func (e *Engine) recordPatchOutcomes(calls []message.ToolCall, results []message.ToolResult, tr *PatchFailureTracker) string {
	if len(calls) == 0 || len(results) == 0 {
		return ""
	}
	resByID := make(map[string]*message.ToolResult, len(results))
	for i := range results {
		resByID[results[i].ToolCallID] = &results[i]
	}
	var spiralPath string
	for _, call := range calls {
		if call.Name != "fs.edit" && call.Name != "fs.write" {
			continue
		}
		res, ok := resByID[call.ID]
		if !ok {
			continue
		}
		path := extractPatchPath(call.Arguments)
		if path == "" {
			continue
		}
		if res.IsError {
			if tr.RecordFailure(path) && spiralPath == "" {
				spiralPath = path
			}
		} else {
			tr.RecordSuccess(path)
		}
	}
	return spiralPath
}

// extractPatchPath pulls "path" out of fs.edit / fs.write arguments. Returns
// "" when the args are unreadable — the tracker treats that as "skip".
func extractPatchPath(args json.RawMessage) string {
	var a struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return ""
	}
	return a.Path
}

func (e *Engine) buildRequest(ctx context.Context) provider.Request {
	// Use AllMessages (prefix + history) if context window manages prefix docs
	messages := e.historySnapshot()
	if e.cfg.Context != nil {
		messages = e.cfg.Context.AllMessages()
	}
	// For local models, compact tool results from previous rounds to stay
	// within small context windows. Cloud models keep full results.
	if e.isLocalArm() {
		messages = compactPreviousToolResults(messages)
	}
	systemPrompt := e.cfg.System
	if e.cfg.Firewall != nil {
		messages = e.cfg.Firewall.ScanOutgoingMessages(messages)
		systemPrompt = e.cfg.Firewall.ScanSystemPrompt(systemPrompt)
	}

	turnOpts := e.snapshotTurnOpts()
	req := provider.Request{
		Model:        e.activeModel(),
		SystemPrompt: systemPrompt,
		Messages:     messages,
		ToolChoice:   turnOpts.ToolChoice,
		Temperature:  e.cfg.Temperature,
	}

	// Only include tools if the model supports them.
	// When a forced arm is set, check its ToolUse capability directly.
	// For multi-arm routing (no forced arm), include tools and let the
	// router's feasibility filter handle capability matching.
	caps := e.resolveCapabilities(ctx)
	includeTools := false
	if e.cfg.Router != nil {
		includeTools = e.forcedArmSupportsTools()
	} else {
		includeTools = caps == nil || caps.ToolUse
	}
	if includeTools {
		twoStage := e.useTwoStageTools()
		selected := e.snapshotSelectedCategory()

		if twoStage && selected == "" {
			// Round 1 of two-stage: send only the synthetic select_category tool
			// and force the model to call it. Small SLMs given a single optional
			// tool will often emit prose instead of calling it.
			req.Tools = []provider.ToolDefinition{buildSelectCategoryDef()}
			req.ToolChoice = provider.ToolChoiceRequired
			e.logger.Debug("two-stage: round 1 — emitting select_category only",
				"model", req.Model,
			)
		} else {
			allowed := turnOpts.AllowedTools
			for _, t := range e.cfg.Tools.All() {
				// Skip deferred tools until the model requests them
				if dt, ok := t.(tool.DeferrableTool); ok && dt.ShouldDefer() && !e.isToolActivated(t.Name()) {
					continue
				}
				// Filter to allowed tools when a restrict list is set
				if allowed != nil && !slices.Contains(allowed, t.Name()) {
					continue
				}
				// Under two-stage round 2+, only schemas in the selected category.
				if twoStage && tool.CategoryOf(t) != selected {
					continue
				}
				req.Tools = append(req.Tools, provider.ToolDefinition{
					Name:        t.Name(),
					Description: t.Description(),
					Parameters:  t.Parameters(),
				})
			}
			// Keep select_category available while two-stage is active so the
			// model can switch categories without aborting the turn.
			if twoStage {
				req.Tools = append(req.Tools, buildSelectCategoryDef())
			}
			e.logger.Debug("tools included in request",
				"model", req.Model,
				"count", len(req.Tools),
				"two_stage", twoStage,
				"category", string(selected),
			)
		}
	} else {
		e.logger.Debug("tools omitted — model does not support tool use",
			"model", req.Model,
		)
	}

	// Inject coordinator guidance for orchestration tasks
	if e.cfg.Router != nil {
		prompt := e.latestUserPrompt()
		if e.classify(ctx, prompt).Type == router.TaskOrchestration {
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
	// Intercept the synthetic select_category tool first — it never reaches
	// the registry and produces its own synthetic tool result.
	calls, syntheticResults := e.interceptSelectCategoryCalls(calls)

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
				e.markToolActivated(call.Name)
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

	results := make([]message.ToolResult, 0, len(calls)+len(syntheticResults))
	results = append(results, syntheticResults...)
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

	// Path restriction: deny bash and validate fs tool paths against AllowedPaths.
	if denied, blocked := checkPathRestriction(call, t, args, e.snapshotTurnOpts().AllowedPaths); blocked {
		return denied
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
	//
	// The payload contains pre-scan output by design. Shell hooks need
	// raw access (audit, forensic, local alert); LLM-bound hook types
	// (prompt, agent) inherit redaction at the SafeProvider boundary on
	// the outbound LLM call, so raw payload here does not equal raw
	// payload to a remote model. See ADR-004
	// (docs/essentials/decisions/004-posttooluse-hook-ordering.md) for
	// the full rationale and the conditions under which this needs to
	// be revisited.
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

// handleRequestTooLarge attempts compaction on 413 and retries once. The
// request is rebuilt from the compacted history, so callers don't pass it in.
func (e *Engine) handleRequestTooLarge(ctx context.Context, origErr error) (stream.Stream, error) {
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

	e.replaceHistory(e.cfg.Context.Messages())
	req := e.buildRequest(ctx)

	if e.cfg.Router != nil {
		prompt := e.latestUserPrompt()
		task := e.classify(ctx, prompt)
		if e.cfg.Context != nil {
			task.EstimatedTokens = int(e.cfg.Context.Tracker().CountTokens(prompt))
		} else {
			task.EstimatedTokens = int(gnomactx.EstimateTokens(prompt))
		}
		s, _, err := e.cfg.Router.Stream(ctx, task, req)
		return s, err
	}
	return e.activeProvider().Stream(ctx, req)
}

// retryOnTransient retries the stream call on 429/5xx with exponential backoff.
// Returns the original error if not retryable or all retries exhausted.
func (e *Engine) retryOnTransient(ctx context.Context, firstErr error, skipDelay bool, fn func() (stream.Stream, error)) (stream.Stream, error) {
	var provErr *provider.ProviderError
	if !errors.As(firstErr, &provErr) || !provErr.Retryable {
		e.logger.Debug("error not retryable",
			"is_provider_error", errors.As(firstErr, &provErr),
			"error", firstErr,
		)
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
		delay := delays[attempt]
		if skipDelay {
			delay = 0
		}

		e.logger.Debug("retrying after transient error",
			"attempt", attempt+1,
			"delay", delay,
			"status", provErr.StatusCode,
		)

		if delay > 0 {
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		} else {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
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

// annotateStreamError wraps a stream error with diagnostic context when the
// failure is a deterministic tool-parse error from a local server. The extra
// context is visible in the TUI (slog.Debug goes to a file).
func (e *Engine) annotateStreamError(err error, toolCount int) error {
	var provErr *provider.ProviderError
	if errors.As(err, &provErr) && provErr.StatusCode == 500 &&
		strings.Contains(strings.ToLower(provErr.Message), "parse tool call") {
		toolSupport := e.forcedArmSupportsTools()
		return fmt.Errorf("stream error (tools_sent=%d, probe_tool_support=%v): %w\n"+
			"hint: the model's chat template claims tool support but it generated invalid tool JSON. "+
			"Ensure llama.cpp is started with --jinja, or try a model with better tool-calling ability",
			toolCount, toolSupport, err)
	}
	return fmt.Errorf("stream error: %w", err)
}

