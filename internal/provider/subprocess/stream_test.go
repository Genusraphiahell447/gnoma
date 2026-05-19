package subprocess

import (
	"context"
	"os/exec"
	"runtime"
	"testing"

	"somegit.dev/Owlibou/gnoma/internal/stream"
)

// TestSubprocessStream_EchoShell runs a real subprocess (printf) and verifies
// the stream delivers the expected events.
func TestSubprocessStream_EchoShell(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell printf not available on windows")
	}

	// Feed a vibe-format line through a printf subprocess.
	// We use vibe format because it's the simplest (no "done" event needed).
	cmd := exec.CommandContext(context.Background(), "sh", "-c",
		`printf '{"role":"assistant","content":"hello from subprocess","reasoning_content":null,"tool_calls":null,"message_id":"abc"}\n'`)

	s, err := newSubprocessStream(context.Background(), cmd, newVibeParser())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()

	var texts []string
	for s.Next() {
		ev := s.Current()
		if ev.Type == stream.EventTextDelta {
			texts = append(texts, ev.Text)
		}
	}
	if err := s.Err(); err != nil {
		t.Fatalf("stream error: %v", err)
	}
	if len(texts) == 0 {
		t.Fatal("no text events received")
	}
	if texts[0] != "hello from subprocess" {
		t.Errorf("got text %q, want %q", texts[0], "hello from subprocess")
	}
}

func TestSubprocessStream_ContextCancel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sleep not available on windows")
	}

	ctx, cancel := context.WithCancel(context.Background())

	cmd := exec.CommandContext(ctx, "sh", "-c", "sleep 30")
	s, err := newSubprocessStream(ctx, cmd, newVibeParser())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()

	cancel()
	// Drain — should stop quickly due to context cancellation.
	for s.Next() {
	}
	// No error assertion: context cancel may or may not propagate as stream error.
}

func TestSubprocessStream_ProcessError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh not available on windows")
	}

	cmd := exec.CommandContext(context.Background(), "sh", "-c", "exit 1")
	s, err := newSubprocessStream(context.Background(), cmd, newVibeParser())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()

	for s.Next() {
	}
	// A non-zero exit should surface as a stream error.
	if s.Err() == nil {
		t.Error("expected stream error for non-zero exit, got nil")
	}
}
