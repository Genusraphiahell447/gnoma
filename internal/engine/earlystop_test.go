package engine

import (
	"strings"
	"testing"
)

func TestRepetitionDetector_NoRepetition(t *testing.T) {
	d := NewRepetitionDetector()
	chunks := []string{
		"Let me think about this step by step. ",
		"First, I need to read the file. ",
		"Then I will identify the section that needs to change. ",
		"After that, I can apply the edit and verify the result. ",
	}
	for i, c := range chunks {
		if d.Feed(c) {
			t.Fatalf("unexpected repetition at chunk %d (%q)", i, c)
		}
	}
}

func TestRepetitionDetector_TriggersOnRepeatedPattern(t *testing.T) {
	d := NewRepetitionDetector()
	// 60-char phrase repeated 5x → 300 chars total. Falls inside the
	// 200-char window with multiple repeats.
	phrase := "I need to read the file and then apply the edit carefully. "
	triggered := false
	for range 5 {
		if d.Feed(phrase) {
			triggered = true
			break
		}
	}
	if !triggered {
		t.Fatal("expected repetition detection on repeated phrase")
	}
}

func TestRepetitionDetector_DoesNotTriggerOnNaturalText(t *testing.T) {
	d := NewRepetitionDetector()
	// Natural code-review-style text with repeated bigrams but distinct sentences.
	text := strings.Repeat(
		"The function returns an error when the path is invalid or unreadable. ",
		1) +
		"It accepts a context and a byte slice as arguments. " +
		"The implementation walks the slice and validates each entry. " +
		"On success it produces a structured result containing the parsed value. " +
		"This avoids reallocating the underlying buffer on each call. " +
		"Tests cover the happy path and the malformed input cases. " +
		"Future work may add streaming support for very large inputs."
	if d.Feed(text) {
		t.Fatal("false positive on natural prose")
	}
}

func TestRepetitionDetector_Reset(t *testing.T) {
	d := NewRepetitionDetector()
	phrase := "loop loop loop loop loop loop loop loop loop loop loop loop "
	for range 5 {
		d.Feed(phrase)
	}
	d.Reset()
	// After reset, a small amount of new text must not trigger.
	if d.Feed("hello world") {
		t.Fatal("detector triggered immediately after reset on fresh input")
	}
}

func TestPatchFailureTracker_TriggersAtThreshold(t *testing.T) {
	tr := NewPatchFailureTracker()
	for i := range 3 {
		if tr.RecordFailure("/foo.go") {
			t.Fatalf("triggered too early at attempt %d", i+1)
		}
	}
	if !tr.RecordFailure("/foo.go") {
		t.Fatal("should trigger on 4th failure")
	}
}

func TestPatchFailureTracker_TriggerResetsPath(t *testing.T) {
	tr := NewPatchFailureTracker()
	for range 4 {
		tr.RecordFailure("/foo.go")
	}
	// Subsequent failure on same path starts fresh (we already escalated).
	if tr.RecordFailure("/foo.go") {
		t.Fatal("should not re-trigger immediately after escalation")
	}
}

func TestPatchFailureTracker_SuccessDecrements(t *testing.T) {
	tr := NewPatchFailureTracker()
	tr.RecordFailure("/foo.go")
	tr.RecordFailure("/foo.go")
	tr.RecordSuccess("/foo.go") // back to 1
	if tr.RecordFailure("/foo.go") {
		t.Fatal("triggered at 2 after decrement")
	}
	if tr.RecordFailure("/foo.go") {
		t.Fatal("triggered at 3 after decrement")
	}
	if !tr.RecordFailure("/foo.go") {
		t.Fatal("should trigger at 4")
	}
}

func TestPatchFailureTracker_PerPathIsolation(t *testing.T) {
	tr := NewPatchFailureTracker()
	for range 4 {
		tr.RecordFailure("/foo.go")
	}
	// /bar.go must be unaffected.
	for i := range 3 {
		if tr.RecordFailure("/bar.go") {
			t.Fatalf("/bar.go triggered too early at attempt %d", i+1)
		}
	}
	if !tr.RecordFailure("/bar.go") {
		t.Fatal("/bar.go should trigger at 4")
	}
}

func TestPatchFailureTracker_Reset(t *testing.T) {
	tr := NewPatchFailureTracker()
	tr.RecordFailure("/foo.go")
	tr.RecordFailure("/foo.go")
	tr.RecordFailure("/foo.go")
	tr.Reset()
	for i := range 3 {
		if tr.RecordFailure("/foo.go") {
			t.Fatalf("triggered too early after Reset at attempt %d", i+1)
		}
	}
}

func TestDetectGreeting(t *testing.T) {
	cases := []struct {
		name string
		text string
		want bool
	}{
		{"how can I help", "How can I help you today?", true},
		{"what would you like", "What would you like to do?", true},
		{"hello ready", "Hello! I'm ready to assist.", true},
		{"hi there", "Hi there! What can I do for you?", true},
		{"task progress", "I've updated the file as requested.", false},
		{"code reference", "The function in foo.go returns an error.", false},
		{"empty", "", false},
		{"single word", "Done.", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DetectGreeting(tc.text); got != tc.want {
				t.Fatalf("DetectGreeting(%q) = %v, want %v", tc.text, got, tc.want)
			}
		})
	}
}

func TestPatchSpiralInjection_NamesPath(t *testing.T) {
	msg := PatchSpiralInjection("/work/foo.go")
	if !strings.Contains(msg, "/work/foo.go") {
		t.Fatalf("injection missing path: %q", msg)
	}
	if !strings.Contains(strings.ToLower(msg), "fs.write") {
		t.Fatalf("injection should steer toward fs.write rewrite: %q", msg)
	}
}
