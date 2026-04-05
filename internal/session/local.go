package session

import (
	"context"
	"fmt"
	"sync"

	"somegit.dev/Owlibou/gnoma/internal/engine"
	"somegit.dev/Owlibou/gnoma/internal/stream"
)

// Local implements Session using goroutines and channels within the same process.
type Local struct {
	mu sync.Mutex

	eng    *engine.Engine
	state  SessionState
	events chan stream.Event

	// Current turn context
	cancel context.CancelFunc
	turn   *engine.Turn
	err    error

	// Stats
	provider  string
	model     string
	turnCount int
}

// NewLocal creates a channel-based in-process session.
func NewLocal(eng *engine.Engine, providerName, model string) *Local {
	return &Local{
		eng:      eng,
		state:    StateIdle,
		provider: providerName,
		model:    model,
	}
}

func (s *Local) Send(input string) error {
	return s.SendWithOptions(input, engine.TurnOptions{})
}

// SendWithOptions is like Send but applies per-turn engine options.
func (s *Local) SendWithOptions(input string, opts engine.TurnOptions) error {
	s.mu.Lock()
	if s.state != StateIdle {
		s.mu.Unlock()
		return fmt.Errorf("session not idle (state: %s)", s.state)
	}

	s.state = StateStreaming
	s.events = make(chan stream.Event, 64)
	s.turn = nil
	s.err = nil

	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.turnCount++
	s.mu.Unlock()

	// Run engine in background goroutine
	go func() {
		cb := func(evt stream.Event) {
			select {
			case s.events <- evt:
			case <-ctx.Done():
			}
		}

		turn, err := s.eng.SubmitWithOptions(ctx, input, opts, cb)

		s.mu.Lock()
		s.turn = turn
		s.err = err
		if err != nil && ctx.Err() != nil {
			s.state = StateCancelled
		} else if err != nil {
			s.state = StateError
		} else {
			s.state = StateIdle
		}
		s.mu.Unlock()

		close(s.events)
	}()

	return nil
}

func (s *Local) Events() <-chan stream.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.events
}

func (s *Local) TurnResult() (*engine.Turn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.turn, s.err
}

func (s *Local) Cancel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *Local) Close() error {
	s.Cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = StateClosed
	return nil
}

// SetModel updates the displayed model name.
func (s *Local) SetModel(model string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.model = model
}

func (s *Local) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()

	st := Status{
		State:      s.state,
		Provider:   s.provider,
		Model:      s.model,
		TokensUsed: s.eng.Usage().TotalTokens(),
		TurnCount:  s.turnCount,
		TokenState: "ok",
	}

	if w := s.eng.ContextWindow(); w != nil {
		tr := w.Tracker()
		st.TokensMax = tr.MaxTokens()
		st.TokenPercent = tr.PercentUsed()
		st.TokenState = tr.State().String()
	}

	return st
}
