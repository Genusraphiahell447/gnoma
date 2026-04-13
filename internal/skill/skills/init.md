---
name: init
description: Generate or update AGENTS.md project documentation
whenToUse: When user runs /init to create or update project documentation
---
{{define "local-init"}}You are {{if .Args}}updating{{else}}creating{{end}} an AGENTS.md project documentation file for the project at {{.ProjectRoot}}.

Use ONLY these tools: fs_ls, fs_read, fs_glob, fs_grep, fs_write.
Do NOT use bash or spawn_elfs.
IMPORTANT: Keep context small — read only what you need, stop reading when you have enough.

STEP 1 — Read config files (these are loaded at runtime alongside AGENTS.md):
- fs_read CLAUDE.md if it exists. Note which topics it covers — AGENTS.md must NOT repeat any of them.{{if .Args}}
- fs_read the existing AGENTS.md at {{.Args}}. Keep sections that are accurate and not in CLAUDE.md. Remove duplicated or stale content.{{end}}

STEP 2 — Gather project facts (be brief — read as few files as possible):
- fs_ls on the project root.
- fs_read go.mod for dependencies not listed in CLAUDE.md.
- fs_read Makefile for non-standard targets (skip build/test/lint/cover/fmt/vet/clean/tidy/install/run).
- fs_read 1-2 source files to spot project-specific patterns. Stop once you have enough.
- Do NOT use fs_grep — it returns too much output. If you need env var names, look in main.go or config files only.

STEP 3 — Write AGENTS.md to {{.ProjectRoot}}/AGENTS.md.

RULES:
- Do NOT repeat anything from CLAUDE.md — it is loaded alongside AGENTS.md at runtime.
- Quality test: would removing this line cause an AI to make a mistake? If no, cut it.
- No emojis. Plain markdown headers. Terse directive-style bullets.
- Short code examples only where the pattern is non-obvious — cite the source file.
- Do not fabricate. Only write what you observed.

INCLUDE (only if not in CLAUDE.md): key dependencies with import paths, non-standard build targets, domain terminology, environment variables, code patterns with real examples, architectural gotchas.
EXCLUDE: anything in CLAUDE.md, standard targets, file listings, generic advice, standard language conventions.{{end}}
{{define "cloud-elfs"}}IMPORTANT: Use only fs.ls, fs.glob, fs.grep, and fs.read for all analysis. Do NOT use bash — it will be denied and will cause you to fail. Your first action must be spawn_elfs.

Use spawn_elfs to analyze the project in parallel. Spawn at least these elfs simultaneously:

- Elf 1 (task_type: "explain"): Explore project structure at {{.ProjectRoot}}.
  - Run fs.ls on root and every immediate subdirectory.
  - Read go.mod (or package.json/Cargo.toml/pyproject.toml): extract module path, Go/runtime version, and key external dependencies with exact import paths. List TUI/UI framework deps (e.g. charm.land/*, tview) separately from backend/LLM deps.
  - Read Makefile or build scripts: note targets beyond the standard (build/test/lint/fmt/vet/clean/tidy/install). Note non-standard flags, multi-step sequences, or env vars they require.
  - Read existing AI config files if present: CLAUDE.md, .cursor/rules, .cursorrules, .github/copilot-instructions.md, .gnoma/GNOMA.md. These will be loaded at runtime — do NOT copy their content into AGENTS.md. Only note what topics they cover so the synthesis step knows what to skip.
  - Build a domain glossary: read the primary type-definition files in these packages (use fs.ls to find them): internal/message, internal/engine, internal/router, internal/elf, internal/provider, internal/context, internal/security, internal/session. For each exported type, struct, or interface whose name would be ambiguous or non-obvious to an outside AI, add a one-line entry: Name → what it is in this project. Specifically look for: Arm, Turn, Elf, Accumulator, Firewall, LimitPool, TaskType, Incognito, Stream, Event, Session, Router. Do not list generic config struct fields.
  - Report: module path, runtime version, non-standard Makefile targets only (skip standard ones: build/test/lint/cover/fmt/vet/clean/tidy/install/run), full dependency list (TUI + backend separated), domain glossary.

- Elf 2 (task_type: "explain"): Discover non-standard code conventions at {{.ProjectRoot}}.
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

- Elf 3 (task_type: "explain"): Extract setup requirements and gotchas at {{.ProjectRoot}}.
  - Read README.md, CONTRIBUTING.md, docs/ contents if they exist.
  - Find required environment variables: use fs.grep to search for os.Getenv and os.LookupEnv across all .go files. List every unique variable name found and what it configures based on surrounding context. Also check .env.example if it exists.
  - Note non-obvious setup steps (token scopes, local service dependencies, build prerequisites not in the Makefile).
  - Note repo etiquette ONLY if not already covered by CLAUDE.md — skip commit format and co-signing if CLAUDE.md documents them.
  - Note architectural gotchas explicitly called out in comments or docs — skip generic advice.
  - Skip anything obvious for a project of this type.{{end}}
{{define "synth-rules"}}After all elfs complete, you may spawn additional focused elfs with agent tool if specific gaps need investigation.

Then synthesize and write AGENTS.md to {{.ProjectRoot}}/AGENTS.md using fs.write.

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
Format: terse directive-style bullets. Short code examples where the pattern is non-obvious. No prose paragraphs.
No emojis anywhere in the output. Use plain markdown headers.{{end}}
{{if .Local}}{{template "local-init" .}}{{else}}{{if .Args}}You are updating the AGENTS.md project documentation file for the project at {{.ProjectRoot}}.

{{template "cloud-elfs" .}}
- Elf 4 (task_type: "review"): Read the existing AGENTS.md at {{.Args}}.
  - For each section: accurate (keep), stale (update), missing (add), bloat (cut — fails quality test).
  - Specifically flag: anything duplicated from CLAUDE.md or other loaded AI config files (remove it), fabricated content (remove it), and missing language-version-specific idioms.
  - Report a structured diff: keep / update / add / remove.

{{template "synth-rules" .}}

When updating: tighten as well as correct. Remove duplication and bloat even if it was in the old version.{{else}}You are creating an AGENTS.md project documentation file for the project at {{.ProjectRoot}}.

{{template "cloud-elfs" .}}

{{template "synth-rules" .}}{{end}}{{end}}
