# ADR-003: Plugin Trust via TOFU Manifest Pinning

**Status:** Accepted
**Date:** 2026-05-19

## Context

Plugins ship arbitrary code paths: they declare hook executables (run on every
matching tool call) and MCP server commands (long-lived subprocesses with
stdin/stdout protocol access). The plugin loader resolves these against the
plugin directory and exec's them with gnoma's full privileges.

Before this ADR, the loader had:

- Path traversal guards in the manifest (`checkSafePath`).
- An enabled/disabled allowlist by plugin name (`PluginsSection`).
- No integrity check on the manifest itself.

The gap: once a plugin sits in `~/.config/gnoma/plugins/` (placed there by the
user, a setup script, or any process with write access to the directory), its
manifest can be edited or replaced silently. A modified `plugin.json` can add
a new hook on a popular event, or swap the MCP command to point at a malicious
binary, and the next gnoma startup will load it with no signal to the user.

Audit finding C2 flagged this as the critical gap in the plugin trust model.

## Decision

The plugin loader records and verifies a SHA-256 of every enrolled plugin's
`plugin.json` bytes using a **Trust-On-First-Use** (TOFU) discipline. Pins are
persisted in `~/.config/gnoma/plugins.pins.toml`.

For every enabled plugin on every startup:

1. **No pin recorded** — compute the hash, write it to the pin store, log a
   prominent warning naming the plugin and the hash. The plugin is loaded.
2. **Pin matches** — load silently.
3. **Pin mismatches** — refuse to load. Log an error with the pinned hash, the
   actual hash, and a hint on re-enrollment. Other plugins continue loading.

The hash covers the entire file, not just selected fields. Any edit to the
manifest — including a version bump, a typo fix, or a new skill glob —
triggers re-enrollment. This is intentional: the user re-confirms trust on
every change to the trust surface.

A pin-store I/O failure (read or write) is logged and downgrades to
load-without-pinning so a corrupted file cannot lock the user out of plugins
they previously trusted.

## Alternatives Considered

### Alternative A: Cryptographic signatures

- **Pros:** Strong identity assertion; cross-machine portability of trust.
- **Cons:** Requires plugin authors to manage signing keys, a verification key
  to be configured in gnoma, and a workflow to distribute fingerprints.
  Overkill for a tool that targets developer workstations with a single user.

### Alternative B: Require explicit enrollment (no TOFU)

- **Pros:** No silent first-load; user explicitly opts into every plugin.
- **Cons:** Friction that punishes the common case (user puts a plugin in
  their plugin dir on purpose, runs gnoma, expects it to work). The first-run
  TOFU warning preserves the audit trail without blocking workflow.

### Alternative C: Hash only the executable bits

- **Pros:** Fewer benign re-enrollments on documentation tweaks.
- **Cons:** Skill globs, plugin name, and `gnoma_version` constraints all
  influence what gnoma loads or how. Selecting a subset would create subtle
  bypasses (e.g. moving an existing hook into a newly-added `Capabilities`
  section the hash skipped). Whole-file hash has no such surface.

## Consequences

**Positive:**
- Tampering with an installed plugin's `plugin.json` between gnoma runs is
  loud and refused, not silent.
- Every plugin's enrollment is auditable from a single TOML file.
- No external infrastructure (no PKI, no registry).

**Negative:**
- Legitimate plugin upgrades require a manual pin removal. For active plugin
  development this is friction; the workaround is to keep development plugins
  in a project directory (which still pins, but is throwaway per project).
- A user who ignores the TOFU warning gains nothing.

**Neutral:**
- Pin file lives next to the global config — same trust boundary as the
  config itself. Compromising that directory already compromises gnoma.
