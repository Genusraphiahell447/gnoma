package permission

import (
	"path/filepath"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// Action is the decision for a permission rule.
type Action string

const (
	ActionAllow Action = "allow"
	ActionDeny  Action = "deny"
)

// Rule defines a single permission rule.
type Rule struct {
	Tool    string `toml:"tool"`    // glob pattern: "bash", "fs.*", "*"
	Pattern string `toml:"pattern"` // optional: argument pattern
	Action  Action `toml:"action"`
}

// Matches returns true if the rule matches the given tool name.
func (r Rule) Matches(toolName string) bool {
	matched, _ := filepath.Match(r.Tool, toolName)
	return matched
}

// SplitCompoundCommand decomposes a shell command into individual simple commands
// using a proper POSIX shell parser (mvdan.cc/sh). Recursively walks BinaryCmd
// nodes (&&, ||) and statement lists (;).
func SplitCompoundCommand(cmd string) []string {
	reader := strings.NewReader(cmd)
	parser := syntax.NewParser(syntax.KeepComments(false))

	file, err := parser.Parse(reader, "")
	if err != nil {
		return []string{cmd}
	}

	var commands []string
	printer := syntax.NewPrinter()

	for _, stmt := range file.Stmts {
		extractCommands(stmt.Cmd, printer, &commands)
	}

	if len(commands) == 0 {
		return []string{cmd}
	}
	return commands
}

func extractCommands(node syntax.Command, printer *syntax.Printer, out *[]string) {
	if node == nil {
		return
	}
	// Only split on && and || (logical operators), not pipes
	if bin, ok := node.(*syntax.BinaryCmd); ok {
		if bin.Op == syntax.AndStmt || bin.Op == syntax.OrStmt {
			if bin.X != nil {
				extractCommands(bin.X.Cmd, printer, out)
			}
			if bin.Y != nil {
				extractCommands(bin.Y.Cmd, printer, out)
			}
			return
		}
	}
	// Everything else (simple command, pipe, subshell) — print as one unit
	var b strings.Builder
	printer.Print(&b, node)
	if s := strings.TrimSpace(b.String()); s != "" {
		*out = append(*out, s)
	}
}
