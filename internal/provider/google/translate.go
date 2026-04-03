package google

import (
	"encoding/json"
	"strings"

	"somegit.dev/Owlibou/gnoma/internal/message"
	"somegit.dev/Owlibou/gnoma/internal/provider"

	"google.golang.org/genai"
)

// --- gnoma → Google ---

func translateContents(msgs []message.Message) []*genai.Content {
	// Build a tool call ID → name map from history for FunctionResponse correlation
	toolNameMap := buildToolNameMap(msgs)

	var out []*genai.Content
	for _, m := range msgs {
		if m.Role == message.RoleSystem {
			continue
		}
		c := translateContent(m, toolNameMap)
		if c != nil {
			out = append(out, c)
		}
	}
	return out
}

// buildToolNameMap scans message history for tool calls and maps ID → name.
func buildToolNameMap(msgs []message.Message) map[string]string {
	m := make(map[string]string)
	for _, msg := range msgs {
		for _, c := range msg.Content {
			if c.Type == message.ContentToolCall && c.ToolCall != nil {
				m[c.ToolCall.ID] = c.ToolCall.Name
			}
		}
	}
	return m
}

func translateContent(m message.Message, toolNameMap map[string]string) *genai.Content {
	role := "user"
	if m.Role == message.RoleAssistant {
		role = "model"
	}

	var parts []*genai.Part
	for _, c := range m.Content {
		switch c.Type {
		case message.ContentText:
			if c.Text != "" {
				parts = append(parts, &genai.Part{Text: c.Text})
			}
		case message.ContentToolCall:
			if c.ToolCall != nil {
				var args map[string]any
				if c.ToolCall.Arguments != nil {
					_ = json.Unmarshal(c.ToolCall.Arguments, &args)
				}
				parts = append(parts, &genai.Part{
					FunctionCall: &genai.FunctionCall{
						ID:   c.ToolCall.ID,
						Name: sanitizeToolName(c.ToolCall.Name),
						Args: args,
					},
				})
			}
		case message.ContentToolResult:
			if c.ToolResult != nil {
				result := map[string]any{"output": c.ToolResult.Content}
				if c.ToolResult.IsError {
					result["error"] = true
				}
				// Google requires the function name for correlation.
				name := sanitizeToolName(toolNameMap[c.ToolResult.ToolCallID])
				parts = append(parts, &genai.Part{
					FunctionResponse: &genai.FunctionResponse{
						ID:       c.ToolResult.ToolCallID,
						Name:     name,
						Response: result,
					},
				})
			}
		}
	}

	if len(parts) == 0 {
		return nil
	}

	return &genai.Content{Role: role, Parts: parts}
}

func translateTools(defs []provider.ToolDefinition) []*genai.Tool {
	if len(defs) == 0 {
		return nil
	}

	var funcs []*genai.FunctionDeclaration
	for _, d := range defs {
		// Parse JSON Schema into the OpenAPI-style schema Google expects
		var schema map[string]any
		if d.Parameters != nil {
			_ = json.Unmarshal(d.Parameters, &schema)
		}

		funcs = append(funcs, &genai.FunctionDeclaration{
			Name:        sanitizeToolName(d.Name),
			Description: d.Description,
			Parameters:  schemaFromMap(schema),
		})
	}

	return []*genai.Tool{{FunctionDeclarations: funcs}}
}

// schemaFromMap converts a JSON Schema map to genai.Schema.
func schemaFromMap(m map[string]any) *genai.Schema {
	if m == nil {
		return nil
	}
	s := &genai.Schema{}
	if t, ok := m["type"].(string); ok {
		s.Type = genai.Type(strings.ToUpper(t))
	}
	if desc, ok := m["description"].(string); ok {
		s.Description = desc
	}
	if props, ok := m["properties"].(map[string]any); ok {
		s.Properties = make(map[string]*genai.Schema)
		for k, v := range props {
			if pm, ok := v.(map[string]any); ok {
				s.Properties[k] = schemaFromMap(pm)
			}
		}
	}
	if req, ok := m["required"].([]any); ok {
		for _, r := range req {
			if rs, ok := r.(string); ok {
				s.Required = append(s.Required, rs)
			}
		}
	}
	return s
}

func translateConfig(req provider.Request) *genai.GenerateContentConfig {
	cfg := &genai.GenerateContentConfig{
		Tools: translateTools(req.Tools),
	}

	if req.SystemPrompt != "" {
		cfg.SystemInstruction = &genai.Content{
			Parts: []*genai.Part{{Text: req.SystemPrompt}},
		}
	}
	if req.MaxTokens > 0 {
		mt := int32(req.MaxTokens)
		cfg.MaxOutputTokens = mt
	}
	if req.Temperature != nil {
		t := float32(*req.Temperature)
		cfg.Temperature = &t
	}
	if req.TopP != nil {
		p := float32(*req.TopP)
		cfg.TopP = &p
	}
	if req.TopK != nil {
		k := float32(*req.TopK)
		cfg.TopK = &k
	}
	if len(req.StopSequences) > 0 {
		cfg.StopSequences = req.StopSequences
	}

	return cfg
}

// --- Google → gnoma ---

func translateFinishReason(fr genai.FinishReason) message.StopReason {
	switch fr {
	case genai.FinishReasonStop:
		return message.StopEndTurn
	case genai.FinishReasonMaxTokens:
		return message.StopMaxTokens
	default:
		return message.StopEndTurn
	}
}

// --- Tool name sanitization ---

func sanitizeToolName(name string) string {
	return strings.ReplaceAll(name, ".", "_")
}

func unsanitizeToolName(name string) string {
	if strings.HasPrefix(name, "fs_") {
		return "fs." + name[3:]
	}
	return name
}
