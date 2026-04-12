package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	gnomacfg "somegit.dev/Owlibou/gnoma/internal/config"
	"somegit.dev/Owlibou/gnoma/internal/message"
)

// initPrompt builds the prompt sent to the LLM for /init.
// existingPath is the absolute path to an existing AGENTS.md, or "" if none exists.
// The 3 base elfs always run. When existingPath is set, a 4th elf reads the current file.
// The LLM is free to spawn additional elfs if it identifies gaps.
func initPrompt(root, existingPath string) string {
	baseElfs := fmt.Sprintf(`IMPORTANT: Use only fs.ls, fs.glob, fs.grep, and fs.read for all analysis. Do NOT use bash — it will be denied and will cause you to fail. Your first action must be spawn_elfs.

Use spawn_elfs to analyze the project in parallel. Spawn at least these elfs simultaneously:

- Elf 1 (task_type: "explain"): Explore project structure at %s.
  - Run fs.ls on root and every immediate subdirectory.
  - Read go.mod (or package.json/Cargo.toml/pyproject.toml): extract module path, Go/runtime version, and key external dependencies with exact import paths. List TUI/UI framework deps (e.g. charm.land/*, tview) separately from backend/LLM deps.
  - Read Makefile or build scripts: note targets beyond the standard (build/test/lint/fmt/vet/clean/tidy/install). Note non-standard flags, multi-step sequences, or env vars they require.
  - Read existing AI config files if present: CLAUDE.md, .cursor/rules, .cursorrules, .github/copilot-instructions.md, .gnoma/GNOMA.md. These will be loaded at runtime — do NOT copy their content into AGENTS.md. Only note what topics they cover so the synthesis step knows what to skip.
  - Build a domain glossary: read the primary type-definition files in these packages (use fs.ls to find them): internal/message, internal/engine, internal/router, internal/elf, internal/provider, internal/context, internal/security, internal/session. For each exported type, struct, or interface whose name would be ambiguous or non-obvious to an outside AI, add a one-line entry: Name → what it is in this project. Specifically look for: Arm, Turn, Elf, Accumulator, Firewall, LimitPool, TaskType, Incognito, Stream, Event, Session, Router. Do not list generic config struct fields.
  - Report: module path, runtime version, non-standard Makefile targets only (skip standard ones: build/test/lint/cover/fmt/vet/clean/tidy/install/run), full dependency list (TUI + backend separated), domain glossary.

- Elf 2 (task_type: "explain"): Discover non-standard code conventions at %s.
  - Use fs.glob **/*.go (or language equivalent) to find source files. Read at least 8 files spanning different packages — prefer non-trivial ones (engine, provider, tool implementations, tests).
  - Use fs.grep to locate each pattern below. NEVER use internal/tui as a source for code examples — it is application glue, not where idioms live. For each match found: read the file, then paste the relevant lines with the file path as the first comment (e.g. '// internal/foo/bar.go'). If fs.grep returns no matches outside internal/tui, omit that pattern entirely. Do NOT invent or paraphrase.
    * new(expr): fs.grep '= new(' across **/*.go, exclude internal/tui
    * errors.AsType: fs.grep 'errors.AsType' across **/*.go
    * WaitGroup.Go: fs.grep '\.Go(func' across **/*.go
    * testing/synctest: fs.grep 'synctest' across **/*.go
    * Discriminated union: fs.grep 'Content|EventType|ContentType' across internal/message, internal/stream — look for a struct with a Type field switched on by callers
    * Pull-based iterator: fs.grep 'func.*Next\(\)' across **/*.go — look for Next/Current/Err/Close pattern
    * json.RawMessage passthrough: fs.grep 'json.RawMessage' across internal/tool — find a Parameters() or Execute() signature
    * errgroup: fs.grep 'errgroup' across **/*.go
    * Channel semaphore: fs.grep 'chan struct{}' across **/*.go, look for concurrency-limiting usage
  - Error handling: fs.grep 'var Err' across **/*.go — paste a real sentinel definition. fs.grep 'fmt.Errorf' across **/*.go and look for error-wrapping calls — paste a real one. File path required on each.
  - Test conventions: fs.grep '//go:build' across **/*_test.go for build tags. fs.grep 't.Helper()' across **/*_test.go for helper convention. fs.grep 't.TempDir()' across **/*_test.go. Paste one real example each with file path.
  - Report ONLY what differs from standard language knowledge. Skip obvious conventions.

- Elf 3 (task_type: "explain"): Extract setup requirements and gotchas at %s.
  - Read README.md, CONTRIBUTING.md, docs/ contents if they exist.
  - Find required environment variables: use fs.grep to search for os.Getenv and os.LookupEnv across all .go files. List every unique variable name found and what it configures based on surrounding context. Also check .env.example if it exists.
  - Note non-obvious setup steps (token scopes, local service dependencies, build prerequisites not in the Makefile).
  - Note repo etiquette ONLY if not already covered by CLAUDE.md — skip commit format and co-signing if CLAUDE.md documents them.
  - Note architectural gotchas explicitly called out in comments or docs — skip generic advice.
  - Skip anything obvious for a project of this type.`, root, root, root)

	synthRules := fmt.Sprintf(`After all elfs complete, you may spawn additional focused elfs with agent tool if specific gaps need investigation.

Then synthesize and write AGENTS.md to %s/AGENTS.md using fs.write.

CRITICAL RULE — DO NOT DUPLICATE LOADED FILES:
CLAUDE.md (and other AI config files) are loaded directly into the AI's context at runtime.
Writing their content into AGENTS.md is pure noise — it will be read twice and adds nothing.
AGENTS.md must only contain information those files do not already cover.
If CLAUDE.md thoroughly covers a topic (e.g. Go style, commit format, provider list), skip it.

QUALITY TEST: Before writing each line — would removing this cause an AI assistant to make a mistake on this codebase? If no, cut it.

INCLUDE (only if not already in CLAUDE.md or equivalent):
- Module path and key dependencies with exact import paths (especially non-obvious or private ones)
- Build/test commands the AI cannot guess from manifest files alone (non-standard targets, flags, sequences)
- Language-version-specific idioms in use: e.g. Go 1.26 new(expr), errors.AsType, WaitGroup.Go; show code examples
- Non-standard type patterns: discriminated unions, pull-based iterators, json.RawMessage passthrough — with examples
- Domain terminology: project-specific names that differ from industry-standard meanings
- Testing quirks: build tags, helper conventions, concurrency test tools, mock policy
- Required env var names and what they configure (not "see .env.example" — list them)
- Non-obvious architectural constraints or gotchas not derivable from reading the code

EXCLUDE:
- Anything already documented in CLAUDE.md or other AI config files that will be loaded at runtime
- File-by-file directory listing (discoverable via fs.ls)
- Standard language conventions the AI already knows
- Generic advice ("write clean code", "handle errors", "use descriptive names")
- Standard Makefile/build targets (build, test, lint, cover, fmt, vet, clean, tidy, install, run) — do not list them at all, not even as a summary line; only write non-standard targets
- The "Standard Targets: ..." line itself — it adds nothing and must not appear
- Planned features not yet in code
- Vague statements ("see config files for details", "follow project conventions") — include the actual detail or nothing

Do not fabricate. Only write what was observed in files you actually read.
Format: terse directive-style bullets. Short code examples where the pattern is non-obvious. No prose paragraphs.`, root)

	if existingPath != "" {
		return fmt.Sprintf(`You are updating the AGENTS.md project documentation file for the project at %s.

%s
- Elf 4 (task_type: "review"): Read the existing AGENTS.md at %s.
  - For each section: accurate (keep), stale (update), missing (add), bloat (cut — fails quality test).
  - Specifically flag: anything duplicated from CLAUDE.md or other loaded AI config files (remove it), fabricated content (remove it), and missing language-version-specific idioms.
  - Report a structured diff: keep / update / add / remove.

%s

When updating: tighten as well as correct. Remove duplication and bloat even if it was in the old version.`,
			root, baseElfs, existingPath, synthRules)
	}

	return fmt.Sprintf(`You are creating an AGENTS.md project documentation file for the project at %s.

%s

%s`, root, baseElfs, synthRules)
}

// loadAgentsMD reads AGENTS.md from disk and appends it to the context window prefix.
func (m Model) loadAgentsMD() Model {
	root := gnomacfg.ProjectRoot()
	path := filepath.Join(root, "AGENTS.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return m
	}
	if m.config.Engine != nil {
		if w := m.config.Engine.ContextWindow(); w != nil {
			w.AddPrefix(
				message.NewUserText(fmt.Sprintf("[Project docs: AGENTS.md]\n\n%s", string(data))),
				message.NewAssistantText("I've read the project documentation and will follow these guidelines."),
			)
		}
	}
	m.messages = append(m.messages, chatMessage{role: "system",
		content: fmt.Sprintf("AGENTS.md written to %s — loaded into context for this session.", path)})
	return m
}

// extractMarkdownDoc strips preamble and returns everything from the first heading onward.
func extractMarkdownDoc(s string) string {
	for _, line := range strings.Split(s, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			idx := strings.Index(s, line)
			return strings.TrimSpace(s[idx:])
		}
	}
	return ""
}

// looksLikeAgentsMD returns true if s appears to be a real markdown document
// (not a refusal or planning response): substantial length and at least one
// section heading.
func looksLikeAgentsMD(s string) bool {
	return len(s) >= 300 && strings.Contains(s, "##")
}
