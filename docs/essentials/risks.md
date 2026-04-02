---
essential: risks
status: complete
last_updated: 2026-04-02
project: gnoma
depends_on: []
---

# Risk / Unknowns

| ID | Risk | Severity | Mitigation | Status |
|----|------|----------|-----------|--------|
| R-001 | SDK breaking changes — provider SDKs are pre-1.0 and may change APIs | Medium | Pin versions, integration tests per provider, adapter layer absorbs changes | Open |
| R-002 | Google range-to-pull bridge goroutine leak — context cancellation edge cases | Medium | Thorough testing with `testing/synctest`, always select on `ctx.Done()` | Open |
| R-003 | Thinking block round-trip fidelity — Anthropic signatures must survive serialization | Medium | Unit tests with real signature values, golden file tests | Open |
| R-004 | Tool call ID generation inconsistency — Google/Ollama may return empty IDs | Low | Generate UUID if provider returns empty, documented in provider adapter | Open |
| R-005 | Mistral SDK 2.2.0 stability — user-maintained SDK, recently updated | Low | User maintains it, can fix bugs directly. Integration tests catch regressions. | Accepted |
| R-006 | Bubble Tea v2 maturity — v2 is relatively new | Low | Pin version, fallback to v1 if blockers. TUI is last milestone item. | Open |
| R-007 | Multi-provider routing complexity — coordinating elfs on different providers with different capabilities | High | Design routing interface early (M4), start simple (manual provider assignment), add rules incrementally | Open |
| R-008 | Context compaction coherence — summarization may lose critical details | Medium | Truncation as safe default, summarization opt-in, compact boundaries for recovery | Open |
| R-009 | Permission prompt UX in pipe mode — no TUI for interactive prompts | Low | Default to `allow` or `deny` in pipe mode, require explicit flag | Open |

## Open Questions

- [ ] How should routing rules be expressed in config? Per-task rules, model capability tags, cost-based? — needs research before M5
- [ ] Which local tokenizer library to use? (tiktoken port, sentencepiece, or provider-specific)
- [ ] Serve mode protocol — choose what fits best when implementing M7
- [x] ~~Should gnoma embed a tokenizer?~~ → Yes, include local tokenizer (M4)
- [x] ~~Session persistence format?~~ → SQLite (M7)
- [x] ~~Mistral SDK as long-term reference?~~ → Yes for now, revisit after M2

## Changelog

- 2026-04-02: Initial version
