package tui

import (
	"testing"
)

func TestMatchCompletion(t *testing.T) {
	cmds := []cmdEntry{
		{"/clear", "clear history"},
		{"/compact", "compact context"},
		{"/config", "settings"},
		{"/help", "show help"},
		{"/model", "switch model"},
		{"/permission", "set permission"},
		{"/quit", "quit"},
	}

	tests := []struct {
		input string
		want  string
	}{
		{"/h", "/help"},
		{"/he", "/help"},
		{"/help", ""},     // already complete
		{"/cl", "/clear"}, // unambiguous prefix
		{"/co", ""},       // ambiguous: /compact, /config
		{"/com", "/compact"},
		{"/con", "/config"},
		{"/q", "/quit"},
		{"/model ", ""}, // has args — no command completion
		{"hello", ""},   // not a slash command
		{"/", ""},       // too short
		{"/x", ""},      // no match
	}

	for _, tt := range tests {
		got := matchCompletion(tt.input, cmds, nil, nil)
		if got != tt.want {
			t.Errorf("matchCompletion(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestFuzzyMatch(t *testing.T) {
	tests := []struct {
		pattern string
		text    string
		want    bool
	}{
		{"hlp", "help", true},
		{"clr", "clear", true},
		{"mdl", "model", true},
		{"help", "help", true}, // exact match
		{"HELP", "help", true}, // case insensitive
		{"xyz", "help", false}, // no match
		{"", "help", true},     // empty pattern matches everything
		{"hx", "help", false},  // x not present
		{"elp", "help", true},  // subsequence not at start
	}

	for _, tt := range tests {
		got := fuzzyMatch(tt.pattern, tt.text)
		if got != tt.want {
			t.Errorf("fuzzyMatch(%q, %q) = %v, want %v", tt.pattern, tt.text, got, tt.want)
		}
	}
}

func TestFuzzyMatchCommands(t *testing.T) {
	cmds := []cmdEntry{
		{"/clear", "clear history"},
		{"/compact", "compact context"},
		{"/config", "settings"},
		{"/help", "show help"},
		{"/model", "switch model"},
	}

	tests := []struct {
		query     string
		wantLen   int
		wantFirst string
	}{
		{"", 5, "/clear"},    // empty = all commands
		{"h", 1, "/help"},    // only /help contains h as subsequence
		{"hel", 1, "/help"},  // only /help
		{"mdl", 1, "/model"}, // subsequence match
		{"xyz", 0, ""},       // no match
	}

	for _, tt := range tests {
		got := fuzzyMatchCommands(tt.query, cmds)
		if len(got) != tt.wantLen {
			t.Errorf("fuzzyMatchCommands(%q): got %d results, want %d (got: %v)", tt.query, len(got), tt.wantLen, got)
		}
		if tt.wantFirst != "" && len(got) > 0 && got[0].name != tt.wantFirst {
			t.Errorf("fuzzyMatchCommands(%q): first result = %q, want %q", tt.query, got[0].name, tt.wantFirst)
		}
	}
}

func TestMatchArgCompletion(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"/permission a", "/permission auto"},
		{"/permission au", "/permission auto"},
		{"/permission auto", ""},                 // already complete
		{"/permission d", "/permission default"}, // first match
		{"/perm b", "/perm bypass"},
		{"/perm p", "/perm plan"},
		{"/model foo", ""}, // no arg completion for /model yet
	}

	for _, tt := range tests {
		got := matchArgCompletion(tt.input, nil, nil)
		if got != tt.want {
			t.Errorf("matchArgCompletion(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestMatchArgCompletion_Profile(t *testing.T) {
	profiles := []string{"experiment", "private", "work"}
	tests := []struct {
		input string
		want  string
	}{
		{"/profile w", "/profile work"},
		{"/profile p", "/profile private"},
		{"/profile work", ""}, // already complete
		{"/profile e", "/profile experiment"},
		{"/profile z", ""}, // no match
		{"/profile ", ""},  // empty arg — wait for input
	}
	for _, tt := range tests {
		got := matchArgCompletion(tt.input, profiles, nil)
		if got != tt.want {
			t.Errorf("matchArgCompletion(%q, profiles) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestMatchCompletion_DispatchesToProfileArgCompletion(t *testing.T) {
	// End-to-end: matchCompletion sees "/profile w", forwards to
	// matchArgCompletion with profileNames, gets back "/profile work".
	cmds := []cmdEntry{{"/profile", "profiles"}}
	got := matchCompletion("/profile w", cmds, []string{"work", "private"}, nil)
	if got != "/profile work" {
		t.Errorf("matchCompletion(/profile w) = %q, want /profile work", got)
	}
}

func TestMatchArgCompletion_ProfileNoNamesAvailable(t *testing.T) {
	// When profile mode isn't engaged, profileNames is nil/empty and the
	// completer must not try to suggest anything.
	got := matchArgCompletion("/profile w", nil, nil)
	if got != "" {
		t.Errorf("matchArgCompletion(profile, nil) = %q, want empty", got)
	}
}

func TestMatchArgCompletion_Provider(t *testing.T) {
	providers := []string{"anthropic", "openai", "google"}
	tests := []struct {
		input string
		want  string
	}{
		{"/provider a", "/provider anthropic"},
		{"/provider o", "/provider openai"},
		{"/provider openai", ""}, // already complete
		{"/provider g", "/provider google"},
		{"/provider z", ""}, // no match
		{"/provider ", ""},  // empty arg — wait for input
	}
	for _, tt := range tests {
		got := matchArgCompletion(tt.input, nil, providers)
		if got != tt.want {
			t.Errorf("matchArgCompletion(%q, providers) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestMatchCompletion_DispatchesToProviderArgCompletion(t *testing.T) {
	cmds := []cmdEntry{{"/provider", "providers"}}
	got := matchCompletion("/provider a", cmds, nil, []string{"anthropic", "openai"})
	if got != "/provider anthropic" {
		t.Errorf("matchCompletion(/provider a) = %q, want /provider anthropic", got)
	}
}
