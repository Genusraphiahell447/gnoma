package context

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// DefaultMaxResultSize is the threshold for persisting tool results.
	DefaultMaxResultSize = 50_000 // chars
	// PreviewSize is the number of chars to show inline.
	PreviewSize = 2000
	// ToolResultsDir is the subdirectory for persisted results.
	ToolResultsDir = "tool-results"
)

// PersistLargeResult checks if a tool result exceeds the size limit.
// If so, writes it to disk and returns a preview + file path.
// Otherwise returns the original content unchanged.
func PersistLargeResult(content, toolUseID, sessionDir string) (string, bool) {
	if len(content) <= DefaultMaxResultSize {
		return content, false
	}

	// Create directory
	dir := filepath.Join(sessionDir, ToolResultsDir)
	os.MkdirAll(dir, 0o755)

	// Write full result to disk
	filename := toolUseID + ".txt"
	path := filepath.Join(dir, filename)
	os.WriteFile(path, []byte(content), 0o644)

	// Build preview
	preview := content
	if len(preview) > PreviewSize {
		preview = preview[:PreviewSize]
	}

	return fmt.Sprintf("<persisted-output>\nFull output saved to: %s\n\nPreview (first %d chars):\n%s\n</persisted-output>",
		path, PreviewSize, preview), true
}

// TruncateToolResult truncates a tool result to a maximum size with an indicator.
func TruncateToolResult(content string, maxSize int) string {
	if len(content) <= maxSize {
		return content
	}
	lines := strings.Split(content[:maxSize], "\n")
	return strings.Join(lines, "\n") + fmt.Sprintf("\n\n... (truncated, %d total chars)", len(content))
}
