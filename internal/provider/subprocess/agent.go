package subprocess

import (
	"context"
	"os/exec"
	"sort"
	"sync"
	"time"

	"somegit.dev/Owlibou/gnoma/internal/provider"
)

// StreamFormat identifies the line-delimited JSON format a CLI agent emits.
type StreamFormat string

const (
	FormatClaudeStreamJSON StreamFormat = "claude-stream-json"
	FormatGeminiStreamJSON StreamFormat = "gemini-stream-json"
	FormatVibeStreaming     StreamFormat = "vibe-streaming"
)

// CLIAgent describes a known CLI agent binary.
type CLIAgent struct {
	Name        string
	DisplayName string
	ProbeArgs   []string           // args to fetch version (e.g. ["--version"])
	PromptArgs  func(string) []string // build argv for a non-interactive prompt run
	Format      StreamFormat
	Capabilities provider.Capabilities
}

// DiscoveredAgent is a CLIAgent confirmed present on PATH with its resolved path.
type DiscoveredAgent struct {
	CLIAgent
	Path    string
	Version string
}

// knownAgents is the registry of CLI agents Gnoma supports.
var knownAgents = []CLIAgent{
	{
		Name:        "claude",
		DisplayName: "Claude Code",
		ProbeArgs:   []string{"--version"},
		PromptArgs: func(p string) []string {
			return []string{"-p", p, "--output-format", "stream-json", "--verbose"}
		},
		Format: FormatClaudeStreamJSON,
		// ToolUse=true: the claude CLI is a full agent with its own tool loop.
		// This is a routing capability flag, not a provider-layer capability.
		Capabilities: provider.Capabilities{
			ToolUse:       true,
			ContextWindow: 200000,
		},
	},
	{
		Name:        "gemini",
		DisplayName: "Gemini CLI",
		ProbeArgs:   []string{"--version"},
		PromptArgs: func(p string) []string {
			return []string{"-p", p, "--output-format", "stream-json", "--yolo"}
		},
		Format: FormatGeminiStreamJSON,
		Capabilities: provider.Capabilities{
			ToolUse:       true,
			ContextWindow: 1048576,
		},
	},
	{
		Name:        "vibe",
		DisplayName: "Mistral Vibe",
		ProbeArgs:   []string{"--version"},
		PromptArgs: func(p string) []string {
			return []string{"-p", p, "--output", "streaming", "--trust"}
		},
		Format: FormatVibeStreaming,
		Capabilities: provider.Capabilities{
			ToolUse:       true,
			ContextWindow: 128000,
		},
	},
}

// newParser returns a FormatParser for the given format.
func newParser(f StreamFormat) FormatParser {
	switch f {
	case FormatClaudeStreamJSON:
		return newClaudeParser()
	case FormatGeminiStreamJSON:
		return newGeminiParser()
	case FormatVibeStreaming:
		return newVibeParser()
	default:
		return nil
	}
}

// DiscoverCLIAgents scans PATH for known CLI agents in parallel and returns the
// ones that are present and respond to their probe command.
func DiscoverCLIAgents(ctx context.Context) []DiscoveredAgent {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var mu sync.Mutex
	var found []DiscoveredAgent
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)

	for _, agent := range knownAgents {
		path, err := exec.LookPath(agent.Name)
		if err != nil {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(a CLIAgent, p string) {
			defer wg.Done()
			defer func() { <-sem }()
			version := probeAgentVersion(ctx, p, a.ProbeArgs)
			mu.Lock()
			found = append(found, DiscoveredAgent{CLIAgent: a, Path: p, Version: version})
			mu.Unlock()
		}(agent, path)
	}
	wg.Wait()

	// Stable order: match knownAgents ordering.
	order := make(map[string]int, len(knownAgents))
	for i, a := range knownAgents {
		order[a.Name] = i
	}
	sort.Slice(found, func(i, j int) bool {
		return order[found[i].Name] < order[found[j].Name]
	})
	return found
}

func probeAgentVersion(ctx context.Context, path string, args []string) string {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	out, err := cmd.Output()
	if err != nil && len(out) == 0 {
		return ""
	}
	// Return the first non-empty line.
	for _, b := range splitNL(out) {
		if len(b) > 0 {
			return string(b)
		}
	}
	return ""
}

// splitNL splits bytes by newlines, trimming carriage returns.
func splitNL(b []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i, c := range b {
		if c == '\n' {
			line := b[start:i]
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
			lines = append(lines, line)
			start = i + 1
		}
	}
	if start < len(b) {
		lines = append(lines, b[start:])
	}
	return lines
}
