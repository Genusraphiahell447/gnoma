# M6/M7 Close-out: Tool Persistence, Tokenizer, Router Feedback, Coordinator Mode

**Date:** 2026-04-05  
**Milestones:** M6 (Context Intelligence), M7 (Elfs)  
**Status:** Approved

---

## Context

Gnoma's M6/M7 gap audit left four items unfinished:

1. **Tool result persistence** — currently only fires for results >50K chars and writes to `.gnoma/sessions/`. The vision is every meaningful result (>1KB) persisted to `/tmp` so tools can share state across a session.
2. **Local tokenizer** — token counting uses a `len/4` heuristic. This causes compaction to fire too early or too late and makes context window sizing inaccurate.
3. **Router feedback** — `ReportOutcome` is a `slog.Debug` stub. Elf success/failure signals are captured but never used to influence arm selection.
4. **Coordinator mode** — completely unimplemented. Needed to close M7 and unblock M8/M10 coordinator work.

The `/tmp` tool result files become the shared artifact layer connecting all four: elfs write results to shared files, the coordinator discovers them via `list_results`, and the router uses elf outcomes (with result file references) for quality tracking.

---

## 1. Tool Result Persistence

### What changes

**New package:** `internal/tool/persist`

```
persist/
  store.go       -- Store type, session dir management
  store_test.go
```

**`Store` type:**

```go
type Store struct {
    dir string  // /tmp/gnoma-<sessionID>/tool-results
}

func New(sessionID string) *Store
func (s *Store) Save(toolName, callID, content string) (path string, persisted bool)
func (s *Store) List(filter string) ([]ResultFile, error)  // filter = glob on tool name prefix
func (s *Store) Read(path string) (string, error)           // validates path is within session dir
```

```go
type ResultFile struct {
    Path     string
    ToolName string
    CallID   string
    Size     int64
    ModTime  time.Time
}
```

**Threshold:** `len(content) >= 1024` bytes. Below this, `Save` returns `("", false)` — no file written.

**File naming:** `/tmp/gnoma-<sessionID>/tool-results/<toolName>-<callID>.txt`
- Example: `/tmp/gnoma-20260405-150405-abc123/tool-results/bash-toolu_01AbCd.txt`

**Session ID:** `<YYYYMMDD-HHMMSS>-<6 random hex chars>` — generated once at engine startup, passed into `Store.New()`.

**Inline context replacement** (what the LLM sees instead of the full result):
```
[Tool result saved: /tmp/gnoma-<session>/tool-results/<tool>-<id>.txt]

Preview (first 2000 chars):
<truncated content>
```

**Engine integration:**
- `engine.Engine` gains a `store *persist.Store` field
- `executeSingleTool` in `loop.go` replaces the `PersistLargeResult` call with `store.Save()`
- The existing `PersistLargeResult` function in `internal/context/persist.go` is retired (deleted)
- `elf.Manager` receives the `Store` and passes it to each elf's engine config

**Cleanup:** No explicit cleanup. `/tmp/gnoma-*` dirs are session-scoped; OS garbage-collects `/tmp` on reboot or via tmpwatch/systemd-tmpfiles.

### Key constraint

`Store.Read()` must validate that the requested path is prefixed with the session's tool-results dir. This prevents `read_result` from being used to traverse arbitrary filesystem paths.

---

## 2. Local Tokenizer (tiktoken-go)

### What changes

**New dependency:** `github.com/pkoukk/tiktoken-go`

**New package:** `internal/tokenizer`

```
tokenizer/
  tokenizer.go
  tokenizer_test.go
```

**`Tokenizer` type:**

```go
type Tokenizer struct {
    enc      *tiktoken.Tiktoken  // nil until first use
    encoding string              // e.g. "cl100k_base"
    mu       sync.Mutex
}

func New(encoding string) *Tokenizer
func ForProvider(providerName string) *Tokenizer
func (t *Tokenizer) Count(text string) int
```

**Provider → encoding mapping:**

| Provider | Encoding |
|----------|----------|
| `anthropic` | `cl100k_base` |
| `openai` | `cl100k_base` |
| `mistral` | `o200k_base` |
| `google` | `o200k_base` |
| `ollama` | `o200k_base` |
| `llamacpp` | `o200k_base` |
| (unknown) | `cl100k_base` (fallback) |

**Lazy loading:** Encoding is loaded on first `Count()` call inside a `sync.Once` equivalent (`sync.Mutex` guard, check-and-initialize). Encoding files are ~2MB per vocab and are embedded by tiktoken-go via `go:embed`.

**Fallback:** If tiktoken initialization fails (e.g., unsupported encoding, memory pressure), `Count()` falls back to `len(text)/4` and logs a `slog.Warn` once via `sync.Once`.

### Context tracker changes

- `context.Tracker` gains a `tokenizer *tokenizer.Tokenizer` field (optional; nil → heuristic)
- `EstimateTokens(text)` replaced by `CountTokens(tok *Tokenizer, text string)` — uses tokenizer if non-nil, else heuristic
- `EstimateMessages` renamed `CountMessages`, same pattern
- Tracker initialized with tokenizer in `main.go`: `tokenizer.ForProvider(cfg.Provider.Name)`
- **Context window size fix:** `MaxTokens` set from `arm.Capabilities.ContextWindow` instead of `cfg.Provider.MaxTokens * 20`. This field is already populated for all providers.
- **Prefix token counting:** Prefix messages are counted at load time and added to the tracker's initial baseline so they're visible to compaction logic.

---

## 3. Router Feedback (Heuristic Quality Tracking)

### What changes

**New file:** `internal/router/feedback.go`

```go
type QualityTracker struct {
    mu     sync.RWMutex
    scores map[string]map[TaskType]*EMAScore  // armID -> taskType -> score
}

type EMAScore struct {
    Value float64
    Count int
}

const qualityAlpha = 0.3
const minObservations = 3  // below this, fall back to heuristic-only

func NewQualityTracker() *QualityTracker
func (qt *QualityTracker) Record(armID string, taskType TaskType, success bool)
func (qt *QualityTracker) Quality(armID string, taskType TaskType) (score float64, hasData bool)
```

`Record`:
- Maps `success` to observation: `1.0` (success) or `0.0` (failure)
- EMA update: `score.Value = qualityAlpha*observation + (1-qualityAlpha)*score.Value`
- Increments `Count`

`Quality`:
- Returns `(0, false)` when `Count < minObservations`
- Returns `(score.Value, true)` otherwise

### Outcome struct extension

`router.Outcome` gains one field:
```go
type Outcome struct {
    ArmID           string
    TaskType        TaskType
    Success         bool
    Tokens          int
    Duration        time.Duration
    ResultFilePaths []string  // NEW: paths to /tmp tool result files (for future M9 analysis)
}
```

The `ResultFilePaths` field is populated by the `agent`/`spawn_elfs` tools: snapshot `store.List()` before spawning the elf, snapshot again after `Wait()` returns, then diff — files present in the post-snapshot but not the pre-snapshot are attributed to that elf's run.

### Router integration

- `Router` gains a `quality *QualityTracker` field, initialized in `New()`
- `ReportOutcome` calls `qt.Record(o.ArmID, o.TaskType, o.Success)` (replaces slog.Debug stub)
- `scoreArm()` updated to blend observed and heuristic quality:
  ```go
  hq := heuristicQuality(arm, task)
  if observed, hasData := r.quality.Quality(arm.ID, task.Type); hasData {
      quality = 0.7*observed + 0.3*hq
  } else {
      quality = hq
  }
  ```

### What this does NOT include (M9)

- No Thompson Sampling / Beta distributions
- No state persistence across restarts
- No delayed attribution for orchestration tasks
- No implicit feedback (edit distance, escalation signals)

---

## 4. Coordinator Mode

### What changes

**New tools in `internal/tool/agent/`:**

`list_results.go` — `ListResultsTool` (name: `list_results`):
```go
// Parameters: filter string (optional, glob on tool name prefix, e.g. "bash*")
// Returns: formatted list of result files in the session:
//   /tmp/gnoma-<session>/tool-results/bash-toolu_abc.txt  [bash, 4.2KB, 15:04:05]
//   /tmp/gnoma-<session>/tool-results/fs.grep-toolu_def.txt  [fs.grep, 1.1KB, 15:04:12]
// IsReadOnly: true, IsDestructive: false
```

`read_result.go` — `ReadResultTool` (name: `read_result`):
```go
// Parameters: path string (required)
// Validates: path must be prefixed with store.Dir() — no path traversal
// Returns: full file content
// IsReadOnly: true, IsDestructive: false
```

Both tools receive the `*persist.Store` as a constructor argument.

**Coordinator system prompt injection** in `internal/engine/loop.go`:

When `router.ClassifyTask()` returns `TaskOrchestration`, the engine prepends a coordinator block to the request's system prompt:

```
You are operating in coordinator mode. Your role is to decompose complex work into parallel tasks and orchestrate elfs.

Rules:
- Use `spawn_elfs` to dispatch N tasks in parallel when they don't share write state.
- Use `list_results` to discover outputs produced by prior tool calls in this session.
- Pass result file paths to elfs in their prompts so they can read prior outputs with `read_result` or `fs.read`.
- Writes are serial: if two elfs would write the same file, sequence them.
- Synthesize elf outputs into a coherent final answer.
```

This prompt injection is conditional: only fires when `ClassifyTask(latestUserMessage).Type == TaskOrchestration`. It does not create a new engine mode.

**Tool registration** in `main.go`:
```go
reg.Register(agent.NewListResultsTool(store))
reg.Register(agent.NewReadResultTool(store))
```

---

## File Map

| File | Action |
|------|--------|
| `internal/tool/persist/store.go` | New |
| `internal/tool/persist/store_test.go` | New |
| `internal/tokenizer/tokenizer.go` | New |
| `internal/tokenizer/tokenizer_test.go` | New |
| `internal/router/feedback.go` | New |
| `internal/router/feedback_test.go` | New |
| `internal/tool/agent/list_results.go` | New |
| `internal/tool/agent/read_result.go` | New |
| `internal/engine/engine.go` | Modify: add `store`, `tokenizer` fields to `Config` |
| `internal/engine/loop.go` | Modify: replace `PersistLargeResult`, add coordinator prompt injection |
| `internal/context/tracker.go` | Modify: accept `*tokenizer.Tokenizer`, update `EstimateTokens` |
| `internal/context/window.go` | Modify: use `CountMessages`, fix `MaxTokens` derivation |
| `internal/context/persist.go` | Delete: retire `PersistLargeResult` / `TruncateToolResult` |
| `internal/router/router.go` | Modify: add `QualityTracker`, wire `ReportOutcome` |
| `internal/router/selector.go` | Modify: blend observed quality into `scoreArm()` |
| `internal/router/arm.go` | Modify: extend `Outcome` with `ResultFilePaths` |
| `internal/elf/manager.go` | Modify: accept and forward `*persist.Store` to elf engines |
| `cmd/gnoma/main.go` | Modify: init `Store`, `Tokenizer`, register new tools |
| `go.mod` | Modify: add `github.com/pkoukk/tiktoken-go` |

---

## Verification

1. **Persistence:** Run `echo "list all go files" | gnoma --provider anthropic` and check `/tmp/gnoma-*/tool-results/` for result files. Verify small results (<1KB) are absent, large ones present with preview in conversation.
2. **Tokenizer:** Set breakpoints or add `slog.Debug` in `Count()` to confirm tiktoken is invoked. Check that context window percentage in TUI tracks accurately against provider-reported token counts.
3. **Router feedback:** Spawn 5 elfs, mix of successes and failures. Check that `scoreArm()` values differ from pure heuristic via a debug log or test. Run `make test ./internal/router/...`.
4. **Coordinator:** Send a prompt containing "orchestrate" / "coordinate" to the TUI. Verify coordinator system prompt appears in the request (add a debug log or check via provider trace). Run a multi-elf workflow where elf B references elf A's `/tmp` output.
5. **Tests:** `make test` must pass. New packages have unit tests covering `Store`, `Tokenizer`, `QualityTracker`, and the two new tools.
