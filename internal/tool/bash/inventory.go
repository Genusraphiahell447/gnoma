package bash

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

const inventoryTimeout = 15 * time.Second

// SystemInventory holds dynamically discovered system information.
type SystemInventory struct {
	// Core
	OS       string
	Shell    string
	Hardware HardwareInfo

	// Discovered
	Tools        []string  // all executables found in PATH
	Runtimes     []Runtime // detected runtimes with versions
	PackageCount int       // total installed packages
	PackageMgr   string    // detected package manager
	DevPackages  []string  // development-relevant packages
}

type HardwareInfo struct {
	CPU      string
	Cores    int
	MemTotal string
	GPU      string
}

type Runtime struct {
	Name    string
	Version string
}

// HarvestInventory dynamically discovers system info.
func HarvestInventory(ctx context.Context) *SystemInventory {
	ctx, cancel := context.WithTimeout(ctx, inventoryTimeout)
	defer cancel()

	inv := &SystemInventory{
		OS:       detectOS(ctx),
		Shell:    os.Getenv("SHELL"),
		Hardware: detectHardware(ctx),
	}

	inv.Tools = scanPATH()
	inv.Runtimes = probeRuntimes(ctx, inv.Tools)
	inv.PackageMgr, inv.PackageCount, inv.DevPackages = queryPackageManager(ctx)

	return inv
}

// Summary returns a compact one-paragraph description for the system prompt.
// Minimal tokens — just enough for the LLM to know the system's capabilities.
func (inv *SystemInventory) Summary() string {
	if inv == nil {
		return ""
	}

	var b strings.Builder
	b.WriteString("System: ")

	if inv.OS != "" {
		b.WriteString(inv.OS)
	}
	if inv.Shell != "" {
		fmt.Fprintf(&b, ", %s", filepath.Base(inv.Shell))
	}
	if inv.Hardware.CPU != "" {
		fmt.Fprintf(&b, ". CPU: %s (%d cores)", inv.Hardware.CPU, inv.Hardware.Cores)
	}
	if inv.Hardware.MemTotal != "" {
		fmt.Fprintf(&b, ", RAM: %s", inv.Hardware.MemTotal)
	}
	if inv.Hardware.GPU != "" {
		fmt.Fprintf(&b, ", GPU: %s", inv.Hardware.GPU)
	}

	// Top runtimes (max 6)
	if len(inv.Runtimes) > 0 {
		top := inv.Runtimes
		if len(top) > 6 {
			top = top[:6]
		}
		names := make([]string, len(top))
		for i, rt := range top {
			names[i] = rt.Name
		}
		fmt.Fprintf(&b, ". Runtimes: %s", strings.Join(names, ", "))
		if len(inv.Runtimes) > 6 {
			fmt.Fprintf(&b, " +%d more", len(inv.Runtimes)-6)
		}
	}

	if inv.PackageCount > 0 {
		fmt.Fprintf(&b, ". %d packages (%s)", inv.PackageCount, inv.PackageMgr)
	}
	fmt.Fprintf(&b, ", %d commands in PATH.", len(inv.Tools))
	b.WriteString(" Use system_info tool for full details.")

	return b.String()
}

// QuerySection returns detailed info for a specific section.
// Sections: "all", "runtimes", "packages", "tools", "hardware"
func (inv *SystemInventory) QuerySection(section string) string {
	if inv == nil {
		return "no inventory available"
	}

	switch strings.ToLower(section) {
	case "runtimes":
		return inv.formatRuntimes()
	case "packages", "dev":
		return inv.formatPackages()
	case "tools", "commands":
		return inv.formatTools()
	case "hardware", "hw":
		return inv.formatHardware()
	case "all", "":
		return inv.formatAll()
	default:
		return fmt.Sprintf("unknown section %q. Available: runtimes, packages, tools, hardware, all", section)
	}
}

func (inv *SystemInventory) formatRuntimes() string {
	if len(inv.Runtimes) == 0 {
		return "no runtimes detected"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Detected runtimes (%d):\n", len(inv.Runtimes))
	for _, rt := range inv.Runtimes {
		fmt.Fprintf(&b, "  %s: %s\n", rt.Name, rt.Version)
	}
	return b.String()
}

func (inv *SystemInventory) formatPackages() string {
	var b strings.Builder
	if inv.PackageMgr != "" {
		fmt.Fprintf(&b, "Package manager: %s (%d total packages)\n", inv.PackageMgr, inv.PackageCount)
	}
	if len(inv.DevPackages) > 0 {
		fmt.Fprintf(&b, "Dev packages (%d):\n  %s\n", len(inv.DevPackages), strings.Join(inv.DevPackages, ", "))
	}
	return b.String()
}

func (inv *SystemInventory) formatTools() string {
	if len(inv.Tools) == 0 {
		return "no tools found in PATH"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Executables in PATH (%d):\n  %s\n", len(inv.Tools), strings.Join(inv.Tools, ", "))
	return b.String()
}

func (inv *SystemInventory) formatHardware() string {
	var b strings.Builder
	b.WriteString("Hardware:\n")
	fmt.Fprintf(&b, "  OS: %s\n", inv.OS)
	fmt.Fprintf(&b, "  Shell: %s\n", inv.Shell)
	hw := inv.Hardware
	if hw.CPU != "" {
		fmt.Fprintf(&b, "  CPU: %s\n", hw.CPU)
	}
	fmt.Fprintf(&b, "  Cores: %d\n", hw.Cores)
	if hw.MemTotal != "" {
		fmt.Fprintf(&b, "  Memory: %s\n", hw.MemTotal)
	}
	if hw.GPU != "" {
		fmt.Fprintf(&b, "  GPU: %s\n", hw.GPU)
	}
	return b.String()
}

func (inv *SystemInventory) formatAll() string {
	var b strings.Builder
	b.WriteString(inv.formatHardware())
	b.WriteString("\n")
	b.WriteString(inv.formatRuntimes())
	b.WriteString("\n")
	b.WriteString(inv.formatPackages())
	return b.String()
}

// --- Hardware Detection ---

func detectHardware(ctx context.Context) HardwareInfo {
	hw := HardwareInfo{
		Cores: runtime.NumCPU(),
	}

	// CPU model
	if cpuInfo := runQuiet(ctx, "sh", "-c", `command grep -m1 'model name' /proc/cpuinfo 2>/dev/null | cut -d: -f2`); cpuInfo != "" {
		hw.CPU = strings.TrimSpace(cpuInfo)
	}

	// Total memory
	if memInfo := runQuiet(ctx, "sh", "-c", `command grep MemTotal /proc/meminfo 2>/dev/null | awk '{printf "%.0f GB", $2/1024/1024}'`); memInfo != "" {
		hw.MemTotal = memInfo
	}

	// GPU (try lspci first, then nvidia-smi)
	if gpu := runQuiet(ctx, "sh", "-c", `lspci 2>/dev/null | command grep -i 'vga\|3d\|display' | head -1 | sed 's/.*: //'`); gpu != "" {
		hw.GPU = gpu
	}

	return hw
}

// --- PATH Scanning ---

func scanPATH() []string {
	seen := make(map[string]bool)
	var names []string

	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
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

// --- Runtime Probing ---

var runtimePatterns = []struct {
	name   string
	binary string
	args   []string
}{
	{"go", "go", []string{"version"}},
	{"rust", "rustc", []string{"--version"}},
	{"zig", "zig", []string{"version"}},
	{"nim", "nim", []string{"--version"}},
	{"crystal", "crystal", []string{"--version"}},
	{"gcc", "gcc", []string{"--version"}},
	{"clang", "clang", []string{"--version"}},
	{"nasm", "nasm", []string{"-v"}},
	{"python3", "python3", []string{"--version"}},
	{"python2", "python2", []string{"--version"}},
	{"perl", "perl", []string{"--version"}},
	{"ruby", "ruby", []string{"--version"}},
	{"lua", "lua", []string{"-v"}},
	{"luajit", "luajit", []string{"-v"}},
	{"guile", "guile", []string{"--version"}},
	{"php", "php", []string{"--version"}},
	{"r", "R", []string{"--version"}},
	{"node", "node", []string{"--version"}},
	{"deno", "deno", []string{"--version"}},
	{"bun", "bun", []string{"--version"}},
	{"java", "java", []string{"-version"}},
	{"kotlin", "kotlin", []string{"-version"}},
	{"scala", "scala", []string{"-version"}},
	{"groovy", "groovy", []string{"--version"}},
	{"clojure", "clj", []string{"--version"}},
	{"haskell", "ghc", []string{"--version"}},
	{"ocaml", "ocaml", []string{"-version"}},
	{"elixir", "elixir", []string{"--version"}},
	{"racket", "racket", []string{"--version"}},
	{"dart", "dart", []string{"--version"}},
	{"julia", "julia", []string{"--version"}},
	{"swift", "swift", []string{"--version"}},
	{"dotnet", "dotnet", []string{"--version"}},
	{"mono", "mono", []string{"--version"}},
	{"cargo", "cargo", []string{"--version"}},
	{"npm", "npm", []string{"--version"}},
	{"yarn", "yarn", []string{"--version"}},
	{"pnpm", "pnpm", []string{"--version"}},
	{"pip", "pip3", []string{"--version"}},
	{"gem", "gem", []string{"--version"}},
}

func probeRuntimes(ctx context.Context, available []string) []Runtime {
	availSet := make(map[string]bool, len(available))
	for _, name := range available {
		availSet[name] = true
	}

	var mu sync.Mutex
	var runtimes []Runtime
	var wg sync.WaitGroup
	sem := make(chan struct{}, 10)

	for _, p := range runtimePatterns {
		if !availSet[p.binary] {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(name, binary string, args []string) {
			defer wg.Done()
			defer func() { <-sem }()
			if v := probeVersion(ctx, binary, args); v != "" {
				mu.Lock()
				runtimes = append(runtimes, Runtime{Name: name, Version: v})
				mu.Unlock()
			}
		}(p.name, p.binary, p.args)
	}
	wg.Wait()

	sort.Slice(runtimes, func(i, j int) bool { return runtimes[i].Name < runtimes[j].Name })
	return runtimes
}

func probeVersion(ctx context.Context, binary string, args []string) string {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Stdin = nil
	output, err := cmd.CombinedOutput()
	if err != nil && len(output) == 0 {
		return ""
	}
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

// --- Package Manager ---

func queryPackageManager(ctx context.Context) (mgr string, total int, devPkgs []string) {
	managers := []struct {
		name, binary string
		args         []string
	}{
		{"pacman", "pacman", []string{"-Qq"}},
		{"apt", "dpkg", []string{"--get-selections"}},
		{"dnf", "rpm", []string{"-qa", "--qf", "%{NAME}\\n"}},
		{"brew", "brew", []string{"list", "--formula", "-1"}},
		{"nix", "nix-env", []string{"-q"}},
		{"apk", "apk", []string{"list", "--installed", "-q"}},
	}

	for _, pm := range managers {
		if _, err := exec.LookPath(pm.binary); err != nil {
			continue
		}
		cmd := exec.CommandContext(ctx, pm.binary, pm.args...)
		output, err := cmd.Output()
		if err != nil {
			continue
		}
		lines := strings.Split(strings.TrimSpace(string(output)), "\n")
		return pm.name, len(lines), filterDevPackages(lines)
	}
	return "", 0, nil
}

var devPrefixes = []string{
	"python", "ruby", "nodejs", "go", "rustup", "lua", "luajit",
	"perl", "dart", "deno", "bun", "zig", "nim", "crystal",
	"elixir", "erlang", "ghc", "ocaml", "swift", "kotlin", "scala",
	"julia", "php", "dotnet", "mono", "clojure", "racket", "guile",
	"openjdk", "jdk", "gcc", "clang", "llvm", "cmake", "make",
	"ninja", "meson", "nasm", "gdb", "valgrind", "strace",
	"docker", "podman", "kubectl", "helm", "terraform", "ansible",
	"npm", "yarn", "pnpm", "cargo", "rubygems", "composer",
	"sqlite", "postgresql", "mysql", "redis", "mongodb",
	"git", "mercurial", "subversion", "neovim", "vim", "emacs",
}

func filterDevPackages(packages []string) []string {
	var devPkgs []string
	seen := make(map[string]bool)

	for _, pkg := range packages {
		pkg = strings.TrimSpace(pkg)
		if f := strings.Fields(pkg); len(f) > 0 {
			pkg = f[0]
		}
		if pkg == "" || seen[pkg] {
			continue
		}
		lower := strings.ToLower(pkg)

		// Skip sub-packages
		if strings.HasPrefix(lower, "python-") || strings.HasPrefix(lower, "haskell-") ||
			strings.HasPrefix(lower, "perl-") || strings.HasPrefix(lower, "ruby-") ||
			strings.HasPrefix(lower, "lua-") || strings.HasPrefix(lower, "lua51-") ||
			strings.HasPrefix(lower, "lua54-") || strings.HasPrefix(lower, "lib32-") ||
			strings.HasPrefix(lower, "lib") || strings.HasPrefix(lower, "ttf-") ||
			strings.HasPrefix(lower, "otf-") || strings.HasPrefix(lower, "qt5-") ||
			strings.HasPrefix(lower, "qt6-") || strings.HasPrefix(lower, "gtk") ||
			strings.HasPrefix(lower, "gst-") {
			continue
		}

		for _, prefix := range devPrefixes {
			if lower == prefix || strings.HasPrefix(lower, prefix+"-") ||
				strings.HasPrefix(lower, prefix+"1") || strings.HasPrefix(lower, prefix+"2") ||
				strings.HasPrefix(lower, prefix+"3") {
				seen[pkg] = true
				devPkgs = append(devPkgs, pkg)
				break
			}
		}
	}
	sort.Strings(devPkgs)
	return devPkgs
}

func detectOS(ctx context.Context) string {
	if out := runQuiet(ctx, "uname", "-srm"); out != "" {
		return strings.TrimSpace(out)
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
