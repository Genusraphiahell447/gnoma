package tui

import "testing"

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
