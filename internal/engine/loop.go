package engine

import (
	"context"
	"encoding/json"
	"fmt"

	"somegit.dev/Owlibou/gnoma/internal/message"
	"somegit.dev/Owlibou/gnoma/internal/permission"
	"somegit.dev/Owlibou/gnoma/internal/provider"
	"somegit.dev/Owlibou/gnoma/internal/router"
	"somegit.dev/Owlibou/gnoma/internal/stream"
)

// Submit sends a user message and runs the agentic loop to completion.
// The callback receives real-time streaming events.
func (e *Engine) Submit(ctx context.Context, input string, cb Callback) (*Turn, error) {
	userMsg := message.NewUserText(input)
	e.history = append(e.history, userMsg)

	return e.runLoop(ctx, cb)
}

// SubmitMessages is like Submit but accepts pre-built messages.
func (e *Engine) SubmitMessages(ctx context.Context, msgs []message.Message, cb Callback) (*Turn, error) {
	e.history = append(e.history, msgs...)

	return e.runLoop(ctx, cb)
}

func (e *Engine) runLoop(ctx context.Context, cb Callback) (*Turn, error) {
	turn := &Turn{}

	for {
		turn.Rounds++
		if e.cfg.MaxTurns > 0 && turn.Rounds > e.cfg.MaxTurns {
			return turn, fmt.Errorf("safety limit: %d rounds exceeded", e.cfg.MaxTurns)
		}

		// Build provider request (gates tools on model capabilities)
		req := e.buildRequest(ctx)

		// Route and stream
		var s stream.Stream
		var err error

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
			task.EstimatedTokens = 4000 // rough default

			e.logger.Debug("routing request",
				"task_type", task.Type,
				"complexity", task.ComplexityScore,
				"round", turn.Rounds,
			)

			var arm *router.Arm
			s, arm, err = e.cfg.Router.Stream(ctx, task, req)
			if arm != nil {
				e.logger.Debug("streaming request",
					"provider", arm.Provider.Name(),
					"model", arm.ModelName,
					"arm", arm.ID,
					"messages", len(req.Messages),
					"tools", len(req.Tools),
					"round", turn.Rounds,
				)
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
			return nil, fmt.Errorf("provider stream: %w", err)
		}

		// Consume stream, forwarding events to callback
		acc := stream.NewAccumulator()
		var stopReason message.StopReason
		var model string

		for s.Next() {
			evt := s.Current()
			acc.Apply(evt)

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
		if err := s.Err(); err != nil {
			s.Close()
			return nil, fmt.Errorf("stream error: %w", err)
		}
		s.Close()

		// Build response
		resp := acc.Response(stopReason, model)
		turn.Usage.Add(resp.Usage)
		turn.Messages = append(turn.Messages, resp.Message)
		e.history = append(e.history, resp.Message)
		e.usage.Add(resp.Usage)

		e.logger.Debug("turn response",
			"stop_reason", resp.StopReason,
			"tool_calls", len(resp.Message.ToolCalls()),
			"round", turn.Rounds,
		)

		// Decide next action
		switch resp.StopReason {
		case message.StopEndTurn, message.StopMaxTokens, message.StopSequence:
			return turn, nil

		case message.StopToolUse:
			results, err := e.executeTools(ctx, resp.Message.ToolCalls(), cb)
			if err != nil {
				return nil, fmt.Errorf("tool execution: %w", err)
			}
			toolMsg := message.NewToolResults(results...)
			turn.Messages = append(turn.Messages, toolMsg)
			e.history = append(e.history, toolMsg)
			// Continue loop — re-query provider with tool results

		default:
			// Unknown stop reason or empty — treat as end of turn
			return turn, nil
		}
	}
}

func (e *Engine) buildRequest(ctx context.Context) provider.Request {
	// Scan messages through firewall if configured
	messages := e.history
	systemPrompt := e.cfg.System
	if e.cfg.Firewall != nil {
		messages = e.cfg.Firewall.ScanOutgoingMessages(messages)
		systemPrompt = e.cfg.Firewall.ScanSystemPrompt(systemPrompt)
	}

	req := provider.Request{
		Model:        e.cfg.Model,
		SystemPrompt: systemPrompt,
		Messages:     messages,
	}

	// Only include tools if the model supports them
	caps := e.resolveCapabilities(ctx)
	if caps == nil || caps.ToolUse {
		// nil caps = unknown model, include tools optimistically
		for _, t := range e.cfg.Tools.All() {
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

	return req
}

func (e *Engine) executeTools(ctx context.Context, calls []message.ToolCall, cb Callback) ([]message.ToolResult, error) {
	results := make([]message.ToolResult, 0, len(calls))

	for _, call := range calls {
		t, ok := e.cfg.Tools.Get(call.Name)
		if !ok {
			e.logger.Warn("unknown tool", "name", call.Name)
			results = append(results, message.ToolResult{
				ToolCallID: call.ID,
				Content:    fmt.Sprintf("unknown tool: %s", call.Name),
				IsError:    true,
			})
			continue
		}

		// Permission check
		if e.cfg.Permissions != nil {
			info := permission.ToolInfo{
				Name:        call.Name,
				IsReadOnly:  t.IsReadOnly(),
				IsDestructive: t.IsDestructive(),
			}
			if err := e.cfg.Permissions.Check(ctx, info, call.Arguments); err != nil {
				e.logger.Info("tool permission denied", "name", call.Name, "error", err)
				results = append(results, message.ToolResult{
					ToolCallID: call.ID,
					Content:    fmt.Sprintf("permission denied: %v", err),
					IsError:    true,
				})
				continue
			}
		}

		e.logger.Debug("executing tool", "name", call.Name, "id", call.ID)

		result, err := t.Execute(ctx, call.Arguments)
		if err != nil {
			e.logger.Error("tool execution failed", "name", call.Name, "error", err)
			results = append(results, message.ToolResult{
				ToolCallID: call.ID,
				Content:    err.Error(),
				IsError:    true,
			})
			continue
		}

		// Scan tool result through firewall
		output := result.Output
		if e.cfg.Firewall != nil {
			output = e.cfg.Firewall.ScanToolResult(output)
		}

		// Emit tool result event for the UI
		if cb != nil {
			cb(stream.Event{
				Type:       stream.EventToolResult,
				ToolName:   call.Name,
				ToolOutput: truncate(output, 2000),
			})
		}

		results = append(results, message.ToolResult{
			ToolCallID: call.ID,
			Content:    output,
		})
	}

	return results, nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// toolDefFromTool converts a tool.Tool to provider.ToolDefinition.
// Unused currently but kept for reference when building tool definitions dynamically.
func toolDefFromJSON(name, description string, params json.RawMessage) provider.ToolDefinition {
	return provider.ToolDefinition{
		Name:        name,
		Description: description,
		Parameters:  params,
	}
}
