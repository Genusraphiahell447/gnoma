package engine

import "somegit.dev/Owlibou/gnoma/internal/stream"

// Callback receives streaming events for real-time UI updates.
// Called synchronously on the engine goroutine for each event.
type Callback func(stream.Event)
