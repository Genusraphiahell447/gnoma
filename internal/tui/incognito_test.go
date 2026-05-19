package tui

import (
	"strings"
	"testing"

	"somegit.dev/Owlibou/gnoma/internal/provider"
	"somegit.dev/Owlibou/gnoma/internal/router"
	"somegit.dev/Owlibou/gnoma/internal/security"
)

func newToggleTestModel(rtr *router.Router, fw *security.Firewall) Model {
	return Model{
		config: Config{
			Firewall: fw,
			Router:   rtr,
		},
	}
}

func TestAttemptIncognitoToggle_NilFirewallReturnsRefused(t *testing.T) {
	m := newToggleTestModel(nil, nil)
	_, status, refused := m.attemptIncognitoToggle()
	if !refused {
		t.Error("expected refused=true when firewall is nil")
	}
	if !strings.Contains(status, "firewall") {
		t.Errorf("status = %q, want mention of firewall", status)
	}
}

func TestAttemptIncognitoToggle_NoForcedArmFlipsOn(t *testing.T) {
	rtr := router.New(router.Config{})
	fw := security.NewFirewall(security.FirewallConfig{ScanOutgoing: true})
	m := newToggleTestModel(rtr, fw)

	newM, status, refused := m.attemptIncognitoToggle()
	if refused {
		t.Fatalf("expected refused=false, got refused; status=%q", status)
	}
	if !newM.incognito {
		t.Error("expected newM.incognito = true after toggle")
	}
	if !fw.Incognito().Active() {
		t.Error("firewall incognito should be active after toggle")
	}
	if !rtr.LocalOnly() {
		t.Error("router localOnly should be true after toggle")
	}
	if !strings.Contains(status, "incognito ON") {
		t.Errorf("status = %q, want incognito ON marker", status)
	}
}

func TestAttemptIncognitoToggle_ForcedLocalArmAllowed(t *testing.T) {
	rtr := router.New(router.Config{})
	rtr.RegisterArm(&router.Arm{
		ID:           router.NewArmID("ollama", "qwen"),
		IsLocal:      true,
		Capabilities: provider.Capabilities{ToolUse: true},
	})
	rtr.ForceArm(router.NewArmID("ollama", "qwen"))

	fw := security.NewFirewall(security.FirewallConfig{ScanOutgoing: true})
	m := newToggleTestModel(rtr, fw)

	_, _, refused := m.attemptIncognitoToggle()
	if refused {
		t.Error("forced LOCAL arm + incognito should NOT be refused")
	}
}

func TestAttemptIncognitoToggle_ForcedCloudArmRefused(t *testing.T) {
	rtr := router.New(router.Config{})
	rtr.RegisterArm(&router.Arm{
		ID:           router.NewArmID("anthropic", "sonnet"),
		IsLocal:      false,
		Capabilities: provider.Capabilities{ToolUse: true},
	})
	rtr.ForceArm(router.NewArmID("anthropic", "sonnet"))

	fw := security.NewFirewall(security.FirewallConfig{ScanOutgoing: true})
	m := newToggleTestModel(rtr, fw)

	_, status, refused := m.attemptIncognitoToggle()
	if !refused {
		t.Fatalf("forced CLOUD arm + incognito should be refused; status=%q", status)
	}
	if fw.Incognito().Active() {
		t.Error("firewall must NOT activate when toggle is refused")
	}
	if rtr.LocalOnly() {
		t.Error("router localOnly must NOT flip when toggle is refused")
	}
	if !strings.Contains(status, "non-local") && !strings.Contains(status, "pin") {
		t.Errorf("status should explain the refusal; got %q", status)
	}
}

func TestNew_SeedsIncognitoFromActiveFirewall(t *testing.T) {
	fw := security.NewFirewall(security.FirewallConfig{ScanOutgoing: true})
	fw.Incognito().Activate()

	m := New(nil, Config{Firewall: fw})
	if !m.incognito {
		t.Error("New() should seed m.incognito=true when firewall already active")
	}
}

func TestNew_SeedsIncognitoFalseWhenFirewallInactive(t *testing.T) {
	fw := security.NewFirewall(security.FirewallConfig{ScanOutgoing: true})

	m := New(nil, Config{Firewall: fw})
	if m.incognito {
		t.Error("New() should seed m.incognito=false when firewall inactive")
	}
}

func TestNew_SeedsIncognitoFalseWhenNoFirewall(t *testing.T) {
	m := New(nil, Config{})
	if m.incognito {
		t.Error("New() should seed m.incognito=false when no firewall")
	}
}

func TestAttemptIncognitoToggle_TurningOffNotBlockedByForcedCloud(t *testing.T) {
	// Once incognito is ON, the user must always be able to turn it OFF
	// regardless of the forced-arm state. Otherwise they're trapped.
	rtr := router.New(router.Config{})
	rtr.RegisterArm(&router.Arm{
		ID:           router.NewArmID("anthropic", "sonnet"),
		IsLocal:      false,
		Capabilities: provider.Capabilities{ToolUse: true},
	})
	// Note: not forcing the arm yet — start incognito on a clean state,
	// then pretend a forced cloud arm appears (which shouldn't happen in
	// practice, but the toggle-off path must be robust).
	fw := security.NewFirewall(security.FirewallConfig{ScanOutgoing: true})
	fw.Incognito().Activate()
	rtr.SetLocalOnly(true)
	rtr.ForceArm(router.NewArmID("anthropic", "sonnet"))

	m := newToggleTestModel(rtr, fw)
	m.incognito = true

	newM, _, refused := m.attemptIncognitoToggle()
	if refused {
		t.Fatal("turning incognito OFF must never be refused")
	}
	if newM.incognito {
		t.Error("incognito should be false after toggle-off")
	}
	if fw.Incognito().Active() {
		t.Error("firewall incognito should be off")
	}
	if rtr.LocalOnly() {
		t.Error("router localOnly should be off")
	}
}
