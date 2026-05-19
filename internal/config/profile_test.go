package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// profileTestEnv stages a fake $XDG_CONFIG_HOME with a base config and an
// optional profiles directory, then chdirs into a project dir. Callers
// supply the base config TOML and a map of profile-name → profile TOML.
// An empty profiles map means no profiles/ directory is created at all
// (legacy fallback). To create profiles/ but leave it empty, pass a non-nil
// empty map.
func profileTestEnv(t *testing.T, baseTOML string, profiles map[string]string) (projectDir string) {
	t.Helper()
	globalDir := t.TempDir()
	gnomaDir := filepath.Join(globalDir, "gnoma")
	if err := os.MkdirAll(gnomaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if baseTOML != "" {
		if err := os.WriteFile(filepath.Join(gnomaDir, "config.toml"), []byte(baseTOML), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if profiles != nil {
		profilesDir := filepath.Join(gnomaDir, "profiles")
		if err := os.MkdirAll(profilesDir, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, body := range profiles {
			if err := os.WriteFile(filepath.Join(profilesDir, name+".toml"), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}

	projectDir = t.TempDir()
	pGnomaDir := filepath.Join(projectDir, ".gnoma")
	if err := os.MkdirAll(pGnomaDir, 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("XDG_CONFIG_HOME", globalDir)
	origDir, _ := os.Getwd()
	_ = os.Chdir(projectDir)
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	return projectDir
}

func TestLoadWithProfile_NoProfilesDir_FallsBackToLegacy(t *testing.T) {
	profileTestEnv(t, `
[provider]
default = "anthropic"
`, nil)

	cfg, prof, err := LoadWithProfile("")
	if err != nil {
		t.Fatalf("LoadWithProfile: %v", err)
	}
	if prof.Active {
		t.Errorf("Profile.Active = true, want false when no profiles/ dir")
	}
	if cfg.Provider.Default != "anthropic" {
		t.Errorf("Provider.Default = %q, want anthropic", cfg.Provider.Default)
	}
}

func TestLoadWithProfile_NoProfilesDir_FlagSet_Errors(t *testing.T) {
	profileTestEnv(t, `[provider]
default = "anthropic"
`, nil)

	_, _, err := LoadWithProfile("work")
	if err == nil {
		t.Fatal("expected error when --profile is set but no profiles/ dir exists")
	}
	if !strings.Contains(err.Error(), "work") {
		t.Errorf("error should mention requested profile name, got: %v", err)
	}
}

func TestLoadWithProfile_ProfilesDir_NoFlag_NoDefault_Errors(t *testing.T) {
	profileTestEnv(t, `[provider]
default = "anthropic"
`, map[string]string{
		"work":    `[provider]` + "\n" + `default = "openai"`,
		"private": `[provider]` + "\n" + `default = "mistral"`,
	})

	_, _, err := LoadWithProfile("")
	if err == nil {
		t.Fatal("expected error when profiles/ exists but no default_profile or flag")
	}
	if !strings.Contains(err.Error(), "work") || !strings.Contains(err.Error(), "private") {
		t.Errorf("error should list available profiles, got: %v", err)
	}
}

func TestLoadWithProfile_DefaultProfileFromBase(t *testing.T) {
	profileTestEnv(t, `default_profile = "work"

[provider]
default = "anthropic"
`, map[string]string{
		"work": `[provider]
default = "openai"
`,
	})

	cfg, prof, err := LoadWithProfile("")
	if err != nil {
		t.Fatalf("LoadWithProfile: %v", err)
	}
	if !prof.Active || prof.Name != "work" {
		t.Errorf("Profile = %+v, want {work true}", prof)
	}
	if cfg.Provider.Default != "openai" {
		t.Errorf("Provider.Default = %q, want openai (profile override)", cfg.Provider.Default)
	}
}

func TestLoadWithProfile_FlagOverridesDefault(t *testing.T) {
	profileTestEnv(t, `default_profile = "work"

[provider]
default = "anthropic"
`, map[string]string{
		"work":    `[provider]` + "\n" + `default = "openai"`,
		"private": `[provider]` + "\n" + `default = "mistral"`,
	})

	cfg, prof, err := LoadWithProfile("private")
	if err != nil {
		t.Fatalf("LoadWithProfile: %v", err)
	}
	if prof.Name != "private" {
		t.Errorf("Profile.Name = %q, want private", prof.Name)
	}
	if cfg.Provider.Default != "mistral" {
		t.Errorf("Provider.Default = %q, want mistral", cfg.Provider.Default)
	}
}

func TestLoadWithProfile_FlagProfileMissing_Errors(t *testing.T) {
	profileTestEnv(t, `[provider]
default = "anthropic"
`, map[string]string{
		"work": `[provider]` + "\n" + `default = "openai"`,
	})

	_, _, err := LoadWithProfile("nonexistent")
	if err == nil {
		t.Fatal("expected error when --profile points to missing file")
	}
	if !strings.Contains(err.Error(), "nonexistent") || !strings.Contains(err.Error(), "work") {
		t.Errorf("error should mention bad name and list available, got: %v", err)
	}
}

func TestLoadWithProfile_DefaultProfileMissing_Errors(t *testing.T) {
	profileTestEnv(t, `default_profile = "ghost"

[provider]
default = "anthropic"
`, map[string]string{
		"work": `[provider]` + "\n" + `default = "openai"`,
	})

	_, _, err := LoadWithProfile("")
	if err == nil {
		t.Fatal("expected error when default_profile points to missing file")
	}
	if !strings.Contains(err.Error(), "ghost") {
		t.Errorf("error should mention bad default_profile name, got: %v", err)
	}
}

func TestLoadWithProfile_MergesScalarAndMaps(t *testing.T) {
	profileTestEnv(t, `default_profile = "work"

[provider]
default = "anthropic"
model = "claude-base"
max_tokens = 4096

[provider.api_keys]
anthropic = "BASE_A"
openai    = "BASE_O"

[cli_agents]
claude = "claude-base"
gemini = "gemini-base"
`, map[string]string{
		"work": `[provider]
default = "openai"

[provider.api_keys]
mistral = "PROFILE_M"

[cli_agents]
claude = "claude-work"
`,
	})

	cfg, _, err := LoadWithProfile("")
	if err != nil {
		t.Fatalf("LoadWithProfile: %v", err)
	}
	// Profile overrode default.
	if cfg.Provider.Default != "openai" {
		t.Errorf("Default = %q, want openai", cfg.Provider.Default)
	}
	// Profile didn't touch model/max_tokens — base wins.
	if cfg.Provider.Model != "claude-base" {
		t.Errorf("Model = %q, want claude-base (base preserved)", cfg.Provider.Model)
	}
	if cfg.Provider.MaxTokens != 4096 {
		t.Errorf("MaxTokens = %d, want 4096 (base preserved)", cfg.Provider.MaxTokens)
	}
	// Map per-key merge.
	if cfg.Provider.APIKeys["anthropic"] != "BASE_A" {
		t.Errorf("APIKeys[anthropic] = %q, want BASE_A", cfg.Provider.APIKeys["anthropic"])
	}
	if cfg.Provider.APIKeys["openai"] != "BASE_O" {
		t.Errorf("APIKeys[openai] = %q, want BASE_O", cfg.Provider.APIKeys["openai"])
	}
	if cfg.Provider.APIKeys["mistral"] != "PROFILE_M" {
		t.Errorf("APIKeys[mistral] = %q, want PROFILE_M", cfg.Provider.APIKeys["mistral"])
	}
	// cli_agents map per-key.
	if cfg.CLIAgents["claude"] != "claude-work" {
		t.Errorf("CLIAgents[claude] = %q, want claude-work", cfg.CLIAgents["claude"])
	}
	if cfg.CLIAgents["gemini"] != "gemini-base" {
		t.Errorf("CLIAgents[gemini] = %q, want gemini-base", cfg.CLIAgents["gemini"])
	}
}

func TestLoadWithProfile_ArmsMerge_AppendByID(t *testing.T) {
	profileTestEnv(t, `default_profile = "work"

[[arms]]
id = "anthropic/opus"
cost_weight = 1.0
strengths = ["planning"]

[[arms]]
id = "openai/gpt"
cost_weight = 0.8
`, map[string]string{
		"work": `
[[arms]]
id = "anthropic/opus"
cost_weight = 0.3

[[arms]]
id = "mistral/large"
cost_weight = 0.5
`,
	})

	cfg, _, err := LoadWithProfile("")
	if err != nil {
		t.Fatalf("LoadWithProfile: %v", err)
	}
	if len(cfg.Arms) != 3 {
		t.Fatalf("len(Arms) = %d, want 3, got %+v", len(cfg.Arms), cfg.Arms)
	}
	byID := make(map[string]ArmConfig, 3)
	for _, a := range cfg.Arms {
		byID[a.ID] = a
	}
	// anthropic/opus: profile overrides cost_weight (and strengths get
	// dropped because profile replaced the entry).
	if byID["anthropic/opus"].CostWeight != 0.3 {
		t.Errorf("opus.CostWeight = %v, want 0.3 (profile override)", byID["anthropic/opus"].CostWeight)
	}
	// openai/gpt: untouched by profile.
	if byID["openai/gpt"].CostWeight != 0.8 {
		t.Errorf("gpt.CostWeight = %v, want 0.8 (base preserved)", byID["openai/gpt"].CostWeight)
	}
	// mistral/large: new from profile.
	if byID["mistral/large"].CostWeight != 0.5 {
		t.Errorf("mistral.CostWeight = %v, want 0.5 (profile new)", byID["mistral/large"].CostWeight)
	}
}

func TestLoadWithProfile_HooksMerge_Append(t *testing.T) {
	profileTestEnv(t, `default_profile = "work"

[[hooks]]
name = "base-hook"
event = "pre_tool_use"
type = "command"
exec = "echo base"
`, map[string]string{
		"work": `
[[hooks]]
name = "profile-hook"
event = "post_tool_use"
type = "command"
exec = "echo profile"
`,
	})

	cfg, _, err := LoadWithProfile("")
	if err != nil {
		t.Fatalf("LoadWithProfile: %v", err)
	}
	if len(cfg.Hooks) != 2 {
		t.Fatalf("len(Hooks) = %d, want 2", len(cfg.Hooks))
	}
	if cfg.Hooks[0].Name != "base-hook" {
		t.Errorf("Hooks[0] = %q, want base-hook first", cfg.Hooks[0].Name)
	}
	if cfg.Hooks[1].Name != "profile-hook" {
		t.Errorf("Hooks[1] = %q, want profile-hook second", cfg.Hooks[1].Name)
	}
}

func TestLoadWithProfile_MCPServersMerge_AppendByName(t *testing.T) {
	profileTestEnv(t, `default_profile = "work"

[[mcp_servers]]
name = "git"
command = "mcp-git-base"

[[mcp_servers]]
name = "shared"
command = "mcp-shared"
`, map[string]string{
		"work": `
[[mcp_servers]]
name = "git"
command = "mcp-git-work"

[[mcp_servers]]
name = "extra"
command = "mcp-extra"
`,
	})

	cfg, _, err := LoadWithProfile("")
	if err != nil {
		t.Fatalf("LoadWithProfile: %v", err)
	}
	if len(cfg.MCPServers) != 3 {
		t.Fatalf("len(MCPServers) = %d, want 3, got %+v", len(cfg.MCPServers), cfg.MCPServers)
	}
	byName := make(map[string]MCPServerConfig, 3)
	for _, s := range cfg.MCPServers {
		byName[s.Name] = s
	}
	if byName["git"].Command != "mcp-git-work" {
		t.Errorf("git.Command = %q, want mcp-git-work (profile override)", byName["git"].Command)
	}
	if byName["shared"].Command != "mcp-shared" {
		t.Errorf("shared.Command = %q, want mcp-shared (base preserved)", byName["shared"].Command)
	}
	if byName["extra"].Command != "mcp-extra" {
		t.Errorf("extra.Command = %q, want mcp-extra (profile new)", byName["extra"].Command)
	}
}

func TestLoadWithProfile_EnvOverridesProfile(t *testing.T) {
	profileTestEnv(t, `default_profile = "work"
`, map[string]string{
		"work": `
[provider.api_keys]
anthropic = "PROFILE_KEY"
`,
	})
	t.Setenv("ANTHROPIC_API_KEY", "ENV_KEY")

	cfg, _, err := LoadWithProfile("")
	if err != nil {
		t.Fatalf("LoadWithProfile: %v", err)
	}
	if cfg.Provider.APIKeys["anthropic"] != "ENV_KEY" {
		t.Errorf("APIKeys[anthropic] = %q, want ENV_KEY", cfg.Provider.APIKeys["anthropic"])
	}
}

func TestProfile_QualityFile(t *testing.T) {
	globalDir := "/test/gnoma"
	cases := []struct {
		name string
		prof Profile
		want string
	}{
		{"legacy", Profile{Active: false}, "/test/gnoma/quality.json"},
		{"active_work", Profile{Active: true, Name: "work"}, "/test/gnoma/quality-work.json"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.prof.QualityFile(globalDir); got != tc.want {
				t.Errorf("QualityFile = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestProfile_SessionDir(t *testing.T) {
	root := "/repo"
	cases := []struct {
		name string
		prof Profile
		want string
	}{
		{"legacy", Profile{Active: false}, "/repo/.gnoma/sessions"},
		{"active_private", Profile{Active: true, Name: "private"}, "/repo/.gnoma/sessions/private"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.prof.SessionDir(root); got != tc.want {
				t.Errorf("SessionDir = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestListProfiles(t *testing.T) {
	profileTestEnv(t, ``, map[string]string{
		"alpha": `[provider]
default = "anthropic"`,
		"beta":  `[provider]` + "\n" + `default = "openai"`,
		"gamma": `[provider]` + "\n" + `default = "mistral"`,
	})

	names, err := ListProfiles()
	if err != nil {
		t.Fatalf("ListProfiles: %v", err)
	}
	if len(names) != 3 || names[0] != "alpha" || names[1] != "beta" || names[2] != "gamma" {
		t.Errorf("ListProfiles = %v, want [alpha beta gamma]", names)
	}
}

func TestListProfiles_NoDir(t *testing.T) {
	profileTestEnv(t, ``, nil)

	names, err := ListProfiles()
	if err != nil {
		t.Fatalf("ListProfiles: %v", err)
	}
	if names != nil {
		t.Errorf("ListProfiles = %v, want nil when no profiles/ dir", names)
	}
}

func TestLoadWithProfile_RejectsTraversalNames(t *testing.T) {
	profileTestEnv(t, ``, map[string]string{
		"work": `[provider]` + "\n" + `default = "openai"`,
	})

	cases := []string{
		"../foo",
		"foo/bar",
		"foo\\bar",
		"..",
		".hidden",
		"work .toml", // space is rejected too
	}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			_, _, err := LoadWithProfile(name)
			if err == nil {
				t.Fatalf("LoadWithProfile(%q) should error, got nil", name)
			}
			if !errors.Is(err, ErrProfileResolution) {
				t.Errorf("error %v does not wrap ErrProfileResolution", err)
			}
		})
	}
}

func TestLoadWithProfile_RejectsTraversalInDefaultProfile(t *testing.T) {
	profileTestEnv(t, `default_profile = "../foo"
`, map[string]string{
		"work": `[provider]` + "\n" + `default = "openai"`,
	})

	_, _, err := LoadWithProfile("")
	if err == nil {
		t.Fatal("expected error when default_profile contains traversal")
	}
	if !errors.Is(err, ErrProfileResolution) {
		t.Errorf("error %v does not wrap ErrProfileResolution", err)
	}
}

func TestLoadWithProfile_ResolutionErrors_AreSentinel(t *testing.T) {
	// All three resolution-failure paths must wrap ErrProfileResolution
	// so the caller in cmd/gnoma can distinguish actionable user errors
	// from generic config failures.
	cases := []struct {
		name     string
		base     string
		profiles map[string]string
		flag     string
	}{
		{
			name:     "flag_set_no_profiles_dir",
			base:     `[provider]` + "\n" + `default = "anthropic"`,
			profiles: nil,
			flag:     "work",
		},
		{
			name:     "dir_present_no_flag_no_default",
			base:     `[provider]` + "\n" + `default = "anthropic"`,
			profiles: map[string]string{"work": `[provider]` + "\n" + `default = "openai"`},
			flag:     "",
		},
		{
			name:     "flag_set_profile_missing",
			base:     `[provider]` + "\n" + `default = "anthropic"`,
			profiles: map[string]string{"work": `[provider]` + "\n" + `default = "openai"`},
			flag:     "missing",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			profileTestEnv(t, tc.base, tc.profiles)
			_, _, err := LoadWithProfile(tc.flag)
			if err == nil {
				t.Fatal("expected error")
			}
			if !errors.Is(err, ErrProfileResolution) {
				t.Errorf("error %v does not wrap ErrProfileResolution", err)
			}
		})
	}
}

func TestLoad_BackwardCompat(t *testing.T) {
	// Existing Load() callers should keep working when no profiles/ dir
	// exists. This guards against accidentally breaking the migration
	// path described in the post-SLM plan.
	profileTestEnv(t, `[provider]
default = "anthropic"
`, nil)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Provider.Default != "anthropic" {
		t.Errorf("Provider.Default = %q, want anthropic", cfg.Provider.Default)
	}
}
