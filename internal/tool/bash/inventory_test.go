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

	// Basic sanity — ls should be in PATH
	hasLS := false
	for _, b := range binaries {
		if b == "ls" {
			hasLS = true
			break
		}
	}
	if !hasLS {
		t.Error("ls should be in PATH")
	}
}

func TestHarvestInventory(t *testing.T) {
	inv := HarvestInventory(context.Background())

	if inv.OS == "" {
		t.Error("OS should be detected")
	}
	t.Logf("OS: %s", inv.OS)

	if inv.Shell == "" {
		t.Error("Shell should be detected")
	}
	t.Logf("Shell: %s", inv.Shell)

	t.Logf("Tools: %d executables in PATH", len(inv.Tools))

	t.Logf("Runtimes (%d):", len(inv.Runtimes))
	for _, rt := range inv.Runtimes {
		t.Logf("  %s: %s", rt.Name, rt.Version)
	}

	// Should find at least Go (we're running Go tests)
	hasGo := false
	for _, rt := range inv.Runtimes {
		if rt.Name == "go" {
			hasGo = true
			break
		}
	}
	if !hasGo {
		t.Error("should detect Go runtime (we're running Go tests)")
	}
}

func TestInventory_String(t *testing.T) {
	inv := &SystemInventory{
		OS:    "Linux 6.18 x86_64",
		Shell: "/usr/bin/zsh",
		Tools: []string{"git", "make", "docker"},
		Runtimes: []Runtime{
			{"go", "go version go1.26.1"},
			{"python3", "Python 3.14.3"},
		},
	}

	s := inv.String()
	if !strings.Contains(s, "Linux") {
		t.Error("should contain OS")
	}
	if !strings.Contains(s, "git") {
		t.Error("should contain tools")
	}
	if !strings.Contains(s, "go:") {
		t.Error("should contain runtimes")
	}
	if !strings.Contains(s, "3 executables in PATH") {
		t.Errorf("should show tool count, got: %s", s)
	}
}

func TestInventory_NilString(t *testing.T) {
	var inv *SystemInventory
	if inv.String() != "" {
		t.Error("nil inventory should return empty string")
	}
}
