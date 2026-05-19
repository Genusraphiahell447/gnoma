package bash

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"somegit.dev/Owlibou/gnoma/internal/tool"
)

const (
	defaultTimeout = 30 * time.Second
	toolName       = "bash"
)

var parameterSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"command": {
			"type": "string",
			"description": "The bash command to execute"
		},
		"timeout": {
			"type": "integer",
			"description": "Timeout in seconds (default 30)"
		}
	},
	"required": ["command"]
}`)

// Tool executes bash commands.
type Tool struct {
	timeout    time.Duration
	workingDir string
	aliases    *AliasMap
}

type Option func(*Tool)

func WithTimeout(d time.Duration) Option {
	return func(t *Tool) { t.timeout = d }
}

func WithWorkingDir(dir string) Option {
	return func(t *Tool) { t.workingDir = dir }
}

func WithAliases(aliases *AliasMap) Option {
	return func(t *Tool) { t.aliases = aliases }
}

// New creates a bash tool.
func New(opts ...Option) *Tool {
	t := &Tool{timeout: defaultTimeout}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

func (t *Tool) Name() string               { return toolName }
func (t *Tool) Description() string         { return "Execute a bash command and return its output" }
func (t *Tool) Parameters() json.RawMessage { return parameterSchema }
func (t *Tool) IsReadOnly() bool            { return false }
func (t *Tool) IsDestructive() bool         { return true }
func (t *Tool) Category() tool.Category     { return tool.CategoryExec }

type bashArgs struct {
	Command string `json:"command"`
	Timeout int    `json:"timeout,omitempty"`
}

func (t *Tool) Execute(ctx context.Context, args json.RawMessage) (tool.Result, error) {
	var a bashArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return tool.Result{}, fmt.Errorf("bash: invalid args: %w", err)
	}

	if a.Command == "" {
		return tool.Result{}, fmt.Errorf("bash: empty command")
	}

	// Expand aliases (first word only, matching bash behavior)
	command := a.Command
	if t.aliases != nil {
		command = t.aliases.ExpandCommand(command)
	}

	// Interactive detection: bail before security checks so the user gets
	// a helpful message rather than a timeout or security error.
	if reason := isInteractiveCmd(command); reason != "" {
		return tool.Result{
			Output:   fmt.Sprintf("%s\n(%s)", interactiveHint, reason),
			Metadata: map[string]any{"interactive": true},
		}, nil
	}

	// Security validation runs on the expanded command
	if violation := ValidateCommand(command); violation != nil {
		return tool.Result{
			Output:   fmt.Sprintf("Command blocked: %s", violation.Message),
			Metadata: map[string]any{"blocked": true, "check": int(violation.Check)},
		}, nil
	}

	timeout := t.timeout
	if a.Timeout > 0 {
		timeout = time.Duration(a.Timeout) * time.Second
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	if t.workingDir != "" {
		cmd.Dir = t.workingDir
	}

	output, err := cmd.CombinedOutput()
	exitCode := 0

	if err != nil {
		// Check timeout first — context deadline may also produce an ExitError
		if ctx.Err() == context.DeadlineExceeded {
			return tool.Result{
				Output:   fmt.Sprintf("Command timed out after %s\n%s", timeout, strings.TrimRight(string(output), "\n")),
				Metadata: map[string]any{"exit_code": -1, "timeout": true},
			}, nil
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return tool.Result{}, fmt.Errorf("bash: exec failed: %w", err)
		}
	}

	result := tool.Result{
		Output:   strings.TrimRight(string(output), "\n"),
		Metadata: map[string]any{"exit_code": exitCode},
	}

	if exitCode != 0 {
		result.Output = fmt.Sprintf("Exit code %d\n%s", exitCode, result.Output)
	}

	return result, nil
}
