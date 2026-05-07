package subprocess

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"somegit.dev/Owlibou/gnoma/internal/stream"
)

// subprocessStream implements stream.Stream by reading line-delimited JSON
// from a subprocess stdout and converting lines via a FormatParser.
type subprocessStream struct {
	cmd       *exec.Cmd
	stdout    io.ReadCloser
	stderrBuf *bytes.Buffer
	scanner   *bufio.Scanner
	parser    FormatParser
	pending   []stream.Event
	current   stream.Event
	err       error
	done      bool
	waited    bool
}

// newSubprocessStream starts cmd, attaches a stdout pipe, and returns the stream.
// The caller must call Close() to release resources.
func newSubprocessStream(ctx context.Context, cmd *exec.Cmd, parser FormatParser) (*subprocessStream, error) {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("subprocess: stdout pipe: %w", err)
	}

	// Capture stderr for error messages; bounded to 8KB.
	stderrBuf := &bytes.Buffer{}
	cmd.Stderr = &limitedWriter{w: stderrBuf, n: 8192}

	// Explicitly close stdin so the subprocess doesn't block waiting for input.
	cmd.Stdin = nil

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("subprocess: start: %w", err)
	}

	_ = ctx // context cancellation is handled by exec.CommandContext in the caller
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB line buffer

	return &subprocessStream{
		cmd:       cmd,
		stdout:    stdout,
		stderrBuf: stderrBuf,
		scanner:   scanner,
		parser:    parser,
	}, nil
}

func (s *subprocessStream) Next() bool {
	if s.done || s.err != nil {
		return false
	}

	for {
		// Drain buffered events first.
		if len(s.pending) > 0 {
			s.current = s.pending[0]
			s.pending = s.pending[1:]
			return true
		}

		// Read next line from subprocess stdout.
		if !s.scanner.Scan() {
			// EOF — process has exited (or pipe closed).
			if err := s.scanner.Err(); err != nil {
				s.err = err
				return false
			}
			// Emit final events from parser.
			final := s.parser.Done()
			if len(final) > 0 {
				s.pending = final
			}
			// Wait for process to exit and surface any non-zero exit code.
			s.reap()
			s.done = true
			if len(s.pending) > 0 {
				s.current = s.pending[0]
				s.pending = s.pending[1:]
				return s.err == nil
			}
			return false
		}

		line := s.scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		evts, err := s.parser.ParseLine(line)
		if err != nil {
			// Non-fatal parse error: skip the line but continue.
			continue
		}
		s.pending = append(s.pending, evts...)
	}
}

func (s *subprocessStream) Current() stream.Event { return s.current }
func (s *subprocessStream) Err() error            { return s.err }

func (s *subprocessStream) Close() error {
	if s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	_ = s.stdout.Close()
	s.reap()
	return nil
}

// reap waits for the process exactly once. Non-zero exit is stored as stream error.
func (s *subprocessStream) reap() {
	if s.waited {
		return
	}
	s.waited = true
	if err := s.cmd.Wait(); err != nil {
		if s.err == nil {
			msg := strings.TrimSpace(s.stderrBuf.String())
			if msg != "" {
				s.err = fmt.Errorf("subprocess: %w: %s", err, msg)
			} else {
				s.err = fmt.Errorf("subprocess: %w", err)
			}
		}
	}
}

// limitedWriter is a writer that stops writing after n bytes.
type limitedWriter struct {
	w io.Writer
	n int
}

func (lw *limitedWriter) Write(p []byte) (int, error) {
	if lw.n <= 0 {
		return len(p), nil // silently discard
	}
	if len(p) > lw.n {
		p = p[:lw.n]
	}
	n, err := lw.w.Write(p)
	lw.n -= n
	return n, err
}
