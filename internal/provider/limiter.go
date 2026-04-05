package provider

import (
	"context"
	"sync"

	"somegit.dev/Owlibou/gnoma/internal/stream"
)

// ConcurrentProvider wraps a Provider with a shared semaphore that limits the
// number of in-flight Stream calls. All engines sharing the same
// ConcurrentProvider instance share the same concurrency budget.
type ConcurrentProvider struct {
	Provider
	sem chan struct{}
}

// WithConcurrency wraps p so that at most max Stream calls can be in-flight
// simultaneously. If max <= 0, p is returned unwrapped.
func WithConcurrency(p Provider, max int) Provider {
	if max <= 0 {
		return p
	}
	sem := make(chan struct{}, max)
	for range max {
		sem <- struct{}{}
	}
	return &ConcurrentProvider{Provider: p, sem: sem}
}

// Stream acquires a concurrency slot, calls the inner provider, and returns a
// stream that releases the slot when Close is called.
func (cp *ConcurrentProvider) Stream(ctx context.Context, req Request) (stream.Stream, error) {
	select {
	case <-cp.sem:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	s, err := cp.Provider.Stream(ctx, req)
	if err != nil {
		cp.sem <- struct{}{}
		return nil, err
	}
	return &semStream{Stream: s, release: func() { cp.sem <- struct{}{} }}, nil
}

// semStream wraps a stream.Stream to release a semaphore slot on Close.
type semStream struct {
	stream.Stream
	release func()
	once    sync.Once
}

func (s *semStream) Close() error {
	s.once.Do(s.release)
	return s.Stream.Close()
}
