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

func TestChecker_ElfNilPrompt_FsWriteAllowed(t *testing.T) {
	// Elfs use WithDenyPrompt (nil promptFn). Non-destructive fs ops must still
	// be allowed so elfs can write files in auto/acceptEdits modes.
	c := NewChecker(ModeAuto, nil, nil) // nil promptFn simulates elf checker

	// Non-destructive fs.write: allowed
	err := c.Check(context.Background(), ToolInfo{Name: "fs.write"}, json.RawMessage(`{"path":"AGENTS.md"}`))
	if err != nil {
		t.Errorf("elf should be able to write files: %v", err)
	}

	// Destructive fs op: denied
	err = c.Check(context.Background(), ToolInfo{Name: "fs.delete", IsDestructive: true}, json.RawMessage(`{"path":"foo"}`))
	if !errors.Is(err, ErrDenied) {
		t.Error("destructive fs op should be denied without prompt handler")
	}

	// bash: denied
	err = c.Check(context.Background(), ToolInfo{Name: "bash"}, json.RawMessage(`{"command":"echo hi"}`))
	if !errors.Is(err, ErrDenied) {
		t.Error("bash should be denied without prompt handler")
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

	blocked := []struct {
		name     string
		toolName string
		args     string
	}{
		{"env file", "fs.read", `{"path":".env"}`},
		{"git dir", "fs.read", `{"path":".git/config"}`},
		{"ssh key", "fs.read", `{"path":"id_rsa"}`},
		{"aws creds", "fs.read", `{"path":".aws/credentials"}`},
		{"bash env", "bash", `{"command":"cat .env"}`},
	}
	for _, tt := range blocked {
		t.Run(tt.name, func(t *testing.T) {
			err := c.Check(context.Background(), ToolInfo{Name: tt.toolName}, json.RawMessage(tt.args))
			if !errors.Is(err, ErrDenied) {
				t.Errorf("safety check should block: %v", err)
			}
		})
	}

	// Writing a file whose *content* mentions .env (e.g. AGENTS.md docs) must not be blocked.
	t.Run("env mention in content not blocked", func(t *testing.T) {
		args := json.RawMessage(`{"path":"AGENTS.md","content":"Copy .env.example to .env and fill in the values."}`)
		err := c.Check(context.Background(), ToolInfo{Name: "fs.write"}, args)
		if err != nil {
			t.Errorf("fs.write to safe path should not be blocked by content mention: %v", err)
		}
	})
}

func TestChecker_ElfInheritsSafetyPatterns(t *testing.T) {
	// Audit H2: spawn_elfs/agent are exempt from safetyCheck at the orchestration
	// layer, with the rationale that the spawned elf will be checked when it
	// actually accesses files. That contract requires the elf checker (built via
	// WithDenyPrompt) to inherit the parent's safetyDenyPatterns AND for those
	// patterns to still fire even in ModeBypass.
	parent := NewChecker(ModeBypass, nil, nil)
	elfChecker := parent.WithDenyPrompt()

	safetyCases := []struct {
		name     string
		toolName string
		args     string
	}{
		{"env file", "fs.read", `{"path":".env"}`},
		{"ssh key", "fs.read", `{"path":"id_rsa"}`},
		{"aws creds", "fs.read", `{"path":".aws/credentials"}`},
		{"bash on env", "bash", `{"command":"cat .env"}`},
	}
	for _, tt := range safetyCases {
		t.Run(tt.name, func(t *testing.T) {
			err := elfChecker.Check(context.Background(), ToolInfo{Name: tt.toolName}, json.RawMessage(tt.args))
			if !errors.Is(err, ErrDenied) {
				t.Errorf("elf checker must still block %s on %s, got: %v", tt.args, tt.toolName, err)
			}
		})
	}
}

func TestChecker_SafetyCheck_OrchestrationToolsExempt(t *testing.T) {
	// spawn_elfs and agent carry elf PROMPT TEXT as args — arbitrary instruction
	// text that may legitimately mention .env, credentials, etc.
	// Security is enforced inside each spawned elf, not at the orchestration layer.
	c := NewChecker(ModeBypass, nil, nil)

	cases := []struct {
		name     string
		toolName string
		args     string
	}{
		{"spawn_elfs with .env mention", "spawn_elfs", `{"tasks":[{"task":"check .env config","elf":"worker"}]}`},
		{"spawn_elfs with credentials mention", "spawn_elfs", `{"tasks":[{"task":"read credentials file","elf":"worker"}]}`},
		{"agent with .env mention", "agent", `{"prompt":"verify .env is configured correctly"}`},
		{"agent with ssh mention", "agent", `{"prompt":"check .ssh/config for proxy settings"}`},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			err := c.Check(context.Background(), ToolInfo{Name: tt.toolName}, json.RawMessage(tt.args))
			if err != nil {
				t.Errorf("orchestration tool %q should not be blocked by safety check: %v", tt.toolName, err)
			}
		})
	}

	// Non-orchestration tools with the same patterns are still blocked.
	t.Run("bash with .env still blocked", func(t *testing.T) {
		err := c.Check(context.Background(), ToolInfo{Name: "bash"}, json.RawMessage(`{"command":"cat .env"}`))
		if !errors.Is(err, ErrDenied) {
			t.Errorf("bash accessing .env should still be blocked: %v", err)
		}
	})
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

func TestChecker_ConcurrentSetModeAndCheck(t *testing.T) {
	// Verifies no data race between SetMode (TUI goroutine) and Check (engine goroutine).
	// Run with: go test -race ./internal/permission/...
	c := NewChecker(ModeDefault, nil, nil)
	ctx := context.Background()
	info := ToolInfo{Name: "bash", IsReadOnly: true}
	args := json.RawMessage(`{}`)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 1000; i++ {
			c.SetMode(ModeAuto)
			c.SetMode(ModeDefault)
		}
	}()

	for i := 0; i < 1000; i++ {
		c.Check(ctx, info, args) //nolint:errcheck
	}
	<-done
}

// Pins the documented Rule.Pattern semantics so any future refactor that
// changes substring/case/whitespace behaviour will fail loudly. Audit H1.
//
// Uses ModeBypass so the only deny vector is the rule itself (no mode-policy
// or no-prompt fallback can mask the result). Pattern uses a synthetic token
// to avoid overlap with safetyDenyPatterns.
func TestRule_PatternSemantics_SubstringExactCaseSensitive(t *testing.T) {
	const pattern = "deploy --prod"
	rules := []Rule{{Tool: "bash", Pattern: pattern, Action: ActionDeny}}
	c := NewChecker(ModeBypass, rules, nil)
	ctx := context.Background()
	info := ToolInfo{Name: "bash"}

	matches := []string{
		`{"command":"deploy --prod"}`,
		`{"command":"sudo deploy --prod"}`,
		`{"prefix":"x","command":"deploy --prod -y"}`,
	}
	for _, args := range matches {
		t.Run("matches: "+args, func(t *testing.T) {
			err := c.Check(ctx, info, json.RawMessage(args))
			if !errors.Is(err, ErrDenied) {
				t.Errorf("expected deny for %s, got %v", args, err)
			}
		})
	}

	// Documented gotchas — these intentionally do NOT match. The Pattern is a
	// case-sensitive, exact substring; any whitespace or case drift skips it.
	misses := []string{
		`{"command":"deploy  --prod"}`, // double space inside the literal
		`{"command":"DEPLOY --PROD"}`,  // case differs
		`{"command":"deploy\t--prod"}`, // tab instead of space
	}
	for _, args := range misses {
		t.Run("does NOT match: "+args, func(t *testing.T) {
			err := c.Check(ctx, info, json.RawMessage(args))
			if errors.Is(err, ErrDenied) {
				t.Errorf("unexpectedly denied %s — Pattern semantics may have changed", args)
			}
		})
	}
}
