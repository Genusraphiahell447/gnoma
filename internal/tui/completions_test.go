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
		{"/help", ""},          // already complete
		{"/cl", "/clear"},      // unambiguous prefix
		{"/co", ""},            // ambiguous: /compact, /config
		{"/com", "/compact"},
		{"/con", "/config"},
		{"/q", "/quit"},
		{"/model ", ""},        // has args — no command completion
		{"hello", ""},          // not a slash command
		{"/", ""},              // too short
		{"/x", ""},             // no match
	}

	for _, tt := range tests {
		got := matchCompletion(tt.input, cmds)
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
		{"help", "help", true},    // exact match
		{"HELP", "help", true},    // case insensitive
		{"xyz", "help", false},    // no match
		{"", "help", true},        // empty pattern matches everything
		{"hx", "help", false},     // x not present
		{"elp", "help", true},     // subsequence not at start
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
		query    string
		wantLen  int
		wantFirst string
	}{
		{"", 5, "/clear"},     // empty = all commands
		{"h", 1, "/help"},     // only /help contains h as subsequence
		{"hel", 1, "/help"},   // only /help
		{"mdl", 1, "/model"},  // subsequence match
		{"xyz", 0, ""},        // no match
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
		{"/permission auto", ""},              // already complete
		{"/permission d", "/permission default"}, // first match
		{"/perm b", "/perm bypass"},
		{"/perm p", "/perm plan"},
		{"/model foo", ""},                    // no arg completion for /model yet
	}

	for _, tt := range tests {
		got := matchArgCompletion(tt.input)
		if got != tt.want {
			t.Errorf("matchArgCompletion(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
