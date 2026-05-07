package bash

import "strings"

// interactiveHint is the tool result output returned when an interactive command is detected.
const interactiveHint = "Command requires interactive terminal input.\n" +
	"Use /shell in the TUI to open a shell, or /shell <command> to run this command directly."

// isInteractiveCmd reports whether cmd needs an interactive terminal to function.
// Returns a non-empty reason string when the command is interactive, "" otherwise.
func isInteractiveCmd(cmd string) string {
	lower := strings.ToLower(strings.TrimSpace(cmd))

	// prefixInteractive: always interactive regardless of arguments.
	for _, p := range prefixInteractive {
		if lower == p || strings.HasPrefix(lower, p+" ") || strings.HasPrefix(lower, p+"\t") {
			return p + " requires an interactive terminal"
		}
	}

	// exactInteractive: interactive only when invoked as a bare command (REPL mode).
	for _, p := range exactInteractive {
		if lower == p {
			return p + " opens an interactive REPL"
		}
	}

	return ""
}

// prefixInteractive lists commands that are interactive regardless of their arguments.
var prefixInteractive = []string{
	"sudo",
	"ssh",
	"passwd",
	"vim",
	"vi",
	"nano",
	"less",
	"more",
	"htop",
	"top",
	"mysql",
	"psql",
	"ftp",
	"sftp",
	"git push",
}

// exactInteractive lists commands that are interactive only when invoked without arguments
// (i.e., they open a REPL). With arguments they run scripts non-interactively.
var exactInteractive = []string{
	"python3",
	"python",
	"python2",
	"node",
	"irb",
	"iex",
	"ghci",
	"julia",
}
