package hook

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"
)

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
	cmd := exec.CommandContext(ctx, "sh", "-c", c.def.Exec)
	cmd.Stdin = bytes.NewReader(payload)

	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	runErr := cmd.Run()
	duration := time.Since(start)

	// Determine exit code and whether it was a timeout.
	exitCode := 0
	if runErr != nil {
		if ctx.Err() != nil {
			// Context deadline exceeded — apply fail_open policy.
			action := Deny
			if c.def.FailOpen {
				action = Allow
			}
			return HookResult{Action: action, Duration: duration}, fmt.Errorf("hook %q: timed out after %v", c.def.Name, timeout)
		}
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			// Unexpected error launching the process.
			action := Deny
			if c.def.FailOpen {
				action = Allow
			}
			return HookResult{Action: action, Duration: duration}, fmt.Errorf("hook %q: %w", c.def.Name, runErr)
		}
	}

	action, transformed, err := ParseHookOutput(stdout.Bytes(), exitCode)
	if err != nil {
		failAction := Deny
		if c.def.FailOpen {
			failAction = Allow
		}
		return HookResult{Action: failAction, Duration: duration}, fmt.Errorf("hook %q: %w", c.def.Name, err)
	}

	return HookResult{Action: action, Output: transformed, Duration: duration}, nil
}
