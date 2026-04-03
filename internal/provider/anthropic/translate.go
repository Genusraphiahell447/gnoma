package anthropic

import (
	"encoding/json"
	"strings"

	"somegit.dev/Owlibou/gnoma/internal/message"
	"somegit.dev/Owlibou/gnoma/internal/provider"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/packages/param"
)

// unsanitizeToolName reverses the name transformation for tool calls from the model.
func unsanitizeToolName(name string) string {
	// Only reverse fs_ prefix since those are our tool names
	if strings.HasPrefix(name, "fs_") {
		return "fs." + name[3:]
	}
	return name
}

// --- gnoma → Anthropic ---

func translateMessages(msgs []message.Message) []anthropic.MessageParam {
	out := make([]anthropic.MessageParam, 0, len(msgs))
	for _, m := range msgs {
		// Skip system messages — they're passed separately
		if m.Role == message.RoleSystem {
			continue
		}
		out = append(out, translateMessage(m)...)
	}
	return out
}

func translateMessage(m message.Message) []anthropic.MessageParam {
	switch m.Role {
	case message.RoleUser:
		// Check if this is a tool results message
		if len(m.Content) > 0 && m.Content[0].Type == message.ContentToolResult {
			blocks := make([]anthropic.ContentBlockParamUnion, 0, len(m.Content))
			for _, c := range m.Content {
				if c.Type == message.ContentToolResult && c.ToolResult != nil {
					blocks = append(blocks, anthropic.NewToolResultBlock(
						c.ToolResult.ToolCallID,
						c.ToolResult.Content,
						c.ToolResult.IsError,
					))
				}
			}
			return []anthropic.MessageParam{anthropic.NewUserMessage(blocks...)}
		}
		return []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(m.TextContent())),
		}

	case message.RoleAssistant:
		blocks := make([]anthropic.ContentBlockParamUnion, 0, len(m.Content))
		for _, c := range m.Content {
			switch c.Type {
			case message.ContentText:
				if c.Text != "" {
					blocks = append(blocks, anthropic.NewTextBlock(c.Text))
				}
			case message.ContentToolCall:
				if c.ToolCall != nil {
					blocks = append(blocks, anthropic.ContentBlockParamUnion{
						OfToolUse: &anthropic.ToolUseBlockParam{
							ID:    c.ToolCall.ID,
							Name:  c.ToolCall.Name,
							Input: json.RawMessage(c.ToolCall.Arguments),
						},
					})
				}
			case message.ContentThinking:
				if c.Thinking != nil {
					if c.Thinking.Redacted {
						blocks = append(blocks, anthropic.ContentBlockParamUnion{
							OfRedactedThinking: &anthropic.RedactedThinkingBlockParam{
								Data: c.Thinking.Text,
							},
						})
					} else {
						blocks = append(blocks, anthropic.ContentBlockParamUnion{
							OfThinking: &anthropic.ThinkingBlockParam{
								Thinking:  c.Thinking.Text,
								Signature: c.Thinking.Signature,
							},
						})
					}
				}
			}
		}
		if len(blocks) == 0 {
			blocks = append(blocks, anthropic.NewTextBlock(""))
		}
		return []anthropic.MessageParam{anthropic.NewAssistantMessage(blocks...)}

	default:
		return nil
	}
}

// sanitizeToolName replaces characters not allowed by Anthropic's tool name pattern.
// Anthropic requires ^[a-zA-Z0-9_-]{1,128}$
func sanitizeToolName(name string) string {
	return strings.ReplaceAll(name, ".", "_")
}

func translateTools(defs []provider.ToolDefinition) []anthropic.ToolUnionParam {
	if len(defs) == 0 {
		return nil
	}
	tools := make([]anthropic.ToolUnionParam, len(defs))
	for i, d := range defs {
		// Parse JSON Schema into Properties + Required
		var schema struct {
			Properties map[string]any `json:"properties"`
			Required   []string       `json:"required"`
		}
		if d.Parameters != nil {
			_ = json.Unmarshal(d.Parameters, &schema)
		}

		tools[i] = anthropic.ToolUnionParam{
			OfTool: &anthropic.ToolParam{
				Name:        sanitizeToolName(d.Name),
				Description: param.NewOpt(d.Description),
				InputSchema: anthropic.ToolInputSchemaParam{
					Properties: schema.Properties,
					Required:   schema.Required,
				},
			},
		}
	}
	return tools
}

func translateRequest(req provider.Request) anthropic.MessageNewParams {
	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(req.Model),
		MaxTokens: req.MaxTokens,
		Messages:  translateMessages(req.Messages),
		Tools:     translateTools(req.Tools),
	}

	if req.SystemPrompt != "" {
		params.System = []anthropic.TextBlockParam{
			{Text: req.SystemPrompt},
		}
	}

	if req.Temperature != nil {
		params.Temperature = param.NewOpt(*req.Temperature)
	}
	if req.TopP != nil {
		params.TopP = param.NewOpt(*req.TopP)
	}
	if req.TopK != nil {
		params.TopK = param.NewOpt(*req.TopK)
	}
	if len(req.StopSequences) > 0 {
		params.StopSequences = req.StopSequences
	}

	if req.Thinking != nil && req.Thinking.BudgetTokens > 0 {
		params.Thinking = anthropic.ThinkingConfigParamUnion{
			OfEnabled: &anthropic.ThinkingConfigEnabledParam{
				BudgetTokens: req.Thinking.BudgetTokens,
			},
		}
	}

	return params
}

// --- Anthropic → gnoma ---

func translateStopReason(sr anthropic.StopReason) message.StopReason {
	switch sr {
	case anthropic.StopReasonEndTurn:
		return message.StopEndTurn
	case anthropic.StopReasonMaxTokens:
		return message.StopMaxTokens
	case anthropic.StopReasonToolUse:
		return message.StopToolUse
	case anthropic.StopReasonStopSequence:
		return message.StopSequence
	default:
		return message.StopEndTurn
	}
}

func translateUsage(u anthropic.Usage) message.Usage {
	return message.Usage{
		InputTokens:         u.InputTokens,
		OutputTokens:        u.OutputTokens,
		CacheReadTokens:     u.CacheReadInputTokens,
		CacheCreationTokens: u.CacheCreationInputTokens,
	}
}
