# Gnoma — TODO

Active plans, newest first:

- **[`docs/superpowers/plans/2026-05-19-security-wave1-safeprovider.md`](docs/superpowers/plans/2026-05-19-security-wave1-safeprovider.md)**
  — post-audit hardening, Wave 1. Closes the four firewall-bypass
  call sites (SLM classifier, summarizer, prompt hook, routerStreamer)
  by introducing `security.SafeProvider` at the provider boundary.
  **In progress on `feat/security-wave1-safeprovider`** — implementation
  complete; ADR and merge pending. Waves 2 (incognito coherence) and
  3 (scanner + path hygiene) are scoped but not yet drafted.
- **[`docs/superpowers/plans/2026-05-19-post-slm-unlock.md`](docs/superpowers/plans/2026-05-19-post-slm-unlock.md)**
  — outstanding work after the SLM unlock session. Phases A (two-stage
  tool routing), B (CLI agent binary override), C (user profiles), and
  D (per-arm capability tags) are **complete**. Phase E (compound
  tools) is held until ≥50 SLM observations inform which primitives are
  worth adding.
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
