package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"
	gnomacfg "somegit.dev/Owlibou/gnoma/internal/config"
	"somegit.dev/Owlibou/gnoma/internal/elf"
	"somegit.dev/Owlibou/gnoma/internal/engine"
	"somegit.dev/Owlibou/gnoma/internal/message"
	"somegit.dev/Owlibou/gnoma/internal/permission"
	"somegit.dev/Owlibou/gnoma/internal/router"
	"somegit.dev/Owlibou/gnoma/internal/security"
	"somegit.dev/Owlibou/gnoma/internal/session"
	"somegit.dev/Owlibou/gnoma/internal/skill"
	"somegit.dev/Owlibou/gnoma/internal/stream"
)

// version is set from Config.Version at init; falls back to "dev".
var version = "dev"

type streamEventMsg struct{ event stream.Event }
type turnDoneMsg struct {
	err   error
	usage message.Usage
}

// PermReqMsg carries a permission request from engine to TUI.
type PermReqMsg struct {
	ToolName string
	Args     json.RawMessage
}

type elfProgressMsg struct{ progress elf.Progress }
type modelUpdatedMsg struct{} // sent when background discovery reconciles the model name
type clearQuitHintMsg struct{}
type resumeListLoadedMsg struct{ sessions []session.Metadata }
type shellExitMsg struct{ err error }

type chatMessage struct {
	role    string
	content string
}

// Config holds optional dependencies for TUI features.
type Config struct {
	Firewall              *security.Firewall    // for incognito toggle
	Engine                *engine.Engine        // for model switching
	Permissions           *permission.Checker   // for mode switching
	Router                *router.Router        // for model listing
	ElfManager            *elf.Manager          // for CancelAll on escape/quit
	PermCh                chan bool             // TUI → engine: y/n response
	PermReqCh             <-chan PermReqMsg     // engine → TUI: tool requesting approval
	ElfProgress           <-chan elf.Progress   // elf → TUI: structured progress updates
	SessionStore          *session.SessionStore // nil = no persistence
	StartWithResumePicker bool                  // open session picker on launch
	Skills                *skill.Registry       // nil = no skills loaded
	PluginInfos           []PluginInfo          // discovered plugins for /plugins command
	Version               string                // build version string (from ldflags)
	ModelUpdateCh         <-chan struct{}       // signals when the model name changes (discovery reconciliation)
	SLM                   SLMInfo               // SLM backend status for the status bar
	Profile               ProfileInfo           // active profile state for status bar + /profile command
	ProfileNames          []string              // available profile names (sorted) for /profile autocomplete
	SwitchProfile         func(name string)     // optional: when set, /profile <name> calls this and returns tea.Quit
}

// ProfileInfo mirrors the resolved profile state so the TUI can render
// the active profile in the status bar and answer `/profile`. Zero value
// (Active=false) renders nothing.
type ProfileInfo struct {
	Active bool
	Name   string
}

// SLMInfo captures the resolved SLM backend state at startup so the TUI can
// surface it in the status bar. Zero value (Enabled=false) renders nothing.
type SLMInfo struct {
	Enabled bool
	Active  bool   // true when StartBackend returned a usable Boot
	Backend string // resolved backend name: "ollama", "llamafile", etc.
	Model   string // model identifier
	Tools   bool   // whether the model advertises tool support
}

// PluginInfo is a summary of an installed plugin for TUI display.
type PluginInfo struct {
	Name    string
	Version string
	Scope   string
	Enabled bool
}

type Model struct {
	session session.Session
	config  Config
	width   int
	height  int

	messages    []chatMessage
	streaming   bool
	streamBuf   *strings.Builder // regular text content (assistant role)
	thinkingBuf *strings.Builder // reasoning/thinking content (frozen once text starts)
	currentRole string

	input           textarea.Model
	suggestion      string     // ghost-text completion (dimmed, accepted with Tab)
	completionSrc   []cmdEntry // sorted slash commands for completion
	suggestions     []cmdEntry // live dropdown matches for current input
	suggIdx         int        // selected index in dropdown
	inputMode       string     // "", "command" (/), "execute" (!)
	mdRenderer      *glamour.TermRenderer
	mdRendererWidth int                      // cached width to avoid recreating on same-width resizes
	expandOutput    bool                     // ctrl+o toggles expanded tool output
	elfStates       map[string]*elf.Progress // active elf states keyed by ID
	elfOrder        []string                 // insertion-ordered elf IDs for tree rendering
	elfToolActive   bool                     // suppresses next toolresult (elf output)
	cwd             string
	gitBranch       string
	scrollOffset    int
	incognito       bool
	copyMode        bool            // ctrl+] toggles mouse passthrough for terminal text selection
	lastCtrlC       time.Time       // tracks first ctrl+c for double-press detection
	quitHint        bool            // show "ctrl+c to quit" indicator in status bar
	permPending     bool            // waiting for user to approve/deny a tool
	permToolName    string          // which tool is asking
	permArgs        json.RawMessage // tool args for display

	// Settings panel (/config)
	configPanelOpen bool
	configSelected  int

	// Session resume picker
	resumePending     bool
	resumeSessions    []session.Metadata
	resumeSelected    int
	clearPending      bool     // waiting for y/n confirmation on /clear
	modelSnapshot     []string // snapshot of arm IDs from last /model display
	initPending       bool     // true while /init turn is in-flight; triggers AGENTS.md reload on turnDone
	initHadToolCalls  bool     // set when any tool call fires during an init turn
	initRetried       bool     // set after first retry (no-tool-call case) so we don't retry indefinitely
	initWriteNudged   bool     // set after write nudge (spawn_elfs-ran-but-no-fs_write case)
	streamFilterClose string   // non-empty while suppressing a model pseudo-block; value is expected close tag
	runningTools      []string // transient: tool names currently executing (rendered ephemerally, not in chat history)
}

func New(sess session.Session, cfg Config) Model {
	if cfg.Version != "" {
		version = cfg.Version
	}
	ti := textarea.New()
	ti.Placeholder = "Type a message... (Enter to send, Shift+Enter for newline)"
	ti.ShowLineNumbers = false
	ti.DynamicHeight = true
	ti.MinHeight = 2
	ti.MaxHeight = 10
	ti.SetWidth(80)
	ti.CharLimit = 0

	// Prompt only on first line, empty continuation
	ti.SetPromptFunc(2, func(info textarea.PromptInfo) string {
		if info.LineNumber == 0 {
			return "❯ "
		}
		return "  "
	})

	// Remap: Shift+Enter/Ctrl+J for newline (not plain Enter)
	km := ti.KeyMap
	km.InsertNewline = key.NewBinding(key.WithKeys("shift+enter", "ctrl+j"))
	ti.KeyMap = km

	// Remove the highlighted cursor-line background so the input blends with the UI.
	tiStyles := ti.Styles()
	tiStyles.Focused.CursorLine = lipgloss.NewStyle()
	ti.SetStyles(tiStyles)

	ti.Focus()

	cwd, _ := os.Getwd()
	gitBranch := detectGitBranch()

	// Markdown renderer for chat output (74 = 80 - 6 for "◆ "/"  " prefix)
	mdRenderer, _ := glamour.NewTermRenderer(
		glamour.WithStandardStyle("dark"),
		glamour.WithWordWrap(74),
	)

	// Seed incognito state from the firewall so a launch with
	// --incognito starts the TUI with the badge ON, and Ctrl+X first-
	// press correctly toggles OFF (audit finding W2-3).
	var initialIncognito bool
	if cfg.Firewall != nil {
		initialIncognito = cfg.Firewall.Incognito().Active()
	}

	return Model{
		session:       sess,
		config:        cfg,
		input:         ti,
		incognito:     initialIncognito,
		completionSrc: completionSource(cfg.Skills),
		mdRenderer:    mdRenderer,
		elfStates:     make(map[string]*elf.Progress),
		cwd:           cwd,
		gitBranch:     gitBranch,
		streamBuf:     &strings.Builder{},
		thinkingBuf:   &strings.Builder{},
	}
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.input.Focus()}
	if m.config.StartWithResumePicker && m.config.SessionStore != nil {
		store := m.config.SessionStore
		cmds = append(cmds, func() tea.Msg {
			sessions, err := store.List()
			if err != nil || len(sessions) == 0 {
				return nil
			}
			return resumeListLoadedMsg{sessions: sessions}
		})
	}
	if m.config.ModelUpdateCh != nil {
		cmds = append(cmds, m.listenForModelUpdate())
	}
	return tea.Batch(cmds...)
}

func (m Model) listenForModelUpdate() tea.Cmd {
	ch := m.config.ModelUpdateCh
	return func() tea.Msg {
		_, ok := <-ch
		if !ok {
			return nil
		}
		return modelUpdatedMsg{}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.input.SetWidth(m.width - 4)
		// Only recreate markdown renderer when width actually changes.
		wrapWidth := m.width - 6
		if wrapWidth != m.mdRendererWidth {
			m.mdRendererWidth = wrapWidth
			m.mdRenderer, _ = glamour.NewTermRenderer(
				glamour.WithStandardStyle("dark"),
				glamour.WithWordWrap(wrapWidth),
			)
		}
		return m, nil

	case tea.KeyMsg:
		// --- Global keys: work in ALL states ---

		// Escape = global stop, never quits
		if msg.String() == "escape" {
			if m.inputMode != "" {
				m.inputMode = ""
				m.suggestions = nil
				m.suggestion = ""
				m.suggIdx = 0
				m.input.SetValue("")
				m.input.SetPromptFunc(2, func(info textarea.PromptInfo) string {
					if info.LineNumber == 0 {
						return "❯ "
					}
					return "  "
				})
				return m, nil
			}
			if m.resumePending {
				m.resumePending = false
				m.resumeSessions = nil
				m.resumeSelected = 0
				return m, nil
			}
			if m.permPending {
				m.permPending = false
				m.messages = append(m.messages, chatMessage{role: "system",
					content: fmt.Sprintf("✗ %s denied (cancelled)", m.permToolName)})
				m.config.PermCh <- false
			}
			if m.streaming {
				m.session.Cancel()
				if m.config.ElfManager != nil {
					m.config.ElfManager.CancelAll()
				}
				m.streaming = false
				m.messages = append(m.messages, chatMessage{role: "system",
					content: "⏹ stopped"})
			}
			m.scrollOffset = 0
			return m, nil
		}

		// Ctrl+C = clear input (single) or quit (double within 2s)
		if msg.String() == "ctrl+c" {
			now := time.Now()
			if m.quitHint && now.Sub(m.lastCtrlC) < 2*time.Second {
				// Second press within window → clean shutdown
				if m.permPending {
					m.permPending = false
					m.config.PermCh <- false
				}
				if m.streaming {
					m.session.Cancel()
				}
				if m.config.ElfManager != nil {
					m.config.ElfManager.CancelAll()
				}
				return m, tea.Quit
			}
			// First press → clear input, reset mode, show hint, start expiry timer
			m.input.SetValue("")
			if m.inputMode != "" {
				m.inputMode = ""
				m.suggestions = nil
				m.suggestion = ""
				m.suggIdx = 0
				m.input.SetPromptFunc(2, func(info textarea.PromptInfo) string {
					if info.LineNumber == 0 {
						return "❯ "
					}
					return "  "
				})
			}
			m.lastCtrlC = now
			m.quitHint = true
			return m, tea.Tick(2*time.Second, func(time.Time) tea.Msg {
				return clearQuitHintMsg{}
			})
		}

		// --- Clear confirmation Y/N ---
		if m.clearPending {
			switch strings.ToLower(msg.String()) {
			case "y":
				m.clearPending = false
				m.messages = nil
				m.scrollOffset = 0
				if m.config.Engine != nil {
					m.config.Engine.Reset()
				}
			case "n", "escape":
				m.clearPending = false
				m.messages = append(m.messages, chatMessage{role: "system", content: "clear cancelled"})
			}
			return m, nil
		}

		// --- Permission prompt Y/N (only when prompting) ---
		if m.permPending {
			switch strings.ToLower(msg.String()) {
			case "y":
				m.permPending = false
				m.messages = append(m.messages, chatMessage{role: "system",
					content: fmt.Sprintf("✓ %s approved", m.permToolName)})
				m.config.PermCh <- true
				return m, m.listenForEvents() // continue listening
			case "n":
				m.permPending = false
				m.messages = append(m.messages, chatMessage{role: "system",
					content: fmt.Sprintf("✗ %s denied", m.permToolName)})
				m.config.PermCh <- false
				return m, m.listenForEvents() // continue listening
			}
			return m, nil // ignore other keys while prompting
		}

		// --- Settings panel (when /config is open) ---
		if m.configPanelOpen {
			const numSettings = 3
			switch msg.String() {
			case "up", "k":
				if m.configSelected > 0 {
					m.configSelected--
				}
			case "down", "j":
				if m.configSelected < numSettings-1 {
					m.configSelected++
				}
			case "enter":
				m = m.applyConfigSetting()
			case "esc", "ctrl+c", "q":
				m.configPanelOpen = false
			}
			return m, nil
		}

		// --- Session picker (only when resume picker is open) ---
		if m.resumePending {
			switch msg.String() {
			case "up", "k":
				if m.resumeSelected > 0 {
					m.resumeSelected--
				}
			case "down", "j":
				if m.resumeSelected < len(m.resumeSessions)-1 {
					m.resumeSelected++
				}
			case "enter":
				return m.confirmResumeSelection()
			}
			return m, nil // swallow all other keys
		}

		// Input mode switching: "/" activates command mode, "!" activates execute mode.
		// Only triggers when input is empty and no mode is active.
		switch msg.String() {
		case "/":
			if m.input.Value() == "" && m.inputMode == "" {
				m.inputMode = "command"
				m.input.SetPromptFunc(2, func(info textarea.PromptInfo) string {
					if info.LineNumber == 0 {
						return "/ "
					}
					return "  "
				})
				m.suggestions = m.completionSrc
				m.suggIdx = 0
				return m, nil
			}
		case "!":
			if m.input.Value() == "" && m.inputMode == "" {
				m.inputMode = "execute"
				m.input.SetPromptFunc(2, func(info textarea.PromptInfo) string {
					if info.LineNumber == 0 {
						return "! "
					}
					return "  "
				})
				m.suggestions = nil
				m.suggIdx = 0
				return m, nil
			}
		case "backspace":
			if m.input.Value() == "" && m.inputMode != "" {
				m.inputMode = ""
				m.suggestions = nil
				m.suggestion = ""
				m.suggIdx = 0
				m.input.SetPromptFunc(2, func(info textarea.PromptInfo) string {
					if info.LineNumber == 0 {
						return "❯ "
					}
					return "  "
				})
				return m, nil
			}
		}

		switch msg.String() {
		case "ctrl+x":
			// Toggle incognito
			newM, statusMsg, refused := m.attemptIncognitoToggle()
			m = newM
			role := "system"
			if refused {
				role = "error"
			}
			m.messages = append(m.messages, chatMessage{role: role, content: statusMsg})
			if !refused {
				m.injectSystemContext(statusMsg)
			}
			m.scrollOffset = 0
			return m, nil
		case "shift+tab":
			// Cycle permission mode: bypass → default → plan → bypass
			if m.config.Permissions != nil {
				mode := m.config.Permissions.Mode()
				var next permission.Mode
				switch mode {
				case permission.ModeBypass:
					next = permission.ModeDefault
				case permission.ModeDefault:
					next = permission.ModePlan
				case permission.ModePlan:
					next = permission.ModeAcceptEdits
				case permission.ModeAcceptEdits:
					next = permission.ModeAuto
				case permission.ModeAuto:
					next = permission.ModeBypass
				default:
					next = permission.ModeBypass
				}
				m.config.Permissions.SetMode(next)
				msg := fmt.Sprintf("permission mode changed to: %s — previous tool denials no longer apply, retry if asked", next)
				m.messages = append(m.messages, chatMessage{role: "system", content: msg})
				m.injectSystemContext(msg)
				m.scrollOffset = 0
			}
			return m, nil
		case "ctrl+o":
			m.expandOutput = !m.expandOutput
			return m, nil
		case "ctrl+]":
			m.copyMode = !m.copyMode
			return m, nil
		case "up":
			if len(m.suggestions) > 0 {
				if m.suggIdx > 0 {
					m.suggIdx--
				}
				return m, nil
			}
		case "down":
			if len(m.suggestions) > 0 {
				if m.suggIdx < len(m.suggestions)-1 {
					m.suggIdx++
				}
				return m, nil
			}
		case "tab":
			if len(m.suggestions) > 0 {
				name := m.suggestions[m.suggIdx].name
				if m.inputMode == "command" && strings.HasPrefix(name, "/") {
					// In command mode the prompt shows "/"; strip it from the value
					name = name[1:]
				}
				m.input.SetValue(name + " ")
				m.input.CursorEnd()
				m.suggestions = nil
				m.suggestion = ""
				return m, nil
			}
			if m.suggestion != "" {
				m.input.SetValue(m.suggestion)
				m.suggestion = ""
				return m, nil
			}
		case "esc":
			if len(m.suggestions) > 0 {
				m.suggestions = nil
				m.suggestion = ""
				return m, nil
			}
		case "pgup", "shift+up":
			m.scrollOffset += 5
			return m, nil
		case "pgdown", "shift+down":
			m.scrollOffset -= 5
			if m.scrollOffset < 0 {
				m.scrollOffset = 0
			}
			return m, nil
		case "end":
			m.scrollOffset = 0 // re-pin to bottom
			return m, nil
		case "home":
			m.scrollOffset += 50 // jump to top (clamped in renderChat)
			return m, nil
		case "enter":
			if m.streaming {
				return m, nil
			}
			// Command mode + open dropdown: Enter selects the highlighted suggestion
			if m.inputMode == "command" && len(m.suggestions) > 0 {
				selected := m.suggestions[m.suggIdx].name // e.g., "/help"
				m.input.SetValue("")
				m.suggestions = nil
				return m.submitInput(selected[1:]) // strip "/" — submitInput will re-add it
			}
			input := strings.TrimSpace(m.input.Value())
			if input == "" {
				// Empty Enter in any mode resets the mode
				if m.inputMode != "" {
					m.inputMode = ""
					m.suggestions = nil
					m.input.SetPromptFunc(2, func(info textarea.PromptInfo) string {
						if info.LineNumber == 0 {
							return "❯ "
						}
						return "  "
					})
				}
				return m, nil
			}
			m.input.SetValue("")
			return m.submitInput(input)
		}

	case tea.MouseWheelMsg:
		switch msg.Button {
		case tea.MouseWheelUp:
			m.scrollOffset += 3
		case tea.MouseWheelDown:
			m.scrollOffset -= 3
			if m.scrollOffset < 0 {
				m.scrollOffset = 0
			}
		}
		return m, nil

	case shellExitMsg:
		if msg.err != nil {
			m.messages = append(m.messages, chatMessage{role: "error",
				content: "shell exited with error: " + msg.err.Error()})
		} else {
			m.messages = append(m.messages, chatMessage{role: "system", content: "shell session ended"})
		}
		return m, nil

	case clearQuitHintMsg:
		m.quitHint = false
		return m, nil

	case resumeListLoadedMsg:
		if len(msg.sessions) > 0 {
			m.resumePending = true
			m.resumeSessions = msg.sessions
			m.resumeSelected = 0
			m.scrollOffset = 0
		}
		return m, nil

	case elfProgressMsg:
		p := msg.progress
		// Keep completed elfs in tree — only cleared on turnDoneMsg
		if _, exists := m.elfStates[p.ElfID]; !exists {
			m.elfOrder = append(m.elfOrder, p.ElfID)
		}
		m.elfStates[p.ElfID] = &p
		return m, m.listenForEvents()

	case PermReqMsg:
		m.permPending = true
		m.permToolName = msg.ToolName
		m.permArgs = msg.Args
		m.scrollOffset = 0
		// Inline notification so the user sees the prompt even if focused on input.
		m.messages = append(m.messages, chatMessage{role: "system",
			content: fmt.Sprintf("⚠ %s requires approval — press y to allow, n to deny", msg.ToolName)})
		return m, nil

	case modelUpdatedMsg:
		// Discovery reconciled the model name — re-render picks up the new
		// value from session.Status(). Re-listen for further updates.
		if m.config.ModelUpdateCh != nil {
			return m, m.listenForModelUpdate()
		}
		return m, nil

	case streamEventMsg:
		return m.handleStreamEvent(msg.event)

	case turnDoneMsg:
		m.streaming = false
		m.scrollOffset = 0
		m.elfStates = make(map[string]*elf.Progress) // clear elf states
		m.elfOrder = nil
		m.runningTools = nil

		// If /init failed with a tool-parse error on a local model, the model can
		// generate text but not valid tool-call JSON. Retry without tools — ask the
		// model to output AGENTS.md as plain markdown text instead.
		if m.initPending && !m.initRetried && msg.err != nil &&
			strings.Contains(msg.err.Error(), "parse tool call") {
			m.initRetried = true
			m.streaming = true
			m.thinkingBuf.Reset()
			m.streamBuf.Reset()
			if m.config.Engine != nil {
				m.config.Engine.Reset()
			}
			m.messages = append(m.messages, chatMessage{
				role:    "system",
				content: "tool-call JSON failed — retrying without tools (text-only fallback)",
			})
			root := gnomacfg.ProjectRoot()
			textPrompt := fmt.Sprintf(`You are creating an AGENTS.md project documentation file for the project at %s.

You have NO tools available. Based on common Go project conventions, generate a useful AGENTS.md skeleton.

Output the complete document as markdown text, starting with a # heading. Include sections for:
- Module path (use the project directory name as a hint)
- Key dependencies (common for a Go TUI/LLM project)
- Build commands (make build/test/lint/cover)
- Code conventions
- Environment variables
- Domain terminology

Mark anything you're unsure about with TODO. Be terse — directive-style bullets, no prose.`, root)
			// Reset session from StateError so it accepts a new Send.
			m.session.ResetError()
			// Send with empty AllowedTools to suppress all tool schemas.
			opts := engine.TurnOptions{AllowedTools: []string{}}
			if err := m.session.SendWithOptions(textPrompt, opts); err != nil {
				m.messages = append(m.messages, chatMessage{role: "error", content: formatError(err)})
				m.streaming = false
				m.initPending = false
			}
			// Mark as write-nudged so the disk-write logic at turnDone catches the output.
			m.initHadToolCalls = true
			m.initWriteNudged = true
			return m, m.listenForEvents()
		}

		// If /init completed with any content but no tool calls, the model described or
		// planned but didn't call spawn_elfs. Retry once with a fresh context and a
		// short direct prompt that's easier for local models to act on.
		if m.initPending && !m.initRetried && !m.initHadToolCalls && msg.err == nil &&
			(m.thinkingBuf.Len() > 0 || m.streamBuf.Len() > 0) {
			m.initRetried = true
			m.streaming = true
			if m.thinkingBuf.Len() > 0 {
				m.messages = append(m.messages, chatMessage{role: "thinking", content: m.thinkingBuf.String()})
				m.thinkingBuf.Reset()
			}
			if m.streamBuf.Len() > 0 {
				m.messages = append(m.messages, chatMessage{role: m.currentRole, content: m.streamBuf.String()})
				m.streamBuf.Reset()
			}
			// Reset engine context so the retry starts fresh — the long initPrompt +
			// thinking response overwhelms local models before they can emit a tool call.
			if m.config.Engine != nil {
				m.config.Engine.Reset()
			}
			nudge := "Call spawn_elfs now. Spawn 3 elfs in parallel: (1) explore project structure, read go.mod/Makefile/existing AI config files; (2) find non-standard Go conventions and idioms; (3) check README/docs for env vars and setup requirements. Then write AGENTS.md using fs.write."
			if retryStatus := m.session.Status(); isLocalProvider(retryStatus.Provider) {
				nudge = "Call fs_ls on the project root now. Then fs_read go.mod and Makefile. Then fs_glob **/*.go to find source files. Finally fs_write AGENTS.md. Do not explain — call the tools."
			}
			if err := m.session.Send(nudge); err != nil {
				m.messages = append(m.messages, chatMessage{role: "error", content: formatError(err)})
				m.streaming = false
				m.initPending = false
			}
			return m, m.listenForEvents()
		}

		// If /init ran spawn_elfs (tool calls happened) but the model then narrated
		// instead of calling fs_write, nudge it to write the file. Keep the elf research
		// in context — that's the whole point. No engine reset here.
		if m.initPending && !m.initWriteNudged && m.initHadToolCalls && msg.err == nil {
			agentsMD := filepath.Join(m.cwd, "AGENTS.md")
			if _, statErr := os.Stat(agentsMD); os.IsNotExist(statErr) {
				m.initWriteNudged = true
				m.streaming = true
				if m.thinkingBuf.Len() > 0 {
					m.messages = append(m.messages, chatMessage{role: "thinking", content: m.thinkingBuf.String()})
					m.thinkingBuf.Reset()
				}
				if m.streamBuf.Len() > 0 {
					m.messages = append(m.messages, chatMessage{role: m.currentRole, content: m.streamBuf.String()})
					m.streamBuf.Reset()
				}
				// Ask the model to output the document as plain text. Local models
				// reliably generate text; they unreliably call tools. The fallback
				// below will write whatever the model outputs to disk.
				writeNudge := "Output the complete AGENTS.md document now as markdown text. Include: project overview, module path, build commands (make build/test/lint/cover), all dependencies, and coding conventions from the elf research. Do not call any tools — output the markdown document directly, starting with a # heading."
				if err := m.session.Send(writeNudge); err != nil {
					m.messages = append(m.messages, chatMessage{role: "error", content: formatError(err)})
					m.streaming = false
					m.initPending = false
				}
				return m, m.listenForEvents()
			}
		}

		// Fallback: the write nudge asked the model to output AGENTS.md as plain
		// text; write whatever it generated directly to disk. streamBuf holds the
		// model's text response from this (the nudge) turn — it hasn't been flushed
		// yet. Use it if substantial; otherwise fall back to the longest assistant
		// message in history (for models that did generate the report earlier).
		if m.initPending && m.initWriteNudged && m.initHadToolCalls && msg.err == nil {
			agentsMD := filepath.Join(m.cwd, "AGENTS.md")
			if _, statErr := os.Stat(agentsMD); os.IsNotExist(statErr) {
				content := extractMarkdownDoc(sanitizeAssistantText(m.streamBuf.String()))
				if len(content) < 300 {
					// streamBuf is thin — model may have put content in an earlier turn
					for _, histMsg := range m.messages {
						clean := extractMarkdownDoc(sanitizeAssistantText(histMsg.content))
						if histMsg.role == "assistant" && len(clean) > len(content) {
							content = clean
						}
					}
				}
				if looksLikeAgentsMD(content) {
					if err := os.WriteFile(agentsMD, []byte(content), 0644); err == nil {
						m.messages = append(m.messages, chatMessage{
							role:    "system",
							content: fmt.Sprintf("• AGENTS.md written to %s (extracted from model output)", agentsMD),
						})
					}
				}
			}
		}

		// Flush any remaining thinking then text content
		hadOutput := false
		if m.thinkingBuf.Len() > 0 {
			m.messages = append(m.messages, chatMessage{role: "thinking", content: m.thinkingBuf.String()})
			m.thinkingBuf.Reset()
			hadOutput = true
		}
		if m.streamBuf.Len() > 0 {
			m.messages = append(m.messages, chatMessage{role: m.currentRole, content: m.streamBuf.String()})
			m.streamBuf.Reset()
			hadOutput = true
		}
		if !hadOutput && msg.err == nil && !m.initHadToolCalls {
			// Turn completed with no output at all — model likely doesn't support tools.
			m.messages = append(m.messages, chatMessage{
				role:    "error",
				content: "No output. The model may not support function calling or produced only thinking content. Try a more capable model.",
			})
		}
		if msg.err != nil {
			m.messages = append(m.messages, chatMessage{role: "error", content: formatError(msg.err)})
		}
		if m.initPending {
			m.initPending = false
			if msg.err != nil {
				m = m.loadAgentsMDStale()
			} else {
				m = m.loadAgentsMD()
			}
		}
		// Inline cost: show token usage for this turn
		if msg.usage.TotalTokens() > 0 {
			cost := formatTurnUsage(msg.usage)
			m.messages = append(m.messages, chatMessage{role: "cost", content: cost})
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	cmds = append(cmds, cmd)

	// Update completions based on input mode.
	val := m.input.Value()
	switch m.inputMode {
	case "command":
		// Fuzzy-filter commands by the raw query (no "/" prefix in val)
		m.suggestions = fuzzyMatchCommands(val, m.completionSrc)
		m.suggestion = ""
	case "execute":
		m.suggestions = nil
		m.suggestion = ""
	default:
		// Normal mode: prefix-based ghost text and dropdown
		m.suggestion = matchCompletion(val, m.completionSrc, m.config.ProfileNames)
		m.suggestions = matchSuggestions(val, m.completionSrc)
	}
	if len(m.suggestions) == 0 {
		m.suggIdx = 0
	} else if m.suggIdx >= len(m.suggestions) {
		m.suggIdx = len(m.suggestions) - 1
	}

	return m, tea.Batch(cmds...)
}

func (m Model) submitInput(input string) (tea.Model, tea.Cmd) {
	// Prepend mode prefix and reset mode before dispatching.
	switch m.inputMode {
	case "command":
		if strings.TrimSpace(input) == "" {
			m.inputMode = ""
			m.suggestions = nil
			m.input.SetPromptFunc(2, func(info textarea.PromptInfo) string {
				if info.LineNumber == 0 {
					return "❯ "
				}
				return "  "
			})
			return m, nil
		}
		input = "/" + strings.TrimSpace(input)
	case "execute":
		if strings.TrimSpace(input) == "" {
			m.inputMode = ""
			m.input.SetPromptFunc(2, func(info textarea.PromptInfo) string {
				if info.LineNumber == 0 {
					return "❯ "
				}
				return "  "
			})
			return m, nil
		}
		input = "!" + strings.TrimSpace(input)
	}
	m.inputMode = ""
	m.suggestions = nil
	m.suggestion = ""
	m.suggIdx = 0
	m.input.SetPromptFunc(2, func(info textarea.PromptInfo) string {
		if info.LineNumber == 0 {
			return "❯ "
		}
		return "  "
	})

	if strings.HasPrefix(input, "/") {
		return m.handleCommand(input)
	}
	if strings.HasPrefix(input, "!") {
		return m.handleBangCommand(strings.TrimPrefix(input, "!"))
	}

	m.messages = append(m.messages, chatMessage{role: "user", content: input})
	m.streaming = true
	m.currentRole = "assistant"
	m.streamBuf.Reset()
	m.thinkingBuf.Reset()
	m.streamFilterClose = ""

	if err := m.session.Send(input); err != nil {
		m.messages = append(m.messages, chatMessage{role: "error", content: formatError(err)})
		m.streaming = false
		return m, nil
	}
	return m, m.listenForEvents()
}

// handleBangCommand runs a raw shell command and shows the output inline.
func (m Model) handleBangCommand(cmd string) (tea.Model, tea.Cmd) {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return m, nil
	}
	m.messages = append(m.messages, chatMessage{role: "user", content: "! " + cmd})
	out, err := exec.Command(shellExe(), "-c", cmd).CombinedOutput()
	output := strings.TrimRight(string(out), "\n")
	if err != nil {
		m.messages = append(m.messages, chatMessage{role: "error",
			content: fmt.Sprintf("exit: %v\n%s", err, output)})
	} else {
		if output == "" {
			output = "(no output)"
		}
		m.messages = append(m.messages, chatMessage{role: "toolresult", content: output})
	}
	m.scrollOffset = 0
	return m, nil
}

func (m Model) handleCommand(cmd string) (tea.Model, tea.Cmd) {
	parts := strings.Fields(cmd)
	command := parts[0]
	args := ""
	if len(parts) > 1 {
		args = strings.Join(parts[1:], " ")
	}

	switch command {
	case "/quit", "/exit", "/q":
		return m, tea.Quit

	case "/undo":
		// Pop messages until we remove the last assistant turn.
		if len(m.messages) == 0 {
			m.messages = append(m.messages, chatMessage{role: "system", content: "nothing to undo"})
			return m, nil
		}
		// Walk backward: remove everything until we've removed an assistant message
		// and hit a user message (or start of history).
		removedAssistant := false
		for len(m.messages) > 0 {
			last := m.messages[len(m.messages)-1]
			m.messages = m.messages[:len(m.messages)-1]
			if last.role == "assistant" {
				removedAssistant = true
			}
			if removedAssistant && (len(m.messages) == 0 || m.messages[len(m.messages)-1].role == "user") {
				break
			}
		}
		m.scrollOffset = 0
		m.messages = append(m.messages, chatMessage{role: "system", content: "last turn undone"})
		return m, nil

	case "/clear", "/new":
		// Confirm if session has >5 turns.
		turnCount := 0
		for _, msg := range m.messages {
			if msg.role == "user" {
				turnCount++
			}
		}
		if turnCount > 5 && !m.clearPending {
			m.clearPending = true
			m.messages = append(m.messages, chatMessage{role: "system",
				content: fmt.Sprintf("clear %d turns of history? press y to confirm, n to cancel", turnCount)})
			return m, nil
		}
		m.clearPending = false
		m.messages = nil
		m.scrollOffset = 0
		if m.config.Engine != nil {
			m.config.Engine.Reset()
		}
		return m, nil

	case "/compact":
		if m.config.Engine != nil {
			if w := m.config.Engine.ContextWindow(); w != nil {
				before := w.Tracker().Used()
				compacted, err := w.ForceCompact()
				if err != nil {
					m.messages = append(m.messages, chatMessage{role: "error", content: "compaction failed: " + formatError(err)})
				} else if compacted {
					after := w.Tracker().Used()
					msg := fmt.Sprintf("context compacted — %dk → %dk tokens (saved %dk)",
						before/1000, after/1000, (before-after)/1000)
					m.messages = append(m.messages, chatMessage{role: "system", content: msg})
				} else {
					m.messages = append(m.messages, chatMessage{role: "system", content: "no compaction strategy configured"})
				}
			}
		}
		return m, nil

	case "/incognito":
		newM, statusMsg, refused := m.attemptIncognitoToggle()
		m = newM
		role := "system"
		if refused {
			role = "error"
		}
		m.messages = append(m.messages, chatMessage{role: role, content: statusMsg})
		return m, nil

	case "/model":
		if args == "" {
			status := m.session.Status()
			var b strings.Builder
			fmt.Fprintf(&b, "current: %s/%s\n", status.Provider, status.Model)
			if m.config.Router != nil {
				arms := m.config.Router.Arms()
				sort.Slice(arms, func(i, j int) bool {
					return string(arms[i].ID) < string(arms[j].ID)
				})
				// Snapshot model names so /model <n> references this exact ordering.
				m.modelSnapshot = m.modelSnapshot[:0]
				b.WriteString("\nAvailable models:\n")
				for i, arm := range arms {
					m.modelSnapshot = append(m.modelSnapshot, arm.ModelName)
					marker := "  "
					if string(arm.ID) == status.Provider+"/"+status.Model {
						marker = "→ "
					}
					var caps []string
					if arm.Capabilities.ToolUse {
						caps = append(caps, "tools")
					}
					if arm.Capabilities.SupportsThinking() {
						caps = append(caps, "thinking")
					}
					if arm.Capabilities.Vision {
						caps = append(caps, "vision")
					}
					local := ""
					if arm.IsLocal {
						local = " (local)"
					}
					capStr := ""
					if len(caps) > 0 {
						capStr = " [" + strings.Join(caps, ", ") + "]"
					}
					fmt.Fprintf(&b, "%s%d. %s%s%s\n", marker, i+1, arm.ID, capStr, local)
				}
			}
			b.WriteString("\nUsage: /model <name-or-number>")
			m.messages = append(m.messages, chatMessage{role: "system", content: b.String()})
			return m, nil
		}
		if m.config.Engine != nil {
			modelName := args
			// Support numeric selection: /model 3 — uses snapshot from last /model listing.
			if n, err := strconv.Atoi(args); err == nil && n >= 1 {
				if n <= len(m.modelSnapshot) {
					modelName = m.modelSnapshot[n-1]
				} else {
					m.messages = append(m.messages, chatMessage{role: "error",
						content: fmt.Sprintf("no model at index %d — use /model to list available models", n)})
					return m, nil
				}
			}
			// Validate name-based selection against known arms
			if m.config.Router != nil && !isKnownModel(m.config.Router.Arms(), modelName) {
				m.messages = append(m.messages, chatMessage{role: "error",
					content: fmt.Sprintf("unknown model: %q — use /model to list available models", modelName)})
				return m, nil
			}
			m.config.Engine.SetModel(modelName)
			if ls, ok := m.session.(*session.Local); ok {
				ls.SetModel(modelName)
			}
			m.messages = append(m.messages, chatMessage{role: "system",
				content: fmt.Sprintf("model switched to: %s", modelName)})
		}
		return m, nil

	case "/config":
		// /config set <key> <value> — direct write, no panel
		if strings.HasPrefix(args, "set ") {
			parts := strings.SplitN(strings.TrimPrefix(args, "set "), " ", 2)
			if len(parts) != 2 {
				m.messages = append(m.messages, chatMessage{role: "error",
					content: "Usage: /config set <key> <value>\nKeys: provider.default, provider.model, permission.mode"})
				return m, nil
			}
			if err := gnomacfg.SetProjectConfig(parts[0], parts[1]); err != nil {
				m.messages = append(m.messages, chatMessage{role: "error", content: formatError(err)})
			} else {
				m.messages = append(m.messages, chatMessage{role: "system",
					content: fmt.Sprintf("config set: %s = %s (saved to .gnoma/config.toml)", parts[0], parts[1])})
			}
			return m, nil
		}
		// No args — open interactive settings panel
		m.configPanelOpen = true
		m.configSelected = 0
		return m, nil

	case "/elf", "/elfs":
		if args == "" {
			m.messages = append(m.messages, chatMessage{role: "system",
				content: "Elfs are spawned by the LLM via the 'agent' tool.\nAsk the model to use sub-agents for parallel tasks.\n\nExample: \"Research these 3 files in parallel using sub-agents\""})
		}
		return m, nil

	case "/shell":
		shell := shellExe()
		var cmd *exec.Cmd
		if args != "" {
			cmd = exec.Command(shell, "-c", args)
		} else {
			cmd = exec.Command(shell)
		}
		return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
			return shellExitMsg{err: err}
		})

	case "/permission", "/perm":
		if m.config.Permissions == nil {
			m.messages = append(m.messages, chatMessage{role: "error", content: "permission checker not configured"})
			return m, nil
		}
		if args == "" {
			m.messages = append(m.messages, chatMessage{role: "system",
				content: fmt.Sprintf("permission mode: %s\nUsage: /permission <mode> (bypass, default, plan, accept_edits, deny, auto)\nOr press Shift+Tab to cycle", m.config.Permissions.Mode())})
			return m, nil
		}
		mode := permission.Mode(args)
		if !mode.Valid() {
			m.messages = append(m.messages, chatMessage{role: "error",
				content: fmt.Sprintf("invalid mode: %s (valid: bypass, default, plan, accept_edits, deny, auto)", args)})
			return m, nil
		}
		m.config.Permissions.SetMode(mode)
		msg := fmt.Sprintf("permission mode changed to: %s — previous tool denials no longer apply, retry if asked", mode)
		m.messages = append(m.messages, chatMessage{role: "system", content: msg})
		m.injectSystemContext(msg)
		return m, nil

	case "/profile":
		return m.handleProfileCommand(args)

	case "/provider":
		if args != "" {
			m.messages = append(m.messages, chatMessage{role: "system",
				content: fmt.Sprintf("provider switching requires restart: gnoma --provider %s", args)})
			return m, nil
		}
		status := m.session.Status()
		var b strings.Builder
		b.WriteString(fmt.Sprintf("Active: %s/%s\n", status.Provider, status.Model))
		if m.config.Router != nil {
			arms := m.config.Router.Arms()
			if len(arms) > 0 {
				// Group arms by provider prefix
				providers := make(map[string][]string)
				for _, arm := range arms {
					parts := strings.SplitN(string(arm.ID), "/", 2)
					prov := parts[0]
					model := string(arm.ID)
					if len(parts) == 2 {
						model = parts[1]
					}
					tag := ""
					if arm.IsLocal {
						tag = " (local)"
					}
					providers[prov] = append(providers[prov], model+tag)
				}
				b.WriteString("\nRegistered arms:\n")
				for prov, models := range providers {
					b.WriteString(fmt.Sprintf("  %s:\n", prov))
					for _, model := range models {
						b.WriteString(fmt.Sprintf("    - %s\n", model))
					}
				}
			}
		}
		b.WriteString("\nTo switch: gnoma --provider <name>")
		m.messages = append(m.messages, chatMessage{role: "system", content: b.String()})
		return m, nil

	case "/init":
		root := gnomacfg.ProjectRoot()
		agentsPath := filepath.Join(root, "AGENTS.md")
		var existingPath string
		if _, err := os.Stat(agentsPath); err == nil {
			existingPath = agentsPath
		}

		status := m.session.Status()
		local := isLocalProvider(status.Provider)

		var prompt string
		if m.config.Skills != nil {
			if sk := m.config.Skills.Get("init"); sk != nil {
				rendered, err := sk.Render(skill.TemplateData{
					Args:        existingPath,
					ProjectRoot: root,
					Cwd:         m.cwd,
					Local:       local,
				})
				if err == nil {
					prompt = rendered
				}
			}
		}
		// Fallback to hardcoded prompts if skill not found.
		if prompt == "" {
			if local {
				prompt = localInitPrompt(root, existingPath)
			} else {
				prompt = initPrompt(root, existingPath)
			}
		}

		m.messages = append(m.messages, chatMessage{role: "user", content: "/init"})

		m.streaming = true
		m.currentRole = "assistant"
		m.streamBuf.Reset()
		m.thinkingBuf.Reset()
		m.streamFilterClose = ""
		m.initPending = true
		m.initHadToolCalls = false
		m.initRetried = false
		m.initWriteNudged = false

		opts := engine.TurnOptions{}
		if err := m.session.SendWithOptions(prompt, opts); err != nil {
			m.messages = append(m.messages, chatMessage{role: "error", content: formatError(err)})
			m.streaming = false
			m.initPending = false
			return m, nil
		}
		return m, m.listenForEvents()

	case "/replay":
		if len(m.messages) == 0 {
			m.messages = append(m.messages, chatMessage{role: "system", content: "nothing to replay"})
			return m, nil
		}
		// Count total rendered lines to scroll to top
		total := 0
		for _, msg := range m.messages {
			total += len(m.renderMessage(msg))
		}
		m.scrollOffset = total
		m.messages = append(m.messages, chatMessage{role: "system",
			content: fmt.Sprintf("replaying %d messages — scroll down or press End to return", len(m.messages))})
		return m, nil

	case "/resume":
		if m.config.SessionStore == nil {
			m.messages = append(m.messages, chatMessage{role: "system", content: "session persistence is not configured"})
			return m, nil
		}
		if args != "" {
			snap, loadErr := m.config.SessionStore.Load(args)
			if loadErr == nil {
				return m.applySessionSnapshot(snap)
			}
			m.messages = append(m.messages, chatMessage{role: "system",
				content: fmt.Sprintf("session %q not found", args)})
		}
		sessions, err := m.config.SessionStore.List()
		if err != nil {
			m.messages = append(m.messages, chatMessage{role: "error", content: "failed to list sessions: " + err.Error()})
			return m, nil
		}
		if len(sessions) == 0 {
			m.messages = append(m.messages, chatMessage{role: "system", content: "no saved sessions"})
			return m, nil
		}
		m.resumePending = true
		m.resumeSessions = sessions
		m.resumeSelected = 0
		m.scrollOffset = 0
		return m, nil

	case "/help":
		m.messages = append(m.messages, chatMessage{role: "system",
			content: "Commands:\n  /init               generate or update AGENTS.md project docs\n  /clear, /new        clear chat and start new conversation\n  /config             show current config\n  /incognito          toggle incognito (Ctrl+X)\n  /keys               show keyboard shortcuts\n  /model [name]       list/switch models\n  /permission [mode]  set permission mode (Shift+Tab to cycle)\n  /plugins            list installed plugins\n  /profile [name]     list profiles / switch (re-execs gnoma)\n  /provider           show current provider\n  /replay             scroll to top to re-read conversation\n  /resume [id]        list or restore saved sessions\n  /shell [cmd]        open interactive shell (or run cmd in shell)\n  /skills             list loaded skills\n  /usage              show token usage and cost\n  /help               show this help\n  /quit               exit gnoma\n\nSkills (use /<name> [args] to invoke):\n  Add .md files with YAML front matter to .gnoma/skills/ or ~/.config/gnoma/skills/"})
		return m, nil

	case "/keys":
		m.messages = append(m.messages, chatMessage{role: "system",
			content: "Keyboard shortcuts:\n" +
				"  Enter           send message\n" +
				"  Shift+Enter     newline in input\n" +
				"  Tab             accept completion\n" +
				"  Ctrl+C          cancel stream / quit (press twice)\n" +
				"  Ctrl+X          toggle incognito mode\n" +
				"  Shift+Tab       cycle permission mode\n" +
				"  ↑/↓             scroll chat history\n" +
				"  PgUp/PgDn       scroll one page\n" +
				"  Home            jump up 50 lines\n" +
				"  End             scroll to bottom\n" +
				"  Ctrl+Y          toggle copy mode (disables mouse)\n" +
				"  y/n             approve/deny permission prompts"})
		return m, nil

	case "/plugins":
		if len(m.config.PluginInfos) == 0 {
			m.messages = append(m.messages, chatMessage{role: "system", content: "No plugins installed."})
			return m, nil
		}
		var b strings.Builder
		b.WriteString("Installed plugins:\n")
		for _, p := range m.config.PluginInfos {
			status := "enabled"
			if !p.Enabled {
				status = "disabled"
			}
			b.WriteString(fmt.Sprintf("  %s v%s [%s] (%s)\n", p.Name, p.Version, p.Scope, status))
		}
		m.messages = append(m.messages, chatMessage{role: "system", content: b.String()})
		return m, nil

	case "/skills":
		if m.config.Skills == nil || len(m.config.Skills.Names()) == 0 {
			m.messages = append(m.messages, chatMessage{role: "system", content: "No skills loaded."})
			return m, nil
		}
		var b strings.Builder
		b.WriteString("Loaded skills:\n")
		for _, sk := range m.config.Skills.All() {
			b.WriteString(fmt.Sprintf("  /%s", sk.Frontmatter.Name))
			if sk.Frontmatter.Description != "" {
				b.WriteString(fmt.Sprintf(" — %s", sk.Frontmatter.Description))
			}
			b.WriteString(fmt.Sprintf(" [%s]\n", sk.Source))
		}
		m.messages = append(m.messages, chatMessage{role: "system", content: b.String()})
		return m, nil

	case "/usage":
		var b strings.Builder
		b.WriteString("Session usage:\n")
		if m.config.Engine != nil {
			u := m.config.Engine.Usage()
			b.WriteString(fmt.Sprintf("  Input tokens:  %d\n", u.InputTokens))
			b.WriteString(fmt.Sprintf("  Output tokens: %d\n", u.OutputTokens))
			b.WriteString(fmt.Sprintf("  Total tokens:  %d\n", u.TotalTokens()))
			if u.CacheReadTokens > 0 {
				b.WriteString(fmt.Sprintf("  Cache reads:   %d\n", u.CacheReadTokens))
			}
			if w := m.config.Engine.ContextWindow(); w != nil {
				tr := w.Tracker()
				pct := float64(0)
				if tr.MaxTokens() > 0 {
					pct = float64(tr.Used()) / float64(tr.MaxTokens()) * 100
				}
				b.WriteString(fmt.Sprintf("  Context:       %dk / %dk (%.0f%%)\n", tr.Used()/1000, tr.MaxTokens()/1000, pct))
			}
		}
		status := m.session.Status()
		b.WriteString(fmt.Sprintf("  Provider:      %s/%s\n", status.Provider, status.Model))
		b.WriteString(fmt.Sprintf("  Turns:         %d\n", status.TurnCount))
		m.messages = append(m.messages, chatMessage{role: "system", content: b.String()})
		return m, nil

	default:
		// Check skill registry before returning unknown command error.
		if m.config.Skills != nil {
			sk := m.config.Skills.Get(command[1:]) // strip leading /
			if sk != nil {
				args := strings.Join(parts[1:], " ")
				rendered, err := sk.Render(skill.TemplateData{
					Args:        args,
					Cwd:         m.cwd,
					ProjectRoot: gnomacfg.ProjectRoot(),
					Local:       isLocalProvider(m.session.Status().Provider),
				})
				if err != nil {
					m.messages = append(m.messages, chatMessage{role: "error",
						content: fmt.Sprintf("skill %q: %v", sk.Frontmatter.Name, err)})
					return m, nil
				}
				// Display the invocation in chat, then submit the rendered prompt.
				display := command
				if args != "" {
					display += " " + args
				}
				m.messages = append(m.messages, chatMessage{role: "user", content: display})
				m.streaming = true
				m.currentRole = "assistant"
				m.streamBuf.Reset()
				m.thinkingBuf.Reset()
				m.streamFilterClose = ""
				skillOpts := engine.TurnOptions{
					AllowedTools: sk.Frontmatter.AllowedTools,
					AllowedPaths: sk.Frontmatter.Paths,
				}
				if err := m.session.SendWithOptions(rendered, skillOpts); err != nil {
					m.messages = append(m.messages, chatMessage{role: "error", content: formatError(err)})
					m.streaming = false
					return m, nil
				}
				return m, m.listenForEvents()
			}
		}
		m.messages = append(m.messages, chatMessage{role: "error",
			content: fmt.Sprintf("unknown command: %s (try /help)", command)})
		return m, nil
	}
}

// View, renderChat, renderMessage, renderElfTree, renderSeparators, renderInput,
// renderStatus, renderContextBar, formatTokens, formatTurnUsage, wrapText, shortCwd
// are in rendering.go.

// isLocalProvider returns true for providers that run locally (Ollama, llama.cpp).
// These often require tool_choice: required to emit function call JSON.
func isLocalProvider(providerName string) bool {
	return providerName == "ollama" || providerName == "llamacpp"
}

// confirmResumeSelection loads the currently highlighted session and restores it.
func (m Model) confirmResumeSelection() (tea.Model, tea.Cmd) {
	if m.resumeSelected < 0 || m.resumeSelected >= len(m.resumeSessions) {
		m.resumePending = false
		return m, nil
	}
	selected := m.resumeSessions[m.resumeSelected]
	m.resumePending = false
	m.resumeSessions = nil
	m.resumeSelected = 0
	snap, err := m.config.SessionStore.Load(selected.ID)
	if err != nil {
		m.messages = append(m.messages, chatMessage{role: "error",
			content: fmt.Sprintf("failed to load session %q: %v", selected.ID, err)})
		return m, nil
	}
	return m.applySessionSnapshot(snap)
}

// applySessionSnapshot restores engine state from a snapshot and rebuilds the display history.
func (m Model) applySessionSnapshot(snap session.Snapshot) (tea.Model, tea.Cmd) {
	if m.config.Engine != nil {
		m.config.Engine.SetHistory(snap.Messages)
		m.config.Engine.SetUsage(snap.Metadata.Usage)
	}
	m.messages = nil
	for _, msg := range snap.Messages {
		for _, c := range msg.Content {
			switch c.Type {
			case message.ContentText:
				if c.Text != "" {
					m.messages = append(m.messages, chatMessage{
						role:    string(msg.Role),
						content: c.Text,
					})
				}
			case message.ContentThinking:
				if c.Thinking != nil && c.Thinking.Text != "" {
					m.messages = append(m.messages, chatMessage{
						role:    "thinking",
						content: c.Thinking.Text,
					})
				}
			case message.ContentToolResult:
				if c.ToolResult != nil && c.ToolResult.Content != "" {
					m.messages = append(m.messages, chatMessage{
						role:    "toolresult",
						content: c.ToolResult.Content,
					})
				}
			}
		}
	}
	m.messages = append(m.messages, chatMessage{role: "system",
		content: fmt.Sprintf("Session %s resumed (%d turns, %s/%s)",
			snap.ID, snap.Metadata.TurnCount, snap.Metadata.Provider, snap.Metadata.Model)})
	m.scrollOffset = 0
	return m, nil
}

// reModelCodeBlock matches <<tool_code>>…<</tool_code>> blocks that some models
// (e.g. Gemma4) emit as plain text instead of structured function calls.
var reModelCodeBlock = regexp.MustCompile(`(?s)(<<[/]?tool_code>>.*?<<[/]tool_code>>|<<function_call>>.*?<tool_call\|>)`)

// sanitizeAssistantText removes model-specific artifacts (e.g. <<tool_code>> blocks)
// before rendering or writing to disk.
func sanitizeAssistantText(s string) string {
	s = reModelCodeBlock.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

// filterModelCodeBlocks filters <<tool_code>> ... <</tool_code>> spans from a streaming
// text delta, updating the active filter state across chunk boundaries.
// Returns the text that should be written to the stream buffer (may be empty).
// modelBlockPairs lists known open→close tag pairs for model pseudo-tool-call formats.
// Checked in order; first match wins.
var modelBlockPairs = [][2]string{
	{"<<tool_code>>", "<</tool_code>>"},
	{"<<tool_code>>", "<<</tool_code>>"},  // some model variants
	{"<<function_call>>", "<tool_call|>"}, // Gemma function-call format
}

// filterModelCodeBlocks suppresses model-internal pseudo-tool-call blocks from a
// streaming text delta. closeTag must point to the Model's streamFilterClose field;
// it is non-empty while the filter is active and holds the expected closing tag.
// Returns only the text that should be written to streamBuf.
func filterModelCodeBlocks(closeTag *string, text string) string {
	var out strings.Builder

	for text != "" {
		if *closeTag != "" {
			// Inside a filtered block — scan for the expected close tag.
			if idx := strings.Index(text, *closeTag); idx >= 0 {
				text = text[idx+len(*closeTag):]
				*closeTag = ""
			} else {
				return out.String() // close tag not yet arrived, discard rest
			}
		} else {
			// Not filtering — scan for any known open tag.
			earliest := -1
			var openLen int
			var chosenClose string
			for _, pair := range modelBlockPairs {
				idx := strings.Index(text, pair[0])
				if idx >= 0 && (earliest < 0 || idx < earliest) {
					earliest = idx
					openLen = len(pair[0])
					chosenClose = pair[1]
				}
			}
			if earliest < 0 {
				out.WriteString(text)
				return out.String()
			}
			out.WriteString(text[:earliest])
			*closeTag = chosenClose
			text = text[earliest+openLen:]
		}
	}

	return out.String()
}

// injectSystemContext adds context visible to the model without polluting the
// persisted conversation history. Uses the context window prefix when available,
// falls back to direct history injection.
func (m Model) injectSystemContext(text string) {
	if m.config.Engine == nil {
		return
	}
	if w := m.config.Engine.ContextWindow(); w != nil {
		w.AddPrefix(
			message.NewUserText("[system] "+text),
			message.NewAssistantText("Understood."),
		)
		return
	}
	// Fallback for engines without context window (e.g. tests)
	m.config.Engine.InjectMessage(message.NewUserText("[system] " + text))
	m.config.Engine.InjectMessage(message.NewAssistantText("Understood."))
}

// attemptIncognitoToggle flips incognito state subject to the local-only
// constraint: if a non-local arm is currently forced, turning incognito
// ON is refused with an actionable message. Returns the new model, a
// user-facing status string, and whether the toggle was refused.
//
// The firewall (intent) and the router's local-only flag (enforcement)
// are toggled together — they must agree, otherwise the incognito badge
// lies about routing. See plan W2-1.
func (m Model) attemptIncognitoToggle() (Model, string, bool) {
	if m.config.Firewall == nil {
		return m, "firewall not configured", true
	}
	currentlyOn := m.config.Firewall.Incognito().Active()
	if !currentlyOn && m.config.Router != nil {
		if forced := m.config.Router.ForcedArm(); forced != "" {
			if arm, ok := m.config.Router.LookupArm(forced); ok && !arm.IsLocal {
				return m, fmt.Sprintf(
					"⚠ cannot enable incognito: --provider %s is non-local; clear the pin first",
					forced,
				), true
			}
		}
	}
	m.incognito = m.config.Firewall.Incognito().Toggle()
	if m.config.Router != nil {
		m.config.Router.SetLocalOnly(m.incognito)
	}
	var status string
	if m.incognito {
		status = "🔒 incognito ON — no persistence, no learning, local-only routing"
	} else {
		status = "🔓 incognito OFF"
	}
	return m, status, false
}

// updateInputHeight recalculates and sets the textarea viewport height based on
// isKnownModel returns true if modelName matches a ModelName in the provided arms slice.
func isKnownModel(arms []*router.Arm, modelName string) bool {
	for _, arm := range arms {
		if arm.ModelName == modelName {
			return true
		}
	}
	return false
}

// shortPermHint returns a compact string for the separator bar (e.g., "bash: find . -name '*.go'").
func shortPermHint(toolName string, args json.RawMessage) string {
	switch toolName {
	case "bash":
		var a struct{ Command string }
		if json.Unmarshal(args, &a) == nil && a.Command != "" {
			cmd := a.Command
			if len(cmd) > 50 {
				cmd = cmd[:50] + "…"
			}
			return "bash: " + cmd
		}
	case "fs.write", "fs_write":
		var a struct {
			Path string `json:"file_path"`
		}
		if json.Unmarshal(args, &a) == nil && a.Path != "" {
			return "write: " + a.Path
		}
	case "fs.edit", "fs_edit":
		var a struct {
			Path string `json:"file_path"`
		}
		if json.Unmarshal(args, &a) == nil && a.Path != "" {
			return "edit: " + a.Path
		}
	}
	return toolName
}

// formatPermissionPrompt builds a readable prompt showing what the tool wants to do.
func formatPermissionPrompt(toolName string, args json.RawMessage) string {
	var detail string

	switch toolName {
	case "bash":
		var a struct{ Command string }
		if json.Unmarshal(args, &a) == nil && a.Command != "" {
			cmd := a.Command
			if len(cmd) > 120 {
				cmd = cmd[:120] + "…"
			}
			detail = cmd
		}
	case "fs.write", "fs_write":
		var a struct {
			Path    string `json:"file_path"`
			Content string `json:"content"`
		}
		if json.Unmarshal(args, &a) == nil && a.Path != "" {
			detail = a.Path
			if a.Content != "" {
				preview := diffPreviewWrite(a.Content)
				if preview != "" {
					detail += "\n" + preview
				}
			}
		}
	case "fs.edit", "fs_edit":
		var a struct {
			Path      string `json:"file_path"`
			OldString string `json:"old_string"`
			NewString string `json:"new_string"`
		}
		if json.Unmarshal(args, &a) == nil && a.Path != "" {
			detail = a.Path
			if a.OldString != "" || a.NewString != "" {
				preview := diffPreviewEdit(a.OldString, a.NewString)
				if preview != "" {
					detail += "\n" + preview
				}
			}
		}
	default:
		// Generic: try to extract a readable summary from args
		if len(args) > 0 && len(args) < 200 {
			detail = string(args)
		}
	}

	if detail != "" {
		return fmt.Sprintf("⚠ %s wants to execute: %s [y/n]", toolName, detail)
	}
	return fmt.Sprintf("⚠ %s wants to execute [y/n]", toolName)
}

// diffPreviewEdit produces a compact diff preview for fs.edit operations.
func diffPreviewEdit(oldStr, newStr string) string {
	const maxLines = 5
	var b strings.Builder
	for _, line := range strings.SplitN(oldStr, "\n", maxLines+1) {
		if b.Len() > 200 {
			break
		}
		b.WriteString("  - " + line + "\n")
	}
	for _, line := range strings.SplitN(newStr, "\n", maxLines+1) {
		if b.Len() > 400 {
			break
		}
		b.WriteString("  + " + line + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// diffPreviewWrite produces a preview of the first few lines of a write operation.
func diffPreviewWrite(content string) string {
	const maxLines = 5
	lines := strings.SplitN(content, "\n", maxLines+1)
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		lines = append(lines, fmt.Sprintf("  … +%d more lines", strings.Count(content, "\n")-maxLines+1))
	}
	var b strings.Builder
	for _, line := range lines {
		b.WriteString("  + " + line + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// applyConfigSetting applies the Enter action for the currently selected settings item.
func (m Model) applyConfigSetting() Model {
	switch m.configSelected {
	case 0: // Model — cycle through arms
		if m.config.Router == nil {
			return m
		}
		arms := configPanelArms(m.config.Router.Arms())
		if len(arms) == 0 {
			return m
		}
		current := m.config.Router.ForcedArm()
		idx := 0
		for i, a := range arms {
			if a.ID == current {
				idx = (i + 1) % len(arms)
				break
			}
		}
		next := arms[idx]
		m.config.Router.ForceArm(next.ID)
		if m.config.Engine != nil {
			m.config.Engine.SetModel(next.ModelName)
		}
		if ls, ok := m.session.(*session.Local); ok {
			ls.SetModel(next.ModelName)
		}

	case 1: // Permission — cycle modes
		if m.config.Permissions == nil {
			return m
		}
		modes := []permission.Mode{
			permission.ModeAuto,
			permission.ModeDefault,
			permission.ModeAcceptEdits,
			permission.ModePlan,
			permission.ModeBypass,
			permission.ModeDeny,
		}
		cur := m.config.Permissions.Mode()
		next := modes[0]
		for i, md := range modes {
			if md == cur {
				next = modes[(i+1)%len(modes)]
				break
			}
		}
		m.config.Permissions.SetMode(next)

	case 2: // Incognito — toggle (silent; config panel has no status line)
		newM, _, _ := m.attemptIncognitoToggle()
		m = newM
	}
	return m
}

// configPanelArms returns a stable-ordered slice of arms suitable for cycling.
// Order: CLI agents first, then local models, then API arms, excluding stub/SLM.
func configPanelArms(arms []*router.Arm) []*router.Arm {
	var cli, local, api []*router.Arm
	for _, a := range arms {
		if string(a.ID) == "slm/llamafile" {
			continue // SLM is not a user-selectable primary arm
		}
		if a.IsCLIAgent {
			cli = append(cli, a)
		} else if a.IsLocal {
			local = append(local, a)
		} else {
			api = append(api, a)
		}
	}
	result := make([]*router.Arm, 0, len(cli)+len(local)+len(api))
	result = append(result, cli...)
	result = append(result, local...)
	result = append(result, api...)
	return result
}

// shellExe returns the path of the user's preferred interactive shell.
// Priority: $SHELL (Unix) / %COMSPEC% (Windows), then platform default.
func shellExe() string {
	if sh := os.Getenv("SHELL"); sh != "" {
		return sh
	}
	if sh := os.Getenv("COMSPEC"); sh != "" {
		return sh
	}
	if os.PathSeparator == '\\' {
		return "powershell.exe"
	}
	return "/bin/sh"
}

func detectGitBranch() string {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// formatError truncates excessively long error messages to prevent breaking the TUI rendering.
func formatError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if len(msg) > 1000 {
		return msg[:1000] + "\n... [error truncated due to size]"
	}
	return msg
}

// handleProfileCommand implements the /profile slash command.
//
// `/profile` (no args) prints the active profile, the base config path,
// and the list of available profiles. When profile mode isn't engaged,
// it points the user at `gnoma profile list` for setup guidance.
//
// `/profile <name>` validates the name against the cached profile list
// and, when SwitchProfile is wired, signals main to re-exec gnoma with
// `--profile <name>`. The TUI returns tea.Quit so the current process
// can tear down cleanly before the new gnoma takes over. We avoid
// in-process profile swapping because the engine, router, providers,
// and session store would all need coordinated reinitialisation —
// a re-exec is simpler and more correct.
func (m Model) handleProfileCommand(args string) (tea.Model, tea.Cmd) {
	args = strings.TrimSpace(args)

	if args == "" {
		m.messages = append(m.messages, chatMessage{role: "system", content: m.formatProfileSummary()})
		return m, nil
	}

	// Profile mode must be engaged for switching to be meaningful.
	if !m.config.Profile.Active {
		m.messages = append(m.messages, chatMessage{role: "error",
			content: "profile mode is not enabled — see `gnoma profile list` for setup guidance"})
		return m, nil
	}

	name := args
	if !contains(m.config.ProfileNames, name) {
		avail := strings.Join(m.config.ProfileNames, ", ")
		m.messages = append(m.messages, chatMessage{role: "error",
			content: fmt.Sprintf("unknown profile %q — available: %s", name, avail)})
		return m, nil
	}

	if name == m.config.Profile.Name {
		m.messages = append(m.messages, chatMessage{role: "system",
			content: fmt.Sprintf("already using profile %q", name)})
		return m, nil
	}

	if m.config.SwitchProfile == nil {
		m.messages = append(m.messages, chatMessage{role: "error",
			content: "in-process profile switching is not wired in this build; restart with: gnoma --profile " + name})
		return m, nil
	}

	m.messages = append(m.messages, chatMessage{role: "system",
		content: fmt.Sprintf("switching to profile %q — re-executing gnoma...", name)})
	m.config.SwitchProfile(name)
	return m, tea.Quit
}

func (m Model) formatProfileSummary() string {
	var b strings.Builder
	if m.config.Profile.Active {
		fmt.Fprintf(&b, "Active profile: %s\n", m.config.Profile.Name)
	} else {
		b.WriteString("Profile mode: not enabled (legacy single-config)\n")
		b.WriteString("\nTo enable profiles, see `gnoma profile list` or docs/profiles.md.\n")
		return b.String()
	}

	if len(m.config.ProfileNames) == 0 {
		b.WriteString("\n(no other profiles configured)\n")
		return b.String()
	}

	b.WriteString("\nAvailable profiles:\n")
	for _, n := range m.config.ProfileNames {
		marker := "  "
		if n == m.config.Profile.Name {
			marker = "→ "
		}
		fmt.Fprintf(&b, "%s%s\n", marker, n)
	}
	b.WriteString("\nUsage: /profile <name>  — re-execs gnoma with the chosen profile.\n")
	b.WriteString("Conversation history is not preserved across a switch.\n")
	return b.String()
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
