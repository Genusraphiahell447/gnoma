package elf

import "time"

// Progress carries structured status from an active elf to the TUI.
type Progress struct {
	ElfID       string        // unique elf identifier
	Description string        // task prompt (truncated)
	ToolUses    int           // total tool calls completed
	Tokens      int           // tokens consumed (input+output)
	Activity    string        // current line: "⚙ [bash] running…", "→ output…", ""
	Done        bool          // true when elf completes
	Error       string        // non-empty on failure
	Duration    time.Duration // set on Done
}
