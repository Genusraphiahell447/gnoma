package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	gnomacfg "somegit.dev/Owlibou/gnoma/internal/config"
)

// pf and pln write formatted output to w, swallowing the error returns
// io.Writer surfaces. Profile rendering goes to stdout or an in-memory
// buffer; neither write path produces errors worth handling here.
func pf(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}

func pln(w io.Writer, args ...any) {
	_, _ = fmt.Fprintln(w, args...)
}

// runProfileCommand handles `gnoma profile <subcommand>`. Profile is the
// already-resolved active profile (or zero-value Profile if profile mode
// is not engaged).
func runProfileCommand(args []string, cfg *gnomacfg.Config, profile gnomacfg.Profile) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: gnoma profile <command>")
		fmt.Fprintln(os.Stderr, "commands:")
		fmt.Fprintln(os.Stderr, "  list           list profiles in ~/.config/gnoma/profiles/")
		fmt.Fprintln(os.Stderr, "  show <name>    show the effective config a profile produces")
		return 1
	}
	switch args[0] {
	case "list":
		return runProfileList(cfg, profile)
	case "show":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: gnoma profile show <name>")
			return 1
		}
		return runProfileShow(args[1])
	default:
		fmt.Fprintf(os.Stderr, "unknown profile command: %s\n", args[0])
		return 1
	}
}

func runProfileList(cfg *gnomacfg.Config, active gnomacfg.Profile) int {
	profilesDir := filepath.Join(gnomacfg.GlobalConfigDir(), "profiles")
	baseConfigPath := gnomacfg.GlobalConfigPath()

	dirExists := false
	if st, err := os.Stat(profilesDir); err == nil && st.IsDir() {
		dirExists = true
	}

	names, err := gnomacfg.ListProfiles()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: list profiles: %v\n", err)
		return 1
	}

	activeName := ""
	if active.Active {
		activeName = active.Name
	}

	formatProfileList(os.Stdout, names, dirExists, cfg.DefaultProfile, activeName, profilesDir, baseConfigPath)
	return 0
}

// formatProfileList writes a profile listing to w. Extracted for testing.
func formatProfileList(w io.Writer, names []string, dirExists bool, defaultName, activeName, profilesDir, baseConfigPath string) {
	if !dirExists {
		pln(w, "Profile mode is not enabled.")
		pln(w)
		pln(w, "To enable profiles, create the directory:")
		pf(w, "  mkdir -p %s\n", profilesDir)
		pln(w, "Then add profile files, e.g.:")
		pf(w, "  %s/work.toml\n", profilesDir)
		pln(w)
		pln(w, "See docs/profiles.md for examples.")
		return
	}

	pf(w, "Profiles in %s:\n\n", profilesDir)
	if len(names) == 0 {
		pln(w, "  (none — directory is empty)")
		pln(w)
		pf(w, "Base config: %s\n", baseConfigPath)
		return
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	present := make(map[string]bool, len(names))
	for _, n := range names {
		present[n] = true
		markers := profileMarkers(n, defaultName, activeName)
		if markers != "" {
			pf(tw, "  %s\t(%s)\n", n, markers)
		} else {
			pf(tw, "  %s\t\n", n)
		}
	}
	// If default_profile names a profile file that doesn't exist, surface
	// it explicitly so `profile list` doubles as a diagnostic tool.
	if defaultName != "" && !present[defaultName] {
		pf(tw, "  %s\t(default, missing)\n", defaultName)
	}
	_ = tw.Flush()

	pln(w)
	pf(w, "Base config: %s\n", baseConfigPath)
}

func profileMarkers(name, defaultName, activeName string) string {
	var parts []string
	if name == defaultName {
		parts = append(parts, "default")
	}
	if name == activeName {
		parts = append(parts, "active")
	}
	return strings.Join(parts, ", ")
}

func runProfileShow(name string) int {
	cfg, profile, err := gnomacfg.LoadWithProfile(name)
	if err != nil {
		if errors.Is(err, gnomacfg.ErrProfileResolution) {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 2
		}
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	profilesDir := filepath.Join(gnomacfg.GlobalConfigDir(), "profiles")
	profilePath := ""
	if profile.Active {
		profilePath = filepath.Join(profilesDir, profile.Name+".toml")
	}
	baseConfigPath := gnomacfg.GlobalConfigPath()
	globalDir := gnomacfg.GlobalConfigDir()
	projectRoot := gnomacfg.ProjectRoot()

	formatProfileShow(os.Stdout, cfg, profile, profilePath, baseConfigPath, globalDir, projectRoot)
	return 0
}

// formatProfileShow renders the effective config for a profile to w.
// API key *values* are never printed — only the set of configured
// providers. Extracted for testing.
func formatProfileShow(w io.Writer, cfg *gnomacfg.Config, profile gnomacfg.Profile, profilePath, baseConfigPath, globalDir, projectRoot string) {
	if profile.Active {
		pf(w, "Profile: %s\n", profile.Name)
	} else {
		pln(w, "Profile: (legacy mode — no profiles/ directory)")
	}
	pf(w, "Base config: %s\n", baseConfigPath)
	if profilePath != "" {
		pf(w, "Profile file: %s\n", profilePath)
	}
	pln(w)

	pln(w, "[provider]")
	if cfg.Provider.Default != "" {
		pf(w, "  default     = %s\n", cfg.Provider.Default)
	}
	if cfg.Provider.Model != "" {
		pf(w, "  model       = %s\n", cfg.Provider.Model)
	}
	if cfg.Provider.MaxTokens > 0 {
		pf(w, "  max_tokens  = %d\n", cfg.Provider.MaxTokens)
	}
	if len(cfg.Provider.APIKeys) > 0 {
		pf(w, "  api_keys    = %s\n", sortedKeys(cfg.Provider.APIKeys))
	}
	if len(cfg.Provider.Endpoints) > 0 {
		pf(w, "  endpoints   = %s\n", sortedKeys(cfg.Provider.Endpoints))
	}

	if len(cfg.CLIAgents) > 0 {
		pln(w, "\n[cli_agents]")
		keys := make([]string, 0, len(cfg.CLIAgents))
		for k := range cfg.CLIAgents {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			v := cfg.CLIAgents[k]
			if v == "" {
				pf(w, "  %s = (canonical)\n", k)
			} else {
				pf(w, "  %s = %s\n", k, v)
			}
		}
	}

	if cfg.Permission.Mode != "" || len(cfg.Permission.Rules) > 0 {
		pln(w, "\n[permission]")
		if cfg.Permission.Mode != "" {
			pf(w, "  mode = %s\n", cfg.Permission.Mode)
		}
		if n := len(cfg.Permission.Rules); n > 0 {
			pf(w, "  rules: %d\n", n)
		}
	}

	if cfg.SLM.Enabled || cfg.SLM.Backend != "" || cfg.SLM.Model != "" {
		pln(w, "\n[slm]")
		pf(w, "  enabled = %v\n", cfg.SLM.Enabled)
		if cfg.SLM.Backend != "" {
			pf(w, "  backend = %s\n", cfg.SLM.Backend)
		}
		if cfg.SLM.Model != "" {
			pf(w, "  model   = %s\n", cfg.SLM.Model)
		}
		if cfg.SLM.BaseURL != "" {
			pf(w, "  base_url = %s\n", cfg.SLM.BaseURL)
		}
	}

	if cfg.Router.ForceTwoStage {
		pln(w, "\n[router]")
		pf(w, "  force_two_stage = %v\n", cfg.Router.ForceTwoStage)
	}

	if cfg.Tools.BashTimeout.Duration() > 0 || cfg.Tools.MaxFileSize > 0 {
		pln(w, "\n[tools]")
		if cfg.Tools.BashTimeout.Duration() > 0 {
			pf(w, "  bash_timeout   = %s\n", cfg.Tools.BashTimeout.Duration())
		}
		if cfg.Tools.MaxFileSize > 0 {
			pf(w, "  max_file_size  = %d\n", cfg.Tools.MaxFileSize)
		}
	}

	if cfg.Session.MaxKeep > 0 {
		pln(w, "\n[session]")
		pf(w, "  max_keep = %d\n", cfg.Session.MaxKeep)
	}

	pln(w)
	if n := len(cfg.Arms); n > 0 {
		ids := make([]string, 0, n)
		for _, a := range cfg.Arms {
			ids = append(ids, a.ID)
		}
		sort.Strings(ids)
		pf(w, "Arms (%d): %s\n", n, strings.Join(ids, ", "))
	} else {
		pln(w, "Arms (0): (none configured)")
	}

	if n := len(cfg.Hooks); n > 0 {
		names := make([]string, 0, n)
		for _, h := range cfg.Hooks {
			names = append(names, h.Name)
		}
		pf(w, "Hooks (%d): %s\n", n, strings.Join(names, ", "))
	} else {
		pln(w, "Hooks (0)")
	}

	if n := len(cfg.MCPServers); n > 0 {
		names := make([]string, 0, n)
		for _, s := range cfg.MCPServers {
			names = append(names, s.Name)
		}
		pf(w, "MCP servers (%d): %s\n", n, strings.Join(names, ", "))
	} else {
		pln(w, "MCP servers (0)")
	}

	if len(cfg.Plugins.Enabled) > 0 || len(cfg.Plugins.Disabled) > 0 {
		pf(w, "Plugins enabled (%d), disabled (%d)\n", len(cfg.Plugins.Enabled), len(cfg.Plugins.Disabled))
	}

	pln(w)
	pf(w, "Quality data: %s\n", profile.QualityFile(globalDir))
	pf(w, "Session dir:  %s\n", profile.SessionDir(projectRoot))
}

func sortedKeys(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}
