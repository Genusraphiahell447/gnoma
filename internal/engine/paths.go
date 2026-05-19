package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"somegit.dev/Owlibou/gnoma/internal/message"
	"somegit.dev/Owlibou/gnoma/internal/security"
	"somegit.dev/Owlibou/gnoma/internal/tool"
)

// isUnderAllowedPaths reports whether target is equal to or a descendant of any
// path in allowed. Both sides are canonicalized (symlink-evaluated, with the
// non-existent tail preserved) before comparison. Returns false when allowed
// is empty.
//
// The trailing-separator check prevents "/tmp" from matching "/tmpx/foo".
func isUnderAllowedPaths(target string, allowed []string) bool {
	if len(allowed) == 0 {
		return false
	}

	canonicalTarget, err := resolveCanonical(target)
	if err != nil {
		return false
	}

	sep := string(filepath.Separator)
	for _, a := range allowed {
		canonicalAllowed, err := resolveCanonical(a)
		if err != nil {
			continue
		}
		if canonicalTarget == canonicalAllowed || strings.HasPrefix(canonicalTarget, canonicalAllowed+sep) {
			return true
		}
	}
	return false
}

// resolveCanonical absolutises against the process cwd and delegates the
// symlink-aware ancestor walk to security.CanonicalizePath. Kept as a thin
// wrapper so callers in this package can pass relative paths.
func resolveCanonical(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return security.CanonicalizePath(abs)
}

// checkPathRestriction enforces AllowedPaths on a single tool call.
//
// Rules (in order):
//  1. If allowed is empty, everything is permitted (fast-path).
//  2. "bash" is always denied when path restrictions are active.
//  3. Tools implementing tool.PathSensitiveTool have their extracted paths
//     checked against allowed. An empty extracted path is resolved to cwd.
//  4. Tools that do not implement PathSensitiveTool are permitted (they don't
//     declare filesystem access).
//
// Returns (denied result, true) when blocked, or (zero, false) when allowed.
func checkPathRestriction(call message.ToolCall, t tool.Tool, args json.RawMessage, allowed []string) (message.ToolResult, bool) {
	if len(allowed) == 0 {
		return message.ToolResult{}, false
	}

	if call.Name == "bash" {
		return message.ToolResult{
			ToolCallID: call.ID,
			Content:    "bash is not permitted when skill path restrictions are active",
			IsError:    true,
		}, true
	}

	pt, ok := t.(tool.PathSensitiveTool)
	if !ok {
		return message.ToolResult{}, false
	}

	for _, p := range pt.ExtractPaths(args) {
		var resolved string
		if p == "" {
			cwd, err := os.Getwd()
			if err != nil {
				return message.ToolResult{
					ToolCallID: call.ID,
					Content:    fmt.Sprintf("path access denied: cannot determine current directory: %v", err),
					IsError:    true,
				}, true
			}
			resolved = cwd
		} else {
			resolved = p
		}

		if !isUnderAllowedPaths(resolved, allowed) {
			return message.ToolResult{
				ToolCallID: call.ID,
				Content:    fmt.Sprintf("path access denied: %q is not in allowed paths %v", resolved, allowed),
				IsError:    true,
			}, true
		}
	}

	return message.ToolResult{}, false
}
