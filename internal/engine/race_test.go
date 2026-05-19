package engine

import (
	"context"
	"sync"
	"testing"

	"somegit.dev/Owlibou/gnoma/internal/message"
	"somegit.dev/Owlibou/gnoma/internal/stream"
	"somegit.dev/Owlibou/gnoma/internal/tool"
)

// blockingStream emits one text delta, then blocks Next() until release is closed,
// then emits the stop event. Lets a test interleave Submit with concurrent setters.
type blockingStream struct {
	release    chan struct{}
	emitted    bool
	released   bool
	stopReason message.StopReason
	model      string
}

func newBlockingStream(release chan struct{}, model string) *blockingStream {
	return &blockingStream{release: release, model: model, stopReason: message.StopEndTurn}
}

func (s *blockingStream) Next() bool {
	if !s.emitted {
		s.emitted = true
		return true
	}
	if !s.released {
		<-s.release
		s.released = true
		return true
	}
	return false
}

func (s *blockingStream) Current() stream.Event {
	if s.released {
		return stream.Event{Type: stream.EventTextDelta, StopReason: s.stopReason, Model: s.model}
	}
	return stream.Event{Type: stream.EventTextDelta, Text: "hi", Model: s.model}
}

func (s *blockingStream) Err() error   { return nil }
func (s *blockingStream) Close() error { return nil }

func TestEngine_ConcurrentSubmitAndSetters(t *testing.T) {
	release := make(chan struct{})
	mp := &mockProvider{
		name:    "test",
		streams: []stream.Stream{newBlockingStream(release, "mock-model")},
	}
	e, _ := New(Config{Provider: mp, Tools: tool.NewRegistry()})

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		_, _ = e.Submit(context.Background(), "go", nil)
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			e.InjectMessage(message.NewUserText("noise"))
			_ = e.History()
			_ = e.Usage()
		}
		close(release)
	}()

	wg.Wait()
}
