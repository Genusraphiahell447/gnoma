package permission

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestMode_Valid(t *testing.T) {
	valid := []Mode{ModeDefault, ModeAcceptEdits, ModeBypass, ModeDeny, ModePlan, ModeAuto}
	for _, m := range valid {
		if !m.Valid() {
			t.Errorf("mode %q should be valid", m)
		}
	}
	if Mode("bogus").Valid() {
		t.Error("bogus mode should be invalid")
	}
}

func TestChecker_BypassMode(t *testing.T) {
	c := NewChecker(ModeBypass, nil, nil)

	err := c.Check(context.Background(), ToolInfo{Name: "bash", IsDestructive: true}, json.RawMessage(`{"command":"rm -rf /"}`))
	if err != nil {
		t.Errorf("bypass mode should allow everything, got: %v", err)
	}
}

func TestChecker_BypassDenyRuleImmune(t *testing.T) {
	rules := []Rule{{Tool: "bash", Pattern: "rm -rf", Action: ActionDeny}}
	c := NewChecker(ModeBypass, rules, nil)

	err := c.Check(context.Background(), ToolInfo{Name: "bash"}, json.RawMessage(`{"command":"rm -rf /"}`))
	if err == nil {
		t.Error("deny rules should override bypass mode")
	}
}

func TestChecker_DenyMode(t *testing.T) {
	c := NewChecker(ModeDeny, nil, nil)

	err := c.Check(context.Background(), ToolInfo{Name: "bash"}, json.RawMessage(`{}`))
	if !errors.Is(err, ErrDenied) {
		t.Error("deny mode should deny without allow rules")
	}
}

func TestChecker_DenyModeWithAllowRule(t *testing.T) {
	rules := []Rule{{Tool: "fs.*", Action: ActionAllow}}
	c := NewChecker(ModeDeny, rules, nil)

	// Allowed by rule
	err := c.Check(context.Background(), ToolInfo{Name: "fs.read", IsReadOnly: true}, json.RawMessage(`{}`))
	if err != nil {
		t.Errorf("should allow fs.read via rule: %v", err)
	}

	// Not allowed — no matching rule
	err = c.Check(context.Background(), ToolInfo{Name: "bash"}, json.RawMessage(`{}`))
	if !errors.Is(err, ErrDenied) {
		t.Error("bash should be denied without allow rule")
	}
}

func TestChecker_PlanMode(t *testing.T) {
	c := NewChecker(ModePlan, nil, nil)

	// Read-only allowed
	err := c.Check(context.Background(), ToolInfo{Name: "fs.read", IsReadOnly: true}, json.RawMessage(`{}`))
	if err != nil {
		t.Errorf("plan mode should allow read-only: %v", err)
	}

	// Write denied
	err = c.Check(context.Background(), ToolInfo{Name: "fs.write"}, json.RawMessage(`{}`))
	if !errors.Is(err, ErrDenied) {
		t.Error("plan mode should deny writes")
	}

	// Bash denied
	err = c.Check(context.Background(), ToolInfo{Name: "bash"}, json.RawMessage(`{}`))
	if !errors.Is(err, ErrDenied) {
		t.Error("plan mode should deny bash")
	}
}

func TestChecker_AcceptEditsMode(t *testing.T) {
	c := NewChecker(ModeAcceptEdits, nil, func(_ context.Context, _ string, _ json.RawMessage) (bool, error) {
		return false, nil // deny prompt
	})

	// Read-only allowed
	err := c.Check(context.Background(), ToolInfo{Name: "fs.read", IsReadOnly: true}, json.RawMessage(`{}`))
	if err != nil {
		t.Errorf("should allow read-only: %v", err)
	}

	// File edits allowed
	err = c.Check(context.Background(), ToolInfo{Name: "fs.write"}, json.RawMessage(`{}`))
	if err != nil {
		t.Errorf("should allow fs.write in acceptEdits: %v", err)
	}

	// Bash requires prompt — denied since our prompt returns false
	err = c.Check(context.Background(), ToolInfo{Name: "bash"}, json.RawMessage(`{}`))
	if !errors.Is(err, ErrDenied) {
		t.Error("bash should go through prompt in acceptEdits mode")
	}
}

func TestChecker_AutoMode(t *testing.T) {
	c := NewChecker(ModeAuto, nil, func(_ context.Context, _ string, _ json.RawMessage) (bool, error) {
		return true, nil // approve prompt
	})

	// Read-only auto-allowed
	err := c.Check(context.Background(), ToolInfo{Name: "fs.grep", IsReadOnly: true}, json.RawMessage(`{}`))
	if err != nil {
		t.Errorf("auto mode should auto-allow read-only: %v", err)
	}

	// Write goes to prompt — approved
	err = c.Check(context.Background(), ToolInfo{Name: "bash"}, json.RawMessage(`{}`))
	if err != nil {
		t.Errorf("auto mode should prompt for write, prompt approved: %v", err)
	}
}

func TestChecker_DefaultMode_Prompts(t *testing.T) {
	prompted := false
	c := NewChecker(ModeDefault, nil, func(_ context.Context, name string, _ json.RawMessage) (bool, error) {
		prompted = true
		return true, nil
	})

	err := c.Check(context.Background(), ToolInfo{Name: "fs.read", IsReadOnly: true}, json.RawMessage(`{}`))
	if err != nil {
		t.Errorf("should allow after prompt: %v", err)
	}
	if !prompted {
		t.Error("default mode should always prompt")
	}
}

func TestChecker_SafetyCheck(t *testing.T) {
	// Safety checks are bypass-immune
	c := NewChecker(ModeBypass, nil, nil)

	tests := []struct {
		name string
		args string
	}{
		{"env file", `{"path":".env"}`},
		{"git dir", `{"path":".git/config"}`},
		{"ssh key", `{"path":"id_rsa"}`},
		{"aws creds", `{"path":".aws/credentials"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := c.Check(context.Background(), ToolInfo{Name: "fs.read"}, json.RawMessage(tt.args))
			if !errors.Is(err, ErrDenied) {
				t.Errorf("safety check should block: %v", err)
			}
		})
	}
}

func TestChecker_CompoundCommand(t *testing.T) {
	rules := []Rule{{Tool: "bash", Pattern: "rm", Action: ActionDeny}}
	c := NewChecker(ModeBypass, rules, nil)

	// Single safe command — allowed
	err := c.Check(context.Background(), ToolInfo{Name: "bash"}, json.RawMessage(`{"command":"echo hello"}`))
	if err != nil {
		t.Errorf("single safe command should be allowed: %v", err)
	}

	// Compound with denied subcommand
	err = c.Check(context.Background(), ToolInfo{Name: "bash"}, json.RawMessage(`{"command":"echo hello && rm -rf /"}`))
	if !errors.Is(err, ErrDenied) {
		t.Error("compound with denied subcommand should be denied")
	}
}

func TestSplitCompoundCommand(t *testing.T) {
	tests := []struct {
		cmd  string
		want int
	}{
		{"echo hello", 1},
		{"echo hello && echo world", 2},
		{"echo a; echo b; echo c", 3},
		{"echo hello | grep h", 1}, // pipe is one statement
		{"cd src && make && make test", 3},
	}
	for _, tt := range tests {
		parts := SplitCompoundCommand(tt.cmd)
		if len(parts) != tt.want {
			t.Errorf("SplitCompoundCommand(%q) = %d parts %v, want %d", tt.cmd, len(parts), parts, tt.want)
		}
	}
}

func TestRule_Matches(t *testing.T) {
	tests := []struct {
		rule Rule
		tool string
		want bool
	}{
		{Rule{Tool: "bash"}, "bash", true},
		{Rule{Tool: "bash"}, "fs.read", false},
		{Rule{Tool: "fs.*"}, "fs.read", true},
		{Rule{Tool: "fs.*"}, "fs.write", true},
		{Rule{Tool: "fs.*"}, "bash", false},
		{Rule{Tool: "*"}, "anything", true},
	}
	for _, tt := range tests {
		if got := tt.rule.Matches(tt.tool); got != tt.want {
			t.Errorf("Rule{%q}.Matches(%q) = %v, want %v", tt.rule.Tool, tt.tool, got, tt.want)
		}
	}
}

func TestChecker_SetMode(t *testing.T) {
	c := NewChecker(ModeDefault, nil, nil)
	if c.Mode() != ModeDefault {
		t.Errorf("initial mode should be default")
	}
	c.SetMode(ModePlan)
	if c.Mode() != ModePlan {
		t.Errorf("mode should be plan after SetMode")
	}
}
