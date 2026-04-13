package openai

import (
	"encoding/json"
	"strings"

	"somegit.dev/Owlibou/gnoma/internal/message"
	"somegit.dev/Owlibou/gnoma/internal/provider"

	oai "github.com/openai/openai-go"
	"github.com/openai/openai-go/packages/param"
	"github.com/openai/openai-go/shared"
)

func sanitizeToolName(name string) string {
	return strings.ReplaceAll(name, ".", "_")
}

func unsanitizeToolName(name string) string {
	if strings.HasPrefix(name, "fs_") {
		return "fs." + name[3:]
	}
	// Some models (e.g. gemma4 via Ollama) use "fs:grep" instead of "fs_grep"
	if strings.HasPrefix(name, "fs:") {
		return "fs." + name[3:]
	}
	return name
}

// --- gnoma → OpenAI ---

func translateMessages(msgs []message.Message) []oai.ChatCompletionMessageParamUnion {
	out := make([]oai.ChatCompletionMessageParamUnion, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, translateMessage(m)...)
	}
	return out
}

func translateMessage(m message.Message) []oai.ChatCompletionMessageParamUnion {
	switch m.Role {
	case message.RoleSystem:
		return []oai.ChatCompletionMessageParamUnion{
			oai.SystemMessage(m.TextContent()),
		}

	case message.RoleUser:
		// Tool results → individual ToolMessages
		if len(m.Content) > 0 && m.Content[0].Type == message.ContentToolResult {
			var msgs []oai.ChatCompletionMessageParamUnion
			for _, c := range m.Content {
				if c.Type == message.ContentToolResult && c.ToolResult != nil {
					msgs = append(msgs, oai.ToolMessage(c.ToolResult.Content, c.ToolResult.ToolCallID))
				}
			}
			return msgs
		}
		return []oai.ChatCompletionMessageParamUnion{
			oai.UserMessage(m.TextContent()),
		}

	case message.RoleAssistant:
		msg := oai.ChatCompletionMessageParamUnion{
			OfAssistant: &oai.ChatCompletionAssistantMessageParam{
				Content: oai.ChatCompletionAssistantMessageParamContentUnion{
					OfString: param.NewOpt(m.TextContent()),
				},
			},
		}
		// Add tool calls
		for _, tc := range m.ToolCalls() {
			msg.OfAssistant.ToolCalls = append(msg.OfAssistant.ToolCalls, oai.ChatCompletionMessageToolCallParam{
				ID: tc.ID,
				Function: oai.ChatCompletionMessageToolCallFunctionParam{
					Name:      sanitizeToolName(tc.Name),
					Arguments: string(tc.Arguments),
				},
			})
		}
		return []oai.ChatCompletionMessageParamUnion{msg}

	default:
		return nil
	}
}

func translateTools(defs []provider.ToolDefinition) []oai.ChatCompletionToolParam {
	if len(defs) == 0 {
		return nil
	}
	tools := make([]oai.ChatCompletionToolParam, len(defs))
	for i, d := range defs {
		var params shared.FunctionParameters
		if d.Parameters != nil {
			_ = json.Unmarshal(d.Parameters, &params)
		}
		tools[i] = oai.ChatCompletionToolParam{
			Function: shared.FunctionDefinitionParam{
				Name:        sanitizeToolName(d.Name),
				Description: param.NewOpt(d.Description),
				Parameters:  params,
			},
		}
	}
	return tools
}

func translateRequest(req provider.Request) oai.ChatCompletionNewParams {
	params := oai.ChatCompletionNewParams{
		Model:    req.Model,
		Messages: translateMessages(req.Messages),
		Tools:    translateTools(req.Tools),
	}

	if req.MaxTokens > 0 {
		params.MaxCompletionTokens = param.NewOpt(req.MaxTokens)
	}
	if req.Temperature != nil {
		params.Temperature = param.NewOpt(*req.Temperature)
	}
	if req.TopP != nil {
		params.TopP = param.NewOpt(*req.TopP)
	}
	if len(req.StopSequences) > 0 {
		params.Stop = oai.ChatCompletionNewParamsStopUnion{
			OfStringArray: req.StopSequences,
		}
	}
	// Enable usage in streaming
	params.StreamOptions = oai.ChatCompletionStreamOptionsParam{
		IncludeUsage: param.NewOpt(true),
	}

	if len(params.Tools) > 0 {
		choice := "auto"
		if req.ToolChoice != "" {
			choice = string(req.ToolChoice)
		}
		params.ToolChoice = oai.ChatCompletionToolChoiceOptionUnionParam{
			OfAuto: param.NewOpt(choice),
		}
	}

	return params
}

// --- OpenAI → gnoma ---

func translateFinishReason(fr string) message.StopReason {
	switch fr {
	case "stop":
		return message.StopEndTurn
	case "tool_calls":
		return message.StopToolUse
	case "length":
		return message.StopMaxTokens
	default:
		return message.StopEndTurn
	}
}

func translateUsage(u oai.CompletionUsage) *message.Usage {
	return &message.Usage{
		InputTokens:  u.PromptTokens,
		OutputTokens: u.CompletionTokens,
	}
}
