package hook

import (
	"encoding/json"
	"fmt"
)

// MarshalPreToolPayload builds the stdin payload for a PreToolUse hook.
func MarshalPreToolPayload(tool string, args json.RawMessage) []byte {
	b, _ := json.Marshal(map[string]any{
		"event": "pre_tool_use",
		"tool":  tool,
		"args":  args,
	})
	return b
}

// MarshalPostToolPayload builds the stdin payload for a PostToolUse hook.
func MarshalPostToolPayload(tool string, args json.RawMessage, output string, metadata map[string]any) []byte {
	b, _ := json.Marshal(map[string]any{
		"event": "post_tool_use",
		"tool":  tool,
		"args":  args,
		"result": map[string]any{
			"output":   output,
			"metadata": metadata,
		},
	})
	return b
}

// MarshalSessionStartPayload builds the stdin payload for a SessionStart hook.
func MarshalSessionStartPayload(sessionID, mode string) []byte {
	b, _ := json.Marshal(map[string]any{
		"event":      "session_start",
		"session_id": sessionID,
		"mode":       mode,
	})
	return b
}

// MarshalSessionEndPayload builds the stdin payload for a SessionEnd hook.
func MarshalSessionEndPayload(sessionID string, turns int) []byte {
	b, _ := json.Marshal(map[string]any{
		"event":      "session_end",
		"session_id": sessionID,
		"turns":      turns,
	})
	return b
}

// MarshalPreCompactPayload builds the stdin payload for a PreCompact hook.
func MarshalPreCompactPayload(messageCount, tokenEstimate int) []byte {
	b, _ := json.Marshal(map[string]any{
		"event":          "pre_compact",
		"message_count":  messageCount,
		"token_estimate": tokenEstimate,
	})
	return b
}

// MarshalStopPayload builds the stdin payload for a Stop hook.
func MarshalStopPayload(reason string) []byte {
	b, _ := json.Marshal(map[string]any{
		"event":  "stop",
		"reason": reason,
	})
	return b
}

// ExtractToolName extracts the "tool" field from a hook payload.
// Returns "" for non-tool events or malformed payloads.
func ExtractToolName(payload []byte) string {
	var v struct {
		Tool string `json:"tool"`
	}
	if err := json.Unmarshal(payload, &v); err != nil {
		return ""
	}
	return v.Tool
}

// hookOutput is the JSON structure a hook may write to stdout.
type hookOutput struct {
	Action      string          `json:"action"`
	Transformed json.RawMessage `json:"transformed"`
}

// ParseHookOutput parses hook stdout and exit code into an Action and optional
// transformed payload. JSON "action" field overrides the exit code when present.
// Empty stdout falls back to exit code alone.
func ParseHookOutput(stdout []byte, exitCode int) (Action, json.RawMessage, error) {
	if len(stdout) == 0 {
		action, err := ParseAction(exitCode)
		return action, nil, err
	}

	var out hookOutput
	if err := json.Unmarshal(stdout, &out); err != nil {
		return 0, nil, fmt.Errorf("hook: invalid stdout JSON: %w", err)
	}

	var action Action
	if out.Action != "" {
		var err error
		action, err = parseActionString(out.Action)
		if err != nil {
			return 0, nil, err
		}
	} else {
		var err error
		action, err = ParseAction(exitCode)
		if err != nil {
			return 0, nil, err
		}
	}

	var transformed json.RawMessage
	if len(out.Transformed) > 0 {
		transformed = out.Transformed
	}
	return action, transformed, nil
}

// parseActionString maps a JSON "action" string to an Action.
func parseActionString(s string) (Action, error) {
	switch s {
	case "allow":
		return Allow, nil
	case "deny":
		return Deny, nil
	case "skip":
		return Skip, nil
	default:
		return 0, fmt.Errorf("hook: unknown action string %q", s)
	}
}

// ExtractTransformedArgs extracts the "args" field from a transformed PreToolUse payload.
// Returns nil if the field is absent or the payload is malformed.
func ExtractTransformedArgs(payload []byte) json.RawMessage {
	if payload == nil {
		return nil
	}
	var v struct {
		Args json.RawMessage `json:"args"`
	}
	if err := json.Unmarshal(payload, &v); err != nil {
		return nil
	}
	return v.Args
}

// ExtractTransformedOutput extracts the "output" string from a PostToolUse
// transformed payload. Returns "" if the payload is nil or malformed.
func ExtractTransformedOutput(transformed json.RawMessage) string {
	if transformed == nil {
		return ""
	}
	var v struct {
		Output string `json:"output"`
	}
	if err := json.Unmarshal(transformed, &v); err != nil {
		return ""
	}
	return v.Output
}
