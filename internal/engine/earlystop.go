package engine

import (
	"fmt"
	"strings"
)

// Default tuning for the early-stop detectors. These mirror the values used
// in smallcode's reference implementation, adjusted for our streaming shape.
const (
	defaultRepetitionWindow    = 200 // last N chars of stream we inspect
	defaultRepetitionThreshold = 3   // pattern must repeat ≥ this many times
	defaultMaxPatchFailures    = 4   // consecutive failures on a path → escalate
)

var defaultRepetitionSizes = []int{50, 80, 120}

// RepetitionDetector watches a stream's text deltas for a fixed-size pattern
// that recurs ≥ threshold times within the trailing window. Detects the
// "model lost the plot and is now repeating itself" failure mode.
//
// Single-goroutine use only — the loop drives it from the stream consume path.
type RepetitionDetector struct {
	windowChars int
	threshold   int
	sizes       []int
	buf         strings.Builder
}

func NewRepetitionDetector() *RepetitionDetector {
	return &RepetitionDetector{
		windowChars: defaultRepetitionWindow,
		threshold:   defaultRepetitionThreshold,
		sizes:       defaultRepetitionSizes,
	}
}

// Feed appends streamed text to the buffer and returns true when a repetition
// pattern is detected. Once triggered, the caller is expected to act on the
// signal and call Reset before reusing the detector.
func (d *RepetitionDetector) Feed(text string) bool {
	if text == "" {
		return false
	}
	d.buf.WriteString(text)

	// Trim the buffer to bound memory. Keep twice the window so we always
	// have a stable trailing slice to scan.
	if d.buf.Len() > d.windowChars*4 {
		s := d.buf.String()
		keep := s[len(s)-d.windowChars*2:]
		d.buf.Reset()
		d.buf.WriteString(keep)
	}

	s := d.buf.String()
	// We need at least one window's worth of data for the smallest pattern
	// to recur threshold times.
	if len(s) < d.sizes[0]*d.threshold {
		return false
	}
	tail := s
	if len(tail) > d.windowChars {
		tail = tail[len(tail)-d.windowChars:]
	}

	for _, size := range d.sizes {
		if len(tail) < size*d.threshold {
			continue
		}
		pattern := tail[:size]
		count := 0
		for i := 0; i+size <= len(tail); {
			if tail[i:i+size] == pattern {
				count++
				if count >= d.threshold {
					return true
				}
				i += size
				continue
			}
			i++
		}
	}
	return false
}

// Reset clears the accumulated buffer. Call at the start of a new turn.
func (d *RepetitionDetector) Reset() {
	d.buf.Reset()
}

// PatchFailureTracker counts consecutive write/edit failures per file path
// within a turn. Triggers when a single path crosses the configured threshold,
// at which point the loop should steer the model away from further patches
// against that path.
type PatchFailureTracker struct {
	maxFailures int
	failures    map[string]int
}

func NewPatchFailureTracker() *PatchFailureTracker {
	return &PatchFailureTracker{
		maxFailures: defaultMaxPatchFailures,
		failures:    make(map[string]int),
	}
}

// RecordFailure increments the failure count for path and returns true when
// the threshold has just been reached. After triggering, the path's counter
// is reset so subsequent failures don't re-fire the signal until they
// re-accumulate.
func (t *PatchFailureTracker) RecordFailure(path string) bool {
	if path == "" {
		return false
	}
	t.failures[path]++
	if t.failures[path] >= t.maxFailures {
		delete(t.failures, path)
		return true
	}
	return false
}

// RecordSuccess decrements the failure count for path with a floor of 0.
// A run of successful edits should let the path recover, but we don't fully
// reset on a single success — a path that fails three times then succeeds
// once is still a suspicious target.
func (t *PatchFailureTracker) RecordSuccess(path string) {
	if path == "" {
		return
	}
	if n := t.failures[path]; n > 0 {
		t.failures[path] = n - 1
		if t.failures[path] == 0 {
			delete(t.failures, path)
		}
	}
}

// Reset clears all per-path counters. Call at the start of a new turn.
func (t *PatchFailureTracker) Reset() {
	t.failures = make(map[string]int)
}

// greetingMarkers are case-folded substrings that indicate the model has
// dropped its task context and reverted to an opening-of-conversation reply.
// Kept deliberately narrow — we only want to fire on responses that look
// like the start of a new chat, not on any polite phrasing.
var greetingMarkers = []string{
	"how can i help",
	"how can i assist",
	"what would you like",
	"what can i do for you",
	"i'm ready to",
	"hi there",
}

// DetectGreeting reports whether text looks like a greeting/reset response.
// Stateless. The loop should only consult this after a round that contained
// tool calls — a greeting at the start of a turn is fine.
func DetectGreeting(text string) bool {
	if len(text) < 10 {
		return false
	}
	lc := strings.ToLower(text)
	for _, m := range greetingMarkers {
		if strings.Contains(lc, m) {
			return true
		}
	}
	return false
}

// Corrective injections returned to the model when a detector fires. These
// are appended as user messages before the next round so the model sees a
// concrete instruction rather than a system reset.

// RepetitionInjection is the corrective message used when the repetition
// detector fires.
func RepetitionInjection() string {
	return "[system] Your output is repeating itself in a loop. Stop. " +
		"Take a different approach, or state explicitly what is blocking you " +
		"and why the current strategy is not converging."
}

// PatchSpiralInjection is the corrective message used when a single file
// has accumulated too many failed fs.edit attempts. Steers the model toward
// fs.write rather than another patch.
func PatchSpiralInjection(path string) string {
	return fmt.Sprintf(
		"[system] You have failed to edit %s several times. Stop using fs.edit "+
			"on this file. Instead: 1) read the current file with fs.read, "+
			"2) decide what the file should contain in full, "+
			"3) rewrite it with fs.write. Do not attempt another fs.edit on %s.",
		path, path)
}

// GreetingInjection is the corrective message used when the model emits a
// greeting mid-task (context loss).
func GreetingInjection() string {
	return "[system] You produced a greeting instead of continuing the task. " +
		"Look at the conversation above — there is work in progress. " +
		"Resume where you left off. Do not restart the conversation."
}
