# AGENTS.md

## 🚀 Project Overview: Gnoma Agentic Assistant

**Project Name:** Gnoma
**Description:** A provider-agnostic agentic coding assistant written in Go. The name is derived from the northern pygmy-owl (*Glaucidium gnoma*). This system facilitates complex, multi-step reasoning and task execution by orchestrating calls to various external Large Language Model (LLM) providers.

**Module Path:** `somegit.dev/Owlibou/gnoma`

---

## 🛠️ Build & Testing Instructions

The standard build system uses `make` for all development and testing tasks:

*   **Build Binary:** `make build` (Creates the executable in `./bin/gnoma`)
*   **Run All Tests:** `make test`
*   **Lint Code:** `make lint` (Uses `golangci-lint`)
*   **Run Coverage Report:** `make cover`

**Architectural Note:** Changes requiring deep architectural review or boundaries must first consult the design decisions documented in `docs/essentials/INDEX.md`.

---

## 🔗 Dependencies & Providers

The system is designed for provider agnosticism and supports multiple primary backends through standardized interfaces:

*   **Mistral:** Via `github.com/VikingOwl91/mistral-go-sdk`
*   **Anthropic:** Via `github.com/anthropics/anthropic-sdk-go`
*   **OpenAI:** Via `github.com/openai/openai-go`
*   **Google:** Via `google.golang.org/genai`
*   **Ollama/llama.cpp:** Handled via the OpenAI SDK structure with a custom base URL configuration.

---

## 📜 Development Conventions (Mandatory Guidelines)

Adherence to the following conventions is required for maintainability, testability, and consistency across the entire codebase.

### ⭐ Go Idioms & Style
*   **Modern Go:** Adhere strictly to Go 1.26 idioms, including the use of `new(expr)`, `errors.AsType[E]`, `sync.WaitGroup.Go`, and implementing structured logging via `log/slog`.
*   **Data Structures:** Use **structs with explicit type discriminants** to model discriminated unions, *not* Go interfaces.
*   **Streaming:** Implement pull-based stream iterators following the pattern: `Next() / Current() / Err() / Close()`.
*   **API Handling:** Utilize `json.RawMessage` for tool schemas and arguments to ensure zero-cost JSON passthrough.
*   **Configuration:** Favor **Functional Options** for complex configuration structures.
*   **Concurrency:** Use `golang.org/x/sync/errgroup` for managing parallel work groups.

### 🧪 Testing Philosophy
*   **TDD First:** Always write tests *before* writing production code.
*   **Test Style:** Employ table-driven tests extensively.
*   **Contextual Testing:**
    *   Use build tags (`//go:build integration`) for tests that interact with real external APIs.
    *   Use `testing/synctest` for any tests requiring concurrent execution checks.
    *   Use `t.TempDir()` for all file system simulations.

### 🏷️ Naming Conventions
*   **Packages:** Names must be short, entirely lowercase, and contain no underscores.
*   **Interfaces:** Must describe *behavior* (what it does), not *implementation* (how it is done).
*   **Filenames/Types:** Should follow standard Go casing conventions.

### ⚙️ Execution & Pattern Guidelines
*   **Orchestration Flow:** State management should be handled sequentially through specialized manager/worker structs (e.g., `AgentExecutor`).
*   **Error Handling:** Favor structured, wrapped errors over bare `panic`/`recover`.

---
***Note:*** *This document synthesizes the core architectural constraints derived from the project structure.*