package subprocess

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"sort"
	"sync"
	"time"

	"somegit.dev/Owlibou/gnoma/internal/provider"
)

// lookPath is package-level for test override. Defaults to exec.LookPath.
// Tests that swap this must NOT call t.Parallel() and should restore via
// t.Cleanup().
var lookPath = exec.LookPath

// StreamFormat identifies the line-delimited JSON format a CLI agent emits.
type StreamFormat string

const (
	FormatClaudeStreamJSON StreamFormat = "claude-stream-json"
	FormatGeminiStreamJSON StreamFormat = "gemini-stream-json"
	FormatVibeStreaming    StreamFormat = "vibe-streaming"
	FormatAgyText          StreamFormat = "agy-text"
)

// CLIAgent describes a known CLI agent binary.
type CLIAgent struct {
	Name        string
	DisplayName string
	ProbeArgs   []string              // args to fetch version (e.g. ["--version"])
	PromptArgs  func(string) []string // build argv for a non-interactive prompt run
	Format      StreamFormat
	Capabilities provider.Capabilities
	// PromptResponseFormat indicates the agent has no native structured-output
	// mode and must rely on prompt-augmented JSON schema instructions. Treated
	// as a best-effort fallback by buildPrompt — model compliance is not
	// guaranteed.
	PromptResponseFormat bool
}

// DiscoveredAgent is a CLIAgent confirmed present on PATH with its resolved path.
type DiscoveredAgent struct {
	CLIAgent
	Path    string
	Version string
	// OverrideBinary is the binary name from [cli_agents].<name>=<value> when
	// an override caused this agent to resolve to a non-canonical binary.
	// Empty when the canonical binary name was used.
	OverrideBinary string
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
	{
		Name:        "agy",
		DisplayName: "Antigravity",
		ProbeArgs:   []string{"--version"},
		PromptArgs: func(p string) []string {
			// --dangerously-skip-permissions parallels gemini's --yolo and
			// vibe's --trust: required for non-interactive runs since stdin
			// is closed and we cannot answer permission prompts.
			return []string{"--print", p, "--dangerously-skip-permissions"}
		},
		Format: FormatAgyText,
		// JSONOutput / Vision left false: agy v1.0.0 has no native
		// structured-output flag and no image-input mechanism. JSON support
		// is faked via PromptResponseFormat (best-effort, model-dependent);
		// see TODO.md for tracking native stream-json support.
		Capabilities: provider.Capabilities{
			ToolUse:       true,
			ContextWindow: 200000,
		},
		PromptResponseFormat: true,
	},
}

// newParser returns a FormatParser for the given format.
func newParser(f StreamFormat, rf *provider.ResponseFormat) FormatParser {
	switch f {
	case FormatClaudeStreamJSON:
		return newClaudeParser()
	case FormatGeminiStreamJSON:
		return newGeminiParser()
	case FormatVibeStreaming:
		return newVibeParser()
	case FormatAgyText:
		return newAgyParser(rf)
	default:
		return nil
	}
}

// resolveAgentBinary picks the binary name to look up for an agent and
// resolves it on PATH. If override is non-empty, only the override is tried
// (a missing overridden binary returns an error — we do not silently fall
// back to the canonical name, since that would mask a user typo). If
// override is empty, the canonical name is used.
//
// Returns the resolved absolute path, the binary name actually used, and an
// error if PATH lookup failed.
func resolveAgentBinary(canonical, override string) (resolvedPath, binName string, err error) {
	binName = canonical
	if override != "" {
		binName = override
	}
	resolvedPath, err = lookPath(binName)
	if err != nil {
		return "", binName, fmt.Errorf("lookpath %q: %w", binName, err)
	}
	return resolvedPath, binName, nil
}

// DiscoverCLIAgents scans PATH for known CLI agents in parallel and returns the
// ones that are present and respond to their probe command.
//
// overrides maps canonical agent names to override binary names — see
// config.CLIAgentsSection. An empty or nil map disables overrides entirely.
// An empty value for a key (e.g. claude="") is treated as "no override":
// the canonical name is used.
func DiscoverCLIAgents(ctx context.Context, overrides map[string]string) []DiscoveredAgent {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var mu sync.Mutex
	var found []DiscoveredAgent
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)

	for _, agent := range knownAgents {
		override := overrides[agent.Name]
		path, binName, err := resolveAgentBinary(agent.Name, override)
		if err != nil {
			// Only warn when the user explicitly set an override; a missing
			// canonical binary is the common "user doesn't have this agent
			// installed" case and shouldn't be noisy.
			if override != "" {
				slog.Warn("cli_agents override binary not on PATH",
					"agent", agent.Name,
					"override", override,
					"error", err,
				)
			}
			continue
		}
		recordedOverride := ""
		if binName != agent.Name {
			recordedOverride = binName
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(a CLIAgent, p, ov string) {
			defer wg.Done()
			defer func() { <-sem }()
			version := probeAgentVersion(ctx, p, a.ProbeArgs)
			mu.Lock()
			found = append(found, DiscoveredAgent{
				CLIAgent:       a,
				Path:           p,
				Version:        version,
				OverrideBinary: ov,
			})
			mu.Unlock()
		}(agent, path, recordedOverride)
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
