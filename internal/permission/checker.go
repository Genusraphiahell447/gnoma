package permission

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
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
	mu       sync.RWMutex
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

// SetPromptFunc replaces the prompt function (e.g., switching from pipe to TUI prompt).
func (c *Checker) SetPromptFunc(fn PromptFunc) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.promptFn = fn
}

// SetMode changes the active permission mode.
func (c *Checker) SetMode(mode Mode) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.mode = mode
}

// Mode returns the current permission mode.
func (c *Checker) Mode() Mode {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.mode
}

// WithDenyPrompt returns a new Checker with the same mode and rules but a nil prompt
// function. When a tool would normally require prompting, it is auto-denied. Used for
// elf engines where there is no TUI to prompt.
func (c *Checker) WithDenyPrompt() *Checker {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return &Checker{
		mode:               c.mode,
		rules:              c.rules,
		promptFn:           nil,
		safetyDenyPatterns: c.safetyDenyPatterns,
	}
}

// Check evaluates whether a tool call is permitted.
// Returns nil if allowed, ErrDenied if denied.
func (c *Checker) Check(ctx context.Context, info ToolInfo, args json.RawMessage) error {
	c.mu.RLock()
	mode := c.mode
	promptFn := c.promptFn
	c.mu.RUnlock()

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
	if mode == ModeBypass {
		return nil
	}

	// Step 4: Rule-based allow
	if c.matchesRule(info.Name, args, ActionAllow) {
		return nil
	}

	// Step 5: Mode-specific behavior
	switch mode {
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

	// Step 6: Prompt user (using snapshot of promptFn taken before lock release)
	if promptFn == nil {
		// No prompt handler (e.g. elf sub-agent): auto-allow non-destructive fs
		// operations so elfs can write files in auto/acceptEdits modes. Deny
		// everything else that would normally require human approval.
		if strings.HasPrefix(info.Name, "fs.") && !info.IsDestructive {
			return nil
		}
		return fmt.Errorf("%w: no prompt handler for %s", ErrDenied, info.Name)
	}
	approved, err := promptFn(ctx, info.Name, args)
	if err != nil {
		return fmt.Errorf("permission prompt: %w", err)
	}
	if !approved {
		return fmt.Errorf("%w: user denied %s", ErrDenied, info.Name)
	}
	return nil
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
	// Orchestration tools (spawn_elfs, agent) carry elf PROMPTS as args — arbitrary
	// instruction text that may legitimately mention .env, credentials, etc.
	// Security is enforced inside each spawned elf when it actually accesses files.
	if toolName == "spawn_elfs" || toolName == "agent" {
		return nil
	}

	// For fs.* tools, only check the path field — not content being written.
	// Prevents false-positives when writing docs that reference .env, .ssh, etc.
	checkStr := string(args)
	if strings.HasPrefix(toolName, "fs.") {
		var parsed struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(args, &parsed); err == nil && parsed.Path != "" {
			checkStr = parsed.Path
		}
	}
	for _, pattern := range c.safetyDenyPatterns {
		if strings.Contains(checkStr, pattern) {
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

