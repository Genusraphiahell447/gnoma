package persist

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	minPersistSize = 1024 // bytes: results smaller than this are not persisted
	previewSize    = 2000 // chars shown inline in the LLM context
)

// ResultFile describes a persisted tool result.
type ResultFile struct {
	Path     string
	ToolName string
	CallID   string
	Size     int64
	ModTime  time.Time
}

// IncognitoGate is the minimal contract Store needs from
// security.IncognitoMode. Defined locally so the persist package keeps
// a stdlib-only dependency surface; *security.IncognitoMode satisfies
// this interface naturally.
//
// A nil IncognitoGate means "no gate" — Save runs unconditionally.
// A non-nil gate is consulted on every Save call (dynamic), so TUI
// runtime toggles take effect without reconstructing the Store.
type IncognitoGate interface {
	ShouldPersist() bool
}

// Store persists tool results to /tmp for cross-tool session sharing.
type Store struct {
	dir  string        // /tmp/gnoma-<sessionID>/tool-results
	mode IncognitoGate // nil = always persist; non-nil = consult on Save
}

// New creates a Store for the given session ID. Pass mode=nil for the
// pre-W2-2 behaviour (always persist when content is large enough).
// Pass fw.Incognito() to block persistence whenever incognito is active.
// The directory is created on first Save.
func New(sessionID string, mode IncognitoGate) *Store {
	return &Store{
		dir:  filepath.Join("/tmp", "gnoma-"+sessionID, "tool-results"),
		mode: mode,
	}
}

// Dir returns the absolute path to the tool-results directory.
func (s *Store) Dir() string { return s.dir }

// Save writes content to disk if len(content) >= minPersistSize and
// the configured incognito gate (if any) permits persistence.
// Returns (filePath, true) on persistence; ("", false) if content is too
// small, if incognito is active, or if a filesystem error occurred.
func (s *Store) Save(toolName, callID, content string) (string, bool) {
	if len(content) < minPersistSize {
		return "", false
	}
	if s.mode != nil && !s.mode.ShouldPersist() {
		return "", false
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		slog.Warn("persist: failed to create session directory", "dir", s.dir, "error", err)
		return "", false
	}
	// Sanitize tool name and call ID for filesystem (replace dots, slashes, path traversal)
	safeName := strings.NewReplacer(".", "_", "/", "_").Replace(toolName)
	safeCallID := strings.NewReplacer("/", "_", "..", "_").Replace(callID)
	filename := safeName + "-" + safeCallID + ".txt"
	path := filepath.Join(s.dir, filename)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		slog.Warn("persist: failed to write tool result", "path", path, "error", err)
		return "", false
	}
	return path, true
}

// InlineReplacement returns the string that replaces the full content in LLM context.
func InlineReplacement(path, content string) string {
	preview := content
	if len([]rune(preview)) > previewSize {
		preview = string([]rune(preview)[:previewSize])
	}
	return fmt.Sprintf("[Tool result saved: %s]\n\nPreview (first %d chars):\n%s",
		path, previewSize, preview)
}

// List returns all persisted results, optionally filtered by sanitized tool name prefix.
// Tool names are stored with dots and slashes replaced by underscores — e.g.,
// "fs.grep" is stored as "fs_grep". Pass the sanitized form as the filter.
// An empty filter returns all results.
// Returns nil (not error) if the session directory does not yet exist.
func (s *Store) List(toolNameFilter string) ([]ResultFile, error) {
	entries, err := os.ReadDir(s.dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var results []ResultFile
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".txt") {
			continue
		}
		toolName, callID, ok := parseFilename(e.Name())
		if !ok {
			continue
		}
		if toolNameFilter != "" && !strings.HasPrefix(toolName, toolNameFilter) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		results = append(results, ResultFile{
			Path:     filepath.Join(s.dir, e.Name()),
			ToolName: toolName,
			CallID:   callID,
			Size:     info.Size(),
			ModTime:  info.ModTime(),
		})
	}
	return results, nil
}

// Read returns the content of a persisted result file.
// Returns an error if path is outside the session's tool-results directory.
func (s *Store) Read(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("persist: invalid path: %w", err)
	}
	if !strings.HasPrefix(abs, s.dir+string(filepath.Separator)) && abs != s.dir {
		return "", fmt.Errorf("persist: path %q is outside session directory", path)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// parseFilename extracts toolName and callID from "<toolName>-<callID>.txt".
// Tool names may contain underscores (dots were replaced at save time).
func parseFilename(name string) (toolName, callID string, ok bool) {
	name = strings.TrimSuffix(name, ".txt")
	for _, sep := range []string{"-toolu_", "-call-", "-tool_"} {
		if idx := strings.LastIndex(name, sep); idx > 0 {
			return name[:idx], name[idx+1:], true
		}
	}
	// Fallback: split on first dash
	if idx := strings.Index(name, "-"); idx > 0 {
		return name[:idx], name[idx+1:], true
	}
	return name, "", true
}
