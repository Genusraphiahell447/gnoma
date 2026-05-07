# Gnoma - TODO List

---

## Native SLM Runtime (The Preflight Engine)

Integrating a native **Tiny LLM (SLM)** into Gnoma for preflight is a brilliant move. In 2026, using an SLM as a "Dispatcher" or "Gatekeeper" is the industry standard for low-latency, privacy-first agentic tools.

By moving beyond simple model selection and into **Decomposed Execution**, you turn Gnoma from a wrapper into an intelligent orchestrator.

See [`gemma-integration-analysis.md`](gemma-integration-analysis.md) for full architecture analysis, routing prompts, and implementation checklist.

### 1. Native SLM Runtime (The Preflight Engine)
- [ ] **In-Process SLM Integration**: Instead of calling a heavy local server (like Ollama) for every routing decision, integrate a lightweight CGO or pure-Go runner for **Gemma 3 270M** or **Phi-4 Mini**.
- [ ] **Zero-Cold-Start "Hot" Model**: Keep the SLM resident in memory (~300MB RAM). It should be ready to classify intent in **<50ms**, ensuring the user doesn't feel the "preflight" delay.
- [ ] **Static Weight Embedding**: Embed the quantization of your routing model directly into the Gnoma binary (or a hidden `.gnoma/models/` cache) to ensure "just works" functionality offline.

### 2. Deep Intent Routing (Beyond Model Selection)
- [ ] **Skill Decomposer**: Instead of just choosing a model, use the SLM to break a complex user request into a **directed acyclic graph (DAG)** of Gnoma skills (e.g., `[fs.read] -> [elf.parse] -> [sec-audit]`).
- [ ] **Context Flattener**: Use the SLM to prune the user's history and current file context *before* sending it to a large, expensive model (Gemini/Claude). This saves tokens and increases reasoning accuracy.
- [ ] **Interactive vs. Autonomous Toggle**: Have the SLM predict if a command is "High Blast Radius" (e.g., `rm`, `patch`). If so, automatically trigger the **Human-in-the-Loop PTY switch** before the agent even tries to execute.

### 3. Security & "The Iron Law" Integration
- [ ] **USP Pre-Audit**: Use the SLM to run a "Stage 0" security audit on the Agent's proposed plan. If the plan violates any **Wave Protocol** rules, the SLM rejects the plan locally without ever calling the remote LLM.
- [ ] **Hallucination Gate**: Use the SLM to verify that file paths mentioned by the remote LLM actually exist via `fs.LSTool` before presenting them to the user.

### Recommended "Tiny" Stack for Gnoma

To keep Gnoma a single-binary Go tool while adding these features, consider this specific technical path:

| Component | Recommendation | Why? |
| :--- | :--- | :--- |
| **Model** | **Gemma 3 270M (Instruct)** | Optimized specifically for tool-use and routing in 2026. |
| **Runtime** | **`github.com/ollama/ollama/llm` (as library)** | Allows you to leverage Ollama's high-speed inference inside your Go app. |
| **Quantization** | **Q4_K_M GGUF** | Perfect balance of "smarts" and memory footprint for a background CLI. |

### The "Integrated" Flow

1. **User input** arrives in Gnoma.
2. **Native SLM** (Gemma 270M) parses intent: *"Is this a security audit? A simple file read? Or a complex refactor?"*
3. **Local Route:** If it's a simple `elf.parse`, the SLM executes the Go tool directly—**Zero LLM cost.**
4. **Frontier Route:** If complex, the SLM scrubs the context and selects **Gemini 1.5 Pro** or **Claude 3.5**.
5. **PTY Execution:** The result is piped through your **go-pty** wrapper with real-time USP monitoring.

---

## Built-in Security Pilot (USP Integration)

### Overview
Ship the [Universal Security Pilot](https://github.com/VikingOwl91/universal-security-pilot) capabilities as first-class features in gnoma's core, rather than relying on external Markdown files and tool-specific adapters. Gnoma becomes the runtime for USP — the audit engine, remediation workflow, and AI hardening logic live inside the binary.

### Core Capabilities to Internalize
- [ ] **Security audit engine** — the eight-rule zero-trust review (adversarial input, context-aware footguns, identity integrity, atomicity, secret hygiene, AI guarding, SSRF/Dial-Control, multilingual defense)
- [ ] **Wave Protocol enforcement** — mandatory remediation ordering (W0→W1→W2→W3→W4→W5→W6), blast-radius-descending within each wave, cross-wave dependency resolution
- [ ] **Iron Law** — no fix ships without a failing PoC test; enforce this in the remediation workflow
- [ ] **Standards citation** — every finding must map to OWASP Top 10 / ASVS / LLM Top 10 / MITRE ATLAS / CWE IDs
- [ ] **AI hardening** — six-axis LLM hardening (prompt boundaries, output sanitization, BudgetGate, Dial-Control, injection vectors, multilingual defense)

### Implementation Steps
- [ ] **Skill system**: implement `sec-audit`, `sec-fix`, `ai-harden`, `sec-init` as built-in gnoma skills (not external file reads)
- [ ] **Footgun library**: embed the universal footgun catalog (categories A–D) and framework-specific instances as structured data gnoma can query during audits
- [ ] **Severity grading**: Critical/High/Medium/Low/Info with the canonical definitions, used in audit report output
- [ ] **Complexity rubric**: language-specific footgun tables (Go, TS/JS, Rust, Python, etc.) as queryable rules
- [ ] **Canonical patterns**: ship BudgetGate, Dial-Control, Envelope Encryption, OIDC state-verification as referenceable code templates gnoma can suggest or scaffold
- [ ] **Project-local override**: support `.gnoma/security/project-pilot.toml` (or similar) for per-project tightening (never loosening)
- [ ] **Rationalization resistance**: the anti-pressure table from `sec-fix` ("approved", "rushed deadline" do not override discipline)
- [ ] **Report generation**: structured Markdown audit reports with standards citations, severity, and wave assignment

### Considerations
- USP is tool-agnostic by design; gnoma's implementation should preserve the framework's principles while making them native
- The Wave Protocol ordering is load-bearing — W1 (auth) must complete before W2 (network), etc.
- Project-local overrides can tighten but never loosen the canonical rules
- Embed the footgun library as Go structs, not as runtime-parsed Markdown

---

## Local Tmp Folder (`.gnoma/tmp/`)

### Overview
Per-project temporary directory at `.gnoma/tmp/[current-working-dir]` for scratch files, intermediate outputs, and ephemeral state that shouldn't pollute the project tree or system tmp.

### Implementation Steps
- [ ] Create `.gnoma/tmp/` directory structure on first use (lazy initialization)
- [ ] Derive subdirectory name from current working directory (hash or sanitized path)
- [ ] Add helpers to resolve tmp paths: `gnoma.TmpDir(cwd string) string`
- [ ] Auto-cleanup policy (e.g., prune entries older than N days, or on session end)
- [ ] Add `.gnoma/tmp/` to default `.gitignore` generation
- [ ] Use for tool scratch space (e.g., ELF analysis intermediates, diff staging, etc.)

### Considerations
- Avoid collisions when multiple gnoma instances target the same project
- Keep path derivation deterministic so the same project always maps to the same tmp dir
- Respect XDG conventions where applicable (fallback to `~/.gnoma/tmp/` if no project-local `.gnoma/`)

---

## ELF Support

### Overview
This section outlines the steps to add **ELF (Executable and Linkable Format)** support to Gnoma, enabling features like ELF parsing, disassembly, security analysis, and binary manipulation.

---

## 📌 Goals
- Add ELF-specific tools to Gnoma's toolset.
- Enable users to analyze, disassemble, and manipulate ELF binaries.
- Integrate with Gnoma's existing permission and security systems.

---

## ✅ Implementation Steps

### 1. **Design ELF Tools**
- [ ] **`elf.parse`**: Parse and display ELF headers, sections, and segments.
- [ ] **`elf.disassemble`**: Disassemble code sections (e.g., `.text`) using `objdump` or a pure-Go disassembler.
- [ ] **`elf.analyze`**: Perform security analysis (e.g., check for packed binaries, missing security flags).
- [ ] **`elf.patch`**: Modify binary bytes or inject code (advanced feature).
- [ ] **`elf.symbols`**: Extract and list symbols from the symbol table.

### 2. **Implement ELF Tools**
- [ ] Create a new package: `internal/tool/elf`.
- [ ] Implement each tool as a struct with `Name()` and `Run(args map[string]interface{})` methods.
- [ ] Use `debug/elf` (standard library) or third-party libraries like `github.com/xyproto/elf` for parsing.
- [ ] Add support for external tools like `objdump` and `radare2` for disassembly.

#### Example: `elf.Parse` Tool
```go
package elf

import (
    "debug/elf"
    "fmt"
    "os"
)

type ParseTool struct{}

func NewParseTool() *ParseTool {
    return &ParseTool{}
}

func (t *ParseTool) Name() string {
    return "elf.parse"
}

func (t *ParseTool) Run(args map[string]interface{}) (string, error) {
    filePath, ok := args["file"].(string)
    if !ok {
        return "", fmt.Errorf("missing 'file' argument")
    }

    f, err := os.Open(filePath)
    if err != nil {
        return "", fmt.Errorf("failed to open file: %v", err)
    }
    defer f.Close()

    ef, err := elf.NewFile(f)
    if err != nil {
        return "", fmt.Errorf("failed to parse ELF: %v", err)
    }
    defer ef.Close()

    // Extract and format ELF headers
    output := fmt.Sprintf("ELF Header:\n%s\n", ef.FileHeader)
    output += fmt.Sprintf("Sections:\n")
    for _, s := range ef.Sections {
        output += fmt.Sprintf("  - %s (size: %d)\n", s.Name, s.Size)
    }
    output += fmt.Sprintf("Program Headers:\n")
    for _, p := range ef.Progs {
        output += fmt.Sprintf("  - Type: %s, Offset: %d, Vaddr: %x\n", p.Type, p.Off, p.Vaddr)
    }

    return output, nil
}
```

### 3. **Integrate ELF Tools with Gnoma**
- [ ] Update `buildToolRegistry()` in `cmd/gnoma/main.go` to register ELF tools:
  ```go
  func buildToolRegistry() *tool.Registry {
      reg := tool.NewRegistry()
      reg.Register(bash.New())
      reg.Register(fs.NewReadTool())
      reg.Register(fs.NewWriteTool())
      reg.Register(fs.NewEditTool())
      reg.Register(fs.NewGlobTool())
      reg.Register(fs.NewGrepTool())
      reg.Register(fs.NewLSTool())
      reg.Register(elf.NewParseTool())      // New ELF tool
      reg.Register(elf.NewDisassembleTool()) // New ELF tool
      reg.Register(elf.NewAnalyzeTool())    // New ELF tool
      return reg
  }
  ```

### 4. **Add Documentation**
- [ ] Add usage examples to `docs/elf-tools.md`.
- [ ] Update `CLAUDE.md` with ELF tool capabilities.

### 5. **Testing**
- [ ] Test ELF tools on sample binaries (e.g., `/bin/ls`, `/bin/bash`).
- [ ] Test edge cases (e.g., stripped binaries, packed binaries).
- [ ] Ensure integration with Gnoma's permission and security systems.

### 6. **Security Considerations**
- [ ] Sandbox ELF tools to prevent malicious binaries from compromising the system.
- [ ] Validate file paths and arguments to avoid directory traversal or arbitrary file writes.
- [ ] Use Gnoma's firewall to scan ELF tool outputs for suspicious patterns.

---

## 🛠️ Dependencies
- **Go Libraries**:
  - [`debug/elf`](https://pkg.go.dev/debug/elf) (standard library).
  - [`github.com/xyproto/elf`](https://github.com/xyproto/elf) (third-party).
  - [`github.com/anchore/go-elf`](https://github.com/anchore/go-elf) (third-party).
- **External Tools**:
  - `objdump` (for disassembly).
  - `readelf` (for detailed ELF analysis).
  - `radare2` (for advanced reverse engineering).

---

## 📝 Example Usage
### Interactive Mode
```
> Use the elf.parse tool to analyze /bin/ls
> elf.parse --file /bin/ls
```

### Pipe Mode
```bash
echo '{"file": "/bin/ls"}' | gnoma --tool elf.parse
```

---

## 🚀 Future Enhancements
- Add support for **PE (Portable Executable)** and **Mach-O** formats.
- Integrate with **Ghidra** or **IDA Pro** for advanced analysis.
- Add **automated exploit detection** for binaries.

---

## The Core Engine (Multi-Platform PTY)

### 1. PTY Implementation
- [ ] Implement [github.com/aymanbagabas/go-pty](https://github.com/aymanbagabas/go-pty): This is your primary "wrapper." It detects the OS at compile-time and uses ConPTY on Windows or standard /dev/ptmx on Unix.
- [ ] Windows ConPTY Check: Add a startup check to ensure the user is on a modern version of Windows (Build 18362+), as older versions don't support the ConPTY API that go-pty relies on.

### 2. Shell Detection Logic
- [ ] Unix: Check $SHELL, fallback to /bin/sh.
- [ ] Windows: Check %COMSPEC%, fallback to powershell.exe or cmd.exe.

---

## Interaction & Interception

### 1. Stream Management
- [ ] The "Pass-Through" Multiplexer: Create a controller to pipe PTY output to your UI.

### 2. Interactive Detection
- [ ] Interactive Trigger Detection: Scan the stream for patterns like Password:, [y/n]?, or (git merge) to know when the agent is "stuck."
- [ ] Human-in-the-Loop Switch: Build a mechanism to temporarily bridge os.Stdin directly to the PTY so the user can type a password or confirm a prompt.

---

## The UI & UX Layer

- [ ] Integrate charmbracelet/bubbletea: This manages the CLI state (Agent Thinking vs. Agent Executing).
- [ ] ANSI/Color Support: Use charmbracelet/lipgloss to render the agent's colored terminal output correctly.
- [ ] Markdown Rendering: Use charmbracelet/glamour to format the AI's explanations and code blocks in the terminal.

---

## Agent Intelligence & Environment

- [ ] Terminal Awareness: Pass the PTY window dimensions (Rows/Cols) to the LLM so it doesn't generate output that breaks the layout.
- [ ] Provider Agnostic Interface: Wrap your LLM logic (Gemini, OpenAI, Claude) in a Go interface to allow hot-swapping providers.

---

## Distribution & Build Pipeline

### Cross-Compilation Setup
- [ ] GOOS=windows GOARCH=amd64 (Uses the ConPTY logic).
- [ ] GOOS=linux GOARCH=amd64 (Uses Unix PTY logic).
- [ ] GOOS=darwin GOARCH=arm64 (Apple Silicon support).

### Build Configuration
- [ ] Static Linking: Ensure all CGO requirements (if any) are handled so you can ship a single, zero-dependency .exe or binary.
