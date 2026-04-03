package bash

import "testing"

func TestValidateCommand_Valid(t *testing.T) {
	valid := []string{
		"echo hello",
		"ls -la",
		"cat /etc/hostname",
		"go test ./...",
		"git status",
		"echo 'hello world'",
		`echo "hello world"`,
		"grep -r 'pattern' .",
		"find . -name '*.go'",
	}
	for _, cmd := range valid {
		if v := ValidateCommand(cmd); v != nil {
			t.Errorf("ValidateCommand(%q) = %v, want nil", cmd, v)
		}
	}
}

func TestValidateCommand_Empty(t *testing.T) {
	v := ValidateCommand("")
	if v == nil {
		t.Fatal("expected violation for empty command")
	}
	if v.Check != CheckIncomplete {
		t.Errorf("Check = %d, want %d (incomplete)", v.Check, CheckIncomplete)
	}
}

func TestCheckIncomplete(t *testing.T) {
	tests := []struct {
		cmd  string
		want SecurityCheck
	}{
		{"\techo hello", CheckIncomplete},     // tab start
		{"-flag value", CheckIncomplete},       // flag start
		{"echo hello |", CheckIncomplete},      // trailing pipe
		{"echo hello &", CheckIncomplete},      // trailing ampersand
		{"echo hello ;", CheckIncomplete},      // trailing semicolon
	}
	for _, tt := range tests {
		v := ValidateCommand(tt.cmd)
		if v == nil {
			t.Errorf("ValidateCommand(%q) = nil, want check %d", tt.cmd, tt.want)
			continue
		}
		if v.Check != tt.want {
			t.Errorf("ValidateCommand(%q).Check = %d, want %d", tt.cmd, v.Check, tt.want)
		}
	}
}

func TestCheckControlChars(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
	}{
		{"null byte", "echo hello\x00world"},
		{"bell", "echo \x07"},
		{"backspace", "echo \x08"},
		{"escape", "echo \x1b[31m"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := ValidateCommand(tt.cmd)
			if v == nil {
				t.Error("expected violation")
				return
			}
			if v.Check != CheckControlChars {
				t.Errorf("Check = %d, want %d (control chars)", v.Check, CheckControlChars)
			}
		})
	}
}

func TestCheckControlChars_AllowedChars(t *testing.T) {
	// Tabs and newlines inside quotes are allowed
	valid := []string{
		"echo 'hello\tworld'",
	}
	for _, cmd := range valid {
		if v := checkControlChars(cmd); v != nil {
			t.Errorf("checkControlChars(%q) = %v, want nil", cmd, v)
		}
	}
}

func TestCheckNewlineInjection(t *testing.T) {
	// Unquoted newline
	v := checkNewlineInjection("echo hello\nrm -rf /")
	if v == nil {
		t.Fatal("expected violation for unquoted newline")
	}
	if v.Check != CheckNewlineInjection {
		t.Errorf("Check = %d, want %d", v.Check, CheckNewlineInjection)
	}
}

func TestCheckNewlineInjection_QuotedOK(t *testing.T) {
	// Newlines inside quotes are fine
	allowed := []string{
		"echo 'hello\nworld'",
		`echo "hello` + "\n" + `world"`,
	}
	for _, cmd := range allowed {
		if v := checkNewlineInjection(cmd); v != nil {
			t.Errorf("checkNewlineInjection(%q) = %v, want nil", cmd, v)
		}
	}
}

func TestCheckCmdSubstitution(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
	}{
		{"backtick", "echo `whoami`"},
		{"dollar paren", "echo $(whoami)"},
		{"dollar brace", "echo ${HOME}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := ValidateCommand(tt.cmd)
			if v == nil {
				t.Error("expected violation")
				return
			}
			if v.Check != CheckCmdSubstitution {
				t.Errorf("Check = %d, want %d", v.Check, CheckCmdSubstitution)
			}
		})
	}
}

func TestCheckCmdSubstitution_SingleQuoteOK(t *testing.T) {
	// Inside single quotes, everything is literal
	safe := "echo '$(whoami) and `uname` and ${HOME}'"
	if v := checkCmdSubstitution(safe); v != nil {
		t.Errorf("checkCmdSubstitution(%q) = %v, want nil (single-quoted)", safe, v)
	}
}

func TestCheckDangerousVars(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
	}{
		{"IFS at start", "IFS=: read a b"},
		{"PATH manipulation", "PATH=/tmp:$PATH command"},
		{"ifs with space prefix", " IFS=x echo hi"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := ValidateCommand(tt.cmd)
			if v == nil {
				t.Error("expected violation")
				return
			}
			if v.Check != CheckDangerousVars {
				t.Errorf("Check = %d, want %d", v.Check, CheckDangerousVars)
			}
		})
	}
}

func TestCheckDangerousVars_SafeSubstrings(t *testing.T) {
	// "SWIFT=..." should not trigger PATH check, "TARIFFS=..." should not trigger IFS
	safe := []string{
		"echo SWIFT=enabled",
		"TARIFFS=high echo test",
	}
	for _, cmd := range safe {
		if v := checkDangerousVars(cmd); v != nil {
			t.Errorf("checkDangerousVars(%q) = %v, want nil", cmd, v)
		}
	}
}
