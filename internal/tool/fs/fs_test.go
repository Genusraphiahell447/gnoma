package fs

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- Read ---

func TestReadTool_Interface(t *testing.T) {
	r := NewReadTool()
	if r.Name() != "fs.read" {
		t.Errorf("Name() = %q", r.Name())
	}
	if !r.IsReadOnly() {
		t.Error("should be read-only")
	}
	if r.IsDestructive() {
		t.Error("should not be destructive")
	}
}

func TestReadTool_SimpleFile(t *testing.T) {
	path := writeTestFile(t, "hello\nworld\n")
	r := NewReadTool()

	result, err := r.Execute(context.Background(), mustJSON(t, readArgs{Path: path}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result.Output, "1\thello") {
		t.Errorf("Output should contain line-numbered content, got %q", result.Output)
	}
	if !strings.Contains(result.Output, "2\tworld") {
		t.Errorf("Output missing line 2, got %q", result.Output)
	}
}

func TestReadTool_WithOffset(t *testing.T) {
	path := writeTestFile(t, "line1\nline2\nline3\nline4\nline5\n")
	r := NewReadTool()

	result, err := r.Execute(context.Background(), mustJSON(t, readArgs{Path: path, Offset: 2}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result.Output, "3\tline3") {
		t.Errorf("Output should start at line 3, got %q", result.Output)
	}
	if strings.Contains(result.Output, "1\tline1") {
		t.Error("Output should not contain line 1")
	}
}

func TestReadTool_WithLimit(t *testing.T) {
	path := writeTestFile(t, "a\nb\nc\nd\ne\n")
	r := NewReadTool()

	result, err := r.Execute(context.Background(), mustJSON(t, readArgs{Path: path, Limit: 2}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	lines := strings.Split(result.Output, "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 lines, got %d: %q", len(lines), result.Output)
	}
	if result.Metadata["truncated"] != true {
		t.Error("should be truncated")
	}
}

func TestReadTool_OffsetPastEnd(t *testing.T) {
	path := writeTestFile(t, "one\ntwo\n")
	r := NewReadTool()

	result, err := r.Execute(context.Background(), mustJSON(t, readArgs{Path: path, Offset: 100}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result.Output, "past end") {
		t.Errorf("Output = %q, should mention past end", result.Output)
	}
}

func TestReadTool_FileNotFound(t *testing.T) {
	r := NewReadTool()
	result, err := r.Execute(context.Background(), mustJSON(t, readArgs{Path: "/nonexistent/file.txt"}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result.Output, "Error") {
		t.Errorf("Output = %q, should contain error", result.Output)
	}
}

func TestReadTool_EmptyPath(t *testing.T) {
	r := NewReadTool()
	_, err := r.Execute(context.Background(), mustJSON(t, readArgs{}))
	if err == nil {
		t.Error("expected error for empty path")
	}
}

// --- Write ---

func TestWriteTool_Interface(t *testing.T) {
	w := NewWriteTool()
	if w.Name() != "fs.write" {
		t.Errorf("Name() = %q", w.Name())
	}
	if w.IsReadOnly() {
		t.Error("should not be read-only")
	}
}

func TestWriteTool_CreateFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")

	w := NewWriteTool()
	result, err := w.Execute(context.Background(), mustJSON(t, writeArgs{Path: path, Content: "hello world"}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result.Output, "11 bytes") {
		t.Errorf("Output = %q", result.Output)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "hello world" {
		t.Errorf("file content = %q", string(data))
	}
}

func TestWriteTool_CreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a", "b", "c", "test.txt")

	w := NewWriteTool()
	_, err := w.Execute(context.Background(), mustJSON(t, writeArgs{Path: path, Content: "nested"}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "nested" {
		t.Errorf("file content = %q", string(data))
	}
}

func TestWriteTool_OverwriteExisting(t *testing.T) {
	path := writeTestFile(t, "old content")
	w := NewWriteTool()

	_, err := w.Execute(context.Background(), mustJSON(t, writeArgs{Path: path, Content: "new content"}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "new content" {
		t.Errorf("file content = %q", string(data))
	}
}

// --- Edit ---

func TestEditTool_Interface(t *testing.T) {
	e := NewEditTool()
	if e.Name() != "fs.edit" {
		t.Errorf("Name() = %q", e.Name())
	}
}

func TestEditTool_SingleReplace(t *testing.T) {
	path := writeTestFile(t, "hello world")
	e := NewEditTool()

	result, err := e.Execute(context.Background(), mustJSON(t, editArgs{
		Path: path, OldString: "world", NewString: "gnoma",
	}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result.Output, "1 occurrence") {
		t.Errorf("Output = %q", result.Output)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "hello gnoma" {
		t.Errorf("file content = %q", string(data))
	}
}

func TestEditTool_ReplaceAll(t *testing.T) {
	path := writeTestFile(t, "foo bar foo baz foo")
	e := NewEditTool()

	result, err := e.Execute(context.Background(), mustJSON(t, editArgs{
		Path: path, OldString: "foo", NewString: "qux", ReplaceAll: true,
	}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result.Output, "3 occurrence") {
		t.Errorf("Output = %q", result.Output)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "qux bar qux baz qux" {
		t.Errorf("file content = %q", string(data))
	}
}

func TestEditTool_NonUniqueWithoutReplaceAll(t *testing.T) {
	path := writeTestFile(t, "foo foo foo")
	e := NewEditTool()

	result, err := e.Execute(context.Background(), mustJSON(t, editArgs{
		Path: path, OldString: "foo", NewString: "bar",
	}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result.Output, "3 matches") {
		t.Errorf("Output = %q, should mention multiple matches", result.Output)
	}

	// File should be unchanged
	data, _ := os.ReadFile(path)
	if string(data) != "foo foo foo" {
		t.Errorf("file should be unchanged, got %q", string(data))
	}
}

func TestEditTool_NotFound(t *testing.T) {
	path := writeTestFile(t, "hello world")
	e := NewEditTool()

	result, err := e.Execute(context.Background(), mustJSON(t, editArgs{
		Path: path, OldString: "missing", NewString: "replaced",
	}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result.Output, "not found") {
		t.Errorf("Output = %q, should mention not found", result.Output)
	}
}

func TestEditTool_SameStrings(t *testing.T) {
	e := NewEditTool()
	_, err := e.Execute(context.Background(), mustJSON(t, editArgs{
		Path: "/tmp/x", OldString: "same", NewString: "same",
	}))
	if err == nil {
		t.Error("expected error when old_string == new_string")
	}
}

// --- Glob ---

func TestGlobTool_Interface(t *testing.T) {
	g := NewGlobTool()
	if g.Name() != "fs.glob" {
		t.Errorf("Name() = %q", g.Name())
	}
	if !g.IsReadOnly() {
		t.Error("should be read-only")
	}
}

func TestGlobTool_MatchFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0o644)
	os.WriteFile(filepath.Join(dir, "test.go"), []byte("package main"), 0o644)
	os.WriteFile(filepath.Join(dir, "readme.md"), []byte("# readme"), 0o644)

	g := NewGlobTool()
	result, err := g.Execute(context.Background(), mustJSON(t, globArgs{Pattern: "*.go", Path: dir}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if result.Metadata["count"] != 2 {
		t.Errorf("count = %v, want 2", result.Metadata["count"])
	}
	if !strings.Contains(result.Output, "main.go") {
		t.Errorf("Output missing main.go: %q", result.Output)
	}
	if strings.Contains(result.Output, "readme.md") {
		t.Error("Output should not contain readme.md")
	}
}

func TestGlobTool_NoMatches(t *testing.T) {
	dir := t.TempDir()
	g := NewGlobTool()

	result, err := g.Execute(context.Background(), mustJSON(t, globArgs{Pattern: "*.xyz", Path: dir}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result.Output, "no matches") {
		t.Errorf("Output = %q", result.Output)
	}
}

// --- Grep ---

func TestGrepTool_Interface(t *testing.T) {
	g := NewGrepTool()
	if g.Name() != "fs.grep" {
		t.Errorf("Name() = %q", g.Name())
	}
	if !g.IsReadOnly() {
		t.Error("should be read-only")
	}
}

func TestGrepTool_SingleFile(t *testing.T) {
	path := writeTestFile(t, "hello world\nfoo bar\nhello again\n")
	g := NewGrepTool()

	result, err := g.Execute(context.Background(), mustJSON(t, grepArgs{Pattern: "hello", Path: path}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Metadata["count"] != 2 {
		t.Errorf("count = %v, want 2", result.Metadata["count"])
	}
	if !strings.Contains(result.Output, "1:hello world") {
		t.Errorf("Output = %q", result.Output)
	}
}

func TestGrepTool_Directory(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("func main() {}\nfunc helper() {}"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.go"), []byte("func test() {}"), 0o644)
	os.WriteFile(filepath.Join(dir, "c.txt"), []byte("func ignored() {}"), 0o644)

	g := NewGrepTool()

	// Search all files for "func"
	result, err := g.Execute(context.Background(), mustJSON(t, grepArgs{Pattern: "func", Path: dir}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Metadata["count"].(int) < 3 {
		t.Errorf("count = %v, want >= 3", result.Metadata["count"])
	}

	// With glob filter
	result, err = g.Execute(context.Background(), mustJSON(t, grepArgs{Pattern: "func", Path: dir, Glob: "*.go"}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.Contains(result.Output, "c.txt") {
		t.Error("should not match .txt files with *.go glob")
	}
}

func TestGrepTool_Regex(t *testing.T) {
	path := writeTestFile(t, "error: something failed\nwarning: be careful\nerror: another one\n")
	g := NewGrepTool()

	result, err := g.Execute(context.Background(), mustJSON(t, grepArgs{Pattern: `^error:`, Path: path}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Metadata["count"] != 2 {
		t.Errorf("count = %v, want 2", result.Metadata["count"])
	}
}

func TestGrepTool_InvalidRegex(t *testing.T) {
	g := NewGrepTool()
	result, err := g.Execute(context.Background(), mustJSON(t, grepArgs{Pattern: "[invalid", Path: "."}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result.Output, "Invalid regex") {
		t.Errorf("Output = %q, should mention invalid regex", result.Output)
	}
}

func TestGrepTool_NoMatches(t *testing.T) {
	path := writeTestFile(t, "hello world\n")
	g := NewGrepTool()

	result, err := g.Execute(context.Background(), mustJSON(t, grepArgs{Pattern: "zzzzz", Path: path}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result.Output, "no matches") {
		t.Errorf("Output = %q", result.Output)
	}
}

func TestGrepTool_MaxResults(t *testing.T) {
	var lines strings.Builder
	for i := 0; i < 100; i++ {
		lines.WriteString("match line\n")
	}
	path := writeTestFile(t, lines.String())
	g := NewGrepTool()

	result, err := g.Execute(context.Background(), mustJSON(t, grepArgs{Pattern: "match", Path: path, MaxResults: 5}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Metadata["count"] != 5 {
		t.Errorf("count = %v, want 5", result.Metadata["count"])
	}
	if result.Metadata["truncated"] != true {
		t.Error("should be truncated")
	}
}

// --- LS ---

func TestLSTool_Interface(t *testing.T) {
	l := NewLSTool()
	if l.Name() != "fs.ls" {
		t.Errorf("Name() = %q", l.Name())
	}
	if !l.IsReadOnly() {
		t.Error("should be read-only")
	}
}

func TestLSTool_ListDirectory(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "hello.go"), []byte("package main"), 0o644)
	os.WriteFile(filepath.Join(dir, "readme.md"), []byte("# readme"), 0o644)
	os.MkdirAll(filepath.Join(dir, "subdir"), 0o755)

	l := NewLSTool()
	result, err := l.Execute(context.Background(), mustJSON(t, lsArgs{Path: dir}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !strings.Contains(result.Output, "hello.go") {
		t.Errorf("Output missing hello.go: %q", result.Output)
	}
	if !strings.Contains(result.Output, "readme.md") {
		t.Errorf("Output missing readme.md: %q", result.Output)
	}
	if !strings.Contains(result.Output, "subdir") {
		t.Errorf("Output missing subdir: %q", result.Output)
	}

	if result.Metadata["files"] != 2 {
		t.Errorf("files = %v, want 2", result.Metadata["files"])
	}
	if result.Metadata["dirs"] != 1 {
		t.Errorf("dirs = %v, want 1", result.Metadata["dirs"])
	}
}

func TestLSTool_EmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	l := NewLSTool()

	result, err := l.Execute(context.Background(), mustJSON(t, lsArgs{Path: dir}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result.Output, "empty directory") {
		t.Errorf("Output = %q, should mention empty", result.Output)
	}
}

func TestLSTool_DirectoryNotFound(t *testing.T) {
	l := NewLSTool()
	result, err := l.Execute(context.Background(), mustJSON(t, lsArgs{Path: "/nonexistent/dir"}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result.Output, "Error") {
		t.Errorf("Output = %q, should contain error", result.Output)
	}
}

func TestLSTool_ShowsSizes(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "small.txt"), []byte("hi"), 0o644)

	l := NewLSTool()
	result, err := l.Execute(context.Background(), mustJSON(t, lsArgs{Path: dir}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// Should show "2B" for a 2-byte file
	if !strings.Contains(result.Output, "2B") {
		t.Errorf("Output = %q, should show file size", result.Output)
	}
}

func TestFormatSize(t *testing.T) {
	tests := []struct {
		bytes int64
		want  string
	}{
		{0, "0B"},
		{42, "42B"},
		{1024, "1.0K"},
		{1536, "1.5K"},
		{1048576, "1.0M"},
		{1073741824, "1.0G"},
	}
	for _, tt := range tests {
		got := formatSize(tt.bytes)
		if got != tt.want {
			t.Errorf("formatSize(%d) = %q, want %q", tt.bytes, got, tt.want)
		}
	}
}

// --- Helpers ---

func writeTestFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writeTestFile: %v", err)
	}
	return path
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return data
}
