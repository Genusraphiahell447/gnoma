package bash

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const inventoryTimeout = 15 * time.Second

// SystemInventory holds dynamically discovered system information.
type SystemInventory struct {
	OS       string
	Shell    string
	Tools    []string  // all executables found in PATH
	Runtimes []Runtime // detected runtimes with versions
}

// Runtime is a detected language runtime or package manager.
type Runtime struct {
	Name    string
	Version string
}

// HarvestInventory dynamically discovers installed tools and runtimes.
// No hardcoded lists — scans $PATH and probes for version info.
func HarvestInventory(ctx context.Context) *SystemInventory {
	ctx, cancel := context.WithTimeout(ctx, inventoryTimeout)
	defer cancel()

	inv := &SystemInventory{
		OS:    detectOS(ctx),
		Shell: detectShell(),
	}

	// Scan all executables in PATH
	allBinaries := scanPATH()
	inv.Tools = allBinaries

	// Probe for runtimes: try --version on known runtime name patterns
	inv.Runtimes = probeRuntimes(ctx, allBinaries)

	return inv
}

// scanPATH collects all unique executable names from $PATH directories.
func scanPATH() []string {
	seen := make(map[string]bool)
	var names []string

	pathDirs := filepath.SplitList(os.Getenv("PATH"))
	for _, dir := range pathDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if seen[name] {
				continue
			}
			// Check it's actually executable
			info, err := entry.Info()
			if err != nil {
				continue
			}
			if info.Mode()&0o111 != 0 {
				seen[name] = true
				names = append(names, name)
			}
		}
	}

	sort.Strings(names)
	return names
}

// runtimeCandidate defines how to detect a runtime from a binary name.
type runtimeCandidate struct {
	name    string   // display name
	binary  string   // executable name to look for
	args    []string // version flag
}

// knownRuntimePatterns returns binary names that are likely runtimes.
// We still need a pattern list to know WHICH binaries to try --version on,
// but the actual detection is dynamic (only probes what's in PATH).
var runtimePatterns = []runtimeCandidate{
	// Systems languages
	{"go", "go", []string{"version"}},
	{"rust", "rustc", []string{"--version"}},
	{"zig", "zig", []string{"version"}},
	{"nim", "nim", []string{"--version"}},
	{"crystal", "crystal", []string{"--version"}},
	{"gcc", "gcc", []string{"--version"}},
	{"clang", "clang", []string{"--version"}},
	{"nasm", "nasm", []string{"-v"}},
	// Scripting
	{"python3", "python3", []string{"--version"}},
	{"python2", "python2", []string{"--version"}},
	{"perl", "perl", []string{"--version"}},
	{"ruby", "ruby", []string{"--version"}},
	{"lua", "lua", []string{"-v"}},
	{"luajit", "luajit", []string{"-v"}},
	{"guile", "guile", []string{"--version"}},
	// tcl detection is tricky (needs stdin), skipped
	{"php", "php", []string{"--version"}},
	{"r", "R", []string{"--version"}},
	// JS/TS
	{"node", "node", []string{"--version"}},
	{"deno", "deno", []string{"--version"}},
	{"bun", "bun", []string{"--version"}},
	// JVM
	{"java", "java", []string{"-version"}},
	{"kotlin", "kotlin", []string{"-version"}},
	{"scala", "scala", []string{"-version"}},
	{"groovy", "groovy", []string{"--version"}},
	{"clojure", "clj", []string{"--version"}},
	// Functional
	{"haskell", "ghc", []string{"--version"}},
	{"ocaml", "ocaml", []string{"-version"}},
	{"elixir", "elixir", []string{"--version"}},
	{"erlang", "erl", []string{"-eval", "io:format(erlang:system_info(otp_release)),halt()."}},
	{"racket", "racket", []string{"--version"}},
	// Other
	{"dart", "dart", []string{"--version"}},
	{"julia", "julia", []string{"--version"}},
	{"swift", "swift", []string{"--version"}},
	// .NET
	{"dotnet", "dotnet", []string{"--version"}},
	{"mono", "mono", []string{"--version"}},
	// Package managers
	{"cargo", "cargo", []string{"--version"}},
	{"npm", "npm", []string{"--version"}},
	{"yarn", "yarn", []string{"--version"}},
	{"pnpm", "pnpm", []string{"--version"}},
	{"pip", "pip3", []string{"--version"}},
	{"gem", "gem", []string{"--version"}},
	{"composer", "composer", []string{"--version"}},
	{"mix", "mix", []string{"--version"}},
	{"cabal", "cabal", []string{"--version"}},
	{"stack", "stack", []string{"--version"}},
	{"opam", "opam", []string{"--version"}},
	{"maven", "mvn", []string{"--version"}},
	{"gradle", "gradle", []string{"--version"}},
}

// probeRuntimes checks which runtime candidates exist in the discovered binaries
// and gets their version info. Runs probes concurrently for speed.
func probeRuntimes(ctx context.Context, available []string) []Runtime {
	availSet := make(map[string]bool, len(available))
	for _, name := range available {
		availSet[name] = true
	}

	var mu sync.Mutex
	var runtimes []Runtime
	var wg sync.WaitGroup

	// Semaphore to limit concurrent version probes
	sem := make(chan struct{}, 10)

	for _, candidate := range runtimePatterns {
		if !availSet[candidate.binary] {
			continue
		}

		wg.Add(1)
		sem <- struct{}{}
		go func(c runtimeCandidate) {
			defer wg.Done()
			defer func() { <-sem }()

			version := probeVersion(ctx, c.binary, c.args)
			if version != "" {
				mu.Lock()
				runtimes = append(runtimes, Runtime{Name: c.name, Version: version})
				mu.Unlock()
			}
		}(candidate)
	}

	wg.Wait()

	sort.Slice(runtimes, func(i, j int) bool {
		return runtimes[i].Name < runtimes[j].Name
	})
	return runtimes
}

// probeVersion runs a binary with version args and extracts the first line.
func probeVersion(ctx context.Context, binary string, args []string) string {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Stdin = nil

	// Some tools print version to stderr (java -version)
	output, err := cmd.CombinedOutput()
	if err != nil && len(output) == 0 {
		return ""
	}

	// First non-empty line
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

// String formats the inventory for inclusion in a system prompt.
func (inv *SystemInventory) String() string {
	if inv == nil {
		return ""
	}

	var b strings.Builder
	b.WriteString("System environment:\n")

	if inv.OS != "" {
		fmt.Fprintf(&b, "- OS: %s\n", inv.OS)
	}
	if inv.Shell != "" {
		fmt.Fprintf(&b, "- Shell: %s\n", inv.Shell)
	}
	if len(inv.Runtimes) > 0 {
		b.WriteString("- Runtimes & package managers:\n")
		for _, rt := range inv.Runtimes {
			fmt.Fprintf(&b, "  - %s: %s\n", rt.Name, rt.Version)
		}
	}
	if len(inv.Tools) > 0 {
		fmt.Fprintf(&b, "- Available commands: %d executables in PATH", len(inv.Tools))
		// Include notable tools only (not the full list — saves tokens)
		notable := filterNotable(inv.Tools)
		if len(notable) > 0 {
			fmt.Fprintf(&b, " (notable: %s)", strings.Join(notable, ", "))
		}
		b.WriteString("\n")
	}

	return b.String()
}

// notableTools are development-relevant tools worth highlighting in the system prompt.
var notableTools = map[string]bool{
	// VCS
	"git": true, "gh": true, "tea": true, "lazygit": true,
	// Build
	"make": true, "cmake": true, "just": true,
	// Containers
	"docker": true, "podman": true, "kubectl": true, "helm": true,
	// Data
	"jq": true, "yq": true, "rg": true, "fd": true, "fzf": true,
	// Modern CLI
	"bat": true, "eza": true, "delta": true, "sd": true, "dust": true,
	"hyperfine": true, "tokei": true, "scc": true,
	// Debug
	"gdb": true, "strace": true, "perf": true, "valgrind": true,
	// Database
	"sqlite3": true, "psql": true, "mysql": true, "mongosh": true, "redis-cli": true,
	// Media
	"ffmpeg": true, "convert": true,
	// Infra
	"terraform": true, "ansible": true,
	// Network
	"curl": true, "wget": true, "ssh": true,
	// Editors
	"nvim": true, "vim": true,
	// Multiplexer
	"tmux": true,
}

func filterNotable(tools []string) []string {
	var out []string
	for _, t := range tools {
		if notableTools[t] {
			out = append(out, t)
		}
	}
	return out
}

func detectOS(ctx context.Context) string {
	if output := runQuiet(ctx, "uname", "-srm"); output != "" {
		return strings.TrimSpace(output)
	}
	return ""
}

func detectShell() string {
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}
	return ""
}

func runQuiet(ctx context.Context, name string, args ...string) string {
	cmd := exec.CommandContext(ctx, name, args...)
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}
