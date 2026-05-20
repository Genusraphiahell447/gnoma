package engine

import (
	"context"
	"testing"

	"somegit.dev/Owlibou/gnoma/internal/provider"
	"somegit.dev/Owlibou/gnoma/internal/router"
	"somegit.dev/Owlibou/gnoma/internal/tool"
)

func TestForcedArmSupportsTools_NoRouter(t *testing.T) {
	e := &Engine{cfg: Config{}}
	if !e.forcedArmSupportsTools() {
		t.Error("should return true when no router configured")
	}
}

func TestForcedArmSupportsTools_NoForcedArm(t *testing.T) {
	rtr := router.New(router.Config{})
	e := &Engine{cfg: Config{Router: rtr}}
	if !e.forcedArmSupportsTools() {
		t.Error("should return true when no forced arm (multi-arm routing)")
	}
}

func TestForcedArmSupportsTools_ArmWithTools(t *testing.T) {
	rtr := router.New(router.Config{})
	rtr.RegisterArm(&router.Arm{
		ID:           "llamacpp/qwen3",
		Provider:     secureMock(&mockProvider{name: "llamacpp"}),
		ModelName:    "qwen3",
		IsLocal:      true,
		Capabilities: provider.Capabilities{ToolUse: true},
	})
	rtr.ForceArm("llamacpp/qwen3")

	e := &Engine{cfg: Config{Router: rtr}}
	if !e.forcedArmSupportsTools() {
		t.Error("should return true when forced arm supports tools")
	}
}

func TestForcedArmSupportsTools_ArmWithoutTools(t *testing.T) {
	rtr := router.New(router.Config{})
	rtr.RegisterArm(&router.Arm{
		ID:           "llamacpp/gemma",
		Provider:     secureMock(&mockProvider{name: "llamacpp"}),
		ModelName:    "gemma",
		IsLocal:      true,
		Capabilities: provider.Capabilities{ToolUse: false},
	})
	rtr.ForceArm("llamacpp/gemma")

	e := &Engine{cfg: Config{Router: rtr}}
	if e.forcedArmSupportsTools() {
		t.Error("should return false when forced arm does not support tools")
	}
}

func TestBuildRequest_ForcedArmNoToolSupport_OmitsTools(t *testing.T) {
	rtr := router.New(router.Config{})
	rtr.RegisterArm(&router.Arm{
		ID:           "llamacpp/gemma",
		Provider:     secureMock(&mockProvider{name: "llamacpp"}),
		ModelName:    "gemma",
		IsLocal:      true,
		Capabilities: provider.Capabilities{ToolUse: false},
	})
	rtr.ForceArm("llamacpp/gemma")

	reg := tool.NewRegistry()
	reg.Register(&mockTool{name: "fs.read"})
	reg.Register(&mockTool{name: "bash"})

	e, err := New(Config{
		Provider: secureMock(&mockProvider{name: "llamacpp"}),
		Router:   rtr,
		Tools:    reg,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	req := e.buildRequest(context.Background())
	if len(req.Tools) != 0 {
		t.Errorf("buildRequest() included %d tools, want 0 for arm without tool support", len(req.Tools))
	}
}

func TestBuildRequest_ForcedArmWithToolSupport_IncludesTools(t *testing.T) {
	rtr := router.New(router.Config{})
	rtr.RegisterArm(&router.Arm{
		ID:        "llamacpp/qwen3",
		Provider:  secureMock(&mockProvider{name: "llamacpp"}),
		ModelName: "qwen3",
		IsLocal:   true,
		// ContextWindow > 16384 keeps two-stage routing inactive so this
		// test exercises the plain "tools included" path.
		Capabilities: provider.Capabilities{ToolUse: true, ContextWindow: 32768},
	})
	rtr.ForceArm("llamacpp/qwen3")

	reg := tool.NewRegistry()
	reg.Register(&mockTool{name: "fs.read"})
	reg.Register(&mockTool{name: "bash"})

	e, err := New(Config{
		Provider: secureMock(&mockProvider{name: "llamacpp"}),
		Router:   rtr,
		Tools:    reg,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	req := e.buildRequest(context.Background())
	if len(req.Tools) != 2 {
		t.Errorf("buildRequest() included %d tools, want 2 for arm with tool support", len(req.Tools))
	}
}

func TestBuildRequest_AllowedToolsFilter(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Register(&mockTool{name: "fs.ls"})
	reg.Register(&mockTool{name: "fs.read"})
	reg.Register(&mockTool{name: "fs.write"})
	reg.Register(&mockTool{name: "bash"})
	reg.Register(&mockTool{name: "agent"})

	e, err := New(Config{
		Provider: secureMock(&mockProvider{name: "llamacpp"}),
		Tools:    reg,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Without filter: all 5 tools
	req := e.buildRequest(context.Background())
	if len(req.Tools) != 5 {
		t.Errorf("unfiltered: got %d tools, want 5", len(req.Tools))
	}

	// With filter: only fs.ls and fs.write
	e.turnOpts.AllowedTools = []string{"fs.ls", "fs.write"}
	req = e.buildRequest(context.Background())
	if len(req.Tools) != 2 {
		t.Errorf("filtered: got %d tools, want 2", len(req.Tools))
	}
	names := make(map[string]bool)
	for _, td := range req.Tools {
		names[td.Name] = true
	}
	if !names["fs.ls"] || !names["fs.write"] {
		t.Errorf("filtered tools = %v, want fs.ls and fs.write", names)
	}
}

func TestBuildRequest_Temperature(t *testing.T) {
	temp := 0.7
	e, err := New(Config{
		Provider:    secureMock(&mockProvider{name: "test"}),
		Tools:       tool.NewRegistry(),
		Temperature: &temp,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	req := e.buildRequest(context.Background())
	if req.Temperature == nil {
		t.Fatal("expected Temperature in request, got nil")
	}
	if *req.Temperature != temp {
		t.Errorf("Temperature = %v, want %v", *req.Temperature, temp)
	}
}

func TestBuildRequest_TemperatureNilWhenNotSet(t *testing.T) {
	e, err := New(Config{
		Provider: secureMock(&mockProvider{name: "test"}),
		Tools:    tool.NewRegistry(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	req := e.buildRequest(context.Background())
	if req.Temperature != nil {
		t.Errorf("expected nil Temperature, got %v", *req.Temperature)
	}
}

func TestBuildRequest_MultiArmRouting_IncludesTools(t *testing.T) {
	rtr := router.New(router.Config{})
	rtr.RegisterArm(&router.Arm{
		ID:           "llamacpp/gemma",
		Provider:     secureMock(&mockProvider{name: "llamacpp"}),
		ModelName:    "gemma",
		IsLocal:      true,
		Capabilities: provider.Capabilities{ToolUse: false},
	})
	// No forced arm — multi-arm routing

	reg := tool.NewRegistry()
	reg.Register(&mockTool{name: "fs.read"})

	e, err := New(Config{
		Provider: secureMock(&mockProvider{name: "llamacpp"}),
		Router:   rtr,
		Tools:    reg,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	req := e.buildRequest(context.Background())
	if len(req.Tools) != 1 {
		t.Errorf("buildRequest() included %d tools, want 1 for multi-arm routing (no forced arm)", len(req.Tools))
	}
}
