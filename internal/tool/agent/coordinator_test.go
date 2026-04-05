package agent_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"somegit.dev/Owlibou/gnoma/internal/tool/agent"
	"somegit.dev/Owlibou/gnoma/internal/tool/persist"
)

func makeTestStore(t *testing.T) *persist.Store {
	t.Helper()
	s := persist.New("test-coord-" + t.Name())
	t.Cleanup(func() { os.RemoveAll(s.Dir()) })
	return s
}

func TestListResultsTool_EmptyStore(t *testing.T) {
	s := makeTestStore(t)
	tool := agent.NewListResultsTool(s)
	args, _ := json.Marshal(map[string]string{})
	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Output, "no results") && result.Output != "" {
		// both "no results" message and empty string are acceptable
	}
	// Verify it doesn't error on empty store
	if strings.Contains(result.Output, "error") {
		t.Errorf("unexpected error output for empty store: %s", result.Output)
	}
}

func TestListResultsTool_ListsFiles(t *testing.T) {
	s := makeTestStore(t)
	big := strings.Repeat("x", 1024)
	s.Save("bash", "toolu_aaa", big)
	s.Save("fs.grep", "toolu_bbb", big)

	tool := agent.NewListResultsTool(s)
	args, _ := json.Marshal(map[string]string{})
	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Output, "bash") {
		t.Errorf("expected bash in output, got: %s", result.Output)
	}
	if !strings.Contains(result.Output, "fs") {
		t.Errorf("expected fs in output, got: %s", result.Output)
	}
}

func TestListResultsTool_FilterByToolName(t *testing.T) {
	s := makeTestStore(t)
	big := strings.Repeat("x", 1024)
	s.Save("bash", "toolu_c1", big)
	s.Save("fs.read", "toolu_c2", big)

	tool := agent.NewListResultsTool(s)
	args, _ := json.Marshal(map[string]string{"filter": "bash"})
	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Output, "bash") {
		t.Errorf("filter should include bash, got: %s", result.Output)
	}
	if strings.Contains(result.Output, "fs") {
		t.Errorf("filter should exclude fs.read, got: %s", result.Output)
	}
}

func TestReadResultTool_ReadsFile(t *testing.T) {
	s := makeTestStore(t)
	big := strings.Repeat("hello\n", 200)
	path, _ := s.Save("bash", "toolu_read1", big)

	tool := agent.NewReadResultTool(s)
	args, _ := json.Marshal(map[string]string{"path": path})
	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Output, "hello") {
		t.Errorf("expected file content in output, got: %s", result.Output)
	}
}

func TestReadResultTool_RejectsPathTraversal(t *testing.T) {
	s := makeTestStore(t)
	tool := agent.NewReadResultTool(s)
	args, _ := json.Marshal(map[string]string{"path": "/etc/passwd"})
	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute must not return a hard error; soft rejection in Output expected, got: %v", err)
	}
	if !strings.Contains(result.Output, "outside") {
		t.Errorf("expected 'outside session directory' rejection, got: %s", result.Output)
	}
}
