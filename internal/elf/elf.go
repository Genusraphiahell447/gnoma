package elf

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"somegit.dev/Owlibou/gnoma/internal/engine"
	"somegit.dev/Owlibou/gnoma/internal/message"
	"somegit.dev/Owlibou/gnoma/internal/stream"
)

// Status tracks the lifecycle of an elf.
type Status int

const (
	StatusPending   Status = iota
	StatusRunning
	StatusCompleted
	StatusFailed
	StatusCancelled
)

func (s Status) String() string {
	switch s {
	case StatusPending:
		return "pending"
	case StatusRunning:
		return "running"
	case StatusCompleted:
		return "completed"
	case StatusFailed:
		return "failed"
	case StatusCancelled:
		return "cancelled"
	default:
		return "unknown"
	}
}

// Result is the output of a completed elf.
type Result struct {
	ID              string
	Status          Status
	Messages        []message.Message
	Usage           message.Usage
	Output          string // final text output
	Error           error
	Duration        time.Duration
	ResultFilePaths []string // paths to /tmp results produced by this elf's tools
}

// Elf is a sub-agent with its own engine and conversation history.
type Elf interface {
	// ID returns the unique elf identifier.
	ID() string
	// Status returns the current lifecycle status.
	Status() Status
	// Events returns a channel for streaming events (nil for sync elfs).
	Events() <-chan stream.Event
	// Wait blocks until the elf completes and returns its result.
	Wait() Result
	// Cancel aborts the elf.
	Cancel()
}

var elfCounter atomic.Int64

func nextID(prefix string) string {
	n := elfCounter.Add(1)
	return fmt.Sprintf("%s-%d", prefix, n)
}

// BackgroundElf runs on its own goroutine with an independent engine.
type BackgroundElf struct {
	id           string
	eng          *engine.Engine
	events       chan stream.Event
	result       chan Result
	cancel       context.CancelFunc
	status       atomic.Int32
	startAt      time.Time
	cachedResult Result
	resultOnce   sync.Once
	eventsClose  sync.Once
}

// SpawnBackground creates and starts a background elf.
func SpawnBackground(eng *engine.Engine, prompt string) *BackgroundElf {
	ctx, cancel := context.WithCancel(context.Background())

	elf := &BackgroundElf{
		id:      nextID("elf"),
		eng:     eng,
		events:  make(chan stream.Event, 64),
		result:  make(chan Result, 1),
		cancel:  cancel,
		startAt: time.Now(),
	}
	elf.status.Store(int32(StatusRunning))

	go elf.run(ctx, prompt)

	return elf
}

func (e *BackgroundElf) run(ctx context.Context, prompt string) {
	closeEvents := func() { e.eventsClose.Do(func() { close(e.events) }) }

	defer func() {
		if r := recover(); r != nil {
			closeEvents()
			res := Result{
				ID:       e.id,
				Status:   StatusFailed,
				Error:    fmt.Errorf("elf panicked: %v", r),
				Duration: time.Since(e.startAt),
			}
			e.status.Store(int32(StatusFailed))
			e.result <- res
		}
	}()

	cb := func(evt stream.Event) {
		select {
		case e.events <- evt:
		case <-ctx.Done():
		}
	}

	turn, err := e.eng.Submit(ctx, prompt, cb)

	closeEvents()

	r := Result{
		ID:       e.id,
		Duration: time.Since(e.startAt),
	}

	if ctx.Err() != nil {
		r.Status = StatusCancelled
		r.Error = ctx.Err()
		e.status.Store(int32(StatusCancelled))
	} else if err != nil {
		r.Status = StatusFailed
		r.Error = err
		e.status.Store(int32(StatusFailed))
	} else {
		r.Status = StatusCompleted
		r.Messages = turn.Messages
		r.Usage = turn.Usage
		// Extract final text from last assistant message
		for i := len(turn.Messages) - 1; i >= 0; i-- {
			if turn.Messages[i].Role == message.RoleAssistant {
				r.Output = turn.Messages[i].TextContent()
				break
			}
		}
		e.status.Store(int32(StatusCompleted))
	}

	e.result <- r
}

func (e *BackgroundElf) ID() string          { return e.id }
func (e *BackgroundElf) Status() Status      { return Status(e.status.Load()) }
func (e *BackgroundElf) Events() <-chan stream.Event { return e.events }
func (e *BackgroundElf) Cancel()             { e.cancel() }

func (e *BackgroundElf) Wait() Result {
	e.resultOnce.Do(func() {
		e.cachedResult = <-e.result
	})
	return e.cachedResult
}
