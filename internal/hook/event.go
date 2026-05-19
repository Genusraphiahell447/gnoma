package hook

import "fmt"

// EventType identifies when a hook fires.
type EventType int

const (
	PreToolUse EventType = iota + 1
	// PostToolUse fires after a tool executes, before the firewall scans
	// the tool result. Hooks receive the raw output by design — shell
	// hooks (audit log, forensic hash, local alerting) need it.
	//
	// LLM-bound hook types (CommandTypePrompt, CommandTypeAgent) do NOT
	// leak raw output to a remote model: every LLM round-trip on those
	// paths goes through security.SafeProvider (Wave 1), which scans
	// outgoing messages before delegating. Adding a new hook type that
	// talks to an LLM outside the router would break this guarantee —
	// see docs/essentials/decisions/004-posttooluse-hook-ordering.md.
	PostToolUse
	SessionStart
	SessionEnd
	PreCompact
	Stop
)

func (e EventType) String() string {
	switch e {
	case PreToolUse:
		return "pre_tool_use"
	case PostToolUse:
		return "post_tool_use"
	case SessionStart:
		return "session_start"
	case SessionEnd:
		return "session_end"
	case PreCompact:
		return "pre_compact"
	case Stop:
		return "stop"
	default:
		return fmt.Sprintf("unknown_event(%d)", int(e))
	}
}

// ParseEventType parses a TOML event string into an EventType.
func ParseEventType(s string) (EventType, error) {
	switch s {
	case "pre_tool_use":
		return PreToolUse, nil
	case "post_tool_use":
		return PostToolUse, nil
	case "session_start":
		return SessionStart, nil
	case "session_end":
		return SessionEnd, nil
	case "pre_compact":
		return PreCompact, nil
	case "stop":
		return Stop, nil
	default:
		return 0, fmt.Errorf("hook: unknown event type %q", s)
	}
}

// CommandType is the mechanism a hook uses to evaluate.
type CommandType int

const (
	CommandTypeShell  CommandType = iota + 1 // run a shell command
	CommandTypePrompt                         // send a prompt to an LLM
	CommandTypeAgent                          // spawn an elf
)

func (c CommandType) String() string {
	switch c {
	case CommandTypeShell:
		return "command"
	case CommandTypePrompt:
		return "prompt"
	case CommandTypeAgent:
		return "agent"
	default:
		return fmt.Sprintf("unknown_command(%d)", int(c))
	}
}

// ParseCommandType parses a TOML type string into a CommandType.
func ParseCommandType(s string) (CommandType, error) {
	switch s {
	case "command":
		return CommandTypeShell, nil
	case "prompt":
		return CommandTypePrompt, nil
	case "agent":
		return CommandTypeAgent, nil
	default:
		return 0, fmt.Errorf("hook: unknown command type %q", s)
	}
}

// Action is the outcome of a hook execution.
type Action int

const (
	Allow Action = iota + 1 // exit 0 — hook approves
	Deny                    // exit 2 — hook rejects
	Skip                    // exit 1 — hook abstains
)

func (a Action) String() string {
	switch a {
	case Allow:
		return "allow"
	case Deny:
		return "deny"
	case Skip:
		return "skip"
	default:
		return fmt.Sprintf("unknown_action(%d)", int(a))
	}
}

// ParseAction maps a shell exit code to an Action.
func ParseAction(exitCode int) (Action, error) {
	switch exitCode {
	case 0:
		return Allow, nil
	case 1:
		return Skip, nil
	case 2:
		return Deny, nil
	default:
		return 0, fmt.Errorf("hook: unrecognised exit code %d", exitCode)
	}
}
