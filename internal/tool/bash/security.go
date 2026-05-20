package bash

import (
	"fmt"
	"strings"
	"unicode"
)

// SecurityCheck identifies a specific validation check.
type SecurityCheck int

const (
	CheckIncomplete        SecurityCheck = iota + 1 // fragments, trailing operators
	CheckMetacharacters                             // ; | & $ ` < >
	CheckCmdSubstitution                            // $(), ``, ${}
	CheckRedirection                                // < > >> etc.
	CheckDangerousVars                              // IFS, PATH manipulation
	CheckNewlineInjection                           // embedded newlines
	CheckControlChars                               // ASCII 00-1F (except \n \t)
	CheckJQInjection                                // jq with shell metacharacters
	CheckObfuscatedFlags                            // Unicode lookalike hyphens
	CheckProcEnviron                                // /proc/*/environ access
	CheckBraceExpansion                             // dangerous {a,b} expansion
	CheckUnicodeWhitespace                          // non-ASCII whitespace
	CheckZshDangerous                               // zsh-specific dangerous constructs
	CheckCommentDesync                              // # inside strings hiding commands
	CheckIndirectExec                               // eval, bash -c, curl|bash, source
)

// SecurityViolation describes a failed security check.
type SecurityViolation struct {
	Check   SecurityCheck
	Message string
}

func (v SecurityViolation) Error() string {
	return fmt.Sprintf("bash security check %d: %s", v.Check, v.Message)
}

// ValidateCommand runs the 7 critical security checks against a command string.
// Returns nil if all checks pass, or the first violation found.
func ValidateCommand(cmd string) *SecurityViolation {
	if strings.TrimSpace(cmd) == "" {
		return &SecurityViolation{Check: CheckIncomplete, Message: "empty command"}
	}

	// Check incomplete on raw command (before trimming) to catch tab-starts
	if v := checkIncomplete(cmd); v != nil {
		return v
	}

	cmd = strings.TrimSpace(cmd)

	if v := checkControlChars(cmd); v != nil {
		return v
	}
	if v := checkNewlineInjection(cmd); v != nil {
		return v
	}
	if v := checkCmdSubstitution(cmd); v != nil {
		return v
	}
	if v := checkDangerousVars(cmd); v != nil {
		return v
	}
	if v := checkStandaloneSemicolon(cmd); v != nil {
		return v
	}
	if v := checkSensitiveRedirection(cmd); v != nil {
		return v
	}
	if v := checkJQInjection(cmd); v != nil {
		return v
	}
	if v := checkObfuscatedFlags(cmd); v != nil {
		return v
	}
	if v := checkProcEnviron(cmd); v != nil {
		return v
	}
	if v := checkBraceExpansion(cmd); v != nil {
		return v
	}
	if v := checkUnicodeWhitespace(cmd); v != nil {
		return v
	}
	if v := checkZshDangerous(cmd); v != nil {
		return v
	}
	if v := checkCommentQuoteDesync(cmd); v != nil {
		return v
	}
	if v := checkIndirectExec(cmd); v != nil {
		return v
	}
	return nil
}

// checkIncomplete detects command fragments that shouldn't be executed.
func checkIncomplete(cmd string) *SecurityViolation {
	// Starts with tab (likely a fragment from indented code)
	if cmd[0] == '\t' {
		return &SecurityViolation{Check: CheckIncomplete, Message: "command starts with tab (likely a code fragment)"}
	}
	// Starts with a flag (no command name)
	if cmd[0] == '-' {
		return &SecurityViolation{Check: CheckIncomplete, Message: "command starts with flag (no command name)"}
	}
	// Ends with a dangling operator
	trimmed := strings.TrimRight(cmd, " \t")
	if len(trimmed) > 0 {
		last := trimmed[len(trimmed)-1]
		if last == '|' || last == '&' || last == ';' {
			return &SecurityViolation{Check: CheckIncomplete, Message: "command ends with dangling operator"}
		}
	}
	return nil
}

// checkControlChars blocks ASCII control characters (0x00-0x1F) except \n and \t.
func checkControlChars(cmd string) *SecurityViolation {
	for i, r := range cmd {
		if r < 0x20 && r != '\n' && r != '\t' && r != '\r' {
			return &SecurityViolation{
				Check:   CheckControlChars,
				Message: fmt.Sprintf("control character U+%04X at position %d", r, i),
			}
		}
	}
	return nil
}

// checkNewlineInjection blocks commands with embedded newlines.
// Newlines in quoted strings are legitimate but rare in single commands.
// We allow them inside single/double quotes only.
func checkNewlineInjection(cmd string) *SecurityViolation {
	inSingle := false
	inDouble := false
	escaped := false

	for _, r := range cmd {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' && !inSingle {
			escaped = true
			continue
		}
		if r == '\'' && !inDouble {
			inSingle = !inSingle
			continue
		}
		if r == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}
		if r == '\n' && !inSingle && !inDouble {
			return &SecurityViolation{
				Check:   CheckNewlineInjection,
				Message: "unquoted newline (potential command injection)",
			}
		}
	}
	return nil
}

// checkCmdSubstitution blocks $(), “, and ${} command/variable substitution.
// These allow arbitrary code execution within a command.
func checkCmdSubstitution(cmd string) *SecurityViolation {
	inSingle := false
	escaped := false

	for i, r := range cmd {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' && !inSingle {
			escaped = true
			continue
		}
		if r == '\'' {
			inSingle = !inSingle
			continue
		}

		// Skip checks inside single quotes (literal)
		if inSingle {
			continue
		}

		if r == '`' {
			return &SecurityViolation{
				Check:   CheckCmdSubstitution,
				Message: "backtick command substitution",
			}
		}

		if r == '$' && i+1 < len(cmd) {
			next := rune(cmd[i+1])
			if next == '(' {
				return &SecurityViolation{
					Check:   CheckCmdSubstitution,
					Message: "$() command substitution",
				}
			}
			if next == '{' {
				return &SecurityViolation{
					Check:   CheckCmdSubstitution,
					Message: "${} variable expansion",
				}
			}
		}
	}
	return nil
}

// checkStandaloneSemicolon blocks standalone semicolons used to chain commands.
// Pipes (|) and && / || are allowed (handled by compound command parsing).
func checkStandaloneSemicolon(cmd string) *SecurityViolation {
	inSingle := false
	inDouble := false
	escaped := false

	for _, r := range cmd {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' && !inSingle {
			escaped = true
			continue
		}
		if r == '\'' && !inDouble {
			inSingle = !inSingle
			continue
		}
		if r == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}
		if !inSingle && !inDouble && r == ';' {
			return &SecurityViolation{
				Check:   CheckMetacharacters,
				Message: "standalone semicolon (use && for chaining)",
			}
		}
	}
	return nil
}

// checkSensitiveRedirection blocks output redirection to sensitive paths.
// Detects: >, >>, fd redirects (2>), and no-space variants (>/etc/passwd).
func checkSensitiveRedirection(cmd string) *SecurityViolation {
	sensitiveTargets := []string{
		"/etc/passwd", "/etc/shadow", "/etc/sudoers",
		".bashrc", ".zshrc", ".profile", ".bash_profile",
		".ssh/authorized_keys", ".ssh/config",
		".env",
	}

	for _, target := range sensitiveTargets {
		// Match any form: >, >>, 2>, 2>>, &> followed by optional whitespace then target
		idx := strings.Index(cmd, target)
		if idx <= 0 {
			continue
		}
		// Check what precedes the target (skip whitespace backwards)
		pre := strings.TrimRight(cmd[:idx], " \t")
		if len(pre) > 0 && (pre[len(pre)-1] == '>' || strings.HasSuffix(pre, ">>")) {
			return &SecurityViolation{
				Check:   CheckRedirection,
				Message: fmt.Sprintf("redirection to sensitive path: %s", target),
			}
		}
	}
	return nil
}

// checkJQInjection detects jq commands with embedded shell metacharacters in the filter.
func checkJQInjection(cmd string) *SecurityViolation {
	// Only check commands that invoke jq
	if !strings.Contains(cmd, "jq ") && !strings.HasPrefix(cmd, "jq") {
		return nil
	}
	// jq filters with $( or ` indicate shell injection through jq
	dangerousInJQ := []string{"$(", "`", "system(", "input|"}
	for _, d := range dangerousInJQ {
		if strings.Contains(cmd, d) {
			return &SecurityViolation{
				Check:   CheckJQInjection,
				Message: fmt.Sprintf("jq command with dangerous pattern: %s", d),
			}
		}
	}
	return nil
}

// checkObfuscatedFlags detects Unicode lookalike characters used as hyphens.
// Attackers use en-dash (–), em-dash (—), minus sign (−) instead of ASCII hyphen.
func checkObfuscatedFlags(cmd string) *SecurityViolation {
	lookalikes := []rune{
		'\u2013', // en-dash –
		'\u2014', // em-dash —
		'\u2212', // minus sign −
		'\uFE63', // small hyphen-minus ﹣
		'\uFF0D', // fullwidth hyphen-minus -
	}
	for i, r := range cmd {
		for _, look := range lookalikes {
			if r == look {
				return &SecurityViolation{
					Check:   CheckObfuscatedFlags,
					Message: fmt.Sprintf("Unicode lookalike hyphen U+%04X at position %d", r, i),
				}
			}
		}
	}
	return nil
}

// checkProcEnviron blocks access to /proc/*/environ and /proc/self/mem.
func checkProcEnviron(cmd string) *SecurityViolation {
	dangerous := []string{
		"/proc/self/environ",
		"/proc/self/mem",
		"/proc/self/cmdline",
	}
	lower := strings.ToLower(cmd)
	for _, d := range dangerous {
		if strings.Contains(lower, d) {
			return &SecurityViolation{
				Check:   CheckProcEnviron,
				Message: fmt.Sprintf("access to %s (environment exfiltration)", d),
			}
		}
	}
	// Also catch /proc/*/environ with PID
	if strings.Contains(lower, "/proc/") && strings.Contains(lower, "/environ") {
		return &SecurityViolation{
			Check:   CheckProcEnviron,
			Message: "/proc/PID/environ access (environment exfiltration)",
		}
	}
	return nil
}

// checkBraceExpansion detects dangerous brace expansion patterns.
// {a,b} is used to expand multiple arguments — can bypass argument filters.
func checkBraceExpansion(cmd string) *SecurityViolation {
	inSingle := false
	inDouble := false
	braceDepth := 0

	for _, r := range cmd {
		if r == '\'' && !inDouble {
			inSingle = !inSingle
			continue
		}
		if r == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}
		if inSingle || inDouble {
			continue
		}
		if r == '{' {
			braceDepth++
		}
		if r == '}' && braceDepth > 0 {
			braceDepth--
		}
		// Comma inside braces = brace expansion
		if r == ',' && braceDepth > 0 {
			return &SecurityViolation{
				Check:   CheckBraceExpansion,
				Message: "brace expansion {a,b} (can bypass argument filters)",
			}
		}
	}
	return nil
}

// checkUnicodeWhitespace detects non-ASCII whitespace characters that can hide commands.
func checkUnicodeWhitespace(cmd string) *SecurityViolation {
	for i, r := range cmd {
		if r > 127 && unicode.IsSpace(r) {
			return &SecurityViolation{
				Check:   CheckUnicodeWhitespace,
				Message: fmt.Sprintf("non-ASCII whitespace U+%04X at position %d", r, i),
			}
		}
	}
	return nil
}

// checkZshDangerous detects zsh-specific dangerous constructs.
// Note: <() and >() are intentionally excluded — they are also valid bash process
// substitution patterns used in legitimate commands (e.g., diff <(cmd1) <(cmd2)).
func checkZshDangerous(cmd string) *SecurityViolation {
	dangerousPatterns := []struct {
		pattern string
		msg     string
	}{
		{"=(", "zsh =() process substitution (arbitrary execution)"},
		{"zmodload", "zsh module loading (can load arbitrary code)"},
		{"sysopen", "zsh sysopen (direct file descriptor access)"},
		{"ztcp", "zsh TCP socket access"},
		{"zsocket", "zsh socket access"},
	}
	for _, p := range dangerousPatterns {
		if strings.Contains(cmd, p.pattern) {
			return &SecurityViolation{
				Check:   CheckZshDangerous,
				Message: p.msg,
			}
		}
	}
	return nil
}

// checkCommentQuoteDesync detects # characters that could be interpreted differently
// depending on shell parsing context (e.g., mid-word # in zsh vs bash).
func checkCommentQuoteDesync(cmd string) *SecurityViolation {
	inSingle := false
	inDouble := false
	escaped := false
	prevWasSpace := true

	for _, r := range cmd {
		if escaped {
			escaped = false
			prevWasSpace = false
			continue
		}
		if r == '\\' && !inSingle {
			escaped = true
			continue
		}
		if r == '\'' && !inDouble {
			inSingle = !inSingle
			prevWasSpace = false
			continue
		}
		if r == '"' && !inSingle {
			inDouble = !inDouble
			prevWasSpace = false
			continue
		}
		if inSingle || inDouble {
			prevWasSpace = false
			continue
		}
		// # at start of word is a comment — legit after whitespace
		// # mid-word is suspicious in zsh (history expansion, etc.)
		if r == '#' && !prevWasSpace {
			return &SecurityViolation{
				Check:   CheckCommentDesync,
				Message: "mid-word # character (comment/history expansion ambiguity)",
			}
		}
		prevWasSpace = unicode.IsSpace(r)
	}
	return nil
}

// checkDangerousVars blocks attempts to manipulate IFS or PATH.
func checkDangerousVars(cmd string) *SecurityViolation {
	upper := strings.ToUpper(cmd)
	dangerousPatterns := []struct {
		pattern string
		msg     string
	}{
		{"IFS=", "IFS variable manipulation"},
		{"PATH=", "PATH variable manipulation"},
	}

	for _, p := range dangerousPatterns {
		idx := strings.Index(upper, p.pattern)
		if idx == -1 {
			continue
		}
		// Only flag if it's at the start or preceded by whitespace/semicolon
		if idx == 0 || !unicode.IsLetter(rune(cmd[idx-1])) {
			return &SecurityViolation{Check: CheckDangerousVars, Message: p.msg}
		}
	}
	return nil
}

// checkIndirectExec blocks commands that run arbitrary code indirectly,
// bypassing all other security checks applied to the outer command string.
// These are the highest-risk patterns in an agentic context.
func checkIndirectExec(cmd string) *SecurityViolation {
	lower := strings.ToLower(cmd)

	// Patterns that execute arbitrary content not visible to the checker.
	// Each entry is a substring to look for (after lowercasing).
	patterns := []struct {
		needle string
		msg    string
	}{
		{"eval ", "eval executes arbitrary code (bypasses all checks)"},
		{"eval\t", "eval executes arbitrary code (bypasses all checks)"},
		{"bash -c", "bash -c executes arbitrary inline code"},
		{"sh -c", "sh -c executes arbitrary inline code"},
		{"zsh -c", "zsh -c executes arbitrary inline code"},
		{"| bash", "pipe to bash executes downloaded/piped content"},
		{"| sh", "pipe to sh executes downloaded/piped content"},
		{"| zsh", "pipe to zsh executes downloaded/piped content"},
		{"|bash", "pipe to bash executes downloaded/piped content"},
		{"|sh", "pipe to sh executes downloaded/piped content"},
		{"source ", "source executes arbitrary script files"},
		{"source\t", "source executes arbitrary script files"},
	}

	for _, p := range patterns {
		if strings.Contains(lower, p.needle) {
			return &SecurityViolation{
				Check:   CheckIndirectExec,
				Message: p.msg,
			}
		}
	}

	// Dot-source: ". ./script.sh" or ". /path/script.sh"
	// Careful: don't block ". " that is just "cd" followed by space
	if strings.HasPrefix(lower, ". /") || strings.HasPrefix(lower, ". ./") ||
		strings.Contains(lower, " . /") || strings.Contains(lower, " . ./") {
		return &SecurityViolation{
			Check:   CheckIndirectExec,
			Message: "dot-source executes arbitrary script files",
		}
	}

	return nil
}
