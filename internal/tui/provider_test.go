package tui

import (
	"context"
	"strings"
	"testing"

	"somegit.dev/Owlibou/gnoma/internal/engine"
	"somegit.dev/Owlibou/gnoma/internal/provider"
	"somegit.dev/Owlibou/gnoma/internal/router"
	"somegit.dev/Owlibou/gnoma/internal/security"
	"somegit.dev/Owlibou/gnoma/internal/session"
	"somegit.dev/Owlibou/gnoma/internal/stream"
	"somegit.dev/Owlibou/gnoma/internal/tool"
)

type mockProvider struct {
	name         string
	defaultModel string
}

func (m *mockProvider) Stream(ctx context.Context, req provider.Request) (stream.Stream, error) {
	return nil, nil
}

func (m *mockProvider) Name() string {
	return m.name
}

func (m *mockProvider) Models(ctx context.Context) ([]provider.ModelInfo, error) {
	return nil, nil
}

func (m *mockProvider) DefaultModel() string {
	return m.defaultModel
}

func newTestRouterAndEngine() (*router.Router, *engine.Engine, router.SecureProvider, router.SecureProvider) {
	rtr := router.New(router.Config{})
	p1 := security.WrapProvider(&mockProvider{name: "anthropic", defaultModel: "claude-3-5-sonnet"}, nil)
	p2 := security.WrapProvider(&mockProvider{name: "openai", defaultModel: "gpt-4o"}, nil)

	rtr.RegisterArm(&router.Arm{
		ID:           router.NewArmID("anthropic", "claude-3-5-sonnet"),
		Provider:     p1,
		ModelName:    "claude-3-5-sonnet",
		Capabilities: provider.Capabilities{ToolUse: true},
	})
	rtr.RegisterArm(&router.Arm{
		ID:           router.NewArmID("openai", "gpt-4o"),
		Provider:     p2,
		ModelName:    "gpt-4o",
		Capabilities: provider.Capabilities{ToolUse: true},
	})
	rtr.RegisterArm(&router.Arm{
		ID:           router.NewArmID("openai", "gpt-3.5-turbo"),
		Provider:     p2,
		ModelName:    "gpt-3.5-turbo",
		Capabilities: provider.Capabilities{ToolUse: true},
	})

	eng, err := engine.New(engine.Config{
		Provider: p1,
		Model:    "claude-3-5-sonnet",
		Tools:    tool.NewRegistry(),
	})
	if err != nil {
		panic(err)
	}

	return rtr, eng, p1, p2
}

func TestGetAvailableProviders(t *testing.T) {
	rtr, _, _, _ := newTestRouterAndEngine()
	m := Model{
		config: Config{
			Router: rtr,
		},
	}

	provs := m.getAvailableProviders()
	if len(provs) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(provs))
	}
	if provs[0] != "anthropic" || provs[1] != "openai" {
		t.Errorf("expected [anthropic, openai], got %v", provs)
	}
}

func TestFindBestArmForProvider(t *testing.T) {
	rtr, _, _, _ := newTestRouterAndEngine()
	m := Model{
		config: Config{
			Router: rtr,
		},
	}

	// Should match the default model
	arm1 := m.findBestArmForProvider("openai")
	if arm1 == nil {
		t.Fatal("expected arm for openai")
	}
	if arm1.ModelName != "gpt-4o" {
		t.Errorf("expected gpt-4o, got %s", arm1.ModelName)
	}

	// Should fallback to first arm if default model not found
	rtr.RegisterArm(&router.Arm{
		ID:        router.NewArmID("unknown", "weird-model"),
		Provider:  security.WrapProvider(&mockProvider{name: "unknown", defaultModel: "missing"}, nil),
		ModelName: "weird-model",
	})
	arm2 := m.findBestArmForProvider("unknown")
	if arm2 == nil {
		t.Fatal("expected arm for unknown")
	}
	if arm2.ModelName != "weird-model" {
		t.Errorf("expected weird-model, got %s", arm2.ModelName)
	}
}

func TestCloseAllPickersResetsProvider(t *testing.T) {
	m := Model{providerPickerOpen: true}
	m = m.closeAllPickers()
	if m.providerPickerOpen {
		t.Error("providerPickerOpen should be false after closeAllPickers")
	}
}

func TestGetPickerItemCount_Provider(t *testing.T) {
	rtr, _, _, _ := newTestRouterAndEngine()
	m := Model{
		providerPickerOpen: true,
		config: Config{
			Router: rtr,
		},
	}
	count := m.getPickerItemCount()
	if count != 2 {
		t.Errorf("expected picker item count 2, got %d", count)
	}
}

func TestHandleProviderCommand_ArgsEmptyOpensPicker(t *testing.T) {
	rtr, eng, _, _ := newTestRouterAndEngine()
	sess := session.NewLocal(session.LocalConfig{
		Engine:   eng,
		Provider: "anthropic",
		Model:    "claude-3-5-sonnet",
	})
	m := Model{
		session: sess,
		config: Config{
			Router: rtr,
			Engine: eng,
		},
	}

	res, err := m.handleCommand("/provider")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	newM, ok := res.(Model)
	if !ok {
		t.Fatalf("expected Model type, got %T", res)
	}
	if !newM.providerPickerOpen {
		t.Error("expected provider picker to be open")
	}
}

func TestHandleProviderCommand_ArgsNotEmptySwitchesProvider(t *testing.T) {
	rtr, eng, _, _ := newTestRouterAndEngine()
	sess := session.NewLocal(session.LocalConfig{
		Engine:   eng,
		Provider: "anthropic",
		Model:    "claude-3-5-sonnet",
	})
	m := Model{
		session: sess,
		config: Config{
			Router: rtr,
			Engine: eng,
		},
	}

	res, err := m.handleCommand("/provider openai")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	newM, ok := res.(Model)
	if !ok {
		t.Fatalf("expected Model type, got %T", res)
	}
	if newM.providerPickerOpen {
		t.Error("expected provider picker to be closed")
	}

	status := newM.session.Status()
	if status.Provider != "openai" {
		t.Errorf("expected provider to switch to openai, got %s", status.Provider)
	}
	if status.Model != "gpt-4o" {
		t.Errorf("expected model to switch to gpt-4o, got %s", status.Model)
	}

	// Check messages contain switch system log
	found := false
	for _, msg := range newM.messages {
		if msg.role == "system" && strings.Contains(msg.content, "provider switched to: openai") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected switch system message in history")
	}
}

func TestConfigPanelTransitions(t *testing.T) {
	rtr, eng, _, _ := newTestRouterAndEngine()
	sess := session.NewLocal(session.LocalConfig{
		Engine:   eng,
		Provider: "anthropic",
		Model:    "claude-3-5-sonnet",
	})
	m := Model{
		session:         sess,
		configPanelOpen: true,
		config: Config{
			Router: rtr,
			Engine: eng,
		},
	}

	// 1. Select Provider (index 0)
	m.configSelected = 0
	m = m.applyConfigSetting()
	if m.configPanelOpen {
		t.Error("expected config panel to close when opening provider picker")
	}
	if !m.providerPickerOpen {
		t.Error("expected provider picker to open")
	}

	// Reset state
	m.configPanelOpen = true
	m.providerPickerOpen = false

	// 2. Select Model (index 1)
	m.configSelected = 1
	m = m.applyConfigSetting()
	if m.configPanelOpen {
		t.Error("expected config panel to close when opening model picker")
	}
	if !m.modelPickerOpen {
		t.Error("expected model picker to open")
	}
}

func TestConfigPanelTransitionsWithSLM(t *testing.T) {
	rtr, eng, _, _ := newTestRouterAndEngine()
	sess := session.NewLocal(session.LocalConfig{
		Engine:   eng,
		Provider: "anthropic",
		Model:    "claude-3-5-sonnet",
	})
	m := Model{
		session:         sess,
		configPanelOpen: true,
		config: Config{
			Router: rtr,
			Engine: eng,
			SLM: SLMInfo{
				Active: true,
			},
		},
	}

	// 1. Verify getActiveSettings only has permission and incognito
	settings := m.getActiveSettings()
	if len(settings) != 2 {
		t.Fatalf("expected 2 settings when SLM is active, got %d", len(settings))
	}
	if settings[0] != "permission" || settings[1] != "incognito" {
		t.Errorf("expected settings to be [permission, incognito], got %v", settings)
	}

	// 2. Try handling /model slash command — it should add a system message and not open picker
	retM, _ := m.handleCommand("/model")
	m2 := retM.(Model)
	if m2.modelPickerOpen {
		t.Error("expected model picker not to open when SLM is active")
	}
	if len(m2.messages) == 0 || m2.messages[len(m2.messages)-1].role != "system" {
		t.Error("expected system warning message for blocked model switch")
	}

	// 3. Try handling /provider slash command — it should add a system message and not open picker
	retP, _ := m.handleCommand("/provider")
	m3 := retP.(Model)
	if m3.providerPickerOpen {
		t.Error("expected provider picker not to open when SLM is active")
	}
	if len(m3.messages) == 0 || m3.messages[len(m3.messages)-1].role != "system" {
		t.Error("expected system warning message for blocked provider switch")
	}

	// 4. Verify rendering output mentions "router" instead of anthropic/claude-3-5-sonnet
	statusStr := m.renderStatus()
	if !strings.Contains(statusStr, "router") {
		t.Errorf("expected status bar to contain 'router' when SLM is active, got: %q", statusStr)
	}
	if strings.Contains(statusStr, "anthropic") {
		t.Errorf("expected status bar to hide 'anthropic' when SLM is active, got: %q", statusStr)
	}

	chatStr := m.renderChat(80)
	if !strings.Contains(chatStr, "router (slm:") {
		t.Errorf("expected header to contain 'router (slm:' when SLM is active, got: %q", chatStr)
	}
	if strings.Contains(chatStr, "anthropic") {
		t.Errorf("expected header to hide 'anthropic' when SLM is active, got: %q", chatStr)
	}
}
