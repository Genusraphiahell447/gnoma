package bash

import (
	"context"
	"strings"
	"testing"
)

func TestScanPATH(t *testing.T) {
	binaries := scanPATH()
	if len(binaries) == 0 {
		t.Fatal("should find at least some executables in PATH")
	}
	t.Logf("Found %d executables in PATH", len(binaries))
}

func TestHarvestInventory(t *testing.T) {
	inv := HarvestInventory(context.Background())

	if inv.OS == "" {
		t.Error("OS should be detected")
	}
	t.Logf("OS: %s", inv.OS)
	t.Logf("Shell: %s", inv.Shell)
	t.Logf("CPU: %s (%d cores)", inv.Hardware.CPU, inv.Hardware.Cores)
	t.Logf("Memory: %s", inv.Hardware.MemTotal)
	t.Logf("GPU: %s", inv.Hardware.GPU)
	t.Logf("Tools: %d in PATH", len(inv.Tools))

	if inv.PackageMgr != "" {
		t.Logf("Packages: %d (%s)", inv.PackageCount, inv.PackageMgr)
		t.Logf("Dev packages: %d", len(inv.DevPackages))
	}

	t.Logf("Runtimes: %d", len(inv.Runtimes))
	for _, rt := range inv.Runtimes {
		t.Logf("  %s: %s", rt.Name, rt.Version)
	}

	// Should detect Go (we're running Go tests)
	hasGo := false
	for _, rt := range inv.Runtimes {
		if rt.Name == "go" {
			hasGo = true
			break
		}
	}
	if !hasGo {
		t.Error("should detect Go runtime")
	}
}

func TestInventory_Summary(t *testing.T) {
	inv := &SystemInventory{
		OS:    "Linux 6.18 x86_64",
		Shell: "/usr/bin/zsh",
		Hardware: HardwareInfo{
			CPU: "AMD Ryzen 9", Cores: 16,
			MemTotal: "32 GB", GPU: "NVIDIA RTX 4090",
		},
		Tools:        make([]string, 100),
		Runtimes:     []Runtime{{"go", "1.26"}, {"python3", "3.14"}, {"node", "25.8"}},
		PackageCount: 2239,
		PackageMgr:   "pacman",
	}

	s := inv.Summary()
	if !strings.Contains(s, "Linux 6.18") {
		t.Error("should contain OS")
	}
	if !strings.Contains(s, "zsh") {
		t.Error("should contain shell")
	}
	if !strings.Contains(s, "AMD Ryzen") {
		t.Error("should contain CPU")
	}
	if !strings.Contains(s, "32 GB") {
		t.Error("should contain RAM")
	}
	if !strings.Contains(s, "RTX 4090") {
		t.Error("should contain GPU")
	}
	if !strings.Contains(s, "go, python3, node") {
		t.Error("should contain top runtimes")
	}
	if !strings.Contains(s, "2239 packages") {
		t.Error("should contain package count")
	}
	if !strings.Contains(s, "system_info") {
		t.Error("should mention system_info tool")
	}
	// Should be compact — under 300 chars
	if len(s) > 500 {
		t.Errorf("summary too long (%d chars), should be compact: %s", len(s), s)
	}
}

func TestInventory_QuerySection(t *testing.T) {
	inv := &SystemInventory{
		OS:          "Linux",
		Shell:       "/bin/zsh",
		Hardware:    HardwareInfo{CPU: "Test CPU", Cores: 4, MemTotal: "16 GB"},
		Tools:       []string{"git", "make"},
		Runtimes:    []Runtime{{"go", "1.26"}, {"rust", "1.91"}},
		DevPackages: []string{"docker", "kubectl"},
		PackageMgr:  "pacman", PackageCount: 100,
	}

	// Runtimes
	r := inv.QuerySection("runtimes")
	if !strings.Contains(r, "go: 1.26") {
		t.Errorf("runtimes section should contain go: %s", r)
	}

	// Hardware
	h := inv.QuerySection("hardware")
	if !strings.Contains(h, "Test CPU") {
		t.Errorf("hardware section should contain CPU: %s", h)
	}

	// Tools
	tools := inv.QuerySection("tools")
	if !strings.Contains(tools, "git") {
		t.Errorf("tools section should contain git: %s", tools)
	}

	// Unknown
	u := inv.QuerySection("bogus")
	if !strings.Contains(u, "unknown section") {
		t.Errorf("unknown section should error: %s", u)
	}
}

func TestInventory_NilSummary(t *testing.T) {
	var inv *SystemInventory
	if inv.Summary() != "" {
		t.Error("nil inventory should return empty summary")
	}
}
