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

	xansi "github.com/charmbracelet/x/ansi"

	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/textarea"
	"charm.land/glamour/v2"
	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"
	gnomacfg "somegit.dev/Owlibou/gnoma/internal/config"
	"somegit.dev/Owlibou/gnoma/internal/elf"
	"somegit.dev/Owlibou/gnoma/internal/skill"
	"somegit.dev/Owlibou/gnoma/internal/engine"
	"somegit.dev/Owlibou/gnoma/internal/message"
	"somegit.dev/Owlibou/gnoma/internal/permission"
	"somegit.dev/Owlibou/gnoma/internal/provider"
	"somegit.dev/Owlibou/gnoma/internal/router"
	"somegit.dev/Owlibou/gnoma/internal/security"
	"somegit.dev/Owlibou/gnoma/internal/session"
	"somegit.dev/Owlibou/gnoma/internal/stream"
)

// version is set from Config.Version at init; falls back to "dev".
var version = "dev"

type streamEventMsg struct{ event stream.Event }
type turnDoneMsg struct{ err error }

// PermReqMsg carries a permission request from engine to TUI.
type PermReqMsg struct {
	ToolName string
	Args     json.RawMessage
}

type elfProgressMsg struct{ progress elf.Progress }
type clearQuitHintMsg struct{}
type resumeListLoadedMsg struct{ sessions []session.Metadata }

type chatMessage struct {
	role    string
	content string
}

// Config holds optional dependencies for TUI features.
type Config struct {
	Firewall             *security.Firewall    // for incognito toggle
	Engine               *engine.Engine        // for model switching
	Permissions          *permission.Checker   // for mode switching
	Router               *router.Router        // for model listing
	ElfManager           *elf.Manager          // for CancelAll on escape/quit
	PermCh               chan bool             // TUI → engine: y/n response
	PermReqCh            <-chan PermReqMsg    // engine → TUI: tool requesting approval
	ElfProgress          <-chan elf.Progress   // elf → TUI: structured progress updates
	SessionStore         *session.SessionStore // nil = no persistence
	StartWithResumePicker bool                 // open session picker on launch
	Skills               *skill.Registry       // nil = no skills loaded
	PluginInfos          []PluginInfo          // discovered plugins for /plugins command
	Version              string                // build version string (from ldflags)
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

	messages     []chatMessage
	streaming    bool
	streamBuf    *strings.Builder // regular text content (assistant role)
	thinkingBuf  *strings.Builder // reasoning/thinking content (frozen once text starts)
	currentRole  string

	input          textarea.Model
	mdRenderer     *glamour.TermRenderer
	expandOutput   bool   // ctrl+o toggles expanded tool output
	elfStates      map[string]*elf.Progress // active elf states keyed by ID
	elfOrder       []string                 // insertion-ordered elf IDs for tree rendering
	elfToolActive  bool                     // suppresses next toolresult (elf output)
	cwd            string
	gitBranch      string
	scrollOffset   int
	incognito      bool
	copyMode       bool            // ctrl+] toggles mouse passthrough for terminal text selection
	lastCtrlC      time.Time       // tracks first ctrl+c for double-press detection
	quitHint       bool            // show "ctrl+c to quit" indicator in status bar
	permPending    bool            // waiting for user to approve/deny a tool
	permToolName   string          // which tool is asking
	permArgs       json.RawMessage // tool args for display

	// Session resume picker
	resumePending  bool
	resumeSessions []session.Metadata
	resumeSelected int
	initPending       bool   // true while /init turn is in-flight; triggers AGENTS.md reload on turnDone
	initHadToolCalls  bool   // set when any tool call fires during an init turn
	initRetried       bool   // set after first retry (no-tool-call case) so we don't retry indefinitely
	initWriteNudged   bool   // set after write nudge (spawn_elfs-ran-but-no-fs_write case)
	streamFilterClose  string // non-empty while suppressing a model pseudo-block; value is expected close tag
	runningTools   []string        // transient: tool names currently executing (rendered ephemerally, not in chat history)
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

	ti.Focus()

	cwd, _ := os.Getwd()
	gitBranch := detectGitBranch()

	// Markdown renderer for chat output (74 = 80 - 6 for "◆ "/"  " prefix)
	mdRenderer, _ := glamour.NewTermRenderer(
		glamour.WithStandardStyle("dark"),
		glamour.WithWordWrap(74),
	)

	return Model{
		session:     sess,
		config:      cfg,
		input:       ti,
		mdRenderer:  mdRenderer,
		elfStates:   make(map[string]*elf.Progress),
		cwd:         cwd,
		gitBranch:   gitBranch,
		streamBuf:   &strings.Builder{},
		thinkingBuf: &strings.Builder{},
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
	return tea.Batch(cmds...)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.input.SetWidth(m.width - 4)
		// Recreate markdown renderer with new width (account for "◆ "/"  " prefix)
		m.mdRenderer, _ = glamour.NewTermRenderer(
			glamour.WithStandardStyle("dark"),
			glamour.WithWordWrap(m.width-6),
		)
		return m, nil

	case tea.KeyMsg:
		// --- Global keys: work in ALL states ---

		// Escape = global stop, never quits
		if msg.String() == "escape" {
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

		// Ctrl+C = clear input (single) or quit (double within 1s)
		if msg.String() == "ctrl+c" {
			now := time.Now()
			if m.quitHint && now.Sub(m.lastCtrlC) < time.Second {
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
			// First press → clear input, show hint, start expiry timer
			m.input.SetValue("")
			m.lastCtrlC = now
			m.quitHint = true
			return m, tea.Tick(time.Second, func(time.Time) tea.Msg {
				return clearQuitHintMsg{}
			})
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

		switch msg.String() {
		case "ctrl+x":
			// Toggle incognito
			if m.config.Firewall != nil {
				m.incognito = m.config.Firewall.Incognito().Toggle()
				if m.config.Router != nil {
					m.config.Router.SetLocalOnly(m.incognito)
				}
				var msg string
				if m.incognito {
					msg = "🔒 incognito ON — no persistence, no learning, local-only routing"
				} else {
					msg = "🔓 incognito OFF"
				}
				m.messages = append(m.messages, chatMessage{role: "system", content: msg})
				m.injectSystemContext(msg)
				m.scrollOffset = 0
			}
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
		case "pgup", "shift+up":
			m.scrollOffset += 5
			return m, nil
		case "pgdown", "shift+down":
			m.scrollOffset -= 5
			if m.scrollOffset < 0 {
				m.scrollOffset = 0
			}
			return m, nil
		case "enter":
			if m.streaming {
				return m, nil
			}
			input := strings.TrimSpace(m.input.Value())
			if input == "" {
				return m, nil
			}
			m.input.SetValue("")
			return m.submitInput(input)
		}

	case tea.MouseWheelMsg:
		if msg.Button == tea.MouseWheelUp {
			m.scrollOffset += 3
		} else if msg.Button == tea.MouseWheelDown {
			m.scrollOffset -= 3
			if m.scrollOffset < 0 {
				m.scrollOffset = 0
			}
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
		return m, nil

	case streamEventMsg:
		return m.handleStreamEvent(msg.event)

	case turnDoneMsg:
		m.streaming = false
		m.scrollOffset = 0
		m.elfStates = make(map[string]*elf.Progress) // clear elf states
		m.elfOrder = nil
		m.runningTools = nil

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
			if err := m.session.Send(nudge); err != nil {
				m.messages = append(m.messages, chatMessage{role: "error", content: err.Error()})
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
					m.messages = append(m.messages, chatMessage{role: "error", content: err.Error()})
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
			m.messages = append(m.messages, chatMessage{role: "error", content: msg.err.Error()})
		}
		if m.initPending {
			m.initPending = false
			m = m.loadAgentsMD()
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	cmds = append(cmds, cmd)
	return m, tea.Batch(cmds...)
}

func (m Model) submitInput(input string) (tea.Model, tea.Cmd) {
	if strings.HasPrefix(input, "/") {
		return m.handleCommand(input)
	}

	m.messages = append(m.messages, chatMessage{role: "user", content: input})
	m.streaming = true
	m.currentRole = "assistant"
	m.streamBuf.Reset()
	m.thinkingBuf.Reset()
	m.streamFilterClose = ""

	if err := m.session.Send(input); err != nil {
		m.messages = append(m.messages, chatMessage{role: "error", content: err.Error()})
		m.streaming = false
		return m, nil
	}
	return m, m.listenForEvents()
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

	case "/clear", "/new":
		m.messages = nil
		m.scrollOffset = 0
		if m.config.Engine != nil {
			m.config.Engine.Reset()
		}
		return m, nil

	case "/compact":
		if m.config.Engine != nil {
			if w := m.config.Engine.ContextWindow(); w != nil {
				compacted, err := w.ForceCompact()
				if err != nil {
					m.messages = append(m.messages, chatMessage{role: "error", content: "compaction failed: " + err.Error()})
				} else if compacted {
					m.messages = append(m.messages, chatMessage{role: "system", content: "context compacted — older messages summarized"})
				} else {
					m.messages = append(m.messages, chatMessage{role: "system", content: "no compaction strategy configured"})
				}
			}
		}
		return m, nil

	case "/incognito":
		if m.config.Firewall != nil {
			m.incognito = m.config.Firewall.Incognito().Toggle()
			if m.config.Router != nil {
				m.config.Router.SetLocalOnly(m.incognito)
			}
			if m.incognito {
				m.messages = append(m.messages, chatMessage{role: "system",
					content: "🔒 incognito mode ON — no persistence, no learning, local-only routing"})
			} else {
				m.messages = append(m.messages, chatMessage{role: "system",
					content: "🔓 incognito mode OFF"})
			}
		} else {
			m.messages = append(m.messages, chatMessage{role: "error",
				content: "firewall not configured"})
		}
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
				b.WriteString("\nAvailable models:\n")
				for i, arm := range arms {
					marker := "  "
					if string(arm.ID) == status.Provider+"/"+status.Model {
						marker = "→ "
					}
					var caps []string
					if arm.Capabilities.ToolUse {
						caps = append(caps, "tools")
					}
					if arm.Capabilities.Thinking {
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
			// Support numeric selection: /model 3
			if n, err := strconv.Atoi(args); err == nil && n >= 1 && m.config.Router != nil {
				arms := m.config.Router.Arms()
				sort.Slice(arms, func(i, j int) bool {
					return string(arms[i].ID) < string(arms[j].ID)
				})
				if n <= len(arms) {
					modelName = arms[n-1].ModelName
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
		// /config set <key> <value>
		if strings.HasPrefix(args, "set ") {
			parts := strings.SplitN(strings.TrimPrefix(args, "set "), " ", 2)
			if len(parts) != 2 {
				m.messages = append(m.messages, chatMessage{role: "error",
					content: "Usage: /config set <key> <value>\nKeys: provider.default, provider.model, permission.mode"})
				return m, nil
			}
			if err := gnomacfg.SetProjectConfig(parts[0], parts[1]); err != nil {
				m.messages = append(m.messages, chatMessage{role: "error", content: err.Error()})
			} else {
				m.messages = append(m.messages, chatMessage{role: "system",
					content: fmt.Sprintf("config set: %s = %s (saved to .gnoma/config.toml)", parts[0], parts[1])})
			}
			return m, nil
		}
		status := m.session.Status()
		var b strings.Builder
		b.WriteString("Current configuration:\n")
		fmt.Fprintf(&b, "  provider: %s\n", status.Provider)
		fmt.Fprintf(&b, "  model: %s\n", status.Model)
		if m.config.Permissions != nil {
			fmt.Fprintf(&b, "  permission: %s\n", m.config.Permissions.Mode())
		}
		fmt.Fprintf(&b, "  incognito: %v\n", m.incognito)
		fmt.Fprintf(&b, "  cwd: %s\n", m.cwd)
		if m.gitBranch != "" {
			fmt.Fprintf(&b, "  git branch: %s\n", m.gitBranch)
		}
		b.WriteString("\nConfig files: ~/.config/gnoma/config.toml, .gnoma/config.toml")
		b.WriteString("\nEdit: /config set <key> <value>")
		m.messages = append(m.messages, chatMessage{role: "system", content: b.String()})
		return m, nil

	case "/elf", "/elfs":
		if args == "" {
			m.messages = append(m.messages, chatMessage{role: "system",
				content: "Elfs are spawned by the LLM via the 'agent' tool.\nAsk the model to use sub-agents for parallel tasks.\n\nExample: \"Research these 3 files in parallel using sub-agents\""})
		}
		return m, nil

	case "/shell":
		m.messages = append(m.messages, chatMessage{role: "system",
			content: "interactive shell not yet implemented\nFor now, use ! prefix in your terminal: ! sudo command"})
		return m, nil

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

	case "/provider":
		if args == "" {
			status := m.session.Status()
			m.messages = append(m.messages, chatMessage{role: "system",
				content: fmt.Sprintf("current provider: %s\nUsage: /provider <name> (mistral, anthropic, openai, google, ollama)", status.Provider)})
			return m, nil
		}
		m.messages = append(m.messages, chatMessage{role: "system",
			content: fmt.Sprintf("provider switching requires restart: gnoma --provider %s", args)})
		return m, nil

	case "/init":
		root := gnomacfg.ProjectRoot()
		agentsPath := filepath.Join(root, "AGENTS.md")
		var existingPath string
		if _, err := os.Stat(agentsPath); err == nil {
			existingPath = agentsPath
		}

		prompt := initPrompt(root, existingPath)

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

		// Local models (Ollama, llama.cpp) often narrate tool calls as text instead of
		// invoking them. Force tool_choice: required so the API response includes actual
		// function call JSON rather than a prose description.
		opts := engine.TurnOptions{}
		if status := m.session.Status(); isLocalProvider(status.Provider) {
			opts.ToolChoice = provider.ToolChoiceRequired
		}
		if err := m.session.SendWithOptions(prompt, opts); err != nil {
			m.messages = append(m.messages, chatMessage{role: "error", content: err.Error()})
			m.streaming = false
			m.initPending = false
			return m, nil
		}
		return m, m.listenForEvents()

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
			content: "Commands:\n  /init               generate or update AGENTS.md project docs\n  /clear, /new        clear chat and start new conversation\n  /config             show current config\n  /incognito          toggle incognito (Ctrl+X)\n  /model [name]       list/switch models\n  /permission [mode]  set permission mode (Shift+Tab to cycle)\n  /plugins            list installed plugins\n  /provider           show current provider\n  /resume [id]        list or restore saved sessions\n  /skills             list loaded skills\n  /shell              interactive shell (coming soon)\n  /help               show this help\n  /quit               exit gnoma\n\nSkills (use /<name> [args] to invoke):\n  Add .md files with YAML front matter to .gnoma/skills/ or ~/.config/gnoma/skills/"})
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
				if err := m.session.Send(rendered); err != nil {
					m.messages = append(m.messages, chatMessage{role: "error", content: err.Error()})
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

func (m Model) handleStreamEvent(evt stream.Event) (tea.Model, tea.Cmd) {
	switch evt.Type {
	case stream.EventTextDelta:
		if evt.Text != "" {
			text := filterModelCodeBlocks(&m.streamFilterClose, evt.Text)
			if text != "" {
				m.streamBuf.WriteString(text)
			}
		}
	case stream.EventThinkingDelta:
		// Accumulate reasoning in a separate buffer so it stays frozen/dim
		// while regular text content streams normally below it.
		if m.streamBuf.Len() == 0 {
			m.thinkingBuf.WriteString(evt.Text)
		} else {
			// Text has already started; treat additional thinking as text.
			m.streamBuf.WriteString(evt.Text)
		}
	case stream.EventToolCallStart:
		// Flush both buffers before tool call label
		if m.thinkingBuf.Len() > 0 {
			m.messages = append(m.messages, chatMessage{role: "thinking", content: m.thinkingBuf.String()})
			m.thinkingBuf.Reset()
		}
		if m.streamBuf.Len() > 0 {
			m.messages = append(m.messages, chatMessage{role: m.currentRole, content: m.streamBuf.String()})
			m.streamBuf.Reset()
		}
		if m.initPending {
			m.initHadToolCalls = true
		}
	case stream.EventToolCallDone:
		if evt.ToolCallName == "agent" || evt.ToolCallName == "spawn_elfs" {
			// Suppress tool message — elf tree view handles display
			m.elfToolActive = true
		} else {
			// Track running tools transiently — not in permanent chat history
			m.runningTools = append(m.runningTools, evt.ToolCallName)
		}
	case stream.EventToolResult:
		if m.elfToolActive {
			// Suppress raw elf output — tree shows progress, LLM summarizes
			m.elfToolActive = false
		} else {
			// Pop first running tool (FIFO — results arrive in call order)
			if len(m.runningTools) > 0 {
				m.runningTools = m.runningTools[1:]
			}
			m.messages = append(m.messages, chatMessage{
				role: "toolresult", content: evt.ToolOutput,
			})
		}
	}
	return m, m.listenForEvents()
}

func (m Model) listenForEvents() tea.Cmd {
	ch := m.session.Events()
	permReqCh := m.config.PermReqCh

	elfProgressCh := m.config.ElfProgress

	return func() tea.Msg {
		// Listen for stream events, permission requests, and elf progress
		if permReqCh != nil || elfProgressCh != nil {
			// Build select dynamically — always listen on ch
			select {
			case evt, ok := <-ch:
				if !ok {
					_, err := m.session.TurnResult()
					return turnDoneMsg{err: err}
				}
				return streamEventMsg{event: evt}
			case req, ok := <-permReqCh:
				if ok {
					return req
				}
				return nil
			case progress, ok := <-elfProgressCh:
				if ok {
					return elfProgressMsg{progress: progress}
				}
				return nil
			}
		}

		evt, ok := <-ch
		if !ok {
			_, err := m.session.TurnResult()
			return turnDoneMsg{err: err}
		}
		return streamEventMsg{event: evt}
	}
}

// --- View ---

func (m Model) View() tea.View {
	if m.width == 0 {
		return tea.NewView("")
	}

	status := m.renderStatus()
	input := m.renderInput()
	topLine, bottomLine := m.renderSeparators()

	// Fixed: status bar + separator + input + separator = bottom area
	statusH := lipgloss.Height(status)
	inputH := lipgloss.Height(input)
	chatH := m.height - statusH - inputH - 2

	chat := m.renderChat(chatH)

	v := tea.NewView(lipgloss.JoinVertical(lipgloss.Left,
		chat,
		topLine,
		input,
		bottomLine,
		status,
	))
	if m.copyMode {
		v.MouseMode = tea.MouseModeNone
	} else {
		v.MouseMode = tea.MouseModeCellMotion
	}
	v.AltScreen = true
	return v
}

func (m Model) shortCwd() string {
	dir := m.cwd
	home, _ := os.UserHomeDir()
	if strings.HasPrefix(dir, home) {
		dir = "~" + dir[len(home):]
	}
	return dir
}

func (m Model) renderChat(height int) string {
	var lines []string

	// Header info — scrolls with content
	status := m.session.Status()
	lines = append(lines,
		sHeaderBrand.Render(" gnoma ")+"  "+sHeaderDim.Render("gnoma "+version),
		"    "+sHeaderModel.Render(fmt.Sprintf("%s/%s", status.Provider, status.Model))+
			sHeaderDim.Render(" · ")+sHeaderDim.Render(m.shortCwd()),
		"",
	)

	if len(m.messages) == 0 && !m.streaming {
		lines = append(lines,
			sHint.Render("    Type a message and press Enter."),
			sHint.Render("    /help for commands, Ctrl+C to cancel or quit."),
			"",
		)
	}

	for _, msg := range m.messages {
		lines = append(lines, m.renderMessage(msg)...)
	}

	// Elf tree view — shows active elfs with structured progress
	if m.streaming && len(m.elfStates) > 0 {
		lines = append(lines, m.renderElfTree()...)
	}

	// Transient: running tools (disappear when tool completes)
	for _, name := range m.runningTools {
		lines = append(lines, "  "+sToolOutput.Render(fmt.Sprintf("⚙ [%s] running...", name)))
	}

	// Transient: permission prompt (disappear when approved/denied)
	if m.permPending {
		lines = append(lines, "")
		lines = append(lines, sSystem.Render("• "+formatPermissionPrompt(m.permToolName, m.permArgs)))
		lines = append(lines, "")
	}

	// Transient: session resume picker
	if m.resumePending && len(m.resumeSessions) > 0 {
		lines = append(lines, "")
		lines = append(lines, sSystem.Render("  Sessions  ↑↓ · Enter to load · Esc to cancel"))
		lines = append(lines, "")
		for i, s := range m.resumeSessions {
			age := time.Since(s.UpdatedAt).Truncate(time.Minute)
			row := fmt.Sprintf("%-26s  %s/%s  %d turns  %s ago",
				s.ID, s.Provider, s.Model, s.TurnCount, age)
			if i == m.resumeSelected {
				lines = append(lines, sText.Render("→ "+row))
			} else {
				lines = append(lines, sHint.Render("  "+row))
			}
		}
		lines = append(lines, "")
	}

	// Streaming: show frozen thinking above live text content
	if m.streaming {
		maxWidth := m.width - 2
		if m.thinkingBuf.Len() > 0 {
			// Thinking is frozen once text starts; show dim with hollow diamond.
			// Cap at 3 lines while streaming (ctrl+o expands).
			const liveThinkMax = 3
			thinkLines := strings.Split(wrapText(m.thinkingBuf.String(), maxWidth), "\n")
			showN := len(thinkLines)
			if !m.expandOutput && showN > liveThinkMax {
				showN = liveThinkMax
			}
			for i, line := range thinkLines[:showN] {
				if i == 0 {
					lines = append(lines, sThinkingLabel.Render("◇ ")+sThinkingBody.Render(line))
				} else {
					lines = append(lines, sThinkingBody.Render("  "+line))
				}
			}
			if !m.expandOutput && len(thinkLines) > liveThinkMax {
				lines = append(lines, sHint.Render(fmt.Sprintf("  +%d lines (ctrl+o to expand)", len(thinkLines)-liveThinkMax)))
			}
		}
		if m.streamBuf.Len() > 0 {
			// Regular text content — strip model artifacts before display
			liveText := sanitizeAssistantText(m.streamBuf.String())
			for i, line := range strings.Split(wrapText(liveText, maxWidth), "\n") {
				if i == 0 {
					lines = append(lines, styleAssistantLabel.Render("◆ ")+line)
				} else {
					lines = append(lines, "  "+line)
				}
			}
		} else if m.thinkingBuf.Len() == 0 {
			lines = append(lines, styleAssistantLabel.Render("◆ ")+sCursor.Render("█"))
		}
	}

	// Join all logical lines then split by newlines
	raw := strings.Join(lines, "\n")
	rawLines := strings.Split(raw, "\n")

	// Hard-wrap any remaining overlong lines to get accurate physical line count
	// for the scroll logic. Content should already be word-wrapped by renderMessage,
	// but ANSI escape overhead can push a styled line past m.width.
	var physLines []string
	for _, line := range rawLines {
		if lipgloss.Width(line) <= m.width {
			physLines = append(physLines, line)
		} else {
			// Actually split the line using ANSI-aware hard wrap so the scroll
			// offset math and the rendered content agree.
			split := strings.Split(xansi.Hardwrap(line, m.width, false), "\n")
			physLines = append(physLines, split...)
		}
	}

	// Apply scroll: offset from bottom
	if len(physLines) > height && height > 0 {
		maxScroll := len(physLines) - height
		offset := m.scrollOffset
		if offset > maxScroll {
			offset = maxScroll
		}
		end := len(physLines) - offset
		start := end - height
		if start < 0 {
			start = 0
		}
		physLines = physLines[start:end]
	}

	// Hard truncate to exactly height lines — prevent overflow
	if len(physLines) > height && height > 0 {
		physLines = physLines[:height]
	}

	content := strings.Join(physLines, "\n")

	// Pad to fill height if content is shorter
	contentH := strings.Count(content, "\n") + 1
	if contentH < height {
		content += strings.Repeat("\n", height-contentH)
	}

	return content
}

func (m Model) renderMessage(msg chatMessage) []string {
	var lines []string
	indent := "  " // 2-space indent for continuation lines

	switch msg.role {
	case "user":
		// ❯ first line, indented continuation — word-wrapped to terminal width
		maxWidth := m.width - 2 // 2 for the "❯ " / "  " prefix
		msgLines := strings.Split(wrapText(msg.content, maxWidth), "\n")
		for i, line := range msgLines {
			if i == 0 {
				lines = append(lines, sUserLabel.Render("❯ ")+sUserLabel.Render(line))
			} else {
				lines = append(lines, sUserLabel.Render(indent+line))
			}
		}
		lines = append(lines, "")

	case "thinking":
		// Thinking/reasoning content — dim italic with hollow diamond label.
		// Collapsed to 3 lines by default; ctrl+o expands.
		const thinkingMaxLines = 3
		maxWidth := m.width - 2
		msgLines := strings.Split(wrapText(msg.content, maxWidth), "\n")
		showLines := len(msgLines)
		if !m.expandOutput && showLines > thinkingMaxLines {
			showLines = thinkingMaxLines
		}
		for i, line := range msgLines[:showLines] {
			if i == 0 {
				lines = append(lines, sThinkingLabel.Render("◇ ")+sThinkingBody.Render(line))
			} else {
				lines = append(lines, sThinkingBody.Render(indent+line))
			}
		}
		if !m.expandOutput && len(msgLines) > thinkingMaxLines {
			remaining := len(msgLines) - thinkingMaxLines
			lines = append(lines, sHint.Render(indent+fmt.Sprintf("+%d lines (ctrl+o to expand)", remaining)))
		}
		lines = append(lines, "")

	case "assistant":
		// Render markdown with glamour; strip model-specific artifacts first.
		clean := sanitizeAssistantText(msg.content)
		rendered := clean
		if m.mdRenderer != nil {
			if md, err := m.mdRenderer.Render(clean); err == nil {
				rendered = strings.TrimSpace(md)
			}
		}
		renderedLines := strings.Split(rendered, "\n")
		for i, line := range renderedLines {
			if i == 0 {
				lines = append(lines, styleAssistantLabel.Render("◆ ")+line)
			} else {
				lines = append(lines, indent+line)
			}
		}
		lines = append(lines, "")

	case "tool":
		maxW := m.width - len([]rune(indent))
		for _, line := range strings.Split(wrapText(msg.content, maxW), "\n") {
			lines = append(lines, indent+sToolOutput.Render(line))
		}

	case "toolresult":
		resultLines := strings.Split(msg.content, "\n")
		maxShow := 10
		if m.expandOutput {
			maxShow = len(resultLines) // show all
		}
		maxW := m.width - 4 // indent(2) + indent(2)
		for i, line := range resultLines {
			if i >= maxShow {
				remaining := len(resultLines) - maxShow
				lines = append(lines, indent+indent+sHint.Render(
					fmt.Sprintf("+%d lines (ctrl+o to expand)", remaining)))
				break
			}
			// Wrap this logical line into sub-lines, then diff-color each sub-line
			for _, subLine := range strings.Split(wrapText(line, maxW), "\n") {
				trimmed := strings.TrimSpace(subLine)
				if strings.HasPrefix(trimmed, "+") && !strings.HasPrefix(trimmed, "++") && len(trimmed) > 1 {
					lines = append(lines, indent+indent+sDiffAdd.Render(subLine))
				} else if strings.HasPrefix(trimmed, "-") && !strings.HasPrefix(trimmed, "--") && len(trimmed) > 1 {
					lines = append(lines, indent+indent+sDiffRemove.Render(subLine))
				} else {
					lines = append(lines, indent+indent+sToolResult.Render(subLine))
				}
			}
		}
		lines = append(lines, "")

	case "system":
		maxW := m.width - 4 // "• "(2) + indent(2)
		for i, line := range strings.Split(wrapText(msg.content, maxW), "\n") {
			if i == 0 {
				lines = append(lines, sSystem.Render("• "+line))
			} else {
				lines = append(lines, sSystem.Render(indent+line))
			}
		}
		lines = append(lines, "")

	case "error":
		maxW := m.width - 2 // "✗ " = 2
		for _, line := range strings.Split(wrapText(msg.content, maxW), "\n") {
			lines = append(lines, sError.Render("✗ "+line))
		}
		lines = append(lines, "")
	}

	return lines
}

func (m Model) renderElfTree() []string {
	if len(m.elfOrder) == 0 {
		return nil
	}

	var lines []string

	// Count running vs done
	running := 0
	for _, id := range m.elfOrder {
		if p, ok := m.elfStates[id]; ok && !p.Done {
			running++
		}
	}

	// Header
	if running > 0 {
		header := fmt.Sprintf("● Running %d elf", len(m.elfOrder))
		if len(m.elfOrder) != 1 {
			header += "s"
		}
		header += "…"
		lines = append(lines, sStatusStreaming.Render(header))
	} else {
		header := fmt.Sprintf("● %d elf", len(m.elfOrder))
		if len(m.elfOrder) != 1 {
			header += "s"
		}
		header += " completed"
		lines = append(lines, sToolOutput.Render(header))
	}

	for i, elfID := range m.elfOrder {
		p, ok := m.elfStates[elfID]
		if !ok {
			continue
		}

		isLast := i == len(m.elfOrder)-1

		// Branch character
		branch := "├─"
		childPrefix := "│  "
		if isLast {
			branch = "└─"
			childPrefix = "   "
		}

		// Main line: branch + description + stats
		var stats []string
		if p.ToolUses > 0 {
			stats = append(stats, fmt.Sprintf("%d tool uses", p.ToolUses))
		}
		if p.Tokens > 0 {
			stats = append(stats, formatTokens(p.Tokens))
		}

		statsStr := ""
		if len(stats) > 0 {
			statsStr = " · " + strings.Join(stats, " · ")
		}
		desc := p.Description
		if len(statsStr) > 0 {
			// Truncate description so the combined line fits on one terminal row
			maxDescW := m.width - 4 - len([]rune(branch)) - len([]rune(statsStr))
			if maxDescW > 10 && len([]rune(desc)) > maxDescW {
				desc = string([]rune(desc)[:maxDescW-1]) + "…"
			}
		}
		line := sToolOutput.Render(branch+" ") + sText.Render(desc)
		if len(statsStr) > 0 {
			line += sToolResult.Render(statsStr)
		}
		lines = append(lines, line)

		// Activity sub-line
		var activity string
		if p.Done {
			if p.Error != "" {
				activity = sError.Render("Error: " + p.Error)
			} else {
				dur := p.Duration.Round(time.Millisecond)
				activity = sToolOutput.Render(fmt.Sprintf("Done (%s)", dur))
			}
		} else {
			activity = p.Activity
			if activity == "" {
				activity = "working…"
			}
			activity = sToolResult.Render(activity)
		}
		// Wrap activity so long error/path strings don't overflow the terminal.
		actPrefix := childPrefix + "└─ "
		actMaxW := m.width - len([]rune(actPrefix))
		actLines := strings.Split(wrapText(activity, actMaxW), "\n")
		for j, al := range actLines {
			if j == 0 {
				lines = append(lines, sToolResult.Render(actPrefix)+al)
			} else {
				lines = append(lines, sToolResult.Render(childPrefix+"   ")+al)
			}
		}
	}

	lines = append(lines, "") // spacing after tree
	return lines
}

func formatTokens(tokens int) string {
	if tokens >= 1_000_000 {
		return fmt.Sprintf("%.1fM tokens", float64(tokens)/1_000_000)
	}
	if tokens >= 1_000 {
		return fmt.Sprintf("%.1fk tokens", float64(tokens)/1_000)
	}
	return fmt.Sprintf("%d tokens", tokens)
}

func (m Model) renderSeparators() (string, string) {
	lineColor := cSurface // default dim
	modeLabel := ""

	if m.config.Permissions != nil {
		mode := m.config.Permissions.Mode()
		lineColor = ModeColor(mode)
		modeLabel = string(mode)
	}

	// Incognito adds amber overlay but keeps mode visible
	if m.incognito {
		lineColor = cYellow
		modeLabel = "🔒 " + modeLabel
	}

	// Permission pending — flash the line with command summary
	if m.permPending {
		lineColor = cRed
		hint := shortPermHint(m.permToolName, m.permArgs)
		modeLabel = "⚠ " + hint + " [y/n]"
	}

	lineStyle := lipgloss.NewStyle().Foreground(lineColor)
	labelStyle := lipgloss.NewStyle().Foreground(lineColor).Bold(true)

	// Top line: ─── with mode label on right ─── bypass ───
	label := " " + modeLabel + " "
	labelW := lipgloss.Width(labelStyle.Render(label))
	lineW := m.width - labelW
	if lineW < 4 {
		lineW = 4
	}
	leftW := lineW - 2
	rightW := 2

	topLine := lineStyle.Render(strings.Repeat("─", leftW)) +
		labelStyle.Render(label) +
		lineStyle.Render(strings.Repeat("─", rightW))

	// Bottom line: plain colored line
	bottomLine := lineStyle.Render(strings.Repeat("─", m.width))

	return topLine, bottomLine
}

func (m Model) renderInput() string {
	return m.input.View()
}

func (m Model) renderStatus() string {
	status := m.session.Status()

	// Left: provider + model + incognito
	provModel := fmt.Sprintf(" %s/%s", status.Provider, status.Model)
	if m.incognito {
		provModel += " " + sStatusIncognito.Render("🔒")
	}
	left := sStatusHighlight.Render(provModel)

	// Center: cwd + git branch
	dir := filepath.Base(m.cwd)
	centerParts := []string{"📁 " + dir}
	if m.gitBranch != "" {
		centerParts = append(centerParts, sStatusBranch.Render(" "+m.gitBranch))
	}
	center := sStatusDim.Render(strings.Join(centerParts, ""))

	// Right: tokens with state color + turns
	tokenStr := fmt.Sprintf("tokens: %d", status.TokensUsed)
	if status.TokenPercent > 0 {
		tokenStr = fmt.Sprintf("tokens: %d (%d%%)", status.TokensUsed, status.TokenPercent)
	}
	var tokenStyle lipgloss.Style
	switch status.TokenState {
	case "warning":
		tokenStyle = lipgloss.NewStyle().Foreground(cYellow)
	case "critical":
		tokenStyle = lipgloss.NewStyle().Foreground(cRed).Bold(true)
	default:
		tokenStyle = sStatusDim
	}
	right := tokenStyle.Render(tokenStr) + sStatusDim.Render(fmt.Sprintf(" │ turns: %d ", status.TurnCount))

	if m.quitHint {
		right = lipgloss.NewStyle().Foreground(cRed).Bold(true).Render("ctrl+c to quit ") + sStatusDim.Render("│ ") + right
	}
	if m.copyMode {
		right = lipgloss.NewStyle().Foreground(cYellow).Bold(true).Render("✂ COPY ") + sStatusDim.Render("│ ") + right
	}
	if m.streaming {
		right = sStatusStreaming.Render("● streaming ") + sStatusDim.Render("│ ") + right
	}

	// Compose with spacing
	leftW := lipgloss.Width(left)
	centerW := lipgloss.Width(center)
	rightW := lipgloss.Width(right)

	gap1 := (m.width-leftW-centerW-rightW)/2 - 1
	if gap1 < 1 {
		gap1 = 1
	}
	gap2 := m.width - leftW - gap1 - centerW - rightW
	if gap2 < 0 {
		gap2 = 0
	}

	bar := left + strings.Repeat(" ", gap1) + center + strings.Repeat(" ", gap2) + right
	return sStatusBar.Width(m.width).Render(bar)
}

// wrapText word-wraps text at word boundaries, preserving existing newlines.
// Uses ANSI-aware wrapping so lipgloss-styled text is measured correctly.
func wrapText(text string, width int) string {
	if width <= 0 {
		return text
	}
	return xansi.Wordwrap(text, width, "")
}

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
		if t := msg.TextContent(); t != "" {
			m.messages = append(m.messages, chatMessage{
				role:    string(msg.Role),
				content: t,
			})
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

// extractMarkdownDoc strips any narrative preamble before the first # heading
// and returns the markdown portion. Returns "" if no heading is found.
func extractMarkdownDoc(s string) string {
	for _, line := range strings.Split(s, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			// Found the first heading — return everything from here
			idx := strings.Index(s, line)
			return strings.TrimSpace(s[idx:])
		}
	}
	return ""
}

// looksLikeAgentsMD returns true if s appears to be a real markdown document
// (not a refusal or planning response): substantial length and at least one
// section heading.
func looksLikeAgentsMD(s string) bool {
	return len(s) >= 300 && strings.Contains(s, "##")
}

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
	{"<<tool_code>>", "<<</tool_code>>"},   // some model variants
	{"<<function_call>>", "<tool_call|>"},  // Gemma function-call format
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
			var openLen, closeLen int
			var chosenClose string
			for _, pair := range modelBlockPairs {
				idx := strings.Index(text, pair[0])
				if idx >= 0 && (earliest < 0 || idx < earliest) {
					earliest = idx
					openLen = len(pair[0])
					chosenClose = pair[1]
					closeLen = len(chosenClose)
					_ = closeLen
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

// initPrompt builds the prompt sent to the LLM for /init.
// existingPath is the absolute path to an existing AGENTS.md, or "" if none exists.
// The 3 base elfs always run. When existingPath is set, a 4th elf reads the current file.
// The LLM is free to spawn additional elfs if it identifies gaps.
func initPrompt(root, existingPath string) string {
	baseElfs := fmt.Sprintf(`IMPORTANT: Use only fs.ls, fs.glob, fs.grep, and fs.read for all analysis. Do NOT use bash — it will be denied and will cause you to fail. Your first action must be spawn_elfs.

Use spawn_elfs to analyze the project in parallel. Spawn at least these elfs simultaneously:

- Elf 1 (task_type: "explain"): Explore project structure at %s.
  - Run fs.ls on root and every immediate subdirectory.
  - Read go.mod (or package.json/Cargo.toml/pyproject.toml): extract module path, Go/runtime version, and key external dependencies with exact import paths. List TUI/UI framework deps (e.g. charm.land/*, tview) separately from backend/LLM deps.
  - Read Makefile or build scripts: note targets beyond the standard (build/test/lint/fmt/vet/clean/tidy/install). Note non-standard flags, multi-step sequences, or env vars they require.
  - Read existing AI config files if present: CLAUDE.md, .cursor/rules, .cursorrules, .github/copilot-instructions.md, .gnoma/GNOMA.md. These will be loaded at runtime — do NOT copy their content into AGENTS.md. Only note what topics they cover so the synthesis step knows what to skip.
  - Build a domain glossary: read the primary type-definition files in these packages (use fs.ls to find them): internal/message, internal/engine, internal/router, internal/elf, internal/provider, internal/context, internal/security, internal/session. For each exported type, struct, or interface whose name would be ambiguous or non-obvious to an outside AI, add a one-line entry: Name → what it is in this project. Specifically look for: Arm, Turn, Elf, Accumulator, Firewall, LimitPool, TaskType, Incognito, Stream, Event, Session, Router. Do not list generic config struct fields.
  - Report: module path, runtime version, non-standard Makefile targets only (skip standard ones: build/test/lint/cover/fmt/vet/clean/tidy/install/run), full dependency list (TUI + backend separated), domain glossary.

- Elf 2 (task_type: "explain"): Discover non-standard code conventions at %s.
  - Use fs.glob **/*.go (or language equivalent) to find source files. Read at least 8 files spanning different packages — prefer non-trivial ones (engine, provider, tool implementations, tests).
  - Use fs.grep to locate each pattern below. NEVER use internal/tui as a source for code examples — it is application glue, not where idioms live. For each match found: read the file, then paste the relevant lines with the file path as the first comment (e.g. '// internal/foo/bar.go'). If fs.grep returns no matches outside internal/tui, omit that pattern entirely. Do NOT invent or paraphrase.
    * new(expr): fs.grep '= new(' across **/*.go, exclude internal/tui
    * errors.AsType: fs.grep 'errors.AsType' across **/*.go
    * WaitGroup.Go: fs.grep '\.Go(func' across **/*.go
    * testing/synctest: fs.grep 'synctest' across **/*.go
    * Discriminated union: fs.grep 'Content\|EventType\|ContentType' across internal/message, internal/stream — look for a struct with a Type field switched on by callers
    * Pull-based iterator: fs.grep 'func.*Next\(\)' across **/*.go — look for Next/Current/Err/Close pattern
    * json.RawMessage passthrough: fs.grep 'json.RawMessage' across internal/tool — find a Parameters() or Execute() signature
    * errgroup: fs.grep 'errgroup' across **/*.go
    * Channel semaphore: fs.grep 'chan struct{}' across **/*.go, look for concurrency-limiting usage
  - Error handling: fs.grep 'var Err' across **/*.go — paste a real sentinel definition. fs.grep 'fmt.Errorf' across **/*.go and look for error-wrapping calls — paste a real one. File path required on each.
  - Test conventions: fs.grep '//go:build' across **/*_test.go for build tags. fs.grep 't\.Helper()' across **/*_test.go for helper convention. fs.grep 't\.TempDir()' across **/*_test.go. Paste one real example each with file path.
  - Report ONLY what differs from standard language knowledge. Skip obvious conventions.

- Elf 3 (task_type: "explain"): Extract setup requirements and gotchas at %s.
  - Read README.md, CONTRIBUTING.md, docs/ contents if they exist.
  - Find required environment variables: use fs.grep to search for os.Getenv and os.LookupEnv across all .go files. List every unique variable name found and what it configures based on surrounding context. Also check .env.example if it exists.
  - Note non-obvious setup steps (token scopes, local service dependencies, build prerequisites not in the Makefile).
  - Note repo etiquette ONLY if not already covered by CLAUDE.md — skip commit format and co-signing if CLAUDE.md documents them.
  - Note architectural gotchas explicitly called out in comments or docs — skip generic advice.
  - Skip anything obvious for a project of this type.`, root, root, root)

	synthRules := fmt.Sprintf(`After all elfs complete, you may spawn additional focused elfs with agent tool if specific gaps need investigation.

Then synthesize and write AGENTS.md to %s/AGENTS.md using fs.write.

CRITICAL RULE — DO NOT DUPLICATE LOADED FILES:
CLAUDE.md (and other AI config files) are loaded directly into the AI's context at runtime.
Writing their content into AGENTS.md is pure noise — it will be read twice and adds nothing.
AGENTS.md must only contain information those files do not already cover.
If CLAUDE.md thoroughly covers a topic (e.g. Go style, commit format, provider list), skip it.

QUALITY TEST: Before writing each line — would removing this cause an AI assistant to make a mistake on this codebase? If no, cut it.

INCLUDE (only if not already in CLAUDE.md or equivalent):
- Module path and key dependencies with exact import paths (especially non-obvious or private ones)
- Build/test commands the AI cannot guess from manifest files alone (non-standard targets, flags, sequences)
- Language-version-specific idioms in use: e.g. Go 1.26 new(expr), errors.AsType, WaitGroup.Go; show code examples
- Non-standard type patterns: discriminated unions, pull-based iterators, json.RawMessage passthrough — with examples
- Domain terminology: project-specific names that differ from industry-standard meanings
- Testing quirks: build tags, helper conventions, concurrency test tools, mock policy
- Required env var names and what they configure (not "see .env.example" — list them)
- Non-obvious architectural constraints or gotchas not derivable from reading the code

EXCLUDE:
- Anything already documented in CLAUDE.md or other AI config files that will be loaded at runtime
- File-by-file directory listing (discoverable via fs.ls)
- Standard language conventions the AI already knows
- Generic advice ("write clean code", "handle errors", "use descriptive names")
- Standard Makefile/build targets (build, test, lint, cover, fmt, vet, clean, tidy, install, run) — do not list them at all, not even as a summary line; only write non-standard targets
- The "Standard Targets: ..." line itself — it adds nothing and must not appear
- Planned features not yet in code
- Vague statements ("see config files for details", "follow project conventions") — include the actual detail or nothing

Do not fabricate. Only write what was observed in files you actually read.
Format: terse directive-style bullets. Short code examples where the pattern is non-obvious. No prose paragraphs.`, root)

	if existingPath != "" {
		return fmt.Sprintf(`You are updating the AGENTS.md project documentation file for the project at %s.

%s
- Elf 4 (task_type: "review"): Read the existing AGENTS.md at %s.
  - For each section: accurate (keep), stale (update), missing (add), bloat (cut — fails quality test).
  - Specifically flag: anything duplicated from CLAUDE.md or other loaded AI config files (remove it), fabricated content (remove it), and missing language-version-specific idioms.
  - Report a structured diff: keep / update / add / remove.

%s

When updating: tighten as well as correct. Remove duplication and bloat even if it was in the old version.`,
			root, baseElfs, existingPath, synthRules)
	}

	return fmt.Sprintf(`You are creating an AGENTS.md project documentation file for the project at %s.

%s

%s`, root, baseElfs, synthRules)
}

// loadAgentsMD reads AGENTS.md from disk and appends it to the context window prefix.
func (m Model) loadAgentsMD() Model {
	root := gnomacfg.ProjectRoot()
	path := filepath.Join(root, "AGENTS.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return m
	}
	if m.config.Engine != nil {
		if w := m.config.Engine.ContextWindow(); w != nil {
			w.AddPrefix(
				message.NewUserText(fmt.Sprintf("[Project docs: AGENTS.md]\n\n%s", string(data))),
				message.NewAssistantText("I've read the project documentation and will follow these guidelines."),
			)
		}
	}
	m.messages = append(m.messages, chatMessage{role: "system",
		content: fmt.Sprintf("AGENTS.md written to %s — loaded into context for this session.", path)})
	return m
}

// injectSystemContext adds a message to the engine's conversation history
// so the model sees it as context in subsequent turns.
func (m Model) injectSystemContext(text string) {
	if m.config.Engine != nil {
		m.config.Engine.InjectMessage(message.NewUserText("[system] " + text))
		// Immediately follow with a synthetic assistant acknowledgment
		// so the conversation stays in user→assistant alternation
		m.config.Engine.InjectMessage(message.NewAssistantText("Understood."))
	}
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
			Path string `json:"file_path"`
		}
		if json.Unmarshal(args, &a) == nil && a.Path != "" {
			detail = a.Path
		}
	case "fs.edit", "fs_edit":
		var a struct {
			Path string `json:"file_path"`
		}
		if json.Unmarshal(args, &a) == nil && a.Path != "" {
			detail = a.Path
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

func detectGitBranch() string {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
