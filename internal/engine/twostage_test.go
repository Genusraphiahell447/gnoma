package engine

import (
	"context"
	"encoding/json"
	"slices"
	"sort"
	"testing"

	"somegit.dev/Owlibou/gnoma/internal/message"
	"somegit.dev/Owlibou/gnoma/internal/provider"
	"somegit.dev/Owlibou/gnoma/internal/router"
	"somegit.dev/Owlibou/gnoma/internal/tool"
)

// categorizedMockTool extends mockTool with a Category() method so tests can
// exercise the two-stage category filter.
type categorizedMockTool struct {
	mockTool
	cat tool.Category
}

func (c *categorizedMockTool) Category() tool.Category { return c.cat }

// twoStageEngine builds an engine wired to a small local forced arm so
// useTwoStageTools() returns true.
func twoStageEngine(t *testing.T, reg *tool.Registry) *Engine {
	t.Helper()
	rtr := router.New(router.Config{})
	rtr.RegisterArm(&router.Arm{
		ID:           "llamacpp/qwen3-1b",
		Provider:     secureMock(&mockProvider{name: "llamacpp"}),
		ModelName:    "qwen3-1b",
		IsLocal:      true,
		Capabilities: provider.Capabilities{ToolUse: true, ContextWindow: 8192},
	})
	rtr.ForceArm("llamacpp/qwen3-1b")
	e, err := New(Config{
		Provider: secureMock(&mockProvider{name: "llamacpp"}),
		Router:   rtr,
		Tools:    reg,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return e
}

func TestUseTwoStageTools(t *testing.T) {
	smallLocal := &router.Arm{
		ID:           "llamacpp/qwen3-1b",
		Provider:     secureMock(&mockProvider{name: "llamacpp"}),
		ModelName:    "qwen3-1b",
		IsLocal:      true,
		Capabilities: provider.Capabilities{ToolUse: true, ContextWindow: 8192},
	}
	bigLocal := &router.Arm{
		ID:           "llamacpp/qwen3-30b",
		Provider:     secureMock(&mockProvider{name: "llamacpp"}),
		ModelName:    "qwen3-30b",
		IsLocal:      true,
		Capabilities: provider.Capabilities{ToolUse: true, ContextWindow: 32768},
	}
	cloud := &router.Arm{
		ID:           "anthropic/sonnet",
		Provider:     secureMock(&mockProvider{name: "anthropic"}),
		ModelName:    "sonnet",
		IsLocal:      false,
		Capabilities: provider.Capabilities{ToolUse: true, ContextWindow: 200000},
	}
	localUnknownCtx := &router.Arm{
		ID:           "ollama/mystery",
		Provider:     secureMock(&mockProvider{name: "ollama"}),
		ModelName:    "mystery",
		IsLocal:      true,
		Capabilities: provider.Capabilities{ToolUse: true, ContextWindow: 0},
	}

	cases := []struct {
		name    string
		arm     *router.Arm
		forced  bool
		want    bool
		message string
	}{
		{
			name: "small local arm triggers two-stage",
			arm:  smallLocal,
			want: true,
		},
		{
			name: "large local arm does not trigger two-stage",
			arm:  bigLocal,
			want: false,
		},
		{
			name: "cloud arm never triggers two-stage",
			arm:  cloud,
			want: false,
		},
		{
			name: "local arm with unknown context window triggers two-stage",
			arm:  localUnknownCtx,
			want: true,
		},
		{
			name:   "ForceTwoStageTools overrides cloud arm",
			arm:    cloud,
			forced: true,
			want:   true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rtr := router.New(router.Config{})
			rtr.RegisterArm(tc.arm)
			rtr.ForceArm(tc.arm.ID)

			e, err := New(Config{
				Provider:           secureMock(&mockProvider{name: string(tc.arm.ID.Provider())}),
				Router:             rtr,
				Tools:              tool.NewRegistry(),
				ForceTwoStageTools: tc.forced,
			})
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			if got := e.useTwoStageTools(); got != tc.want {
				t.Errorf("useTwoStageTools() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestUseTwoStageTools_NoRouter(t *testing.T) {
	e, err := New(Config{
		Provider: secureMock(&mockProvider{name: "anthropic"}),
		Tools:    tool.NewRegistry(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if e.useTwoStageTools() {
		t.Error("useTwoStageTools() = true, want false when no router")
	}
}

func TestUseTwoStageTools_NoRouter_ForcedOverride(t *testing.T) {
	e, err := New(Config{
		Provider:           secureMock(&mockProvider{name: "anthropic"}),
		Tools:              tool.NewRegistry(),
		ForceTwoStageTools: true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if !e.useTwoStageTools() {
		t.Error("useTwoStageTools() = false, want true when ForceTwoStageTools is set even without router")
	}
}

func TestUseTwoStageTools_NoForcedArm(t *testing.T) {
	rtr := router.New(router.Config{})
	rtr.RegisterArm(&router.Arm{
		ID:           "llamacpp/qwen3-1b",
		Provider:     secureMock(&mockProvider{name: "llamacpp"}),
		ModelName:    "qwen3-1b",
		IsLocal:      true,
		Capabilities: provider.Capabilities{ToolUse: true, ContextWindow: 8192},
	})
	// No ForceArm called — multi-arm routing
	e, err := New(Config{
		Provider: secureMock(&mockProvider{name: "llamacpp"}),
		Router:   rtr,
		Tools:    tool.NewRegistry(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if e.useTwoStageTools() {
		t.Error("useTwoStageTools() = true, want false for multi-arm routing")
	}
}

func TestBuildRequest_TwoStage_Round1_EmitsSyntheticOnly(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Register(&categorizedMockTool{mockTool: mockTool{name: "fs.read", readOnly: true}, cat: tool.CategoryRead})
	reg.Register(&categorizedMockTool{mockTool: mockTool{name: "fs.write"}, cat: tool.CategoryWrite})
	reg.Register(&categorizedMockTool{mockTool: mockTool{name: "bash"}, cat: tool.CategoryExec})

	e := twoStageEngine(t, reg)

	req := e.buildRequest(context.Background())

	if len(req.Tools) != 1 {
		t.Fatalf("round 1 should emit exactly one tool (synthetic); got %d: %+v", len(req.Tools), req.Tools)
	}
	if req.Tools[0].Name != SyntheticSelectCategoryName {
		t.Errorf("round 1 tool name = %q, want %q", req.Tools[0].Name, SyntheticSelectCategoryName)
	}
	if req.ToolChoice != provider.ToolChoiceRequired {
		t.Errorf("round 1 ToolChoice = %q, want %q", req.ToolChoice, provider.ToolChoiceRequired)
	}
}

func TestBuildRequest_TwoStage_Round1_SyntheticEnumMatchesAllCategories(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Register(&categorizedMockTool{mockTool: mockTool{name: "fs.read", readOnly: true}, cat: tool.CategoryRead})
	e := twoStageEngine(t, reg)

	req := e.buildRequest(context.Background())
	if len(req.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(req.Tools))
	}

	var schema struct {
		Properties struct {
			Category struct {
				Enum []string `json:"enum"`
			} `json:"category"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(req.Tools[0].Parameters, &schema); err != nil {
		t.Fatalf("unmarshal synthetic params: %v", err)
	}
	want := make([]string, 0, len(tool.AllCategories()))
	for _, c := range tool.AllCategories() {
		want = append(want, string(c))
	}
	got := slices.Clone(schema.Properties.Category.Enum)
	sort.Strings(got)
	sort.Strings(want)
	if !slices.Equal(got, want) {
		t.Errorf("category enum = %v, want %v", got, want)
	}
}

func TestBuildRequest_TwoStage_Round2_FiltersByCategory(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Register(&categorizedMockTool{mockTool: mockTool{name: "fs.read", readOnly: true}, cat: tool.CategoryRead})
	reg.Register(&categorizedMockTool{mockTool: mockTool{name: "fs.write"}, cat: tool.CategoryWrite})
	reg.Register(&categorizedMockTool{mockTool: mockTool{name: "fs.edit"}, cat: tool.CategoryWrite})
	reg.Register(&categorizedMockTool{mockTool: mockTool{name: "bash"}, cat: tool.CategoryExec})

	e := twoStageEngine(t, reg)
	e.setSelectedCategory(tool.CategoryWrite)

	req := e.buildRequest(context.Background())

	names := toolNamesIn(req.Tools)
	sort.Strings(names)
	want := []string{SyntheticSelectCategoryName, "fs.edit", "fs.write"}
	sort.Strings(want)
	if !slices.Equal(names, want) {
		t.Errorf("round 2 tools = %v, want %v", names, want)
	}

	// ToolChoice should not be forced in round 2 — let the model decide.
	if req.ToolChoice == provider.ToolChoiceRequired {
		t.Errorf("round 2 ToolChoice = %q, should not be Required", req.ToolChoice)
	}
}

func TestBuildRequest_TwoStage_UncategorizedToolDefaultsToMeta(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Register(&mockTool{name: "agent"}) // no Category() method → meta
	reg.Register(&categorizedMockTool{mockTool: mockTool{name: "fs.read", readOnly: true}, cat: tool.CategoryRead})

	e := twoStageEngine(t, reg)
	e.setSelectedCategory(tool.CategoryMeta)

	req := e.buildRequest(context.Background())
	names := toolNamesIn(req.Tools)
	if !slices.Contains(names, "agent") {
		t.Errorf("meta filter should include uncategorized 'agent'; got %v", names)
	}
	if slices.Contains(names, "fs.read") {
		t.Errorf("meta filter should exclude 'fs.read'; got %v", names)
	}
}

func TestBuildRequest_NonTwoStage_UnchangedBehavior(t *testing.T) {
	// Large local arm — two-stage should not activate.
	rtr := router.New(router.Config{})
	rtr.RegisterArm(&router.Arm{
		ID:           "llamacpp/qwen3-30b",
		Provider:     secureMock(&mockProvider{name: "llamacpp"}),
		ModelName:    "qwen3-30b",
		IsLocal:      true,
		Capabilities: provider.Capabilities{ToolUse: true, ContextWindow: 32768},
	})
	rtr.ForceArm("llamacpp/qwen3-30b")

	reg := tool.NewRegistry()
	reg.Register(&categorizedMockTool{mockTool: mockTool{name: "fs.read", readOnly: true}, cat: tool.CategoryRead})
	reg.Register(&categorizedMockTool{mockTool: mockTool{name: "fs.write"}, cat: tool.CategoryWrite})

	e, err := New(Config{
		Provider: secureMock(&mockProvider{name: "llamacpp"}),
		Router:   rtr,
		Tools:    reg,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	req := e.buildRequest(context.Background())
	if len(req.Tools) != 2 {
		t.Errorf("non-two-stage path: got %d tools, want 2", len(req.Tools))
	}
	for _, td := range req.Tools {
		if td.Name == SyntheticSelectCategoryName {
			t.Errorf("non-two-stage path should not emit synthetic select_category")
		}
	}
}

func TestInterceptSelectCategoryCalls_UpdatesStateAndReturnsResult(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Register(&categorizedMockTool{mockTool: mockTool{name: "fs.write"}, cat: tool.CategoryWrite})

	e := twoStageEngine(t, reg)

	call := message.ToolCall{
		ID:        "call-1",
		Name:      SyntheticSelectCategoryName,
		Arguments: json.RawMessage(`{"category": "write"}`),
	}
	real, synth := e.interceptSelectCategoryCalls([]message.ToolCall{call})

	if len(real) != 0 {
		t.Errorf("real calls = %d, want 0", len(real))
	}
	if len(synth) != 1 {
		t.Fatalf("synthetic results = %d, want 1", len(synth))
	}
	if synth[0].IsError {
		t.Errorf("synthetic result should not be error: %s", synth[0].Content)
	}
	if synth[0].ToolCallID != "call-1" {
		t.Errorf("ToolCallID = %q, want call-1", synth[0].ToolCallID)
	}
	if e.snapshotSelectedCategory() != tool.CategoryWrite {
		t.Errorf("selectedCategory = %q, want write", e.snapshotSelectedCategory())
	}
}

func TestInterceptSelectCategoryCalls_InvalidCategoryReturnsError(t *testing.T) {
	e := twoStageEngine(t, tool.NewRegistry())

	call := message.ToolCall{
		ID:        "call-1",
		Name:      SyntheticSelectCategoryName,
		Arguments: json.RawMessage(`{"category": "bogus"}`),
	}
	_, synth := e.interceptSelectCategoryCalls([]message.ToolCall{call})
	if len(synth) != 1 {
		t.Fatalf("synthetic results = %d, want 1", len(synth))
	}
	if !synth[0].IsError {
		t.Errorf("invalid category should yield error result")
	}
	if e.snapshotSelectedCategory() != "" {
		t.Errorf("invalid category should clear selectedCategory; got %q", e.snapshotSelectedCategory())
	}
}

func TestInterceptSelectCategoryCalls_InvalidJSONReturnsError(t *testing.T) {
	e := twoStageEngine(t, tool.NewRegistry())

	call := message.ToolCall{
		ID:        "call-1",
		Name:      SyntheticSelectCategoryName,
		Arguments: json.RawMessage(`not-json`),
	}
	_, synth := e.interceptSelectCategoryCalls([]message.ToolCall{call})
	if len(synth) != 1 || !synth[0].IsError {
		t.Fatalf("invalid JSON should yield single error result")
	}
}

func TestInterceptSelectCategoryCalls_MixedRealAndSynthetic(t *testing.T) {
	e := twoStageEngine(t, tool.NewRegistry())

	calls := []message.ToolCall{
		{ID: "real-1", Name: "fs.read", Arguments: json.RawMessage(`{}`)},
		{ID: "synth-1", Name: SyntheticSelectCategoryName, Arguments: json.RawMessage(`{"category":"read"}`)},
		{ID: "real-2", Name: "bash", Arguments: json.RawMessage(`{}`)},
	}
	real, synth := e.interceptSelectCategoryCalls(calls)
	if len(real) != 2 {
		t.Errorf("real calls = %d, want 2", len(real))
	}
	if len(synth) != 1 {
		t.Errorf("synthetic results = %d, want 1", len(synth))
	}
	if e.snapshotSelectedCategory() != tool.CategoryRead {
		t.Errorf("selectedCategory = %q, want read", e.snapshotSelectedCategory())
	}
}

func TestResetTwoStageState_ClearsCategory(t *testing.T) {
	e := twoStageEngine(t, tool.NewRegistry())
	e.setSelectedCategory(tool.CategoryExec)
	e.resetTwoStageState()
	if e.snapshotSelectedCategory() != "" {
		t.Errorf("resetTwoStageState should clear category; got %q", e.snapshotSelectedCategory())
	}
}

func toolNamesIn(defs []provider.ToolDefinition) []string {
	names := make([]string, len(defs))
	for i, d := range defs {
		names[i] = d.Name
	}
	return names
}
