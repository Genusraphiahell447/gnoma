# Plugin Trust

gnoma plugins ship arbitrary executables (hooks and MCP servers) that run with
your user privileges. To prevent silent tampering with an already-installed
plugin, gnoma pins each plugin's `plugin.json` by SHA-256 the first time it
loads, and refuses to load on subsequent runs if the manifest has changed.

This is the same Trust-On-First-Use (TOFU) discipline that SSH uses for host
keys, applied to plugin manifests.

## Pin file

```
~/.config/gnoma/plugins.pins.toml
```

Format:

```toml
[pins]
git-tools     = "a3f1c5d8e9b2..."
docker-tools  = "7c4b9f0e2a8d..."
```

The file is created the first time gnoma loads any plugin. It is owned by the
same trust boundary as `~/.config/gnoma/config.toml` — anyone who can write
to your config directory can also re-pin plugins.

## First load (TOFU warning)

When gnoma sees a plugin it has no pin for, it records the hash automatically
and logs a warning that names the plugin and the hash:

```
WARN enrolling new plugin (trust on first use)  name=git-tools scope=user
     sha256=a3f1c5d8e9b2c7f0...
```

The plugin loads normally. If this is the plugin you intended to install, you
can ignore the warning. If it appeared unexpectedly, inspect the plugin
directory and the matching `plugins.pins.toml` entry before running gnoma
again.

## Subsequent loads

- **Match:** silent. The plugin loads with no log line.
- **Mismatch:** the plugin is refused. gnoma logs an error and **does not**
  overwrite the pin:

```
ERROR refusing plugin — manifest changed since enrolment  name=git-tools
      pinned=a3f1c5d8...  actual=b9e4d2c1...
      hint="remove the entry from plugins.pins.toml to re-enrol"
```

Other plugins are unaffected.

## Re-enrolling a plugin

When you legitimately update a plugin and want gnoma to accept the new
manifest:

1. Inspect the changes (`git diff`, manifest diff, or just review the new
   `plugin.json`). Confirm the update is what you expected.
2. Delete the plugin's line from `plugins.pins.toml`.
3. Run gnoma. The new hash is recorded as a fresh TOFU enrollment and a
   warning is logged.

If you trust the source and want to skip the inspect-and-delete step, you can
overwrite the pin directly: replace the hex value in the file with the hash
gnoma logged for the new manifest. (gnoma prints the actual hash in the
mismatch error.)

## Disabling pinning

There is no opt-out. If the pin file cannot be read or written (permissions,
disk full, etc.), gnoma logs a warning and loads plugins without pinning for
that run — but it does not silently disable the mechanism for subsequent
runs.

## What the hash covers

The full bytes of `plugin.json`. Any edit — version bump, comment, whitespace
— invalidates the pin. This is intentional: the trust surface includes every
field the loader reads, and selectively hashing fields would create bypass
opportunities.

The hash does **not** cover the rest of the plugin directory (skill files,
hook scripts, MCP server binaries). Those are referenced by paths declared
in the manifest; tampering with a hook script while leaving `plugin.json`
untouched is **not** detected. Treat the plugin directory itself as a trust
boundary at filesystem-permissions level.

## Threat model

Pinning protects against:

- Silent tampering of an installed plugin's manifest by another process or
  user with write access to the plugin directory.
- Accidental drift (e.g. `git pull` in a plugin you forgot was a working copy)
  pulling in capabilities you didn't intend to grant.

Pinning does **not** protect against:

- The first install — TOFU trusts whatever is on disk at first sight.
- Tampering with the pin file itself by a process that already has write
  access to your config directory.
- Tampering with hook scripts or MCP binaries that live alongside the
  manifest (see "What the hash covers").

For the gnoma threat model (single-user dev workstation), pinning closes the
largest realistic gap: post-install manifest drift on a machine the user
shares with other processes or where a plugin source is a live working copy.

## See also

- ADR-003: [Plugin Trust via TOFU Manifest Pinning](essentials/decisions/003-plugin-trust.md)
