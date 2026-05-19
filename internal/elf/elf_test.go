package elf

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"somegit.dev/Owlibou/gnoma/internal/engine"
	"somegit.dev/Owlibou/gnoma/internal/message"
	"somegit.dev/Owlibou/gnoma/internal/provider"
	"somegit.dev/Owlibou/gnoma/internal/router"
	"somegit.dev/Owlibou/gnoma/internal/security"
	"somegit.dev/Owlibou/gnoma/internal/stream"
	"somegit.dev/Owlibou/gnoma/internal/tool"
)

// --- Mock Provider ---

type mockProvider struct {
	name    string
	calls   atomic.Int64
	streams []stream.Stream
}

func (m *mockProvider) Name() string         { return m.name }
func (m *mockProvider) DefaultModel() string  { return "mock" }
func (m *mockProvider) Models(_ context.Context) ([]provider.ModelInfo, error) { return nil, nil }
func (m *mockProvider) Stream(_ context.Context, _ provider.Request) (stream.Stream, error) {
	idx := m.calls.Add(1) - 1
	if int(idx) >= len(m.streams) {
		return nil, fmt.Errorf("no more streams")
	}
	return m.streams[idx], nil
}

type eventStream struct {
	events []stream.Event
	idx    int
}

func newEventStream(text string) *eventStream {
	return &eventStream{
		events: []stream.Event{
			{Type: stream.EventTextDelta, Text: text},
			{Type: stream.EventTextDelta, StopReason: message.StopEndTurn},
		},
	}
}

func (s *eventStream) Next() bool         { s.idx++; return s.idx <= len(s.events) }
func (s *eventStream) Current() stream.Event { return s.events[s.idx-1] }
func (s *eventStream) Err() error          { return nil }
func (s *eventStream) Close() error        { return nil }

// --- Tests ---

func TestBackgroundElf_RunsAndCompletes(t *testing.T) {
	mp := &mockProvider{
		name:    "test",
		streams: []stream.Stream{newEventStream("Hello from elf!")},
	}
	eng, _ := engine.New(engine.Config{Provider: mp, Tools: tool.NewRegistry()})

	elf := SpawnBackground(eng, "say hello")

	if elf.Status() != StatusRunning {
		t.Errorf("initial status = %s, want running", elf.Status())
	}

	result := elf.Wait()

	if result.Status != StatusCompleted {
		t.Errorf("result status = %s, want completed", result.Status)
	}
	if result.Output != "Hello from elf!" {
		t.Errorf("output = %q", result.Output)
	}
	if result.Duration <= 0 {
		t.Error("duration should be positive")
	}
	if elf.Status() != StatusCompleted {
		t.Errorf("final status = %s, want completed", elf.Status())
	}
}

func TestBackgroundElf_Cancel(t *testing.T) {
	// Stream that blocks
	slowStream := &slowEventStream{}
	mp := &mockProvider{
		name:    "test",
		streams: []stream.Stream{slowStream},
	}
	eng, _ := engine.New(engine.Config{Provider: mp, Tools: tool.NewRegistry()})

	elf := SpawnBackground(eng, "slow task")

	time.Sleep(10 * time.Millisecond)
	elf.Cancel()

	result := elf.Wait()
	if result.Status != StatusCancelled && result.Status != StatusFailed {
		t.Errorf("status = %s, want cancelled or failed", result.Status)
	}
}

func TestBackgroundElf_CollectEvents(t *testing.T) {
	mp := &mockProvider{
		name:    "test",
		streams: []stream.Stream{newEventStream("event test")},
	}
	eng, _ := engine.New(engine.Config{Provider: mp, Tools: tool.NewRegistry()})

	elf := SpawnBackground(eng, "generate events")

	var events []stream.Event
	for evt := range elf.Events() {
		events = append(events, evt)
	}

	if len(events) == 0 {
		t.Error("should receive events")
	}
}

func TestManager_SpawnAndList(t *testing.T) {
	mp := &mockProvider{
		name: "test",
		streams: []stream.Stream{
			newEventStream("elf 1"),
			newEventStream("elf 2"),
		},
	}

	rtr := router.New(router.Config{})
	rtr.RegisterArm(&router.Arm{
		ID:        "test/mock",
		Provider:  mp,
		ModelName: "mock",
		Capabilities: provider.Capabilities{ToolUse: true},
	})

	mgr := NewManager(ManagerConfig{
		Router: rtr,
		Tools:  tool.NewRegistry(),
	})

	// Spawn two elfs
	e1, err := mgr.Spawn(context.Background(), router.TaskGeneration, "task 1", "you are elf 1", 30)
	if err != nil {
		t.Fatalf("Spawn 1: %v", err)
	}

	e2, err := mgr.Spawn(context.Background(), router.TaskReview, "task 2", "you are elf 2", 30)
	if err != nil {
		t.Fatalf("Spawn 2: %v", err)
	}

	// List should have 2
	if len(mgr.List()) != 2 {
		t.Errorf("List() = %d, want 2", len(mgr.List()))
	}

	// Wait for both
	r1 := e1.Wait()
	r2 := e2.Wait()

	if r1.Status != StatusCompleted {
		t.Errorf("elf 1 status = %s", r1.Status)
	}
	if r2.Status != StatusCompleted {
		t.Errorf("elf 2 status = %s", r2.Status)
	}

	// Active should be 0
	if len(mgr.Active()) != 0 {
		t.Errorf("Active() = %d, want 0", len(mgr.Active()))
	}

	// Cleanup
	mgr.Cleanup()
	if len(mgr.List()) != 0 {
		t.Errorf("after cleanup, List() = %d", len(mgr.List()))
	}
}

func TestManager_WaitAll(t *testing.T) {
	mp := &mockProvider{
		name: "test",
		streams: []stream.Stream{
			newEventStream("result A"),
			newEventStream("result B"),
			newEventStream("result C"),
		},
	}

	rtr := router.New(router.Config{})
	rtr.RegisterArm(&router.Arm{
		ID: "test/mock", Provider: mp, ModelName: "mock",
		Capabilities: provider.Capabilities{ToolUse: true},
	})

	mgr := NewManager(ManagerConfig{Router: rtr, Tools: tool.NewRegistry()})

	_, _ = mgr.Spawn(context.Background(), router.TaskGeneration, "a", "", 30)
	_, _ = mgr.Spawn(context.Background(), router.TaskGeneration, "b", "", 30)
	_, _ = mgr.Spawn(context.Background(), router.TaskGeneration, "c", "", 30)

	results := mgr.WaitAll()
	if len(results) != 3 {
		t.Fatalf("WaitAll() = %d results, want 3", len(results))
	}

	completed := 0
	for _, r := range results {
		if r.Status == StatusCompleted {
			completed++
		}
	}
	if completed != 3 {
		t.Errorf("%d completed, want 3", completed)
	}
}

func TestBackgroundElf_WaitIdempotent(t *testing.T) {
	mp := &mockProvider{
		name:    "test",
		streams: []stream.Stream{newEventStream("hello")},
	}
	eng, _ := engine.New(engine.Config{Provider: mp, Tools: tool.NewRegistry()})
	elf := SpawnBackground(eng, "do something")

	r1 := elf.Wait()
	r2 := elf.Wait() // must not deadlock

	if r1.Status != r2.Status {
		t.Errorf("Wait() returned different statuses: %s vs %s", r1.Status, r2.Status)
	}
	if r1.Output != r2.Output {
		t.Errorf("Wait() returned different outputs: %q vs %q", r1.Output, r2.Output)
	}
}

func TestBackgroundElf_PanicRecovery(t *testing.T) {
	// A provider that panics on Stream() — simulates an engine crash
	panicProvider := &panicOnStreamProvider{}
	eng, _ := engine.New(engine.Config{Provider: panicProvider, Tools: tool.NewRegistry()})
	elf := SpawnBackground(eng, "do something")

	result := elf.Wait() // must not hang

	if result.Status != StatusFailed {
		t.Errorf("status = %s, want failed", result.Status)
	}
	if result.Error == nil {
		t.Error("error should be non-nil after panic recovery")
	}
}

type panicOnStreamProvider struct{}

func (p *panicOnStreamProvider) Name() string        { return "panic" }
func (p *panicOnStreamProvider) DefaultModel() string { return "panic" }
func (p *panicOnStreamProvider) Models(_ context.Context) ([]provider.ModelInfo, error) {
	return nil, nil
}
func (p *panicOnStreamProvider) Stream(_ context.Context, _ provider.Request) (stream.Stream, error) {
	panic("intentional test panic")
}

func TestManager_CleanupRemovesMeta(t *testing.T) {
	mp := &mockProvider{
		name:    "test",
		streams: []stream.Stream{newEventStream("result")},
	}

	rtr := router.New(router.Config{})
	rtr.RegisterArm(&router.Arm{
		ID: "test/mock", Provider: mp, ModelName: "mock",
		Capabilities: provider.Capabilities{ToolUse: true},
	})

	mgr := NewManager(ManagerConfig{Router: rtr, Tools: tool.NewRegistry()})
	e, _ := mgr.Spawn(context.Background(), router.TaskGeneration, "task", "", 30)
	e.Wait()

	// Before cleanup: elf and meta both present
	mgr.mu.RLock()
	_, elfExists := mgr.elfs[e.ID()]
	_, metaExists := mgr.meta[e.ID()]
	mgr.mu.RUnlock()

	if !elfExists || !metaExists {
		t.Fatal("elf and meta should exist before cleanup")
	}

	mgr.Cleanup()

	// After cleanup: both removed
	mgr.mu.RLock()
	_, elfExists = mgr.elfs[e.ID()]
	_, metaExists = mgr.meta[e.ID()]
	mgr.mu.RUnlock()

	if elfExists {
		t.Error("elf should be removed after cleanup")
	}
	if metaExists {
		t.Error("meta should be removed after cleanup (was leaking)")
	}
}

func TestManager_ReportResultSuppressedWhenIncognito(t *testing.T) {
	// Incognito sessions must not leave bandit signal behind, even for
	// background elf turns. ReportOutcome should be skipped; pool
	// reservations must still commit so capacity accounting stays sane.
	mp := &mockProvider{
		name:    "test",
		streams: []stream.Stream{newEventStream("result")},
	}

	rtr := router.New(router.Config{})
	armID := router.ArmID("test/mock")
	rtr.RegisterArm(&router.Arm{
		ID: armID, Provider: mp, ModelName: "mock",
		Capabilities: provider.Capabilities{ToolUse: true},
	})

	fw := security.NewFirewall(security.FirewallConfig{ScanOutgoing: true})
	fw.Incognito().Activate()

	mgr := NewManager(ManagerConfig{
		Router:   rtr,
		Tools:    tool.NewRegistry(),
		Firewall: fw,
	})

	e, err := mgr.Spawn(context.Background(), router.TaskGeneration, "task", "", 30)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	result := e.Wait()
	mgr.ReportResult(result)

	if _, hasData := rtr.QualityTracker().Quality(armID, router.TaskGeneration); hasData {
		t.Error("quality tracker received outcome despite incognito — gating not effective")
	}
}

// slowEventStream blocks until context cancelled
type slowEventStream struct {
	done bool
}

func (s *slowEventStream) Next() bool {
	if s.done {
		return false
	}
	time.Sleep(100 * time.Millisecond)
	return false
}
func (s *slowEventStream) Current() stream.Event { return stream.Event{} }
func (s *slowEventStream) Err() error            { return context.Canceled }
func (s *slowEventStream) Close() error          { s.done = true; return nil }
