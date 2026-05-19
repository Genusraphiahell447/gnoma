package security

import (
	"encoding/json"
	"log/slog"

	"somegit.dev/Owlibou/gnoma/internal/message"
)

// Firewall scans outgoing LLM requests and incoming tool results
// for secrets, sensitive data, and dangerous Unicode. Core security
// layer — not a plugin, everyone benefits by default.
type Firewall struct {
	scanner   *Scanner
	incognito *IncognitoMode
	logger    *slog.Logger

	// Config
	scanOutgoing    bool
	scanToolResults bool
}

type FirewallConfig struct {
	ScanOutgoing      bool
	ScanToolResults   bool
	RedactHighEntropy bool
	EntropyThreshold  float64
	Logger            *slog.Logger
}

func NewFirewall(cfg FirewallConfig) *Firewall {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Firewall{
		scanner:         NewScanner(cfg.EntropyThreshold, cfg.RedactHighEntropy),
		incognito:       NewIncognitoMode(),
		logger:          logger,
		scanOutgoing:    cfg.ScanOutgoing,
		scanToolResults: cfg.ScanToolResults,
	}
}

// Incognito returns the incognito mode controller.
func (f *Firewall) Incognito() *IncognitoMode {
	return f.incognito
}

// Scanner returns the secret scanner for adding custom patterns.
func (f *Firewall) Scanner() *Scanner {
	return f.scanner
}

// ScanOutgoingMessages scans all message content before sending to provider.
// Returns cleaned messages with secrets redacted.
func (f *Firewall) ScanOutgoingMessages(msgs []message.Message) []message.Message {
	if !f.scanOutgoing {
		return msgs
	}

	cleaned := make([]message.Message, len(msgs))
	for i, m := range msgs {
		cleaned[i] = f.scanMessage(m)
	}
	return cleaned
}

// ScanToolResult scans a tool execution result for secrets.
// Returns the cleaned content.
func (f *Firewall) ScanToolResult(content string) string {
	if !f.scanToolResults {
		return content
	}
	return f.scanAndRedact(content, "tool_result")
}

// ScanSystemPrompt scans the system prompt for accidentally embedded secrets.
func (f *Firewall) ScanSystemPrompt(prompt string) string {
	return f.scanAndRedact(prompt, "system_prompt")
}

func (f *Firewall) scanMessage(m message.Message) message.Message {
	cleaned := message.Message{Role: m.Role}
	cleaned.Content = make([]message.Content, len(m.Content))

	for i, c := range m.Content {
		switch c.Type {
		case message.ContentText:
			cleaned.Content[i] = message.NewTextContent(
				f.scanAndRedact(c.Text, "message_text"),
			)
		case message.ContentToolResult:
			if c.ToolResult != nil {
				tr := *c.ToolResult
				tr.Content = f.scanAndRedact(tr.Content, "tool_result")
				cleaned.Content[i] = message.NewToolResultContent(tr)
			} else {
				cleaned.Content[i] = c
			}
		case message.ContentToolCall:
			// Scan LLM-generated tool arguments for accidentally embedded secrets
			if c.ToolCall != nil {
				tc := *c.ToolCall
				scanned := f.scanAndRedact(string(tc.Arguments), "tool_call_args")
				tc.Arguments = json.RawMessage(scanned)
				cleaned.Content[i] = message.NewToolCallContent(tc)
			} else {
				cleaned.Content[i] = c
			}
		default:
			// Thinking blocks — pass through
			cleaned.Content[i] = c
		}
	}
	return cleaned
}

func (f *Firewall) scanAndRedact(content, source string) string {
	// Unicode sanitization first
	content = SanitizeUnicode(content)

	// Secret scanning
	matches := f.scanner.Scan(content)
	if len(matches) == 0 {
		return content
	}

	for _, m := range matches {
		switch m.Action {
		case ActionBlock:
			f.logger.Error("blocked: secret detected",
				"pattern", m.Pattern,
				"source", source,
			)
			return "[BLOCKED: content contained a secret]"
		default:
			f.logger.Debug("secret redacted",
				"pattern", m.Pattern,
				"action", m.Action,
				"source", source,
			)
		}
	}

	return Redact(content, matches)
}
