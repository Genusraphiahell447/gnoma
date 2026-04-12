package tui

import (
	"sort"
	"strings"

	"somegit.dev/Owlibou/gnoma/internal/skill"
)

// builtinCommands is the static list of slash commands.
var builtinCommands = []string{
	"/clear",
	"/compact",
	"/config",
	"/exit",
	"/help",
	"/incognito",
	"/init",
	"/model",
	"/new",
	"/perm",
	"/permission",
	"/plugins",
	"/provider",
	"/quit",
	"/resume",
	"/skills",
	"/usage",
}

// permissionModes lists valid modes for /permission completion.
var permissionModes = []string{
	"auto", "default", "accept_edits", "bypass", "deny", "plan",
}

// completionSource builds a sorted command list from builtins + skills.
func completionSource(skills *skill.Registry) []string {
	cmds := make([]string, len(builtinCommands))
	copy(cmds, builtinCommands)

	if skills != nil {
		for _, s := range skills.All() {
			cmds = append(cmds, "/"+s.Frontmatter.Name)
		}
	}
	sort.Strings(cmds)
	return cmds
}

// matchCompletion finds the best completion for the current input.
// Returns the full command string if a unique prefix match exists, or empty string.
func matchCompletion(input string, commands []string) string {
	if !strings.HasPrefix(input, "/") || len(input) < 2 {
		return ""
	}

	// Don't complete if there are args (space after command).
	if strings.Contains(input, " ") {
		return matchArgCompletion(input)
	}

	lower := strings.ToLower(input)
	var match string
	for _, cmd := range commands {
		if strings.HasPrefix(cmd, lower) {
			if match != "" {
				return "" // ambiguous — multiple matches, no ghost text
			}
			match = cmd
		}
	}
	if match == input {
		return "" // already complete
	}
	return match
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
