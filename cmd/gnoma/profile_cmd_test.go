package main

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	gnomacfg "somegit.dev/Owlibou/gnoma/internal/config"
)

func TestArgsWithProfileReplaced(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		next string
		want []string
	}{
		{
			name: "no_prior_profile_flag",
			in:   []string{"gnoma", "--verbose"},
			next: "private",
			want: []string{"--verbose", "--profile", "private"},
		},
		{
			name: "replaces_double_dash_pair",
			in:   []string{"gnoma", "--profile", "work", "--verbose"},
			next: "private",
			want: []string{"--verbose", "--profile", "private"},
		},
		{
			name: "replaces_double_dash_equals",
			in:   []string{"gnoma", "--profile=work", "--max-turns", "50"},
			next: "private",
			want: []string{"--max-turns", "50", "--profile", "private"},
		},
		{
			name: "replaces_single_dash_pair",
			in:   []string{"gnoma", "-profile", "work"},
			next: "private",
			want: []string{"--profile", "private"},
		},
		{
			name: "replaces_single_dash_equals",
			in:   []string{"gnoma", "-profile=work"},
			next: "private",
			want: []string{"--profile", "private"},
		},
		{
			name: "preserves_positional_args",
			in:   []string{"gnoma", "providers"},
			next: "work",
			want: []string{"providers", "--profile", "work"},
		},
		{
			name: "preserves_mixed_flags_and_positional",
			in:   []string{"gnoma", "--verbose", "--profile", "old", "profile", "show", "work"},
			next: "new",
			want: []string{"--verbose", "profile", "show", "work", "--profile", "new"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := argsWithProfileReplaced(tc.in, tc.next)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("argsWithProfileReplaced(%v, %q) = %v, want %v", tc.in, tc.next, got, tc.want)
			}
		})
	}
}

func TestReExecForProfileSwitch_RejectsBadName(t *testing.T) {
	// Belt-and-braces validation: bad names must be refused even if the
	// caller skipped upstream validation.
	cases := []string{"", "../foo", "foo bar", "foo;rm", "../../etc/passwd"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			err := reExecForProfileSwitch(name)
			if err == nil {
				t.Errorf("expected error for %q, got nil", name)
			}
		})
	}
}

func TestFormatProfileList_NoProfilesDir(t *testing.T) {
	var buf bytes.Buffer
	formatProfileList(&buf, nil, false, "", "", "/home/x/.config/gnoma/profiles", "/home/x/.config/gnoma/config.toml")
	out := buf.String()
	if !strings.Contains(out, "not enabled") {
		t.Errorf("expected hint about profiles not being enabled, got:\n%s", out)
	}
	if !strings.Contains(out, "/home/x/.config/gnoma/profiles") {
		t.Errorf("expected hint to mention the expected profiles directory path, got:\n%s", out)
	}
}

func TestFormatProfileList_Markers(t *testing.T) {
	var buf bytes.Buffer
	formatProfileList(&buf,
		[]string{"experiment", "private", "work"},
		true,      // dirExists
		"work",    // defaultName
		"private", // activeName (e.g. --profile private set)
		"/home/x/.config/gnoma/profiles",
		"/home/x/.config/gnoma/config.toml",
	)
	out := buf.String()

	// All three profile names should appear.
	for _, want := range []string{"experiment", "private", "work"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing profile %q:\n%s", want, out)
		}
	}
	// Markers: work is default, private is active.
	if !strings.Contains(out, "work") || !strings.Contains(out, "default") {
		t.Errorf("expected 'default' marker next to work, got:\n%s", out)
	}
	if !strings.Contains(out, "active") {
		t.Errorf("expected 'active' marker, got:\n%s", out)
	}
}

func TestFormatProfileList_DefaultEqualsActive(t *testing.T) {
	var buf bytes.Buffer
	formatProfileList(&buf,
		[]string{"work"},
		true,
		"work", // default
		"work", // active (no --profile, falling through to default)
		"/cfg/profiles",
		"/cfg/config.toml",
	)
	out := buf.String()
	// Both markers should appear on the same line.
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "work") && strings.Contains(line, "default") {
			if !strings.Contains(line, "active") {
				t.Errorf("when default==active, the same line should carry both markers, got:\n%s", line)
			}
			return
		}
	}
	t.Errorf("did not find a line with both default and active markers:\n%s", out)
}

func TestFormatProfileList_DefaultMissing(t *testing.T) {
	var buf bytes.Buffer
	formatProfileList(&buf,
		[]string{"work"},
		true,
		"ghost", // default points at a file that doesn't exist
		"",
		"/cfg/profiles",
		"/cfg/config.toml",
	)
	out := buf.String()
	if !strings.Contains(out, "ghost") || !strings.Contains(out, "missing") {
		t.Errorf("expected 'ghost (default, missing)' diagnostic, got:\n%s", out)
	}
}

func TestFormatProfileShow_PopulatedConfig(t *testing.T) {
	cfg := gnomacfg.Defaults()
	cfg.Provider.Default = "anthropic"
	cfg.Provider.APIKeys["anthropic"] = "sk-test"
	cfg.Provider.Endpoints["ollama"] = "http://localhost:11434/v1"

	cfg.CLIAgents = gnomacfg.CLIAgentsSection{
		"claude": "claude-work",
		"gemini": "", // canonical
	}
	cfg.Permission.Mode = "default"
	cfg.Permission.Rules = []gnomacfg.PermissionRule{
		{Tool: "bash", Pattern: "rm *", Action: "deny"},
	}
	cfg.Arms = []gnomacfg.ArmConfig{
		{ID: "anthropic/opus", CostWeight: 0.3},
		{ID: "openai/gpt", CostWeight: 1.0},
	}
	cfg.MCPServers = []gnomacfg.MCPServerConfig{
		{Name: "git", Command: "mcp-git"},
		{Name: "fs", Command: "mcp-fs"},
	}
	cfg.Plugins.Enabled = []string{"git-tools"}
	cfg.Router.ForceTwoStage = true

	prof := gnomacfg.Profile{Active: true, Name: "work"}

	var buf bytes.Buffer
	formatProfileShow(&buf, &cfg, prof, "/p/work.toml", "/p/config.toml", "/p", "/r")
	out := buf.String()

	wantSubstrs := []string{
		"endpoints",
		"ollama", // endpoint name listed
		"gemini = (canonical)",
		"claude = claude-work",
		"rules: 1",
		"anthropic/opus",
		"openai/gpt",
		"git", "fs", // MCP server names
		"force_two_stage = true",
		"Plugins enabled (1)",
	}
	for _, s := range wantSubstrs {
		if !strings.Contains(out, s) {
			t.Errorf("output missing %q:\n%s", s, out)
		}
	}
	// API key value must not leak.
	if strings.Contains(out, "sk-test") {
		t.Errorf("API key value leaked:\n%s", out)
	}
}

func TestFormatProfileShow_Headers(t *testing.T) {
	cfg := gnomacfg.Defaults()
	cfg.Provider.Default = "anthropic"
	cfg.Provider.Model = "claude-sonnet-4"
	cfg.Provider.APIKeys["anthropic"] = "sk-test"
	cfg.CLIAgents = gnomacfg.CLIAgentsSection{"claude": "claude-work"}
	cfg.Permission.Mode = "default"
	cfg.SLM.Enabled = true
	cfg.SLM.Backend = "ollama"
	cfg.SLM.Model = "reecdev/tiny3.5:1.5b"

	prof := gnomacfg.Profile{Active: true, Name: "work"}

	var buf bytes.Buffer
	formatProfileShow(&buf, &cfg, prof,
		"/home/x/.config/gnoma/profiles/work.toml",
		"/home/x/.config/gnoma/config.toml",
		"/home/x/.config/gnoma",
		"/repo",
	)
	out := buf.String()

	wantSubstrs := []string{
		"Profile: work",
		"profiles/work.toml",
		"config.toml",
		"anthropic",
		"claude-sonnet-4",
		"claude-work",
		"ollama",
		"reecdev/tiny3.5:1.5b",
		"quality-work.json",
		"sessions/work",
	}
	for _, s := range wantSubstrs {
		if !strings.Contains(out, s) {
			t.Errorf("output missing %q:\n%s", s, out)
		}
	}
}

func TestFormatProfileShow_LegacyMode(t *testing.T) {
	// When profile.Active is false (no profiles/ dir), show should still
	// work and surface the legacy paths.
	cfg := gnomacfg.Defaults()
	cfg.Provider.Default = "anthropic"
	prof := gnomacfg.Profile{Active: false}

	var buf bytes.Buffer
	formatProfileShow(&buf, &cfg, prof,
		"",
		"/home/x/.config/gnoma/config.toml",
		"/home/x/.config/gnoma",
		"/repo",
	)
	out := buf.String()
	if !strings.Contains(out, "quality.json") || strings.Contains(out, "quality-.json") {
		t.Errorf("legacy mode should use unsuffixed quality.json, got:\n%s", out)
	}
	if strings.Contains(out, "sessions/") && !strings.Contains(out, ".gnoma/sessions") {
		t.Errorf("legacy mode should show plain sessions/ path:\n%s", out)
	}
}

func TestFormatProfileShow_RedactsAPIKeys(t *testing.T) {
	// API keys should never leak into 'gnoma profile show' output —
	// users may pipe this into chat or paste it for help.
	cfg := gnomacfg.Defaults()
	cfg.Provider.APIKeys["anthropic"] = "sk-secret-NEVER-LEAK-123456789"
	prof := gnomacfg.Profile{Active: true, Name: "work"}

	var buf bytes.Buffer
	formatProfileShow(&buf, &cfg, prof, "/p/work.toml", "/p/config.toml", "/p", "/r")
	out := buf.String()
	if strings.Contains(out, "sk-secret-NEVER-LEAK-123456789") {
		t.Errorf("API key value leaked to profile show output:\n%s", out)
	}
	// But the key name should be listed so users know what's configured.
	if !strings.Contains(out, "anthropic") {
		t.Errorf("provider name (anthropic) should be listed:\n%s", out)
	}
}
