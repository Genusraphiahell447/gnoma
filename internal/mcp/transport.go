package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const maxStderrCapture = 64 << 10 // 64KB

// Transport manages a stdio connection to an MCP server process.
type Transport struct {
	command string
	args    []string
	env     map[string]string

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	stderr limitedWriter

	nextID atomic.Int64
	mu     sync.Mutex // serializes writes to stdin

	logger *slog.Logger
}

// NewTransport creates a transport for the given command. Call Start to spawn the process.
func NewTransport(command string, args []string, env map[string]string, logger *slog.Logger) *Transport {
	return &Transport{
		command: command,
		args:    args,
		env:     env,
		logger:  logger,
	}
}

// Start spawns the MCP server process.
func (t *Transport) Start(ctx context.Context) error {
	t.cmd = exec.CommandContext(ctx, t.command, t.args...)
	t.cmd.Env = t.buildEnv()
	// Create a new process group so Close can kill the entire tree.
	t.cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	t.stderr = limitedWriter{max: maxStderrCapture}
	t.cmd.Stderr = &t.stderr

	var err error
	t.stdin, err = t.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("mcp: stdin pipe: %w", err)
	}

	stdout, err := t.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("mcp: stdout pipe: %w", err)
	}
	t.stdout = bufio.NewReader(stdout)

	if err := t.cmd.Start(); err != nil {
		return fmt.Errorf("mcp: start %q: %w", t.command, err)
	}

	t.logger.Debug("mcp transport started", "command", t.command, "pid", t.cmd.Process.Pid)
	return nil
}

// Call sends a JSON-RPC request and waits for the response.
func (t *Transport) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := t.nextID.Add(1)

	var rawParams json.RawMessage
	if params != nil {
		var err error
		rawParams, err = json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("mcp: marshal params: %w", err)
		}
	}

	req := Request{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  rawParams,
	}

	if err := t.send(ctx, req); err != nil {
		return nil, err
	}

	return t.readResponse(ctx, id)
}

// Notify sends a JSON-RPC notification (no response expected).
func (t *Transport) Notify(ctx context.Context, method string, params any) error {
	var rawParams json.RawMessage
	if params != nil {
		var err error
		rawParams, err = json.Marshal(params)
		if err != nil {
			return fmt.Errorf("mcp: marshal params: %w", err)
		}
	}

	notif := Notification{
		JSONRPC: "2.0",
		Method:  method,
		Params:  rawParams,
	}

	return t.send(ctx, notif)
}

// Close gracefully shuts down the server process.
func (t *Transport) Close() error {
	if t.stdin != nil {
		if err := t.stdin.Close(); err != nil {
			t.logger.Debug("mcp: stdin close error", "command", t.command, "error", err)
		}
	}

	if t.cmd == nil || t.cmd.Process == nil {
		return nil
	}

	// Give the process a chance to exit gracefully.
	done := make(chan error, 1)
	go func() {
		done <- t.cmd.Wait()
	}()

	// Try graceful exit first (stdin closed above), then escalate.
	select {
	case err := <-done:
		return err
	case <-time.After(2 * time.Second):
	}

	// Graceful didn't work — kill the entire process group.
	t.logger.Warn("mcp: server did not exit, killing process group", "command", t.command)
	if err := syscall.Kill(-t.cmd.Process.Pid, syscall.SIGKILL); err != nil {
		t.logger.Warn("mcp: SIGKILL to process group failed",
			"command", t.command, "pid", t.cmd.Process.Pid, "error", err)
	}
	return <-done
}

// Stderr returns captured stderr output from the server process.
func (t *Transport) Stderr() string {
	return t.stderr.String()
}

func (t *Transport) send(ctx context.Context, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("mcp: marshal: %w", err)
	}
	data = append(data, '\n')

	t.mu.Lock()
	defer t.mu.Unlock()

	if ctx.Err() != nil {
		return ctx.Err()
	}

	if _, err := t.stdin.Write(data); err != nil {
		return fmt.Errorf("mcp: write: %w", err)
	}
	return nil
}

func (t *Transport) readResponse(ctx context.Context, expectedID int64) (json.RawMessage, error) {
	type readResult struct {
		line []byte
		err  error
	}

	ch := make(chan readResult, 1)
	go func() {
		line, err := t.stdout.ReadBytes('\n')
		ch <- readResult{line, err}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case rr := <-ch:
		if rr.err != nil {
			stderr := t.Stderr()
			if stderr != "" {
				return nil, fmt.Errorf("mcp: read: %w (stderr: %s)", rr.err, stderr)
			}
			return nil, fmt.Errorf("mcp: read: %w", rr.err)
		}

		var resp Response
		if err := json.Unmarshal(rr.line, &resp); err != nil {
			return nil, fmt.Errorf("mcp: decode response: %w", err)
		}

		if resp.Error != nil {
			return nil, resp.Error
		}

		return resp.Result, nil
	}
}

func (t *Transport) buildEnv() []string {
	env := os.Environ()
	for k, v := range t.env {
		env = append(env, k+"="+v)
	}
	return env
}

// limitedWriter captures up to max bytes.
type limitedWriter struct {
	buf bytes.Buffer
	max int
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	remaining := w.max - w.buf.Len()
	if remaining <= 0 {
		return len(p), nil // silently discard
	}
	if len(p) > remaining {
		p = p[:remaining]
	}
	return w.buf.Write(p)
}

func (w *limitedWriter) String() string {
	return w.buf.String()
}
