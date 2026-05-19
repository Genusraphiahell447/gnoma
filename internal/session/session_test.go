package session

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"somegit.dev/Owlibou/gnoma/internal/engine"
	"somegit.dev/Owlibou/gnoma/internal/message"
	"somegit.dev/Owlibou/gnoma/internal/provider"
	"somegit.dev/Owlibou/gnoma/internal/stream"
	"somegit.dev/Owlibou/gnoma/internal/tool"
)

// --- Mock Provider ---

type mockProvider struct {
	name    string
	calls   int
	streams []stream.Stream
}

func (m *mockProvider) Name() string         { return m.name }
func (m *mockProvider) DefaultModel() string  { return "mock-model" }
func (m *mockProvider) Models(_ context.Context) ([]provider.ModelInfo, error) {
	return nil, nil
}
func (m *mockProvider) Stream(_ context.Context, _ provider.Request) (stream.Stream, error) {
	if m.calls >= len(m.streams) {
		return nil, fmt.Errorf("no more streams")
	}
	s := m.streams[m.calls]
	m.calls++
	return s, nil
}

type eventStream struct {
	events []stream.Event
	idx    int
}

func newEventStream(stopReason message.StopReason, events ...stream.Event) *eventStream {
	events = append(events, stream.Event{Type: stream.EventTextDelta, StopReason: stopReason})
	return &eventStream{events: events}
}

func (s *eventStream) Next() bool         { s.idx++; return s.idx <= len(s.events) }
func (s *eventStream) Current() stream.Event { return s.events[s.idx-1] }
func (s *eventStream) Err() error          { return nil }
func (s *eventStream) Close() error        { return nil }

// --- Tests ---

func TestLocal_SendAndReceive(t *testing.T) {
	mp := &mockProvider{
		name: "test",
		streams: []stream.Stream{
			newEventStream(message.StopEndTurn,
				stream.Event{Type: stream.EventTextDelta, Text: "Hello "},
				stream.Event{Type: stream.EventTextDelta, Text: "world!"},
			),
		},
	}

	eng, _ := engine.New(engine.Config{Provider: mp, Tools: tool.NewRegistry()})
	sess := NewLocal(LocalConfig{Engine: eng, Provider: "test", Model: "mock-model"})

	// Initial state
	status := sess.Status()
	if status.State != StateIdle {
		t.Errorf("initial state = %s, want idle", status.State)
	}

	// Send
	if err := sess.Send("hello"); err != nil {
		t.Fatalf("Send: %v", err)
	}

	// Collect events
	var texts []string
	for evt := range sess.Events() {
		if evt.Type == stream.EventTextDelta && evt.Text != "" {
			texts = append(texts, evt.Text)
		}
	}

	if len(texts) == 0 {
		t.Error("should receive text events")
	}

	// Turn result
	turn, err := sess.TurnResult()
	if err != nil {
		t.Fatalf("TurnResult: %v", err)
	}
	if turn == nil {
		t.Fatal("turn should not be nil")
	}

	// Back to idle
	status = sess.Status()
	if status.State != StateIdle {
		t.Errorf("state after turn = %s, want idle", status.State)
	}
	if status.TurnCount != 1 {
		t.Errorf("TurnCount = %d, want 1", status.TurnCount)
	}
}

func TestLocal_SendWhileBusy(t *testing.T) {
	mp := &mockProvider{
		name: "test",
		streams: []stream.Stream{
			newEventStream(message.StopEndTurn,
				stream.Event{Type: stream.EventTextDelta, Text: "slow..."},
			),
		},
	}

	eng, _ := engine.New(engine.Config{Provider: mp, Tools: tool.NewRegistry()})
	sess := NewLocal(LocalConfig{Engine: eng, Provider: "test", Model: "model"})

	_ = sess.Send("first")

	// Try to send while still processing
	err := sess.Send("second")
	if err == nil {
		t.Error("should error when sending while busy")
	}

	// Drain events to let first turn complete
	for range sess.Events() {
	}
}

func TestLocal_Cancel(t *testing.T) {
	// Create a slow stream with many events
	events := make([]stream.Event, 100)
	for i := range events {
		events[i] = stream.Event{Type: stream.EventTextDelta, Text: "x"}
	}
	mp := &mockProvider{
		name:    "test",
		streams: []stream.Stream{&slowStream{events: events}},
	}

	eng, _ := engine.New(engine.Config{Provider: mp, Tools: tool.NewRegistry()})
	sess := NewLocal(LocalConfig{Engine: eng, Provider: "test", Model: "model"})

	_ = sess.Send("slow task")

	// Read a few events then cancel
	evts := sess.Events()
	<-evts // wait for first event
	sess.Cancel()

	// Drain remaining
	for range evts {
	}

	// Should be cancelled or error (context.Canceled wraps to error)
	status := sess.Status()
	if status.State != StateCancelled && status.State != StateError && status.State != StateIdle {
		t.Errorf("state after cancel = %s, want cancelled/error/idle", status.State)
	}
}

func TestLocal_Close(t *testing.T) {
	mp := &mockProvider{name: "test"}
	eng, _ := engine.New(engine.Config{Provider: mp, Tools: tool.NewRegistry()})
	sess := NewLocal(LocalConfig{Engine: eng, Provider: "test", Model: "model"})

	if err := sess.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	status := sess.Status()
	if status.State != StateClosed {
		t.Errorf("state after close = %s, want closed", status.State)
	}
}

func TestLocal_StatusTracking(t *testing.T) {
	mp := &mockProvider{
		name: "test",
		streams: []stream.Stream{
			newEventStream(message.StopEndTurn,
				stream.Event{Type: stream.EventUsage, Usage: &message.Usage{InputTokens: 100, OutputTokens: 50}},
				stream.Event{Type: stream.EventTextDelta, Text: "ok"},
			),
			newEventStream(message.StopEndTurn,
				stream.Event{Type: stream.EventUsage, Usage: &message.Usage{InputTokens: 200, OutputTokens: 80}},
				stream.Event{Type: stream.EventTextDelta, Text: "ok"},
			),
		},
	}

	eng, _ := engine.New(engine.Config{Provider: mp, Tools: tool.NewRegistry()})
	sess := NewLocal(LocalConfig{Engine: eng, Provider: "test", Model: "mock-model"})

	// Turn 1
	_ = sess.Send("one")
	for range sess.Events() {
	}

	// Turn 2
	_ = sess.Send("two")
	for range sess.Events() {
	}

	status := sess.Status()
	if status.TurnCount != 2 {
		t.Errorf("TurnCount = %d, want 2", status.TurnCount)
	}
	if status.TokensUsed != 430 { // 100+50+200+80
		t.Errorf("TokensUsed = %d, want 430", status.TokensUsed)
	}
	if status.Provider != "test" {
		t.Errorf("Provider = %q", status.Provider)
	}
	if status.Model != "mock-model" {
		t.Errorf("Model = %q", status.Model)
	}
}

// slowStream produces events slowly then stops.
type slowStream struct {
	events []stream.Event
	idx    int
}

func (s *slowStream) Next() bool {
	if s.idx >= len(s.events) {
		return false
	}
	time.Sleep(50 * time.Millisecond)
	s.idx++
	return true
}
func (s *slowStream) Current() stream.Event { return s.events[s.idx-1] }
func (s *slowStream) Err() error            { return nil }
func (s *slowStream) Close() error          { return nil }

// Ensure Local implements Session interface
var _ Session = (*Local)(nil)

func TestLocal_AutoSave(t *testing.T) {
	mp := &mockProvider{
		name: "test",
		streams: []stream.Stream{
			newEventStream(message.StopEndTurn,
				stream.Event{Type: stream.EventTextDelta, Text: "saved!"},
			),
		},
	}

	eng, _ := engine.New(engine.Config{Provider: mp, Tools: tool.NewRegistry()})
	store := NewSessionStore(t.TempDir(), 10, slog.Default())
	sess := NewLocal(LocalConfig{
		Engine:    eng,
		Provider:  "test",
		Model:     "mock-model",
		SessionID: "test-session-001",
		Store:     store,
	})

	if err := sess.Send("hello"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	for range sess.Events() {
	}

	snap, err := store.Load("test-session-001")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if snap.ID != "test-session-001" {
		t.Errorf("snap.ID = %q, want %q", snap.ID, "test-session-001")
	}
	if snap.Metadata.Provider != "test" {
		t.Errorf("snap.Metadata.Provider = %q, want %q", snap.Metadata.Provider, "test")
	}
	if snap.Metadata.TurnCount != 1 {
		t.Errorf("snap.Metadata.TurnCount = %d, want 1", snap.Metadata.TurnCount)
	}
	if len(snap.Messages) == 0 {
		t.Error("snap.Messages should not be empty after a turn")
	}
}

func TestLocal_AutoSave_SkipsWhenNoStore(t *testing.T) {
	mp := &mockProvider{
		name: "test",
		streams: []stream.Stream{
			newEventStream(message.StopEndTurn,
				stream.Event{Type: stream.EventTextDelta, Text: "ok"},
			),
		},
	}

	eng, _ := engine.New(engine.Config{Provider: mp, Tools: tool.NewRegistry()})
	// No store — must not panic
	sess := NewLocal(LocalConfig{Engine: eng, Provider: "test", Model: "mock-model"})

	if err := sess.Send("hello"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	for range sess.Events() {
	}

	status := sess.Status()
	if status.State != StateIdle {
		t.Errorf("state = %s, want idle", status.State)
	}
}

func TestLocal_SessionID(t *testing.T) {
	mp := &mockProvider{name: "test"}
	eng, _ := engine.New(engine.Config{Provider: mp, Tools: tool.NewRegistry()})
	sess := NewLocal(LocalConfig{Engine: eng, Provider: "test", Model: "m", SessionID: "my-id"})
	if sess.SessionID() != "my-id" {
		t.Errorf("SessionID() = %q, want %q", sess.SessionID(), "my-id")
	}
}

func TestSessionTitle(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"fix the login bug", "fix the login bug"},
		{"first line\nsecond line", "first line"},
		{"  whitespace  ", "whitespace"},
		{"", ""},
		{strings.Repeat("a", 80), strings.Repeat("a", 60) + "…"},
		{strings.Repeat("b", 60), strings.Repeat("b", 60)}, // exactly 60 — no truncation
	}
	for _, tt := range tests {
		got := sessionTitle(tt.input)
		if got != tt.want {
			t.Errorf("sessionTitle(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// Suppress unused import
var _ = json.Marshal
