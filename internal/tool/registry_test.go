package tool

import (
	"context"
	"encoding/json"
	"slices"
	"sort"
	"testing"
)

// stubTool is a minimal Tool implementation for testing.
type stubTool struct {
	name        string
	description string
	params      json.RawMessage
	readOnly    bool
	destructive bool
	execFn      func(ctx context.Context, args json.RawMessage) (Result, error)
}

func (s *stubTool) Name() string                { return s.name }
func (s *stubTool) Description() string         { return s.description }
func (s *stubTool) Parameters() json.RawMessage { return s.params }
func (s *stubTool) IsReadOnly() bool            { return s.readOnly }
func (s *stubTool) IsDestructive() bool         { return s.destructive }
func (s *stubTool) Execute(ctx context.Context, args json.RawMessage) (Result, error) {
	if s.execFn != nil {
		return s.execFn(ctx, args)
	}
	return Result{Output: "ok"}, nil
}

func TestRegistry_RegisterAndGet(t *testing.T) {
	r := NewRegistry()
	r.Register(&stubTool{name: "bash", description: "run commands"})

	tool, ok := r.Get("bash")
	if !ok {
		t.Fatal("Get(bash) should find tool")
	}
	if tool.Name() != "bash" {
		t.Errorf("Name() = %q", tool.Name())
	}
}

func TestRegistry_Get_NotFound(t *testing.T) {
	r := NewRegistry()
	_, ok := r.Get("nonexistent")
	if ok {
		t.Error("Get(nonexistent) should return false")
	}
}

func TestRegistry_Register_Overwrite(t *testing.T) {
	r := NewRegistry()
	r.Register(&stubTool{name: "bash", description: "old"})
	r.Register(&stubTool{name: "bash", description: "new"})

	tool, _ := r.Get("bash")
	if tool.Description() != "new" {
		t.Errorf("Description() = %q, want 'new' (overwritten)", tool.Description())
	}
}

func TestRegistry_All(t *testing.T) {
	r := NewRegistry()
	r.Register(&stubTool{name: "bash"})
	r.Register(&stubTool{name: "fs.read"})
	r.Register(&stubTool{name: "fs.write"})

	all := r.All()
	if len(all) != 3 {
		t.Fatalf("len(All()) = %d, want 3", len(all))
	}

	names := make([]string, len(all))
	for i, t := range all {
		names[i] = t.Name()
	}
	sort.Strings(names)

	want := []string{"bash", "fs.read", "fs.write"}
	if !slices.Equal(names, want) {
		t.Errorf("All() names = %v, want %v", names, want)
	}
}

func TestRegistry_Definitions(t *testing.T) {
	r := NewRegistry()
	r.Register(&stubTool{
		name:        "bash",
		description: "Run a command",
		params:      json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"}}}`),
	})
	r.Register(&stubTool{
		name:        "fs.read",
		description: "Read a file",
		params:      json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`),
	})

	defs := r.Definitions()
	if len(defs) != 2 {
		t.Fatalf("len(Definitions()) = %d, want 2", len(defs))
	}

	// Find bash definition
	var bashDef *Definition
	for i := range defs {
		if defs[i].Name == "bash" {
			bashDef = &defs[i]
			break
		}
	}
	if bashDef == nil {
		t.Fatal("bash definition not found")
	}
	if bashDef.Description != "Run a command" {
		t.Errorf("bash.Description = %q", bashDef.Description)
	}
	if bashDef.Parameters == nil {
		t.Error("bash.Parameters should not be nil")
	}
}

func TestRegistry_MustGet_Panics(t *testing.T) {
	r := NewRegistry()

	defer func() {
		if r := recover(); r == nil {
			t.Error("MustGet should panic for missing tool")
		}
	}()

	r.MustGet("nonexistent")
}

func TestRegistry_MustGet_Success(t *testing.T) {
	r := NewRegistry()
	r.Register(&stubTool{name: "bash"})

	tool := r.MustGet("bash")
	if tool.Name() != "bash" {
		t.Errorf("Name() = %q", tool.Name())
	}
}

func TestRegistry_Empty(t *testing.T) {
	r := NewRegistry()

	if len(r.All()) != 0 {
		t.Error("empty registry should return no tools")
	}
	if len(r.Definitions()) != 0 {
		t.Error("empty registry should return no definitions")
	}
}

func TestStubTool_Execute(t *testing.T) {
	called := false
	tool := &stubTool{
		name: "test",
		execFn: func(ctx context.Context, args json.RawMessage) (Result, error) {
			called = true
			var input struct{ Value string }
			_ = json.Unmarshal(args, &input)
			return Result{
				Output:   "processed: " + input.Value,
				Metadata: map[string]any{"key": "val"},
			}, nil
		},
	}

	result, err := tool.Execute(context.Background(), json.RawMessage(`{"Value":"hello"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !called {
		t.Error("execFn should have been called")
	}
	if result.Output != "processed: hello" {
		t.Errorf("Output = %q", result.Output)
	}
	if result.Metadata["key"] != "val" {
		t.Errorf("Metadata = %v", result.Metadata)
	}
}

func TestToolInterface_ReadOnlyDestructive(t *testing.T) {
	readTool := &stubTool{name: "fs.read", readOnly: true, destructive: false}
	writeTool := &stubTool{name: "fs.write", readOnly: false, destructive: false}
	deleteTool := &stubTool{name: "bash.rm", readOnly: false, destructive: true}

	if !readTool.IsReadOnly() {
		t.Error("fs.read should be read-only")
	}
	if readTool.IsDestructive() {
		t.Error("fs.read should not be destructive")
	}
	if writeTool.IsReadOnly() {
		t.Error("fs.write should not be read-only")
	}
	if deleteTool.IsReadOnly() {
		t.Error("bash.rm should not be read-only")
	}
	if !deleteTool.IsDestructive() {
		t.Error("bash.rm should be destructive")
	}
}
