package mistral

import (
	"encoding/json"

	"github.com/VikingOwl91/mistral-go-sdk/chat"
	"somegit.dev/Owlibou/gnoma/internal/message"
	"somegit.dev/Owlibou/gnoma/internal/provider"
)

// --- gnoma → Mistral ---

func translateMessage(m message.Message) chat.Message {
	switch m.Role {
	case message.RoleSystem:
		return &chat.SystemMessage{Content: chat.TextContent(m.TextContent())}

	case message.RoleUser:
		// Check if this is a tool results message
		if len(m.Content) > 0 && m.Content[0].Type == message.ContentToolResult {
			// Tool results must be sent as individual ToolMessages
			// Return only the first; caller handles multi-result expansion
			tr := m.Content[0].ToolResult
			return &chat.ToolMessage{
				ToolCallID: tr.ToolCallID,
				Content:    chat.TextContent(tr.Content),
			}
		}
		return &chat.UserMessage{Content: chat.TextContent(m.TextContent())}

	case message.RoleAssistant:
		am := chat.AssistantMessage{
			Content: chat.TextContent(m.TextContent()),
		}
		for _, tc := range m.ToolCalls() {
			am.ToolCalls = append(am.ToolCalls, chat.ToolCall{
				ID:   tc.ID,
				Type: "function",
				Function: chat.FunctionCall{
					Name:      tc.Name,
					Arguments: string(tc.Arguments),
				},
			})
		}
		return &am

	default:
		return &chat.UserMessage{Content: chat.TextContent(m.TextContent())}
	}
}

// expandToolResults handles the case where a gnoma Message contains
// multiple ToolResults. Mistral expects one ToolMessage per result.
func expandToolResults(msgs []message.Message) []chat.Message {
	out := make([]chat.Message, 0, len(msgs))
	for _, m := range msgs {
		if m.Role == message.RoleUser && len(m.Content) > 0 && m.Content[0].Type == message.ContentToolResult {
			for _, c := range m.Content {
				if c.Type == message.ContentToolResult && c.ToolResult != nil {
					out = append(out, &chat.ToolMessage{
						ToolCallID: c.ToolResult.ToolCallID,
						Content:    chat.TextContent(c.ToolResult.Content),
					})
				}
			}
			continue
		}
		out = append(out, translateMessage(m))
	}
	return out
}

func translateTools(defs []provider.ToolDefinition) []chat.Tool {
	if len(defs) == 0 {
		return nil
	}
	tools := make([]chat.Tool, len(defs))
	for i, d := range defs {
		var params map[string]any
		if d.Parameters != nil {
			_ = json.Unmarshal(d.Parameters, &params)
		}
		tools[i] = chat.Tool{
			Type: "function",
			Function: chat.Function{
				Name:        d.Name,
				Description: d.Description,
				Parameters:  params,
			},
		}
	}
	return tools
}

func translateRequest(req provider.Request) *chat.CompletionRequest {
	cr := &chat.CompletionRequest{
		Model:    req.Model,
		Messages: expandToolResults(req.Messages),
		Tools:    translateTools(req.Tools),
		Stop:     req.StopSequences,
	}
	if req.MaxTokens > 0 {
		mt := int(req.MaxTokens)
		cr.MaxTokens = &mt
	}
	if req.Temperature != nil {
		cr.Temperature = req.Temperature
	}
	if req.TopP != nil {
		cr.TopP = req.TopP
	}
	if req.ResponseFormat != nil {
		cr.ResponseFormat = translateResponseFormat(req.ResponseFormat)
	}
	return cr
}

func translateResponseFormat(rf *provider.ResponseFormat) *chat.ResponseFormat {
	if rf == nil {
		return nil
	}
	out := &chat.ResponseFormat{
		Type: chat.ResponseFormatType(rf.Type),
	}
	if rf.JSONSchema != nil {
		var schema map[string]any
		if rf.JSONSchema.Schema != nil {
			_ = json.Unmarshal(rf.JSONSchema.Schema, &schema)
		}
		out.JsonSchema = &chat.JsonSchema{
			Name:   rf.JSONSchema.Name,
			Schema: schema,
			Strict: rf.JSONSchema.Strict,
		}
		if rf.JSONSchema.Description != "" {
			desc := rf.JSONSchema.Description
			out.JsonSchema.Description = &desc
		}
	}
	return out
}

// --- Mistral → gnoma ---

func translateFinishReason(fr *chat.FinishReason) message.StopReason {
	if fr == nil {
		return ""
	}
	switch *fr {
	case chat.FinishReasonStop:
		return message.StopEndTurn
	case chat.FinishReasonToolCalls:
		return message.StopToolUse
	case chat.FinishReasonLength, chat.FinishReasonModelLength:
		return message.StopMaxTokens
	default:
		return message.StopEndTurn
	}
}

func translateUsage(u *chat.UsageInfo) *message.Usage {
	if u == nil {
		return nil
	}
	return &message.Usage{
		InputTokens:  int64(u.PromptTokens),
		OutputTokens: int64(u.CompletionTokens),
	}
}
