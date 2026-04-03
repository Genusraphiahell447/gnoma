package permission

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

var ErrDenied = errors.New("permission denied")

// PromptFunc asks the user to approve/deny a tool call.
// Returns true if approved.
type PromptFunc func(ctx context.Context, toolName string, args json.RawMessage) (bool, error)

// ToolInfo provides tool metadata for permission decisions.
type ToolInfo struct {
	Name        string
	IsReadOnly  bool
	IsDestructive bool
}

// Checker evaluates tool permissions using the 7-step decision flow.
//
// Decision flow (from CC, adapted):
// 1. Rule-based deny gates (BEFORE mode — even bypass can't override)
// 2. Tool-specific safety checks (.env, .git, credentials)
// 3. Mode-based bypass
// 4. Rule-based allow
// 5. Mode-specific behavior
// 6. Prompt user if needed
type Checker struct {
	mode     Mode
	rules    []Rule
	promptFn PromptFunc

	// Safety patterns — always checked, even in bypass mode
	safetyDenyPatterns []string
}

func NewChecker(mode Mode, rules []Rule, promptFn PromptFunc) *Checker {
	return &Checker{
		mode:     mode,
		rules:    rules,
		promptFn: promptFn,
		safetyDenyPatterns: []string{
			".env", ".git/", "credentials", "id_rsa", "id_ed25519",
			".ssh/", ".gnupg/", ".aws/credentials",
		},
	}
}

// SetMode changes the active permission mode.
func (c *Checker) SetMode(mode Mode) {
	c.mode = mode
}

// Mode returns the current permission mode.
func (c *Checker) Mode() Mode {
	return c.mode
}

// Check evaluates whether a tool call is permitted.
// Returns nil if allowed, ErrDenied if denied.
func (c *Checker) Check(ctx context.Context, info ToolInfo, args json.RawMessage) error {
	// Step 1: Rule-based deny gates (bypass-immune)
	if c.matchesRule(info.Name, args, ActionDeny) {
		return fmt.Errorf("%w: deny rule matched for %s", ErrDenied, info.Name)
	}

	// Step 2: Safety checks (bypass-immune)
	if err := c.safetyCheck(info.Name, args); err != nil {
		return err
	}

	// For compound bash commands, check each subcommand
	if info.Name == "bash" {
		if err := c.checkCompoundCommand(ctx, info, args); err != nil {
			return err
		}
	}

	// Step 3: Mode-based bypass
	if c.mode == ModeBypass {
		return nil
	}

	// Step 4: Rule-based allow
	if c.matchesRule(info.Name, args, ActionAllow) {
		return nil
	}

	// Step 5: Mode-specific behavior
	switch c.mode {
	case ModeDeny:
		return fmt.Errorf("%w: deny mode, no allow rule for %s", ErrDenied, info.Name)

	case ModePlan:
		if !info.IsReadOnly {
			return fmt.Errorf("%w: plan mode, %s is not read-only", ErrDenied, info.Name)
		}
		return nil

	case ModeAcceptEdits:
		// Auto-allow file reads and edits, prompt for bash/destructive
		if info.IsReadOnly {
			return nil
		}
		if strings.HasPrefix(info.Name, "fs.") && !info.IsDestructive {
			return nil
		}
		// Fall through to prompt

	case ModeAuto:
		// Auto-allow read-only tools
		if info.IsReadOnly {
			return nil
		}
		// Fall through to prompt for write tools

	case ModeDefault:
		// Always prompt
	}

	// Step 6: Prompt user
	return c.prompt(ctx, info.Name, args)
}

func (c *Checker) matchesRule(toolName string, args json.RawMessage, action Action) bool {
	for _, rule := range c.rules {
		if rule.Action != action {
			continue
		}
		if !rule.Matches(toolName) {
			continue
		}
		// If rule has a pattern, check it against serialized args
		if rule.Pattern != "" {
			if !strings.Contains(string(args), rule.Pattern) {
				continue
			}
		}
		return true
	}
	return false
}

func (c *Checker) safetyCheck(toolName string, args json.RawMessage) error {
	argsStr := string(args)
	for _, pattern := range c.safetyDenyPatterns {
		if strings.Contains(argsStr, pattern) {
			return fmt.Errorf("%w: safety check blocked access to %q via %s", ErrDenied, pattern, toolName)
		}
	}
	return nil
}

func (c *Checker) checkCompoundCommand(ctx context.Context, info ToolInfo, args json.RawMessage) error {
	var bashArgs struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(args, &bashArgs); err != nil || bashArgs.Command == "" {
		return nil
	}

	subcommands := SplitCompoundCommand(bashArgs.Command)
	if len(subcommands) <= 1 {
		return nil // single command, handled by main flow
	}

	// Check each subcommand — deny from any subcommand denies the whole compound
	for _, sub := range subcommands {
		subArgs, _ := json.Marshal(map[string]string{"command": sub})
		if c.matchesRule("bash", subArgs, ActionDeny) {
			return fmt.Errorf("%w: deny rule matched subcommand %q", ErrDenied, sub)
		}
	}
	return nil
}

func (c *Checker) prompt(ctx context.Context, toolName string, args json.RawMessage) error {
	if c.promptFn == nil {
		// No prompt function — deny by default
		return fmt.Errorf("%w: no prompt handler for %s", ErrDenied, toolName)
	}

	approved, err := c.promptFn(ctx, toolName, args)
	if err != nil {
		return fmt.Errorf("permission prompt: %w", err)
	}
	if !approved {
		return fmt.Errorf("%w: user denied %s", ErrDenied, toolName)
	}
	return nil
}
