# Gnoma — TODO

The active roadmap lives in:

**[`docs/superpowers/plans/2026-05-07-gnoma-roadmap.md`](docs/superpowers/plans/2026-05-07-gnoma-roadmap.md)**

Phases:
1. M8 Cleanup (wiring gaps)
2. PTY Interactive Shell (`tea.ExecProcess`)
3. SLM Task Classifier (Ollama HTTP, opt-in)
4. Router Revisit (post-SLM, see ADR-013)
5. USP Security Integration
6. ELF Binary Support (deferred/opportunistic)
7. Distribution (CI trigger for goreleaser)

---

## Stable Backlog (not in active phases)

- **Thinking mode** (disabled / budget / adaptive) — M12 in milestones
- **Structured output** with JSON schema validation — M12
- **SQLite session persistence** + serve mode — M10
- **Task learning** (pattern recognition, persistent tasks) — M11
- **Web UI** (`gnoma web`) — M15
- **OAuth / keyring** — M13
- **Observability** (feature flags, cost dashboards) — M14
- **PE / Mach-O support** — future, after ELF Phase 6

---

## Architecture References

- Milestones: `docs/essentials/milestones.md`
- Decisions: `docs/essentials/decisions/`
- ADR-013 (SLM routing, supersedes ADR-009): `docs/essentials/decisions/002-slm-routing.md`
