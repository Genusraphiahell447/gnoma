---
essential: vision
status: complete
last_updated: 2026-04-02
project: gnoma
depends_on: []
---

# Vision

## What

A provider-agnostic agentic coding assistant — a single Go binary that streams, calls tools, and manages conversations across any LLM provider without privileging any one of them. Providers don't just coexist — they collaborate. Elfs (sub-agents) running on different providers work together within a single session, routed by capability, cost, or latency.

Named after the northern pygmy-owl (*Glaucidium gnoma*). Sub-agents are called *elfs* (elf owl, *Micrathene whitneyi*).

## Who

Any developer who wants an AI coding assistant they actually control — from hobbyists running local models on their own hardware, to professionals choosing between cloud providers, to teams where each member prefers a different LLM.

## Problem

Current agentic coding assistants (Claude Code, Cursor, Windsurf, Copilot) lock users to a single provider. Switching costs are high. Behavior is opaque — hidden tool execution, unclear token spend, no way to customize permissions or inject hooks. Local model support is an afterthought.

Worse, these assistants are single-provider silos. You can't have one model coordinate with another, route tasks to the best-fit provider, or mix a cloud model's reasoning with a local model's speed. Every request goes to the same provider regardless of complexity, cost, or capability.

There is no open, extensible assistant that treats all providers as collaborators, gives full visibility into every action, and works just as well with a local Ollama instance as with a cloud API.

## Core Principles

- **Provider freedom** — switch between Anthropic, OpenAI, Google, Mistral, or local models with one config change. No privileged provider.
- **Multi-provider collaboration** — elfs on different providers work together. A coordinator on Claude dispatches research to a local Qwen elf and code review to an OpenAI elf. Routing rules direct tasks by capability, cost, or latency.
- **Transparency** — every tool call, permission check, and token spend is visible. No hidden behavior.
- **Extensibility** — hooks, skills, and MCP let users shape the assistant without forking.
- **Simplicity** — single binary, zero infrastructure, runs anywhere Go compiles.

## Success Criteria

- [ ] gnoma replaces a vendor-locked assistant as the user's daily driver
- [ ] A user can switch providers mid-session with zero friction
- [ ] Elfs run on different providers simultaneously — a coordinator on one provider dispatches work to elfs on other providers
- [ ] Routing rules direct tasks to providers by capability, cost, or latency
- [ ] Local models (Ollama, llama.cpp) work with full tool-use support
- [ ] Every tool call, permission check, and token spend is visible to the user
- [ ] Users extend gnoma via hooks, skills, and MCP without forking

## Changelog

- 2026-04-02: Initial version
