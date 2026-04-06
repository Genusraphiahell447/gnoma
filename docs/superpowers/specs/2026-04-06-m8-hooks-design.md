# M8.1 Hook System — Design Spec

## Context

M8 (Extensibility) is gnoma's next milestone after M7 (Elfs). It covers hooks, skills, MCP client, and plugins. This spec covers the first sub-project: **the hook system** — an event-driven extension mechanism that lets users run shell commands, LLM prompts, or elfs in response to engine lifecycle events.

Hooks enable policy enforcement (block dangerous bash commands), observability (log all tool calls), and transformation (rewrite tool args or results) without modifying gnoma's core code.

**Depends on:** M7 (complete). No dependency on other M8 sub-projects.
**Enables:** Skills, MCP, and plugins will use hooks for lifecycle integration.

---

## 1. Core Types

**Package:** `internal/hook/`

### EventType

```
PreToolUse     — before tool execution; can deny or transform args
PostToolUse    — after tool execution; can transform result (deny treated as skip)
SessionStart   — session begins (TUI launch or pipe mode start)
SessionEnd     — session ends (quit, Ctrl-C, pipe completes)
PreCompact     — before context compaction
Stop           — engine stop signal (max turns, user abort)
```

### CommandType

```
Command  — run a shell command; stdin/stdout JSON protocol
Prompt   — send a prompt to an LLM; use response as hook result
Agent    — spawn an elf; use its output as hook result
```

### HookDef

Parsed from config. Drives handler construction.

```go
type HookDef struct {
    Name        string
    Event       EventType
    Command     CommandType
    Exec        string         // shell command, prompt template, or elf prompt
    Timeout     time.Duration  // default 30s
    FailOpen    bool           // true = allow on timeout/error; false = deny
    ToolPattern string         // glob for tool name filtering (PreToolUse/PostToolUse only)
}
```

### HookResult

```go
type HookResult struct {
    Action   Action         // Allow, Deny, Skip
    Output   string         // transformed payload (empty = no transform)
    Error    error
    Duration time.Duration
}
```

### Action

```
Allow  — exit 0; hook approves
Deny   — exit 2; hook rejects
Skip   — exit 1; hook abstains (doesn't count as deny)
```

### ToolPattern

`ToolPattern` uses `filepath.Match` glob semantics (consistent with permission rules). A PreToolUse hook with `tool_pattern = "bash*"` fires only for bash tool calls. Empty = all tools.

---

## 2. Dispatcher

### Structure

```go
type Dispatcher struct {
    chains map[EventType][]Handler
    router *router.Router   // for prompt/agent hook types
    logger *slog.Logger
}

type Handler struct {
    def      HookDef
    executor Executor
}
```

### Executor Interface

```go
type Executor interface {
    Execute(ctx context.Context, payload []byte) (HookResult, error)
}
```

Three implementations:
- **CommandExecutor** — `os/exec` with stdin JSON pipe, reads stdout JSON, maps exit codes
- **PromptExecutor** — sends `def.Exec` (with template vars) to `router.Select(Task{Type: TaskReview})` → `provider.Stream()`. Uses TaskReview for routing since hooks are evaluative.
- **AgentExecutor** — spawns elf via `elf.Manager` with `TaskReview` task type, interprets output for allow/deny

### Dispatch Flow

`Dispatcher.Fire(event EventType, payload []byte) ([]byte, Action, error)`

1. Look up handler chain for event type
2. For PreToolUse/PostToolUse: filter chain by `ToolPattern` match against tool name in payload
3. Run ALL handlers, each with `context.WithTimeout(ctx, def.Timeout)`
4. Transforms chain: handler N receives (possibly transformed) payload from handler N-1
5. Collect all `HookResult`s
6. Decision logic:
   - ANY handler returns `Deny` → final = **Deny**
   - ANY handler errors AND `FailOpen=false` → final = **Deny**
   - `Skip` results don't count (hook abstains)
   - All remaining are `Allow` → final = **Allow**
   - Empty chain (no handlers) → **Allow**
7. For `PostToolUse`: Deny is treated as Skip (execution already happened)
8. Return `(finalPayload, action, error)`

### Constructor

```go
func NewDispatcher(defs []HookDef, router *router.Router, elfMgr *elf.Manager, logger *slog.Logger) (*Dispatcher, error)
```

Validates defs, constructs appropriate executor per CommandType, groups handlers by EventType.

---

## 3. Protocol

### Command Executor: stdin/stdout JSON

**Input payloads** (written to hook's stdin):

| Event | Payload |
|-------|---------|
| PreToolUse | `{"event":"pre_tool_use","tool":"bash","args":{"command":"rm -rf /tmp"}}` |
| PostToolUse | `{"event":"post_tool_use","tool":"bash","args":{...},"result":{"output":"...","metadata":{...}}}` |
| SessionStart | `{"event":"session_start","session_id":"abc","mode":"tui"}` |
| SessionEnd | `{"event":"session_end","session_id":"abc","turns":42}` |
| PreCompact | `{"event":"pre_compact","message_count":87,"token_estimate":120000}` |
| Stop | `{"event":"stop","reason":"max_turns"}` |

**Output** (hook writes to stdout):

```json
{"action":"allow","transformed":{"command":"rm -rf /tmp --verbose"}}
```

- `action`: `"allow"`, `"deny"`, `"skip"` — overrides exit code if present
- `transformed`: optional; replaces args (PreToolUse) or result (PostToolUse)
- Empty stdout → exit code alone determines action

**Exit codes** (fallback when no JSON stdout):
- 0 = allow
- 1 = skip
- 2 = deny

### Prompt Executor

Template variables in `def.Exec`: `{{.Event}}`, `{{.Tool}}`, `{{.Args}}`, `{{.Result}}`. Tool-related variables (`.Tool`, `.Args`, `.Result`) are empty strings for non-tool events (SessionStart, SessionEnd, PreCompact, Stop).

The executor sends the rendered prompt to an LLM via the router. The response is parsed for the first occurrence of "ALLOW" or "DENY" (case-insensitive). No match = Skip.

### Agent Executor

Same template variables. Spawns an elf with the rendered prompt. Elf output parsed for ALLOW/DENY the same way as the prompt executor. Elf failure → error → fail_open check.

---

## 4. Engine Integration

### 4.1 executeSingleTool (loop.go)

The main injection point. Before `tool.Execute()`:

```go
if e.cfg.Hooks != nil {
    payload := marshalPreToolPayload(toolName, args)
    transformed, action, err := e.cfg.Hooks.Fire(hook.PreToolUse, payload)
    if action == hook.Deny {
        return tool.Result{Output: "denied by hook: " + hookDenyReason(err)}, nil
    }
    if transformed != nil {
        args = transformed // use potentially modified args
    }
}
```

After `tool.Execute()`:

```go
if e.cfg.Hooks != nil {
    payload := marshalPostToolPayload(toolName, args, result)
    transformed, _, _ := e.cfg.Hooks.Fire(hook.PostToolUse, payload)
    if transformed != nil {
        result = unmarshalTransformedResult(transformed)
    }
}
```

### 4.2 Engine lifecycle events

- **SessionStart**: fired in `main.go` after engine construction, before first `Submit()`
- **SessionEnd**: fired in `main.go` shutdown (`defer`), or on `/quit` command
- **Stop**: fired in the engine's turn loop when `MaxTurns` reached or context cancelled

### 4.3 Compaction

- **PreCompact**: wire `Dispatcher.Fire(PreCompact, ...)` to `context.Window.OnPreCompact` callback (field exists in `WindowConfig`, currently unwired in `main.go`)
- No PostCompact hook in scope — existing `OnPostCompact` callback remains as-is

### 4.4 engine.Config

```go
type Config struct {
    // ... existing fields ...
    Hooks *hook.Dispatcher  // nil = no hooks
}
```

All hook call sites are nil-safe: `if e.cfg.Hooks != nil { ... }`.

### 4.5 main.go wiring

```go
// After config loading, before engine construction:
hookDefs := parseHookDefs(cfg.Hooks)  // []HookDef from config
dispatcher, err := hook.NewDispatcher(hookDefs, rtr, elfMgr, logger)

// Pass to engine:
eng, err := engine.New(engine.Config{
    // ...
    Hooks: dispatcher,
})

// Lifecycle hooks:
dispatcher.Fire(hook.SessionStart, sessionStartPayload())
defer dispatcher.Fire(hook.SessionEnd, sessionEndPayload())
```

---

## 5. Config Schema

### TOML format

```toml
[[hooks]]
name = "log-all-tools"
event = "post_tool_use"
type = "command"
exec = "tee -a /tmp/gnoma-tool-log.jsonl"
timeout = "5s"
fail_open = true

[[hooks]]
name = "block-dangerous-bash"
event = "pre_tool_use"
type = "command"
exec = "bash-safety-check.sh"
tool_pattern = "bash*"
timeout = "10s"
fail_open = false

[[hooks]]
name = "llm-safety-review"
event = "pre_tool_use"
type = "prompt"
exec = "Is this tool call safe? Tool: {{.Tool}}, Args: {{.Args}}. Reply ALLOW or DENY."
tool_pattern = "bash*"
timeout = "30s"
fail_open = true
```

### Merge behavior

User hooks (`~/.config/gnoma/config.toml`) run first, then project hooks (`.gnoma/config.toml`). Within a layer, order in the TOML file is preserved. This lets users set global policies while projects add their own.

### Config struct

```go
type HookConfig struct {
    Name        string `toml:"name"`
    Event       string `toml:"event"`
    Type        string `toml:"type"`
    Exec        string `toml:"exec"`
    Timeout     string `toml:"timeout"`
    FailOpen    bool   `toml:"fail_open"`
    ToolPattern string `toml:"tool_pattern"`
}
```

Added to `config.Config`:

```go
type Config struct {
    // ... existing ...
    Hooks []HookConfig `toml:"hooks"`
}
```

---

## 6. Testing Strategy

### Unit tests (`internal/hook/`)

**dispatcher_test.go:**
- Single handler allow/deny/skip
- All-must-allow: 2 allow + 1 deny = deny
- All-must-allow: 2 allow + 1 skip = allow (skip abstains)
- Transform chaining: handler A transforms, handler B receives transformed
- ToolPattern filtering: handler only fires for matching tools
- Empty chain = allow
- PostToolUse deny treated as skip

**command_test.go:**
- Exit 0/1/2 → allow/skip/deny
- Stdin JSON delivered correctly
- Stdout JSON parsed, transformed payload extracted
- Empty stdout (exit code fallback)
- Timeout + fail_open=true → allow
- Timeout + fail_open=false → deny
- Broken hook (crash, invalid JSON) → error + fail_open check

**prompt_test.go:**
- Template variable substitution
- LLM response ALLOW/DENY parsing
- No match → Skip
- Timeout handling

**agent_test.go:**
- Elf spawned with templated prompt
- Output parsed for allow/deny
- Elf failure → error + fail_open check

**config_test.go:**
- Valid TOML round-trip
- Invalid event/type/timeout rejected
- Merge order: user hooks before project hooks

### Integration tests (`internal/engine/`)

**hook_integration_test.go:**
- PreToolUse deny prevents tool execution
- PreToolUse transform modifies args seen by tool
- PostToolUse transform modifies result seen by LLM
- Nil dispatcher = normal execution unchanged

---

## 7. Files

### New files

| File | Purpose |
|------|---------|
| `internal/hook/event.go` | EventType, Action enums |
| `internal/hook/hook.go` | HookDef, HookResult, Handler types |
| `internal/hook/dispatcher.go` | Dispatcher, Fire(), NewDispatcher() |
| `internal/hook/command.go` | CommandExecutor |
| `internal/hook/prompt.go` | PromptExecutor |
| `internal/hook/agent.go` | AgentExecutor |
| `internal/hook/payload.go` | Payload marshal/unmarshal helpers |
| `internal/hook/dispatcher_test.go` | Dispatcher unit tests |
| `internal/hook/command_test.go` | CommandExecutor tests |
| `internal/hook/prompt_test.go` | PromptExecutor tests |
| `internal/hook/agent_test.go` | AgentExecutor tests |
| `internal/hook/config_test.go` | Config parsing tests |

### Modified files

| File | Change |
|------|--------|
| `internal/config/config.go` | Add `Hooks []HookConfig` field |
| `internal/engine/engine.go` | Add `Hooks *hook.Dispatcher` to Config |
| `internal/engine/loop.go` | PreToolUse/PostToolUse/Stop hook calls in executeSingleTool and turn loop |
| `cmd/gnoma/main.go` | Parse hook config, construct Dispatcher, fire SessionStart/End, wire PreCompact |

---

## 8. Verification

```sh
# All hook unit tests
go test ./internal/hook/ -v

# Engine integration tests
go test ./internal/engine/ -run "TestHook" -v

# Full suite (no regressions)
make test

# Build
make build

# Manual: add a test hook to .gnoma/config.toml, run gnoma, verify hook fires
```
