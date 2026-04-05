package session

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"somegit.dev/Owlibou/gnoma/internal/engine"
	"somegit.dev/Owlibou/gnoma/internal/security"
	"somegit.dev/Owlibou/gnoma/internal/stream"
)

// LocalConfig holds all configuration for a Local session.
type LocalConfig struct {
	Engine    *engine.Engine
	Provider  string
	Model     string
	SessionID string                  // identifies this session on disk
	Store     *SessionStore           // nil = no persistence
	Incognito *security.IncognitoMode // nil = always persist
	Logger    *slog.Logger            // nil = slog.Default()
}

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

	// Persistence
	sessionID string
	store     *SessionStore
	incognito *security.IncognitoMode
	createdAt time.Time
	logger    *slog.Logger
}

// NewLocal creates a channel-based in-process session.
func NewLocal(cfg LocalConfig) *Local {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Local{
		eng:       cfg.Engine,
		state:     StateIdle,
		provider:  cfg.Provider,
		model:     cfg.Model,
		sessionID: cfg.SessionID,
		store:     cfg.Store,
		incognito: cfg.Incognito,
		createdAt: time.Now(),
		logger:    logger,
	}
}

// SessionID returns the persistent identifier for this session.
func (s *Local) SessionID() string {
	return s.sessionID
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
		var finalState SessionState
		if err != nil && ctx.Err() != nil {
			s.state = StateCancelled
			finalState = StateCancelled
		} else if err != nil {
			s.state = StateError
			finalState = StateError
		} else {
			s.state = StateIdle
			finalState = StateIdle
		}
		s.mu.Unlock()

		// Auto-save after successful turn (outside lock to avoid holding it during I/O)
		if finalState == StateIdle && s.store != nil && (s.incognito == nil || s.incognito.ShouldPersist()) {
			snap := Snapshot{
				ID: s.sessionID,
				Metadata: Metadata{
					ID:           s.sessionID,
					Provider:     s.provider,
					Model:        s.model,
					TurnCount:    s.turnCount,
					Usage:        s.eng.Usage(),
					CreatedAt:    s.createdAt,
					UpdatedAt:    time.Now(),
					MessageCount: len(s.eng.History()),
				},
				Messages: s.eng.History(),
			}
			if saveErr := s.store.Save(snap); saveErr != nil {
				s.logger.Warn("session auto-save failed", "error", saveErr)
			}
		}

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
