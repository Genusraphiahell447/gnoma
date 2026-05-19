package tui

import (
	"sort"
	"strings"

	"somegit.dev/Owlibou/gnoma/internal/skill"
)

// cmdEntry is a slash command with a short description.
type cmdEntry struct {
	name string
	desc string
}

// builtinCommands is the static list of slash commands with descriptions.
var builtinCommands = []cmdEntry{
	{"/clear", "clear conversation history"},
	{"/compact", "summarize and compact conversation context"},
	{"/config", "open settings panel"},
	{"/exit", "exit gnoma"},
	{"/help", "show available commands and shortcuts"},
	{"/incognito", "toggle incognito mode (no persistence, local-only routing)"},
	{"/init", "initialize project — create AGENTS.md"},
	{"/keys", "show keyboard shortcuts"},
	{"/model", "list or switch active model"},
	{"/new", "start a new conversation"},
	{"/perm", "show or set permission mode"},
	{"/permission", "show or set permission mode"},
	{"/plugins", "list installed plugins"},
	{"/provider", "list or switch provider"},
	{"/quit", "quit gnoma"},
	{"/replay", "replay last assistant response"},
	{"/resume", "browse and resume a saved session"},
	{"/shell", "open interactive shell"},
	{"/skills", "list available skills"},
	{"/usage", "show token usage for this session"},
}

// permissionModes lists valid modes for /permission completion.
var permissionModes = []string{
	"auto", "default", "accept_edits", "bypass", "deny", "plan",
}

// completionSource builds a sorted command list from builtins + skills.
func completionSource(skills *skill.Registry) []cmdEntry {
	entries := make([]cmdEntry, len(builtinCommands))
	copy(entries, builtinCommands)

	if skills != nil {
		for _, s := range skills.All() {
			desc := s.Frontmatter.Description
			if desc == "" {
				desc = "skill"
			}
			entries = append(entries, cmdEntry{"/" + s.Frontmatter.Name, desc})
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].name < entries[j].name
	})
	return entries
}

// matchSuggestions returns all commands whose name has the given prefix.
// Returns nil if input is empty, doesn't start with '/', or contains a space.
func matchSuggestions(input string, commands []cmdEntry) []cmdEntry {
	if !strings.HasPrefix(input, "/") || len(input) < 2 || strings.Contains(input, " ") {
		return nil
	}
	lower := strings.ToLower(input)
	var matches []cmdEntry
	for _, c := range commands {
		if strings.HasPrefix(c.name, lower) {
			matches = append(matches, c)
		}
	}
	return matches
}

// matchCompletion returns the unique ghost-text completion, or "".
// Used for Tab acceptance of a single unambiguous match.
func matchCompletion(input string, commands []cmdEntry) string {
	if !strings.HasPrefix(input, "/") || len(input) < 2 {
		return ""
	}
	if strings.Contains(input, " ") {
		return matchArgCompletion(input)
	}
	suggestions := matchSuggestions(input, commands)
	if len(suggestions) == 1 && suggestions[0].name != input {
		return suggestions[0].name
	}
	return ""
}

// fuzzyMatch returns true if every rune in pattern appears in text in order.
func fuzzyMatch(pattern, text string) bool {
	text = strings.ToLower(text)
	pattern = strings.ToLower(pattern)
	pi := 0
	for _, ch := range text {
		if pi < len(pattern) && rune(pattern[pi]) == ch {
			pi++
		}
	}
	return pi == len(pattern)
}

// fuzzyMatchCommands filters commands whose name (without leading "/") fuzzy-matches query.
func fuzzyMatchCommands(query string, commands []cmdEntry) []cmdEntry {
	if query == "" {
		return commands
	}
	var matches []cmdEntry
	for _, c := range commands {
		name := strings.TrimPrefix(c.name, "/")
		if fuzzyMatch(query, name) {
			matches = append(matches, c)
		}
	}
	return matches
}

// matchArgCompletion handles second-level completion for commands with args.
func matchArgCompletion(input string) string {
	parts := strings.SplitN(input, " ", 2)
	if len(parts) != 2 {
		return ""
	}
	cmd := parts[0]
	arg := parts[1]

	switch cmd {
	case "/permission", "/perm":
		if arg == "" {
			return ""
		}
		lower := strings.ToLower(arg)
		for _, mode := range permissionModes {
			if strings.HasPrefix(mode, lower) && mode != arg {
				return cmd + " " + mode
			}
		}
	}
	return ""
}
