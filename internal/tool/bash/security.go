package bash

import (
	"fmt"
	"strings"
	"unicode"
)

// SecurityCheck identifies a specific validation check.
type SecurityCheck int

const (
	CheckIncomplete      SecurityCheck = iota + 1 // fragments, trailing operators
	CheckMetacharacters                            // ; | & $ ` < >
	CheckCmdSubstitution                           // $(), ``, ${}
	CheckRedirection                               // < > >> etc.
	CheckDangerousVars                             // IFS, PATH manipulation
	CheckNewlineInjection                          // embedded newlines
	CheckControlChars                              // ASCII 00-1F (except \n \t)
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

// checkCmdSubstitution blocks $(), ``, and ${} command/variable substitution.
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
func checkSensitiveRedirection(cmd string) *SecurityViolation {
	sensitiveTargets := []string{
		"/etc/passwd", "/etc/shadow", "/etc/sudoers",
		".bashrc", ".zshrc", ".profile", ".bash_profile",
		".ssh/authorized_keys", ".ssh/config",
		".env",
	}

	for _, target := range sensitiveTargets {
		if strings.Contains(cmd, "> "+target) || strings.Contains(cmd, ">>"+target) {
			return &SecurityViolation{
				Check:   CheckRedirection,
				Message: fmt.Sprintf("redirection to sensitive path: %s", target),
			}
		}
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
