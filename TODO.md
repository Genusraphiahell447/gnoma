# Gnoma — TODO

Active work, newest first.

## In flight

- **Distribution** — `.goreleaser.yml` is configured for
  `linux`/`darwin`/`windows` × `amd64`/`arm64`. Still pending: first
  tag + release pipeline trigger, optional Homebrew tap and Docker
  image, mirror release publishing to GitHub.
- **Compound tools (post-SLM Phase E)** — held until ≥50 SLM
  observations inform which primitives are worth adding. See
  [`docs/superpowers/plans/2026-05-19-post-slm-unlock.md`](docs/superpowers/plans/2026-05-19-post-slm-unlock.md).

## Stable backlog (not in active phases)

- **Thinking mode** (disabled / budget / adaptive) — M12.
- **Structured output** with JSON schema validation — M12.
- **Native agy JSON output** — switch the subprocess provider to
  `--output-format stream-json` once the agy CLI supports it,
  replacing the current prompt-augmentation fallback.
- **SQLite session persistence** + serve mode — M10.
- **Task learning** (pattern recognition, persistent tasks) — M11.
- **Web UI** (`gnoma web`) — M15.
- **OAuth / keyring** — M13.
- **Observability** (feature flags, cost dashboards) — M14.
- **PE / Mach-O ELF support** — future, after ELF Phase 6.

## History

Completed initiatives, kept here as pointers to their plan files:

- **Post-audit security hardening** — complete 2026-05-19. Three waves
  + one ADR closed all 14 findings from the external review:
  - [Wave 1 — SafeProvider boundary](docs/superpowers/plans/2026-05-19-security-wave1-safeprovider.md)
  - [Wave 2 — Incognito coherence](docs/superpowers/plans/2026-05-19-security-wave2-incognito.md)
  - Wave 3 — scanner + path hygiene (rolled out directly without a
    plan file; see commits leading up to 2026-05-19 on `internal/security`)
  - [ADR-004 — PostToolUse hook ordering](docs/essentials/decisions/004-posttooluse-hook-ordering.md)
- **Post-SLM unlock** —
  [plan](docs/superpowers/plans/2026-05-19-post-slm-unlock.md). Phases
  A–D complete (two-stage tool routing, CLI agent binary override,
  user profiles, per-arm capability tags).
- **2026-05-07 roadmap** —
  [plan](docs/superpowers/plans/2026-05-07-gnoma-roadmap.md). M1–M8
  done; SLM classifier (Phase 3) complete; Phase 4 superseded by the
  post-SLM plan.

## Reference

- Milestones: `docs/essentials/milestones.md`
- Decisions: `docs/essentials/decisions/`
- ADR-002 (SLM routing, supersedes earlier ADR-009): `docs/essentials/decisions/002-slm-routing.md`
