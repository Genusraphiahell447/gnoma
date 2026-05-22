package tui

import (
	"encoding/json"
	"fmt"
	"log/slog"
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
	"github.com/atotto/clipboard"
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
	AppConfig             *gnomacfg.Config      // full application configuration
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
	expandOutput    bool                     // Ctrl+O toggles expanded tool output
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

	// Vim mode preference
	vimMode       bool
	vimNormalMode bool

	// Prompt history
	promptHistory []string
	historyIdx    int

	// Interactive sub-menu overlays
	modelPickerOpen    bool
	profilePickerOpen  bool
	skillsPickerOpen   bool
	pluginsPickerOpen  bool
	helpPickerOpen     bool
	themePickerOpen    bool
	providerPickerOpen bool
	pickerSelected     int // generic index for active picker navigation

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

	// Pasted contents
	pastedTexts  map[string]string
	pastedImages map[string]string
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

	var initialVim bool
	themeName := "catppuccin"
	if cfg.AppConfig != nil {
		if cfg.AppConfig.TUI.Theme != "" {
			themeName = cfg.AppConfig.TUI.Theme
		}
		initialVim = cfg.AppConfig.TUI.Vim
	}
	ApplyTheme(themeName)

	compSrc := completionSource(cfg.Skills)
	if cfg.SLM.Active {
		var filtered []cmdEntry
		for _, entry := range compSrc {
			if entry.name == "/model" || entry.name == "/provider" {
				continue
			}
			filtered = append(filtered, entry)
		}
		compSrc = filtered
	}

	m := Model{
		session:       sess,
		config:        cfg,
		input:         ti,
		incognito:     initialIncognito,
		completionSrc: compSrc,
		mdRenderer:    mdRenderer,
		elfStates:     make(map[string]*elf.Progress),
		cwd:           cwd,
		gitBranch:     gitBranch,
		streamBuf:     &strings.Builder{},
		thinkingBuf:   &strings.Builder{},
		promptHistory: loadPromptHistory(),
		vimMode:       initialVim,
		vimNormalMode: false, // Start in Insert Mode
		pastedTexts:   make(map[string]string),
		pastedImages:  make(map[string]string),
	}
	m.historyIdx = len(m.promptHistory)
	m = m.updateInputPrompt()
	return m
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

	case tea.PasteMsg:
		content := msg.Content
		id := fmt.Sprintf("#p%d", len(m.pastedTexts)+1)
		lines := strings.Count(strings.TrimRight(content, "\n"), "\n") + 1
		placeholder := fmt.Sprintf("[Pasted text %s +%d lines]", id, lines)
		m.pastedTexts[id] = content
		m.input.InsertString(placeholder)
		return m, nil

	case tea.KeyMsg:
		// Interactive pickers navigation and action handling
		pickerOpen := m.modelPickerOpen || m.profilePickerOpen || m.skillsPickerOpen || m.pluginsPickerOpen || m.helpPickerOpen || m.themePickerOpen || m.providerPickerOpen
		if pickerOpen {
			switch msg.String() {
			case "up", "k":
				if m.pickerSelected > 0 {
					m.pickerSelected--
				}
				return m, nil
			case "down", "j":
				count := m.getPickerItemCount()
				if m.pickerSelected < count-1 {
					m.pickerSelected++
				}
				return m, nil
			case "enter":
				return m.triggerPickerAction()
			case "q":
				m = m.closeAllPickers()
				return m, nil
			case "escape", "esc", "ctrl+c":
				// Close the overlay but fall through to the global escape /
				// ctrl+c handlers below so streaming cancel and double-tap
				// quit still work while a picker is up.
				m = m.closeAllPickers()
			default:
				// Swallow any other key so picker overlays don't pass
				// stray input through to vim or global handlers.
				return m, nil
			}
		}

		// Vim Mode Handling
		if m.vimMode {
			if !m.vimNormalMode {
				if msg.String() == "escape" {
					m.vimNormalMode = true
					m = m.updateInputPrompt()
					return m, nil
				}
			} else {
				// Normal Mode key overrides
				switch msg.String() {
				case "i":
					m.vimNormalMode = false
					m = m.updateInputPrompt()
					return m, nil
				case "a":
					m.vimNormalMode = false
					m = m.updateInputPrompt()
					m.input, _ = m.input.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyRight}))
					return m, nil
				case "h":
					m.input, _ = m.input.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyLeft}))
					return m, nil
				case "l":
					m.input, _ = m.input.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyRight}))
					return m, nil
				case "j":
					m.input.CursorDown()
					return m, nil
				case "k":
					m.input.CursorUp()
					return m, nil
				case "0":
					m.input.CursorStart()
					return m, nil
				case "$":
					m.input.CursorEnd()
					return m, nil
				case "w":
					m.input, _ = m.input.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyRight, Mod: tea.ModAlt}))
					return m, nil
				case "b":
					m.input, _ = m.input.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyLeft, Mod: tea.ModAlt}))
					return m, nil
				case "x":
					m.input, _ = m.input.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyDelete}))
					return m, nil
				default:
					if len(msg.String()) == 1 {
						return m, nil // swallow any other typing key in Normal Mode
					}
				}
			}
		}

		// Reset history navigation index if the key is not up or down
		if msg.String() != "up" && msg.String() != "down" {
			m.historyIdx = len(m.promptHistory)
		}

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
			numSettings := len(m.getActiveSettings())
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
		case "ctrl+y":
			return m.copyLatestResponse()
		case "ctrl+v":
			// Image paste writes the file to the user's cache directory
			// (not the project workdir, which would pollute the repo).
			// Skipped entirely under /incognito to honor the
			// no-persistence contract; falls through to text clipboard.
			if !m.incognito {
				imgBytes, ext, err := pasteImageFromClipboard()
				if err == nil && len(imgBytes) > 0 {
					if path, errStore := storePastedImage(imgBytes, ext); errStore == nil {
						id := fmt.Sprintf("#img%d", len(m.pastedImages)+1)
						placeholder := fmt.Sprintf("[Pasted image %s]", id)
						m.pastedImages[id] = path
						m.input.InsertString(placeholder)
						return m, nil
					}
				}
			}
			// Fallback to text clipboard paste
			txt, errTxt := clipboard.ReadAll()
			if errTxt == nil && txt != "" {
				id := fmt.Sprintf("#p%d", len(m.pastedTexts)+1)
				lines := strings.Count(strings.TrimRight(txt, "\n"), "\n") + 1
				placeholder := fmt.Sprintf("[Pasted text %s +%d lines]", id, lines)
				m.pastedTexts[id] = txt
				m.input.InsertString(placeholder)
				return m, nil
			}
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
			if len(m.promptHistory) > 0 {
				val := m.input.Value()
				if val == "" || (m.historyIdx < len(m.promptHistory) && val == m.promptHistory[m.historyIdx]) {
					if m.historyIdx > 0 {
						m.historyIdx--
						m.input.SetValue(m.promptHistory[m.historyIdx])
						m.input.CursorEnd()
					}
					return m, nil
				}
			}
		case "down":
			if len(m.suggestions) > 0 {
				if m.suggIdx < len(m.suggestions)-1 {
					m.suggIdx++
				}
				return m, nil
			}
			if len(m.promptHistory) > 0 {
				val := m.input.Value()
				if m.historyIdx < len(m.promptHistory) && val == m.promptHistory[m.historyIdx] {
					m.historyIdx++
					if m.historyIdx < len(m.promptHistory) {
						m.input.SetValue(m.promptHistory[m.historyIdx])
						m.input.CursorEnd()
					} else {
						m.input.SetValue("")
					}
					return m, nil
				}
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
		m.suggestion = matchCompletion(val, m.completionSrc, m.config.ProfileNames, m.getAvailableProviders())
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

	// Save prompt to history. In-memory history stays available for Up/Down
	// recall during the session; only the persistent file is gated by
	// incognito to honor the no-persistence contract.
	if !m.incognito {
		savePromptHistory(input)
	}
	m.promptHistory = append(m.promptHistory, input)
	m.historyIdx = len(m.promptHistory)

	expandedInput := m.expandPlaceholders(input)

	m.messages = append(m.messages, chatMessage{role: "user", content: input})
	m.streaming = true
	m.currentRole = "assistant"
	m.streamBuf.Reset()
	m.thinkingBuf.Reset()
	m.streamFilterClose = ""

	if err := m.session.Send(expandedInput); err != nil {
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

	case "/vim":
		m.vimMode = !m.vimMode
		m.vimNormalMode = false
		m = m.updateInputPrompt()
		m.messages = append(m.messages, chatMessage{role: "system", content: fmt.Sprintf("Vim mode toggled: %v", m.vimMode)})
		if err := gnomacfg.SetProjectConfig("tui.vim", strconv.FormatBool(m.vimMode)); err != nil {
			m.messages = append(m.messages, chatMessage{role: "error", content: formatError(err)})
		}
		return m, nil

	case "/copy":
		return m.copyLatestResponse()

	case "/theme":
		if args == "" {
			m = m.closeAllPickers()
			m.themePickerOpen = true
			m.pickerSelected = 0
			return m, nil
		}
		if ApplyTheme(args) {
			m.messages = append(m.messages, chatMessage{role: "system", content: fmt.Sprintf("Theme switched to: %s", args)})
			if err := gnomacfg.SetProjectConfig("tui.theme", args); err != nil {
				m.messages = append(m.messages, chatMessage{role: "error", content: formatError(err)})
			}
		} else {
			m.messages = append(m.messages, chatMessage{role: "error", content: fmt.Sprintf("Theme not found: %s", args)})
		}
		return m, nil

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
		if m.config.SLM.Active {
			m.messages = append(m.messages, chatMessage{role: "system", content: "Model switching is overruled and disabled when local SLM is active."})
			return m, nil
		}
		if args == "" {
			m = m.closeAllPickers()
			m.modelPickerOpen = true
			m.pickerSelected = 0
			if m.config.Router != nil {
				arms := m.config.Router.Arms()
				sort.Slice(arms, func(i, j int) bool {
					return string(arms[i].ID) < string(arms[j].ID)
				})
				m.modelSnapshot = m.modelSnapshot[:0]
				for _, arm := range arms {
					m.modelSnapshot = append(m.modelSnapshot, arm.ModelName)
				}
			}
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
		if args == "" {
			m = m.closeAllPickers()
			m.profilePickerOpen = true
			m.pickerSelected = 0
			return m, nil
		}
		return m.handleProfileCommand(args)

	case "/provider":
		if m.config.SLM.Active {
			m.messages = append(m.messages, chatMessage{role: "system", content: "Provider switching is overruled and disabled when local SLM is active."})
			return m, nil
		}
		if args == "" {
			m = m.closeAllPickers()
			m.providerPickerOpen = true
			m.pickerSelected = 0
			return m, nil
		}
		return m.switchProvider(args)

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
		m = m.closeAllPickers()
		m.resumePending = true
		m.resumeSessions = sessions
		m.resumeSelected = 0
		m.scrollOffset = 0
		return m, nil

	case "/help":
		if args == "" {
			m = m.closeAllPickers()
			m.helpPickerOpen = true
			m.pickerSelected = 0
			return m, nil
		}
		m.messages = append(m.messages, chatMessage{role: "system",
			content: "Commands:\n  /init               generate or update AGENTS.md project docs\n  /clear, /new        clear chat and start new conversation\n  /config             show current config\n  /incognito          toggle incognito (Ctrl+X)\n  /keys               show keyboard shortcuts\n  /model [name]       list/switch models\n  /permission [mode]  set permission mode (Shift+Tab to cycle)\n  /plugins            list installed plugins\n  /profile [name]     list profiles / switch (re-execs gnoma)\n  /provider           show current provider\n  /replay             scroll to top to re-read conversation\n  /resume [id]        list or restore saved sessions\n  /shell [cmd]        open interactive shell (or run cmd in shell)\n  /skills             list loaded skills\n  /usage              show token usage and cost\n  /help               show this help\n  /quit               exit gnoma\n\nSkills (use /<name> [args] to invoke):\n  Add .md files with YAML front matter to .gnoma/skills/ or ~/.config/gnoma/skills/"})
		return m, nil

	case "/keys":
		if args == "" {
			m = m.closeAllPickers()
			m.helpPickerOpen = true
			m.pickerSelected = 0
			return m, nil
		}
		m.messages = append(m.messages, chatMessage{role: "system",
			content: "Keyboard shortcuts:\n" +
				"  Enter           send message\n" +
				"  Shift+Enter     newline in input\n" +
				"  Tab             accept completion\n" +
				"  Ctrl+C          cancel stream / quit (press twice)\n" +
				"  Ctrl+X          toggle incognito mode\n" +
				"  Ctrl+O          expand/collapse tool output\n" +
				"  Ctrl+Y          copy latest response to clipboard\n" +
				"  Ctrl+V          paste image/text from clipboard\n" +
				"  Ctrl+]          toggle copy mode (disables mouse)\n" +
				"  Shift+Tab       cycle permission mode\n" +
				"  ↑/↓             scroll chat history\n" +
				"  PgUp/PgDn       scroll one page\n" +
				"  Home            jump up 50 lines\n" +
				"  End             scroll to bottom\n" +
				"  y/n             approve/deny permission prompts"})
		return m, nil

	case "/plugins":
		if args == "" {
			if len(m.config.PluginInfos) == 0 {
				m.messages = append(m.messages, chatMessage{role: "system", content: "No plugins installed."})
				return m, nil
			}
			m = m.closeAllPickers()
			m.pluginsPickerOpen = true
			m.pickerSelected = 0
			return m, nil
		}
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
		if args == "" {
			if m.config.Skills == nil || len(m.config.Skills.Names()) == 0 {
				m.messages = append(m.messages, chatMessage{role: "system", content: "No skills loaded."})
				return m, nil
			}
			m = m.closeAllPickers()
			m.skillsPickerOpen = true
			m.pickerSelected = 0
			return m, nil
		}
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

// getActiveSettings returns the active settings keys, hiding model/provider options when local SLM is active.
func (m Model) getActiveSettings() []string {
	if m.config.SLM.Active {
		return []string{"permission", "incognito"}
	}
	return []string{"provider", "model", "permission", "incognito"}
}

// applyConfigSetting applies the Enter action for the currently selected settings item.
func (m Model) applyConfigSetting() Model {
	status := m.session.Status()
	settings := m.getActiveSettings()
	if m.configSelected < 0 || m.configSelected >= len(settings) {
		return m
	}
	switch settings[m.configSelected] {
	case "provider": // Provider — open provider sub-menu picker
		m.configPanelOpen = false
		m.providerPickerOpen = true
		m.pickerSelected = 0
		providers := m.getAvailableProviders()
		for idx, prov := range providers {
			if prov == status.Provider {
				m.pickerSelected = idx
				break
			}
		}

	case "model": // Model — open model sub-menu picker
		m.configPanelOpen = false
		m.modelPickerOpen = true
		m.pickerSelected = 0
		if m.config.Router != nil {
			arms := m.config.Router.Arms()
			sort.Slice(arms, func(i, j int) bool {
				return string(arms[i].ID) < string(arms[j].ID)
			})
			for idx, arm := range arms {
				if arm.ModelName == status.Model {
					m.pickerSelected = idx
					break
				}
			}
		}

	case "permission": // Permission — cycle modes
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

	case "incognito": // Incognito — toggle (silent; config panel has no status line)
		newM, _, _ := m.attemptIncognitoToggle()
		m = newM
	}
	return m
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

func (m Model) updateInputPrompt() Model {
	if m.permPending {
		m.input.SetPromptFunc(17, func(info textarea.PromptInfo) string {
			if info.LineNumber == 0 {
				return "APPROVE? (y/n) ❯ "
			}
			return "                 "
		})
	} else if m.vimMode {
		if m.vimNormalMode {
			m.input.SetPromptFunc(6, func(info textarea.PromptInfo) string {
				if info.LineNumber == 0 {
					return "(N) ❯ "
				}
				return "      "
			})
		} else {
			m.input.SetPromptFunc(6, func(info textarea.PromptInfo) string {
				if info.LineNumber == 0 {
					return "(I) ❯ "
				}
				return "      "
			})
		}
	} else {
		m.input.SetPromptFunc(2, func(info textarea.PromptInfo) string {
			if info.LineNumber == 0 {
				return "❯ "
			}
			return "  "
		})
	}
	m.input.SetWidth(m.width - 4)
	return m
}

func (m Model) copyLatestResponse() (tea.Model, tea.Cmd) {
	var lastAssistant string
	for i := len(m.messages) - 1; i >= 0; i-- {
		if m.messages[i].role == "assistant" {
			lastAssistant = m.messages[i].content
			break
		}
	}
	if lastAssistant == "" {
		m.messages = append(m.messages, chatMessage{role: "error", content: "No assistant response to copy"})
		return m, nil
	}

	if err := clipboard.WriteAll(lastAssistant); err != nil {
		m.messages = append(m.messages, chatMessage{role: "error", content: fmt.Sprintf("Failed to copy clipboard: %v", err)})
	} else {
		m.messages = append(m.messages, chatMessage{role: "system", content: "Copied latest response to clipboard"})
	}
	m.scrollOffset = 0
	return m, nil
}

func loadPromptHistory() []string {
	dir := gnomacfg.GlobalConfigDir()
	path := filepath.Join(dir, "history.txt")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	lines := strings.Split(string(data), "\n")
	var history []string
	for _, line := range lines {
		line = strings.TrimSuffix(line, "\r")
		if line != "" {
			history = append(history, line)
		}
	}
	if len(history) > 500 {
		history = history[len(history)-500:]
	}
	return history
}

const maxPromptHistory = 500

func savePromptHistory(input string) {
	if strings.TrimSpace(input) == "" {
		return
	}
	dir := gnomacfg.GlobalConfigDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		slog.Warn("prompt history: mkdir failed", "err", err, "dir", dir)
		return
	}
	path := filepath.Join(dir, "history.txt")

	// Read current history (best-effort), append, cap to maxPromptHistory.
	var history []string
	if data, err := os.ReadFile(path); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSuffix(line, "\r")
			if line != "" {
				history = append(history, line)
			}
		}
	}
	history = append(history, strings.ReplaceAll(input, "\n", " "))
	if len(history) > maxPromptHistory {
		history = history[len(history)-maxPromptHistory:]
	}

	if err := os.WriteFile(path, []byte(strings.Join(history, "\n")+"\n"), 0o600); err != nil {
		slog.Warn("prompt history: write failed", "err", err, "path", path)
		return
	}
	// os.WriteFile preserves existing perms; force 0600 for files created
	// before the perm tightening landed.
	if err := os.Chmod(path, 0o600); err != nil {
		slog.Warn("prompt history: chmod failed", "err", err, "path", path)
	}
}

// pastedImageStaleAfter bounds how long a pasted-image file lives in the
// user cache before it is eligible for pruning. Long enough to survive
// any reasonable single turn (including provider retries and slow
// subprocess CLIs), short enough that files don't accumulate across
// sessions or days.
const pastedImageStaleAfter = 2 * time.Hour

// pastedImageDir returns the on-disk location where Ctrl+V image pastes
// are written. Uses os.UserCacheDir() (XDG_CACHE_HOME on Linux,
// ~/Library/Caches on macOS, %LocalAppData% on Windows) so paste files
// stay out of the project workdir and live somewhere the OS knows is
// purgeable. The directory is created at mode 0700 because pasted
// images may contain screenshots with sensitive content.
func pastedImageDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "gnoma", "pasted-images")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// pruneStalePastedImages removes pasted-image files older than
// pastedImageStaleAfter. Best-effort; errors are logged but not returned
// so a paste never fails because of cleanup trouble.
func pruneStalePastedImages(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-pastedImageStaleAfter)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			if rmErr := os.Remove(filepath.Join(dir, e.Name())); rmErr != nil {
				slog.Debug("pasted-image prune failed", "name", e.Name(), "err", rmErr)
			}
		}
	}
}

// storePastedImage persists clipboard image bytes to the user cache and
// returns the absolute path. Prunes stale entries on each paste so the
// directory does not grow without bound across sessions.
func storePastedImage(data []byte, ext string) (string, error) {
	dir, err := pastedImageDir()
	if err != nil {
		return "", err
	}
	pruneStalePastedImages(dir)
	name := fmt.Sprintf("pasted_image_%d%s", time.Now().UnixNano(), ext)
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func pasteImageFromClipboard() ([]byte, string, error) {
	// Try wl-paste
	if _, err := exec.LookPath("wl-paste"); err == nil {
		cmd := exec.Command("wl-paste", "--list-types")
		out, err := cmd.Output()
		if err == nil {
			types := string(out)
			var mime string
			if strings.Contains(types, "image/png") {
				mime = "image/png"
			} else if strings.Contains(types, "image/jpeg") {
				mime = "image/jpeg"
			} else if strings.Contains(types, "image/jpg") {
				mime = "image/jpg"
			}

			if mime != "" {
				cmdImg := exec.Command("wl-paste", "--type", mime)
				imgBytes, errImg := cmdImg.Output()
				if errImg == nil && len(imgBytes) > 0 {
					ext := ".png"
					if strings.Contains(mime, "jpeg") || strings.Contains(mime, "jpg") {
						ext = ".jpg"
					}
					return imgBytes, ext, nil
				}
			}
		}
	}

	// Try xclip
	if _, err := exec.LookPath("xclip"); err == nil {
		cmd := exec.Command("xclip", "-selection", "clipboard", "-t", "TARGETS", "-o")
		out, err := cmd.Output()
		if err == nil {
			targets := string(out)
			var mime string
			if strings.Contains(targets, "image/png") {
				mime = "image/png"
			} else if strings.Contains(targets, "image/jpeg") {
				mime = "image/jpeg"
			} else if strings.Contains(targets, "image/jpg") {
				mime = "image/jpg"
			}

			if mime != "" {
				cmdImg := exec.Command("xclip", "-selection", "clipboard", "-t", mime, "-o")
				imgBytes, errImg := cmdImg.Output()
				if errImg == nil && len(imgBytes) > 0 {
					ext := ".png"
					if strings.Contains(mime, "jpeg") || strings.Contains(mime, "jpg") {
						ext = ".jpg"
					}
					return imgBytes, ext, nil
				}
			}
		}
	}

	return nil, "", fmt.Errorf("no image in clipboard or missing tools")
}

func (m Model) closeAllPickers() Model {
	m.modelPickerOpen = false
	m.profilePickerOpen = false
	m.skillsPickerOpen = false
	m.pluginsPickerOpen = false
	m.helpPickerOpen = false
	m.themePickerOpen = false
	m.providerPickerOpen = false
	m.resumePending = false
	m.configPanelOpen = false
	return m
}

func (m Model) getPickerItemCount() int {
	if m.modelPickerOpen {
		if m.config.Router != nil {
			return len(m.config.Router.Arms())
		}
		return 0
	}
	if m.providerPickerOpen {
		return len(m.getAvailableProviders())
	}
	if m.profilePickerOpen {
		return len(m.config.ProfileNames)
	}
	if m.skillsPickerOpen {
		if m.config.Skills != nil {
			return len(m.config.Skills.Names())
		}
		return 0
	}
	if m.pluginsPickerOpen {
		return len(m.config.PluginInfos)
	}
	if m.themePickerOpen {
		return 5
	}
	return 0
}

func (m Model) triggerPickerAction() (tea.Model, tea.Cmd) {
	if m.modelPickerOpen {
		if m.config.Router != nil && m.config.Engine != nil {
			arms := m.config.Router.Arms()
			sort.Slice(arms, func(i, j int) bool {
				return string(arms[i].ID) < string(arms[j].ID)
			})
			if m.pickerSelected >= 0 && m.pickerSelected < len(arms) {
				modelName := arms[m.pickerSelected].ModelName
				m.config.Engine.SetModel(modelName)
				if ls, ok := m.session.(*session.Local); ok {
					ls.SetModel(modelName)
				}
				m.messages = append(m.messages, chatMessage{role: "system", content: fmt.Sprintf("model switched to: %s", modelName)})
			}
		}
		m = m.closeAllPickers()
		return m, nil
	}
	if m.providerPickerOpen {
		providers := m.getAvailableProviders()
		if m.pickerSelected >= 0 && m.pickerSelected < len(providers) {
			provName := providers[m.pickerSelected]
			m = m.closeAllPickers()
			return m.switchProvider(provName)
		}
		m = m.closeAllPickers()
		return m, nil
	}
	if m.profilePickerOpen {
		if m.pickerSelected >= 0 && m.pickerSelected < len(m.config.ProfileNames) {
			profileName := m.config.ProfileNames[m.pickerSelected]
			m = m.closeAllPickers()
			return m.handleProfileCommand(profileName)
		}
		m = m.closeAllPickers()
		return m, nil
	}
	if m.skillsPickerOpen {
		if m.config.Skills != nil {
			names := m.config.Skills.Names()
			sort.Strings(names)
			if m.pickerSelected >= 0 && m.pickerSelected < len(names) {
				skillName := names[m.pickerSelected]
				m = m.closeAllPickers()
				return m.handleCommand("/" + skillName)
			}
		}
		m = m.closeAllPickers()
		return m, nil
	}
	if m.pluginsPickerOpen {
		m = m.closeAllPickers()
		return m, nil
	}
	if m.themePickerOpen {
		themes := []string{"catppuccin", "nord", "gruvbox", "monokai", "solarized-light"}
		if m.pickerSelected >= 0 && m.pickerSelected < len(themes) {
			themeName := themes[m.pickerSelected]
			if ApplyTheme(themeName) {
				m.messages = append(m.messages, chatMessage{role: "system", content: fmt.Sprintf("Theme switched to: %s", themeName)})
				if err := gnomacfg.SetProjectConfig("tui.theme", themeName); err != nil {
					m.messages = append(m.messages, chatMessage{role: "error", content: formatError(err)})
				}
			}
		}
		m = m.closeAllPickers()
		return m, nil
	}
	if m.helpPickerOpen {
		m = m.closeAllPickers()
		return m, nil
	}
	m = m.closeAllPickers()
	return m, nil
}

// placeholderRe matches all four placeholder forms in one pass over the
// original input, so pasted content that happens to contain `#p\d+` or
// `#img\d+` literals is not re-expanded after the bracket form is inlined.
var placeholderRe = regexp.MustCompile(`\[Pasted text (#p\d+)[^\]]*\]|\[Pasted image (#img\d+)\]|#p\d+|#img\d+`)

func (m Model) expandPlaceholders(input string) string {
	return placeholderRe.ReplaceAllStringFunc(input, func(match string) string {
		switch {
		case strings.HasPrefix(match, "[Pasted text "):
			sub := placeholderRe.FindStringSubmatch(match)
			if len(sub) > 1 {
				if val, ok := m.pastedTexts[sub[1]]; ok {
					return val
				}
			}
		case strings.HasPrefix(match, "[Pasted image "):
			sub := placeholderRe.FindStringSubmatch(match)
			if len(sub) > 2 {
				if path, ok := m.pastedImages[sub[2]]; ok {
					return "[Image: " + path + "]"
				}
			}
		case strings.HasPrefix(match, "#p"):
			if val, ok := m.pastedTexts[match]; ok {
				return val
			}
		case strings.HasPrefix(match, "#img"):
			if path, ok := m.pastedImages[match]; ok {
				return "[Image: " + path + "]"
			}
		}
		return match
	})
}

func (m Model) getAvailableProviders() []string {
	if m.config.Router == nil {
		return nil
	}
	seen := make(map[string]bool)
	var list []string
	for _, arm := range m.config.Router.Arms() {
		prov := arm.ID.Provider()
		if !seen[prov] {
			seen[prov] = true
			list = append(list, prov)
		}
	}
	sort.Strings(list)
	return list
}

func (m Model) findBestArmForProvider(provName string) *router.Arm {
	if m.config.Router == nil {
		return nil
	}
	arms := m.config.Router.Arms()
	sort.Slice(arms, func(i, j int) bool {
		return string(arms[i].ID) < string(arms[j].ID)
	})

	var providerDefaultModel string
	var fallbackArm *router.Arm

	for _, arm := range arms {
		if arm.ID.Provider() == provName {
			if fallbackArm == nil {
				fallbackArm = arm
			}
			if providerDefaultModel == "" && arm.Provider != nil {
				providerDefaultModel = arm.Provider.DefaultModel()
			}
			if providerDefaultModel != "" && arm.ModelName == providerDefaultModel {
				return arm
			}
		}
	}
	return fallbackArm
}

func (m Model) switchProvider(provName string) (Model, tea.Cmd) {
	provName = strings.ToLower(strings.TrimSpace(provName))
	providers := m.getAvailableProviders()
	valid := false
	for _, p := range providers {
		if strings.ToLower(p) == provName {
			provName = p // normalize case
			valid = true
			break
		}
	}
	if !valid {
		m.messages = append(m.messages, chatMessage{
			role:    "error",
			content: fmt.Sprintf("unknown provider: %q — available providers: %s", provName, strings.Join(providers, ", ")),
		})
		return m, nil
	}

	arm := m.findBestArmForProvider(provName)
	if arm == nil {
		m.messages = append(m.messages, chatMessage{
			role:    "error",
			content: fmt.Sprintf("no registered arms found for provider: %s", provName),
		})
		return m, nil
	}

	if m.config.Engine != nil {
		m.config.Engine.SetProvider(arm.Provider)
		m.config.Engine.SetModel(arm.ModelName)
	}
	if ls, ok := m.session.(*session.Local); ok {
		ls.SetProvider(provName)
		ls.SetModel(arm.ModelName)
	}

	msg := fmt.Sprintf("provider switched to: %s (model: %s)", provName, arm.ModelName)
	m.messages = append(m.messages, chatMessage{role: "system", content: msg})
	m.injectSystemContext(msg)

	return m, nil
}
