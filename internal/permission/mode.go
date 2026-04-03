package permission

// Mode controls the overall permission behavior.
type Mode string

const (
	// ModeDefault prompts the user for each tool invocation.
	ModeDefault Mode = "default"
	// ModeAcceptEdits auto-allows file edits + reads, prompts for bash/destructive.
	ModeAcceptEdits Mode = "accept_edits"
	// ModeBypass allows everything without prompting.
	ModeBypass Mode = "bypass"
	// ModeDeny denies everything unless an explicit allow rule matches.
	ModeDeny Mode = "deny"
	// ModePlan allows only read-only tools, blocks all writes.
	ModePlan Mode = "plan"
	// ModeAuto uses task type + tool risk scoring to decide.
	// Low-risk read-only tools auto-allow, everything else prompts.
	ModeAuto Mode = "auto"
)

// Valid returns true if the mode is recognized.
func (m Mode) Valid() bool {
	switch m {
	case ModeDefault, ModeAcceptEdits, ModeBypass, ModeDeny, ModePlan, ModeAuto:
		return true
	}
	return false
}
