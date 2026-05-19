package hook

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"time"
)

// failOpenAction returns Allow when FailOpen is set, Deny otherwise, and logs
// a WARN naming the hook and the underlying reason so that abuse or chronic
// hook failure is visible in operator logs (audit H3).
func (c *CommandExecutor) failOpenAction(reason string, cause error) Action {
	if c.def.FailOpen {
		slog.Warn("hook fail-open: allowing tool call despite hook failure",
			"hook", c.def.Name,
			"reason", reason,
			"error", cause,
		)
		return Allow
	}
	return Deny
}

// CommandExecutor runs a shell command and interprets its stdin/stdout.
type CommandExecutor struct {
	def HookDef
}

// NewCommandExecutor constructs a CommandExecutor for the given definition.
func NewCommandExecutor(def HookDef) *CommandExecutor {
	return &CommandExecutor{def: def}
}

// Execute runs the hook command, pipes payload to stdin, and reads stdout.
func (c *CommandExecutor) Execute(ctx context.Context, payload []byte) (HookResult, error) {
	timeout := c.def.timeout()
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	// Exec is a resolved binary path (see plugin/loader.go). No shell wrapping —
	// shell metacharacters in the path are treated as literal filename bytes.
	cmd := exec.CommandContext(ctx, c.def.Exec)
	cmd.Stdin = bytes.NewReader(payload)

	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	runErr := cmd.Run()
	duration := time.Since(start)

	// Determine exit code and whether it was a timeout.
	exitCode := 0
	if runErr != nil {
		if ctx.Err() != nil {
			timeoutErr := fmt.Errorf("hook %q: timed out after %v", c.def.Name, timeout)
			return HookResult{Action: c.failOpenAction("timeout", timeoutErr), Duration: duration}, timeoutErr
		}
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			launchErr := fmt.Errorf("hook %q: %w", c.def.Name, runErr)
			return HookResult{Action: c.failOpenAction("launch_error", launchErr), Duration: duration}, launchErr
		}
	}

	action, transformed, err := ParseHookOutput(stdout.Bytes(), exitCode)
	if err != nil {
		parseErr := fmt.Errorf("hook %q: %w", c.def.Name, err)
		return HookResult{Action: c.failOpenAction("parse_error", parseErr), Duration: duration}, parseErr
	}

	return HookResult{Action: action, Output: transformed, Duration: duration}, nil
}
