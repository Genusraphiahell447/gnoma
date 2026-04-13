# AGENTS.md

## Domain Terminology
- **Elf**: An agent instance.
- **Turn**: A complete sequence of agentic reasoning and tool execution.
- **Routing Arm**: A specific model/provider selected by the `Router` for a task.
- **Stream Event**: Discrete updates during LLM generation (e.g., `EventTextDelta`, `EventToolCallStart`, `EventToolResult`).

## Build & Test Targets
- **Run**: `make run`
- **Test (Verbose)**: `make test-v`
- **Integration Tests**: `make test-integration` (requires `//go:build integration`)

## Key Dependencies
- **Mistral**: `github.com/VikingOwl91/mistral-go-sdk`
- **Anthropic**: `github.com/anthropics/anthropic-sdk-go`
- **OpenAI**: `github.com/openai/openai-go`
- **Google GenAI**: `google.golang.org/genai`
- **TUI**: `charm.land/bubbletea/v2`, `charm.land/lipgloss/v2`
- **Other**: `charm.land/bubbles/v2`, `charm.land/glamour/v2`, `github.com/pkoukk/tiktoken-go`

## Environment Variables
- `MISTRAL_API_KEY`: Required for Mistral provider.
- `ANTHROPIC_API_KEY`: Required for Anthropic provider.
- `OPENAI_API_KEY`: Required for OpenAI provider.
- `GOOGLE_API_KEY`: Required for Google provider.
