package stream

// Stream is the unified pull-based iterator over provider events.
// All provider implementations produce this interface.
//
// Usage:
//
//	for s.Next() {
//	    event := s.Current()
//	    process(event)
//	}
//	if err := s.Err(); err != nil {
//	    handle(err)
//	}
//	s.Close()
type Stream interface {
	// Next advances to the next event. Returns false when exhausted or errored.
	Next() bool
	// Current returns the most recent event. Only valid after Next() returns true.
	Current() Event
	// Err returns the first error encountered, or nil.
	Err() error
	// Close releases resources. Must be called after consumption.
	Close() error
}
