package bash

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"
)

const aliasHarvestTimeout = 5 * time.Second

// AliasMap holds harvested shell aliases.
type AliasMap struct {
	mu      sync.RWMutex
	aliases map[string]string // alias name → expansion
}

func NewAliasMap() *AliasMap {
	return &AliasMap{aliases: make(map[string]string)}
}

// Get returns the expansion for an alias, or empty string if not found.
func (m *AliasMap) Get(name string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	exp, ok := m.aliases[name]
	return exp, ok
}

// Len returns the number of harvested aliases.
func (m *AliasMap) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.aliases)
}

// All returns a copy of all aliases.
func (m *AliasMap) All() map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	cp := make(map[string]string, len(m.aliases))
	for k, v := range m.aliases {
		cp[k] = v
	}
	return cp
}

// AliasSummary returns a compact, LLM-readable summary of command-replacement aliases —
// those where the expansion's first word differs from the alias name (e.g. find → fd).
// Flag-only aliases (ls → ls --color=auto) are excluded. Returns "" if none found.
func (m *AliasMap) AliasSummary() string {
	if m == nil {
		return ""
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	var replacements []string
	for name, expansion := range m.aliases {
		firstWord := expansion
		if idx := strings.IndexAny(expansion, " \t"); idx != -1 {
			firstWord = expansion[:idx]
		}
		if firstWord != name && firstWord != "" {
			replacements = append(replacements, name+" → "+firstWord)
		}
	}

	if len(replacements) == 0 {
		return ""
	}

	sort.Strings(replacements)
	return "Shell command replacements (use replacement's syntax, not original): " +
		strings.Join(replacements, ", ") + "."
}

// ExpandCommand expands the first word of a command if it's a known alias.
// Only the first word is expanded (matching bash alias behavior).
// Returns the original command unchanged if no alias matches.
func (m *AliasMap) ExpandCommand(cmd string) string {
	trimmed := strings.TrimSpace(cmd)
	if trimmed == "" {
		return cmd
	}

	// Extract first word
	firstWord := trimmed
	rest := ""
	if idx := strings.IndexAny(trimmed, " \t"); idx != -1 {
		firstWord = trimmed[:idx]
		rest = trimmed[idx:]
	}

	m.mu.RLock()
	expansion, ok := m.aliases[firstWord]
	m.mu.RUnlock()

	if !ok {
		return cmd
	}
	return expansion + rest
}

// HarvestAliases spawns the user's shell once to collect alias definitions.
// Supports bash, zsh, and fish. Falls back gracefully for unknown shells.
// Safe: only reads alias text definitions, never sources them in execution context.
func HarvestAliases(ctx context.Context) (*AliasMap, error) {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/bash"
	}

	ctx, cancel := context.WithTimeout(ctx, aliasHarvestTimeout)
	defer cancel()

	// Build the alias dump command based on shell type
	shellBase := shellBaseName(shell)
	aliasCmd := aliasCommandFor(shellBase)

	// -i: interactive (loads rc files), -c: run command then exit
	cmd := exec.CommandContext(ctx, shell, "-ic", aliasCmd)
	// Prevent the interactive shell from reading actual stdin
	cmd.Stdin = nil
	// Suppress stderr (shell startup warnings like zsh's "can't change option: zle")
	cmd.Stderr = nil

	// Use Output() but don't fail on non-zero exit — zsh often exits with
	// errors from zle/prompt setup while still producing valid alias output
	output, err := cmd.Output()
	if len(output) == 0 && err != nil {
		return NewAliasMap(), fmt.Errorf("alias harvest (%s): %w", shellBase, err)
	}
	// If we got output, parse it regardless of exit code

	if shellBase == "fish" {
		return ParseFishAliases(string(output))
	}
	return ParseAliases(string(output))
}

// shellBaseName extracts the shell name from a path (e.g., "/bin/zsh" → "zsh").
func shellBaseName(shell string) string {
	parts := strings.Split(shell, "/")
	return parts[len(parts)-1]
}

// aliasCommandFor returns the alias dump command for a given shell.
func aliasCommandFor(shell string) string {
	switch shell {
	case "fish":
		// fish uses `alias` without -p, outputs: alias name 'expansion'
		return "alias 2>/dev/null; true"
	case "zsh":
		// zsh: `alias -p` produces nothing; `alias` outputs name=value (no quotes)
		return "alias 2>/dev/null; true"
	case "bash", "sh", "dash", "ash":
		// POSIX shells use `alias -p`
		return "alias -p 2>/dev/null; true"
	default:
		// Best effort for unknown shells
		return "alias 2>/dev/null; true"
	}
}

// ParseFishAliases parses fish shell alias output.
// Fish format: alias name 'expansion' or alias name "expansion"
func ParseFishAliases(output string) (*AliasMap, error) {
	m := NewAliasMap()

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "alias ") {
			continue
		}
		// Remove "alias " prefix
		rest := strings.TrimPrefix(line, "alias ")

		// Split: name 'expansion' or name "expansion" or name expansion
		spaceIdx := strings.IndexByte(rest, ' ')
		if spaceIdx == -1 {
			continue
		}

		name := rest[:spaceIdx]
		expansion := strings.TrimSpace(rest[spaceIdx+1:])
		expansion = stripQuotes(expansion)

		if name == "" || expansion == "" {
			continue
		}

		if v := ValidateCommand(expansion); v != nil {
			continue
		}

		m.mu.Lock()
		m.aliases[name] = expansion
		m.mu.Unlock()
	}

	return m, nil
}

// ParseAliases parses the output of `alias -p` into an AliasMap.
// Each line is: alias name='expansion' (bash) or name=expansion (zsh)
func ParseAliases(output string) (*AliasMap, error) {
	m := NewAliasMap()

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Strip "alias " prefix if present (bash format)
		line = strings.TrimPrefix(line, "alias ")

		// Split on first '='
		eqIdx := strings.Index(line, "=")
		if eqIdx == -1 {
			continue
		}

		name := line[:eqIdx]
		expansion := line[eqIdx+1:]

		// Strip surrounding quotes from expansion
		expansion = stripQuotes(expansion)

		if name == "" || expansion == "" {
			continue
		}

		// Security: validate the expansion doesn't contain dangerous patterns
		if v := ValidateCommand(expansion); v != nil {
			// Skip aliases with dangerous expansions
			continue
		}

		m.mu.Lock()
		m.aliases[name] = expansion
		m.mu.Unlock()
	}

	return m, nil
}

// stripQuotes removes matching surrounding single or double quotes.
func stripQuotes(s string) string {
	if len(s) < 2 {
		return s
	}
	if (s[0] == '\'' && s[len(s)-1] == '\'') || (s[0] == '"' && s[len(s)-1] == '"') {
		return s[1 : len(s)-1]
	}
	return s
}
