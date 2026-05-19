package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ErrProfileResolution is the sentinel returned (wrapped) when profile
// loading fails because the user's selection cannot be resolved — e.g.
// --profile names a missing file, profiles/ exists with no
// default_profile, or default_profile names a missing file.
//
// Callers can distinguish these actionable misconfigurations from
// generic config-file errors via errors.Is and exit non-zero rather
// than silently falling back to defaults.
var ErrProfileResolution = errors.New("profile resolution")

// Profile carries the resolved profile metadata for the current session.
// Active=false means the profile system is not engaged — either because
// no profiles/ directory exists alongside the base config.toml, or because
// the caller went through the backward-compatible Load() entry point on a
// non-profile installation. Treat Active==false as "use legacy paths" and
// do not assume Name has any particular value.
type Profile struct {
	Name   string
	Active bool
}

// QualityFile returns the absolute path to the router quality file for
// this profile. Legacy callers (Active==false) get the original
// ~/.config/gnoma/quality.json. Active profiles get a per-profile file
// at ~/.config/gnoma/quality-<name>.json so the bandit doesn't
// cross-contaminate between profile workloads. globalDir is the same
// directory GlobalConfigDir() returns.
func (p Profile) QualityFile(globalDir string) string {
	if !p.Active {
		return filepath.Join(globalDir, "quality.json")
	}
	return filepath.Join(globalDir, "quality-"+p.Name+".json")
}

// SessionDir returns the per-profile session directory. Legacy callers
// (Active==false) get <projectRoot>/.gnoma/sessions/; active profiles
// get <projectRoot>/.gnoma/sessions/<name>/ so resuming `work` doesn't
// surface `private` sessions.
func (p Profile) SessionDir(projectRoot string) string {
	if !p.Active {
		return filepath.Join(projectRoot, ".gnoma", "sessions")
	}
	return filepath.Join(projectRoot, ".gnoma", "sessions", p.Name)
}

// LoadWithProfile is the profile-aware entry point.
//
// Layering, lowest to highest priority:
//  1. Defaults
//  2. Global base: ~/.config/gnoma/config.toml
//  3. Selected profile: ~/.config/gnoma/profiles/<name>.toml
//     (only when profiles/ directory exists)
//  4. Project config: .gnoma/config.toml
//  5. Environment variables
//
// flagName carries the --profile argument (empty when not set). When
// empty: use base default_profile if profiles/ is engaged, otherwise
// fall back to legacy behaviour (no profiles).
//
// Profile activation policy: profile mode engages iff
// ~/.config/gnoma/profiles/ exists as a directory. This keeps existing
// single-config installations on their current paths untouched.
func LoadWithProfile(flagName string) (*Config, Profile, error) {
	cfg := Defaults()

	globalPath := globalConfigPath()
	if err := loadTOML(&cfg, globalPath); err != nil && !os.IsNotExist(err) {
		return nil, Profile{}, fmt.Errorf("loading global config %s: %w", globalPath, err)
	}

	profilesDir := filepath.Join(GlobalConfigDir(), "profiles")
	profilesActive := false
	if st, err := os.Stat(profilesDir); err == nil && st.IsDir() {
		profilesActive = true
	}

	resolvedName, err := resolveProfileName(profilesActive, flagName, cfg.DefaultProfile, profilesDir, globalPath)
	if err != nil {
		return nil, Profile{}, err
	}

	if profilesActive {
		profilePath := filepath.Join(profilesDir, resolvedName+".toml")

		// Slices need explicit merge policy: snapshot base, let profile
		// decode overwrite, then re-merge per slice type.
		baseHooks := append([]HookConfig(nil), cfg.Hooks...)
		baseArms := append([]ArmConfig(nil), cfg.Arms...)
		baseMCP := append([]MCPServerConfig(nil), cfg.MCPServers...)
		cfg.Hooks = nil
		cfg.Arms = nil
		cfg.MCPServers = nil

		if err := loadTOML(&cfg, profilePath); err != nil {
			if os.IsNotExist(err) {
				avail, _ := listProfiles(profilesDir)
				return nil, Profile{}, fmt.Errorf("%w: profile %q not found at %s; available: %s",
					ErrProfileResolution, resolvedName, profilePath, strings.Join(avail, ", "))
			}
			return nil, Profile{}, fmt.Errorf("loading profile %s: %w", profilePath, err)
		}

		cfg.Hooks = append(baseHooks, cfg.Hooks...)
		cfg.Arms = mergeArmsByID(baseArms, cfg.Arms)
		cfg.MCPServers = mergeMCPServersByName(baseMCP, cfg.MCPServers)
	}

	// Project layer: same Hooks-append semantics the legacy loader uses.
	priorHooks := append([]HookConfig(nil), cfg.Hooks...)
	cfg.Hooks = nil

	projectPath := projectConfigPath()
	if err := loadTOML(&cfg, projectPath); err != nil && !os.IsNotExist(err) {
		return nil, Profile{}, fmt.Errorf("loading project config %s: %w", projectPath, err)
	}
	cfg.Hooks = append(priorHooks, cfg.Hooks...)

	applyEnv(&cfg)

	prof := Profile{Active: profilesActive}
	if profilesActive {
		prof.Name = resolvedName
	}
	return &cfg, prof, nil
}

func resolveProfileName(profilesActive bool, flagName, defaultProfile, profilesDir, globalPath string) (string, error) {
	switch {
	case !profilesActive && flagName != "":
		return "", fmt.Errorf("%w: --profile %q set but no profiles directory at %s; create %s/%s.toml first",
			ErrProfileResolution, flagName, profilesDir, profilesDir, flagName)
	case !profilesActive:
		return "", nil
	case flagName != "":
		if err := validateProfileName(flagName); err != nil {
			return "", fmt.Errorf("%w: --profile %q: %v", ErrProfileResolution, flagName, err)
		}
		return flagName, nil
	case defaultProfile != "":
		if err := validateProfileName(defaultProfile); err != nil {
			return "", fmt.Errorf("%w: default_profile %q: %v", ErrProfileResolution, defaultProfile, err)
		}
		return defaultProfile, nil
	default:
		avail, _ := listProfiles(profilesDir)
		hint := ""
		if len(avail) > 0 {
			hint = " available: " + strings.Join(avail, ", ")
		}
		return "", fmt.Errorf("%w: profiles directory present but no profile selected: pass --profile <name> or set default_profile in %s.%s",
			ErrProfileResolution, globalPath, hint)
	}
}

// validateProfileName rejects names that would escape the profiles
// directory or produce surprising paths in derived locations like
// quality-<name>.json and sessions/<name>/. Allowed: letters, digits,
// hyphens, underscores. Rejected: empty, anything containing path
// separators, dots, or other characters that could enable traversal
// or shell-meaningful filenames.
func validateProfileName(name string) error {
	if name == "" {
		return errors.New("must not be empty")
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_':
		default:
			return fmt.Errorf("invalid character %q (allowed: letters, digits, '-', '_')", r)
		}
	}
	return nil
}

// ListProfiles returns the sorted profile names in ~/.config/gnoma/profiles/.
// Returns nil (no error) when the directory does not exist.
func ListProfiles() ([]string, error) {
	return listProfiles(filepath.Join(GlobalConfigDir(), "profiles"))
}

func listProfiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".toml") {
			continue
		}
		names = append(names, strings.TrimSuffix(name, ".toml"))
	}
	sort.Strings(names)
	return names, nil
}

// mergeArmsByID returns base arms with each profile arm appended (new
// ID) or used to replace the matching base entry (existing ID). Order
// preserves base ordering, then appends profile-only IDs.
func mergeArmsByID(base, profile []ArmConfig) []ArmConfig {
	if len(profile) == 0 {
		return base
	}
	overrides := make(map[string]ArmConfig, len(profile))
	for _, p := range profile {
		overrides[p.ID] = p
	}
	out := make([]ArmConfig, 0, len(base)+len(profile))
	seen := make(map[string]bool, len(base))
	for _, b := range base {
		if p, ok := overrides[b.ID]; ok {
			out = append(out, p)
		} else {
			out = append(out, b)
		}
		seen[b.ID] = true
	}
	for _, p := range profile {
		if !seen[p.ID] {
			out = append(out, p)
		}
	}
	return out
}

// mergeMCPServersByName mirrors mergeArmsByID for MCP server entries,
// keyed by Name.
func mergeMCPServersByName(base, profile []MCPServerConfig) []MCPServerConfig {
	if len(profile) == 0 {
		return base
	}
	overrides := make(map[string]MCPServerConfig, len(profile))
	for _, p := range profile {
		overrides[p.Name] = p
	}
	out := make([]MCPServerConfig, 0, len(base)+len(profile))
	seen := make(map[string]bool, len(base))
	for _, b := range base {
		if p, ok := overrides[b.Name]; ok {
			out = append(out, p)
		} else {
			out = append(out, b)
		}
		seen[b.Name] = true
	}
	for _, p := range profile {
		if !seen[p.Name] {
			out = append(out, p)
		}
	}
	return out
}
