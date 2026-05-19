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
//
// Pattern matching semantics — important caveats:
//
//   - Tool is matched as a filepath.Match glob ("bash", "fs.*", "*").
//   - Pattern, if set, is a case-sensitive substring match against the
//     JSON-serialized tool arguments. It is NOT a glob, regex, or
//     structured-field match.
//
// Concretely, given `Pattern = "rm -rf"`, these match:
//   - `{"command":"rm -rf /tmp"}`
//   - `{"prefix":"echo rm -rf","other":"x"}`  (substring appears anywhere)
//
// And these DO NOT match:
//   - `{"command":"rm  -rf /"}`        (double space — substring is exact)
//   - `{"command":"RM -RF /"}`         (case differs)
//   - `{"command":"rm\t-rf /"}`        (tab vs space)
//
// Rule authors: write patterns that are unambiguous when serialized as JSON
// (no leading/trailing whitespace, no surrounding quotes) and remember that
// deny rules are bypass-immune — they fire before any mode check.
type Rule struct {
	Tool    string `toml:"tool"`    // glob pattern: "bash", "fs.*", "*"
	Pattern string `toml:"pattern"` // optional: case-sensitive substring on JSON args (see godoc)
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
	// Everything else (simple command, pipe, subshell) — print as one unit.
	// Printer errors on a strings.Builder are unreachable (Builder.Write never errors).
	var b strings.Builder
	_ = printer.Print(&b, node)
	if s := strings.TrimSpace(b.String()); s != "" {
		*out = append(*out, s)
	}
}
