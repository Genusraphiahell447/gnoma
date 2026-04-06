package hook

import (
	"testing"
)

func TestEventType_String(t *testing.T) {
	tests := []struct {
		event EventType
		want  string
	}{
		{PreToolUse, "pre_tool_use"},
		{PostToolUse, "post_tool_use"},
		{SessionStart, "session_start"},
		{SessionEnd, "session_end"},
		{PreCompact, "pre_compact"},
		{Stop, "stop"},
	}
	for _, tt := range tests {
		if got := tt.event.String(); got != tt.want {
			t.Errorf("EventType(%d).String() = %q, want %q", tt.event, got, tt.want)
		}
	}
}

func TestParseEventType(t *testing.T) {
	tests := []struct {
		input   string
		want    EventType
		wantErr bool
	}{
		{"pre_tool_use", PreToolUse, false},
		{"post_tool_use", PostToolUse, false},
		{"session_start", SessionStart, false},
		{"session_end", SessionEnd, false},
		{"pre_compact", PreCompact, false},
		{"stop", Stop, false},
		{"unknown", 0, true},
		{"", 0, true},
		{"PRE_TOOL_USE", 0, true}, // case-sensitive
	}
	for _, tt := range tests {
		got, err := ParseEventType(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseEventType(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if !tt.wantErr && got != tt.want {
			t.Errorf("ParseEventType(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestCommandType_String(t *testing.T) {
	tests := []struct {
		ct   CommandType
		want string
	}{
		{CommandTypeShell, "command"},
		{CommandTypePrompt, "prompt"},
		{CommandTypeAgent, "agent"},
	}
	for _, tt := range tests {
		if got := tt.ct.String(); got != tt.want {
			t.Errorf("CommandType(%d).String() = %q, want %q", tt.ct, got, tt.want)
		}
	}
}

func TestParseCommandType(t *testing.T) {
	tests := []struct {
		input   string
		want    CommandType
		wantErr bool
	}{
		{"command", CommandTypeShell, false},
		{"prompt", CommandTypePrompt, false},
		{"agent", CommandTypeAgent, false},
		{"shell", 0, true},
		{"", 0, true},
	}
	for _, tt := range tests {
		got, err := ParseCommandType(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseCommandType(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if !tt.wantErr && got != tt.want {
			t.Errorf("ParseCommandType(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestAction_String(t *testing.T) {
	tests := []struct {
		action Action
		want   string
	}{
		{Allow, "allow"},
		{Deny, "deny"},
		{Skip, "skip"},
	}
	for _, tt := range tests {
		if got := tt.action.String(); got != tt.want {
			t.Errorf("Action(%d).String() = %q, want %q", tt.action, got, tt.want)
		}
	}
}

func TestParseAction(t *testing.T) {
	tests := []struct {
		exitCode int
		want     Action
		wantErr  bool
	}{
		{0, Allow, false},
		{1, Skip, false},
		{2, Deny, false},
		{3, 0, true},
		{-1, 0, true},
		{127, 0, true},
	}
	for _, tt := range tests {
		got, err := ParseAction(tt.exitCode)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseAction(%d) error = %v, wantErr %v", tt.exitCode, err, tt.wantErr)
			continue
		}
		if !tt.wantErr && got != tt.want {
			t.Errorf("ParseAction(%d) = %v, want %v", tt.exitCode, got, tt.want)
		}
	}
}

func TestHookDef_Validate(t *testing.T) {
	valid := HookDef{
		Name:    "test-hook",
		Event:   PreToolUse,
		Command: CommandTypeShell,
		Exec:    "echo hello",
	}

	tests := []struct {
		name    string
		def     HookDef
		wantErr bool
	}{
		{"valid", valid, false},
		{"empty name", withName(valid, ""), true},
		{"empty exec", withExec(valid, ""), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.def.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("HookDef.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func withName(d HookDef, name string) HookDef { d.Name = name; return d }
func withExec(d HookDef, exec string) HookDef { d.Exec = exec; return d }
