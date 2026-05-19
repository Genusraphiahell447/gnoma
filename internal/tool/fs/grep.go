package fs

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"somegit.dev/Owlibou/gnoma/internal/tool"
)

const (
	grepToolName   = "fs.grep"
	defaultMaxResults = 250
)

var grepParams = json.RawMessage(`{
	"type": "object",
	"properties": {
		"pattern": {
			"type": "string",
			"description": "Regular expression pattern to search for"
		},
		"path": {
			"type": "string",
			"description": "File or directory to search in (defaults to current directory)"
		},
		"glob": {
			"type": "string",
			"description": "File glob filter (e.g. *.go, *.ts)"
		},
		"max_results": {
			"type": "integer",
			"description": "Maximum number of matching lines to return (default 250)"
		}
	},
	"required": ["pattern"]
}`)

type GrepTool struct {
	guard *Guard
}

func NewGrepTool() *GrepTool { return &GrepTool{} }

func (t *GrepTool) SetGuard(g *Guard) { t.guard = g }

func (t *GrepTool) Name() string               { return grepToolName }
func (t *GrepTool) Description() string         { return "Search file contents using a regular expression" }
func (t *GrepTool) Parameters() json.RawMessage { return grepParams }
func (t *GrepTool) IsReadOnly() bool            { return true }
func (t *GrepTool) IsDestructive() bool         { return false }
func (t *GrepTool) Category() tool.Category     { return tool.CategorySearch }

func (t *GrepTool) ExtractPaths(args json.RawMessage) []string {
	var a grepArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return nil
	}
	return []string{a.Path} // empty string = caller resolves to cwd
}

type grepArgs struct {
	Pattern    string `json:"pattern"`
	Path       string `json:"path,omitempty"`
	Glob       string `json:"glob,omitempty"`
	MaxResults int    `json:"max_results,omitempty"`
}

type grepMatch struct {
	File string
	Line int
	Text string
}

func (t *GrepTool) Execute(_ context.Context, args json.RawMessage) (tool.Result, error) {
	var a grepArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return tool.Result{}, fmt.Errorf("fs.grep: invalid args: %w", err)
	}
	if a.Pattern == "" {
		return tool.Result{}, fmt.Errorf("fs.grep: pattern required")
	}

	re, err := regexp.Compile(a.Pattern)
	if err != nil {
		return tool.Result{Output: fmt.Sprintf("Invalid regex: %v", err)}, nil
	}

	maxResults := a.MaxResults
	if maxResults <= 0 {
		maxResults = defaultMaxResults
	}

	root := a.Path
	if root == "" {
		if t.guard != nil {
			root = t.guard.Roots()[0]
		} else {
			root, err = os.Getwd()
			if err != nil {
				return tool.Result{}, fmt.Errorf("fs.grep: %w", err)
			}
		}
	}
	if t.guard != nil {
		resolved, err := t.guard.ResolveRead(root)
		if err != nil {
			return tool.Result{Output: fmt.Sprintf("Error: %v", err)}, nil
		}
		root = resolved
	}

	info, err := os.Stat(root)
	if err != nil {
		return tool.Result{Output: fmt.Sprintf("Error: %v", err)}, nil
	}

	var matches []grepMatch

	if !info.IsDir() {
		matches = grepFile(root, "", re, maxResults)
	} else {
		walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				if d != nil && d.IsDir() && d.Name() != "." && strings.HasPrefix(d.Name(), ".") {
					return filepath.SkipDir
				}
				return nil
			}

			// Apply glob filter
			if a.Glob != "" {
				matched, _ := filepath.Match(a.Glob, d.Name())
				if !matched {
					return nil
				}
			}

			rel, _ := filepath.Rel(root, path)
			fileMatches := grepFile(path, rel, re, maxResults-len(matches))
			matches = append(matches, fileMatches...)

			if len(matches) >= maxResults {
				return filepath.SkipAll
			}
			return nil
		})
		if walkErr != nil {
			return tool.Result{Output: fmt.Sprintf("Error walking directory: %v", walkErr)}, nil
		}
	}

	if len(matches) == 0 {
		return tool.Result{
			Output:   "(no matches)",
			Metadata: map[string]any{"count": 0},
		}, nil
	}

	var b strings.Builder
	for _, m := range matches {
		if m.File != "" {
			fmt.Fprintf(&b, "%s:%d:%s\n", m.File, m.Line, m.Text)
		} else {
			fmt.Fprintf(&b, "%d:%s\n", m.Line, m.Text)
		}
	}

	truncated := len(matches) >= maxResults

	return tool.Result{
		Output: strings.TrimRight(b.String(), "\n"),
		Metadata: map[string]any{
			"count":     len(matches),
			"truncated": truncated,
		},
	}, nil
}

func grepFile(path, displayPath string, re *regexp.Regexp, limit int) []grepMatch {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()

	var matches []grepMatch
	scanner := bufio.NewScanner(f)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		if re.MatchString(line) {
			matches = append(matches, grepMatch{
				File: displayPath,
				Line: lineNum,
				Text: line,
			})
			if len(matches) >= limit {
				break
			}
		}
	}
	return matches
}
