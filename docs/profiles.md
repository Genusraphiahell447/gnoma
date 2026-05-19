# Profiles

Profiles let you keep multiple independent gnoma configurations and switch
between them. Common cases:

- `work` vs. `private` — different API keys, different CLI binaries,
  stricter or looser permission mode per context.
- `experiment` — a non-default SLM model, plan mode, no persistence.

Profile mode is opt-in: gnoma stays on its single-config behavior until
you create `~/.config/gnoma/profiles/`.

## Layout

```
~/.config/gnoma/
├── config.toml            # base settings + default_profile
├── profiles/              # opt-in directory; presence enables profile mode
│   ├── work.toml
│   ├── private.toml
│   └── experiment.toml
├── quality-work.json      # per-profile router quality data
├── quality-private.json
└── quality-experiment.json
```

Per-project, session storage segregates the same way:

```
<projectRoot>/.gnoma/sessions/
├── work/
├── private/
└── experiment/
```

## Loading order

Each `gnoma` invocation merges configuration in this order (lowest to
highest priority):

1. Built-in defaults.
2. `~/.config/gnoma/config.toml` — the **base** config.
3. `~/.config/gnoma/profiles/<name>.toml` — the **active** profile
   (only when `profiles/` exists).
4. `<projectRoot>/.gnoma/config.toml` — project overrides.
5. Environment variables (`ANTHROPIC_API_KEY`, `GNOMA_PROVIDER`, etc.).

The active profile is resolved as follows:

- If `--profile <name>` is passed on the CLI, that wins.
- Otherwise, `default_profile` from the base `config.toml` is used.
- If neither is set and `profiles/` exists, gnoma fails fast with a
  list of available profiles. (Silent fallback to defaults would hide
  configuration mistakes.)

## Example: base + two profiles

`~/.config/gnoma/config.toml`:

```toml
default_profile = "work"

# Settings here apply to every profile unless the profile overrides them.
[tools]
bash_timeout = "30s"
```

`~/.config/gnoma/profiles/work.toml`:

```toml
[provider]
default = "anthropic"
[provider.api_keys]
anthropic = "${ANTHROPIC_WORK_KEY}"

[cli_agents]
claude = "claude-work"

[permission]
mode = "default"

[slm]
backend = "ollama"
model = "reecdev/tiny3.5:1.5b"
```

`~/.config/gnoma/profiles/private.toml`:

```toml
[provider]
default = "openai"
[provider.api_keys]
openai = "${OPENAI_PRIVATE_KEY}"

[cli_agents]
claude = "claude-priv"

[permission]
mode = "auto"

[slm]
backend = "ollama"
model = "reecdev/tiny3.5:500m"
```

`~/.config/gnoma/profiles/experiment.toml`:

```toml
[provider]
default = "mistral"
model = "mistral-large-latest"

[permission]
mode = "plan"

[slm]
enabled = false        # turn the classifier off entirely

[session]
max_keep = 0           # don't keep session history for experiments
```

## Switching

```bash
gnoma --profile work providers       # use work profile
gnoma --profile private              # private profile, default subcommand (TUI)
gnoma                                # base default_profile (here: work)
```

Profile selection is per-invocation. Restart re-reads `default_profile`;
no "last used" persistence — explicit switches stay explicit.

## Inspecting profiles

`gnoma profile list` lists configured profiles and marks the default
plus the currently active one:

```
$ gnoma profile list
Profiles in /home/x/.config/gnoma/profiles:

  experiment
  private    (active)
  work       (default)

Base config: /home/x/.config/gnoma/config.toml
```

If `default_profile` points at a file that doesn't exist, the listing
flags it explicitly so the command doubles as a diagnostic:

```
  ghost  (default, missing)
```

`gnoma profile show <name>` prints the merged effective config a
profile produces — sections, configured providers (key *names* only;
values are never printed), CLI agent overrides, arms, hooks, MCP
servers, and the per-profile quality + session paths:

```
$ gnoma profile show work
Profile: work
Base config: /home/x/.config/gnoma/config.toml
Profile file: /home/x/.config/gnoma/profiles/work.toml

[provider]
  default     = anthropic
  model       = claude-sonnet-4
  api_keys    = anthropic, openai

[cli_agents]
  claude = claude-work
  gemini = (canonical)

[permission]
  mode = default

…

Quality data: /home/x/.config/gnoma/quality-work.json
Session dir:  /repo/.gnoma/sessions/work
```

Both `profile list` and `profile show` work even when profile
resolution is otherwise broken — they're the recovery affordance for
diagnosing misconfigurations.

## Merge semantics

- **Scalars** (`provider.default`, `provider.model`, `tools.bash_timeout`,
  …): the profile value wins if set; otherwise base is preserved.
- **Maps** (`provider.api_keys`, `provider.endpoints`, `cli_agents`,
  `rate_limits`): per-key merge. Profile overrides individual keys
  without erasing the rest.
- **`[[hooks]]`**: profile hooks are appended after base hooks.
- **`[[arms]]`**: merged by `id`. Profile entries override the matching
  base entry; new IDs append. So a profile can tweak one arm's
  `cost_weight` without redeclaring the rest.
- **`[[mcp_servers]]`**: merged by `name` (same policy as arms).
- **`[security]`**, **`[plugins]`**, etc.: profile replaces if the
  profile defines anything in that section.

The project-level `.gnoma/config.toml` layer applies on top of the
merged base+profile result. Environment variables apply last and
override everything.

## Profile name rules

Names must match `[A-Za-z0-9_-]+`. Dots, slashes, spaces, and other
characters are rejected to keep derived paths
(`quality-<name>.json`, `sessions/<name>/`) predictable and to prevent
path traversal via `--profile`.

## Where per-profile data lives

| Data | Path |
|---|---|
| Router quality (bandit telemetry) | `~/.config/gnoma/quality-<profile>.json` |
| Session history | `<projectRoot>/.gnoma/sessions/<profile>/` |
| Plugins | `~/.config/gnoma/plugins/` (shared across profiles) |
| Skills | `~/.config/gnoma/skills/` (shared across profiles) |

Plugins and skills stay global on purpose — they're code, not
preferences. Use profile-specific `[plugins].enabled` / `disabled`
lists if you need a different mix per profile.

## `gnoma router stats` and profiles

When a profile is active, `gnoma router stats` reads
`quality-<profile>.json` and prefixes its output with the profile name
so it's clear which dataset you're looking at. To compare profiles:

```bash
gnoma --profile work    router stats
gnoma --profile private router stats
```

## Backward compatibility

If `~/.config/gnoma/profiles/` does not exist, gnoma behaves exactly
as before:

- Reads `~/.config/gnoma/config.toml` as the only base config.
- Stores quality data at `~/.config/gnoma/quality.json`.
- Stores sessions at `<projectRoot>/.gnoma/sessions/` (no profile
  subdirectory).
- `--profile <name>` returns a clear error pointing you at the
  `profiles/` directory to create.

Existing single-config installations don't need to do anything.
