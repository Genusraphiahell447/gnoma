package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"somegit.dev/Owlibou/gnoma/internal/message"
	"somegit.dev/Owlibou/gnoma/internal/provider"
	"somegit.dev/Owlibou/gnoma/internal/router"
	"somegit.dev/Owlibou/gnoma/internal/security"
	"somegit.dev/Owlibou/gnoma/internal/stream"
	"somegit.dev/Owlibou/gnoma/internal/tool"
)

// --- isFailoverable ---

func TestIsFailoverable(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"context canceled", context.Canceled, false},
		{"context deadline", context.DeadlineExceeded, false},
		{"wrapped context canceled", fmt.Errorf("wrapped: %w", context.Canceled), false},
		{"bad request 400", &provider.ProviderError{StatusCode: 400, Message: "schema"}, false},
		{"too large 413", &provider.ProviderError{StatusCode: 413, Message: "too big"}, false},
		{"auth 401", &provider.ProviderError{StatusCode: 401, Message: "unauthorized"}, true},
		{"forbidden 403", &provider.ProviderError{StatusCode: 403, Message: "forbidden"}, true},
		{"rate limit 429", &provider.ProviderError{StatusCode: 429, Message: "rate"}, true},
		{"server error 500", &provider.ProviderError{StatusCode: 500, Message: "boom"}, true},
		{"subprocess auth error", errors.New("subprocess: exit status 1: Error: Invalid API key"), true},
		{"generic network error", errors.New("connection reset"), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isFailoverable(tc.err); got != tc.want {
				t.Errorf("isFailoverable(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// --- shortFailReason ---

func TestShortFailReason(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "nil",
			err:  nil,
			want: "",
		},
		{
			name: "simple message",
			err:  errors.New("bad thing"),
			want: "bad thing",
		},
		{
			name: "subprocess wrapper stripped",
			err:  errors.New("subprocess: exit status 1: Error: Invalid API key. Try again."),
			want: "Invalid API key. Try again.",
		},
		{
			name: "newlines collapsed",
			err:  errors.New("first line\nsecond line"),
			want: "first line second line",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shortFailReason(tc.err); got != tc.want {
				t.Errorf("shortFailReason() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestShortFailReason_Truncates(t *testing.T) {
	long := strings.Repeat("x", 500)
	got := shortFailReason(errors.New(long))
	if len(got) > 160 {
		t.Errorf("expected truncation to <= 160 chars, got %d", len(got))
	}
	if !strings.HasSuffix(got, "...") {
		t.Errorf("expected ellipsis suffix, got %q", got[len(got)-10:])
	}
}

// --- end-to-end failover via router ---

// erroringStream returns no events and reports err from Err().
type erroringStream struct {
	err  error
	done bool
}

func (s *erroringStream) Next() bool         { return false }
func (s *erroringStream) Current() stream.Event { return stream.Event{} }
func (s *erroringStream) Err() error          { return s.err }
func (s *erroringStream) Close() error        { s.done = true; return nil }

// makeFailoverArm builds a router.Arm wrapping a mock provider whose Stream
// either returns an erroringStream (when failErr is non-nil) or a successful
// eventStream with the given text. isCLIAgent puts the arm into the router's
// tier-0 bucket so tests can deterministically order which arm gets picked
// first (otherwise the quality bandit makes selection flaky).
func makeFailoverArm(t *testing.T, id router.ArmID, text string, failErr error, isCLIAgent bool) *router.Arm {
	t.Helper()
	mp := &mockProvider{name: id.Provider()}
	if failErr != nil {
		mp.streams = []stream.Stream{&erroringStream{err: failErr}}
	} else {
		mp.streams = []stream.Stream{
			newEventStream(message.StopEndTurn, id.Model(),
				stream.Event{Type: stream.EventTextDelta, Text: text},
			),
		}
	}
	return &router.Arm{
		ID:           id,
		Provider:     security.WrapProvider(mp, nil),
		ModelName:    id.Model(),
		IsCLIAgent:   isCLIAgent,
		Capabilities: provider.Capabilities{ToolUse: true, ContextWindow: 32000},
	}
}

func TestEngine_FailsOver_OnConsumptionTimeAuthError(t *testing.T) {
	rtr := router.New(router.Config{})
	// vibe is tier-0 (CLI agent); claude is tier-2 (API). Router picks vibe
	// first; on exclusion after failover, claude is the only remaining arm.
	rtr.RegisterArm(makeFailoverArm(t, router.NewArmID("subprocess", "vibe"), "",
		errors.New("subprocess: exit status 1: Error: Invalid API key"), true))
	rtr.RegisterArm(makeFailoverArm(t, router.NewArmID("anthropic", "claude"), "Hello world", nil, false))

	e, err := New(Config{
		Provider: secureMock(&mockProvider{name: "fallback"}),
		Router:   rtr,
		Tools:    tool.NewRegistry(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var events []stream.Event
	turn, err := e.Submit(context.Background(), "hi", func(evt stream.Event) {
		events = append(events, evt)
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}

	// Failover event must have fired.
	var failover *stream.Event
	for i := range events {
		if events[i].Type == stream.EventFailover {
			failover = &events[i]
			break
		}
	}
	if failover == nil {
		t.Fatal("expected EventFailover in callback events")
	}
	if !strings.Contains(failover.FailedArm, "vibe") {
		t.Errorf("FailedArm = %q, want one mentioning vibe", failover.FailedArm)
	}
	if !strings.Contains(failover.FailedReason, "Invalid API key") {
		t.Errorf("FailedReason = %q, want one mentioning Invalid API key", failover.FailedReason)
	}

	// Final response must be from the successful arm.
	if len(turn.Messages) == 0 || turn.Messages[len(turn.Messages)-1].TextContent() != "Hello world" {
		t.Errorf("final text = %q, want %q",
			turn.Messages[len(turn.Messages)-1].TextContent(), "Hello world")
	}
}

func TestEngine_DoesNotFailOver_AfterContentEmitted(t *testing.T) {
	// One arm. Its stream emits text, then errors. The engine must NOT
	// attempt failover (would-be-duplicate output) — content is visible
	// to the user, so the only honest response is to surface the error.
	// Single-arm setup keeps the test deterministic without needing to
	// stub the router's bandit selection.
	rtr := router.New(router.Config{})
	mp := &mockProvider{
		name: "subprocess",
		streams: []stream.Stream{
			&halfFailStream{
				preEvents: []stream.Event{{Type: stream.EventTextDelta, Text: "partial..."}},
				err:       errors.New("subprocess: exit status 1: Error: disconnected"),
			},
		},
	}
	rtr.RegisterArm(&router.Arm{
		ID:           router.NewArmID("subprocess", "vibe"),
		Provider:     security.WrapProvider(mp, nil),
		ModelName:    "vibe",
		Capabilities: provider.Capabilities{ToolUse: true, ContextWindow: 32000},
	})

	e, _ := New(Config{
		Provider: secureMock(&mockProvider{name: "fallback"}),
		Router:   rtr,
		Tools:    tool.NewRegistry(),
	})

	var events []stream.Event
	_, err := e.Submit(context.Background(), "hi", func(evt stream.Event) {
		events = append(events, evt)
	})
	if err == nil {
		t.Fatal("expected error to bubble up since content was already streamed")
	}
	for _, ev := range events {
		if ev.Type == stream.EventFailover {
			t.Errorf("unexpected EventFailover after content emission")
		}
	}
}

func TestEngine_DoesNotFailOver_OnContextCancel(t *testing.T) {
	rtr := router.New(router.Config{})
	rtr.RegisterArm(makeFailoverArm(t, router.NewArmID("subprocess", "vibe"), "",
		fmt.Errorf("wrapped: %w", context.Canceled), true))
	rtr.RegisterArm(makeFailoverArm(t, router.NewArmID("anthropic", "claude"), "should-not-run", nil, false))

	e, _ := New(Config{
		Provider: secureMock(&mockProvider{name: "fallback"}),
		Router:   rtr,
		Tools:    tool.NewRegistry(),
	})

	var events []stream.Event
	_, err := e.Submit(context.Background(), "hi", func(evt stream.Event) {
		events = append(events, evt)
	})
	if err == nil {
		t.Fatal("expected context-canceled error to surface, got nil")
	}
	for _, ev := range events {
		if ev.Type == stream.EventFailover {
			t.Error("EventFailover must not fire on context.Canceled")
		}
	}
}

// halfFailStream emits preEvents, then reports err from Err().
type halfFailStream struct {
	preEvents []stream.Event
	idx       int
	err       error
}

func (s *halfFailStream) Next() bool {
	if s.idx >= len(s.preEvents) {
		return false
	}
	s.idx++
	return true
}
func (s *halfFailStream) Current() stream.Event { return s.preEvents[s.idx-1] }
func (s *halfFailStream) Err() error            { return s.err }
func (s *halfFailStream) Close() error          { return nil }
