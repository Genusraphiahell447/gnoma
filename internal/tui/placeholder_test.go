package tui

import (
	"strings"
	"testing"
)

func TestExpandPlaceholders_BracketFormExpandsToStoredText(t *testing.T) {
	m := Model{
		pastedTexts: map[string]string{"#p1": "hello world"},
	}
	got := m.expandPlaceholders("see [Pasted text #p1 +0 lines] end")
	want := "see hello world end"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestExpandPlaceholders_RawFormExpandsToStoredText(t *testing.T) {
	m := Model{
		pastedTexts: map[string]string{"#p1": "hello"},
	}
	got := m.expandPlaceholders("ref #p1 here")
	want := "ref hello here"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestExpandPlaceholders_UnknownIDsAreLeftAlone(t *testing.T) {
	m := Model{
		pastedTexts: map[string]string{"#p1": "hello"},
	}
	got := m.expandPlaceholders("ref #p9 here")
	if got != "ref #p9 here" {
		t.Errorf("unknown id should be left intact, got %q", got)
	}
}

// Regression: the bug was that after the bracket form was inlined, a second
// pass scanned the resulting string for raw `#p\d+`. If the pasted content
// itself contained `#p2`, that token was silently corrupted into whatever
// `pastedTexts["#p2"]` mapped to (or stripped if absent).
func TestExpandPlaceholders_PastedContentContainingPlaceholderSyntaxSurvives(t *testing.T) {
	m := Model{
		pastedTexts: map[string]string{
			"#p1": "look at #p2 in this snippet",
			"#p2": "SHOULD_NOT_APPEAR",
		},
	}
	got := m.expandPlaceholders("here: [Pasted text #p1 +0 lines]")
	want := "here: look at #p2 in this snippet"
	if got != want {
		t.Errorf("pasted content was re-expanded:\n got  %q\n want %q", got, want)
	}
	if strings.Contains(got, "SHOULD_NOT_APPEAR") {
		t.Error("nested #p2 inside pasted content was wrongly expanded")
	}
}

func TestExpandPlaceholders_ImageBracketFormExpandsToPath(t *testing.T) {
	m := Model{
		pastedImages: map[string]string{"#img1": "/tmp/x.png"},
	}
	got := m.expandPlaceholders("see [Pasted image #img1] end")
	want := "see [Image: /tmp/x.png] end"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestExpandPlaceholders_MultiplePlaceholdersInOneInput(t *testing.T) {
	m := Model{
		pastedTexts:  map[string]string{"#p1": "AAA", "#p2": "BBB"},
		pastedImages: map[string]string{"#img1": "/tmp/x.png"},
	}
	got := m.expandPlaceholders("[Pasted text #p1 +0 lines] then #p2 then [Pasted image #img1]")
	want := "AAA then BBB then [Image: /tmp/x.png]"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
