# Gnoma — TODO

Active plans, newest first:

- **[`docs/superpowers/plans/2026-05-19-post-slm-unlock.md`](docs/superpowers/plans/2026-05-19-post-slm-unlock.md)**
  — outstanding work after the SLM unlock session: two-stage tool routing,
  CLI agent binary override, user profiles, per-arm capability tags,
  compound tools.
- **[`docs/superpowers/plans/2026-05-07-gnoma-roadmap.md`](docs/superpowers/plans/2026-05-07-gnoma-roadmap.md)**
  — broader roadmap (PTY shell, USP integration, ELF, distribution).
  Phase 4 ("Router Revisit") is superseded by the post-SLM plan above.

Phases (2026-05-07 roadmap):
1. M8 Cleanup (wiring gaps)
2. PTY Interactive Shell (`tea.ExecProcess`)
3. SLM Task Classifier (Ollama HTTP, opt-in) — **complete**
4. Router Revisit — **superseded by post-SLM plan**
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
