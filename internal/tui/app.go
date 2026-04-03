package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"
	"somegit.dev/Owlibou/gnoma/internal/engine"
	"somegit.dev/Owlibou/gnoma/internal/message"
	"somegit.dev/Owlibou/gnoma/internal/permission"
	"somegit.dev/Owlibou/gnoma/internal/router"
	"somegit.dev/Owlibou/gnoma/internal/security"
	"somegit.dev/Owlibou/gnoma/internal/session"
	"somegit.dev/Owlibou/gnoma/internal/stream"
)

const version = "v0.1.0-dev"

type streamEventMsg struct{ event stream.Event }
type turnDoneMsg struct{ err error }
type permReqMsg struct{ toolName string }

type chatMessage struct {
	role    string
	content string
}

// Config holds optional dependencies for TUI features.
type Config struct {
	Firewall    *security.Firewall    // for incognito toggle
	Engine      *engine.Engine        // for model switching
	Permissions *permission.Checker   // for mode switching
	Router      *router.Router       // for model listing
	PermCh      chan bool             // TUI → engine: y/n response
	PermReqCh   <-chan string         // engine → TUI: tool name needing approval
}

type Model struct {
	session session.Session
	config  Config
	width   int
	height  int

	messages    []chatMessage
	streaming   bool
	streamBuf   *strings.Builder
	currentRole string

	input          textarea.Model
	cwd            string
	gitBranch      string
	scrollOffset   int
	incognito      bool
	permPending    bool   // waiting for user to approve/deny a tool
	permToolName   string // which tool is asking
}

func New(sess session.Session, cfg Config) Model {
	ti := textarea.New()
	ti.Placeholder = "Type a message... (Enter to send, Shift+Enter for newline)"
	ti.Prompt = "❯ "
	ti.ShowLineNumbers = false
	ti.SetHeight(1)
	ti.MaxHeight = 10
	ti.SetWidth(80)
	ti.CharLimit = 0

	// Remap: Shift+Enter/Ctrl+J for newline (not plain Enter)
	km := ti.KeyMap
	km.InsertNewline = key.NewBinding(key.WithKeys("shift+enter", "ctrl+j"))
	ti.KeyMap = km

	ti.Focus()

	cwd, _ := os.Getwd()
	gitBranch := detectGitBranch()

	return Model{
		session:   sess,
		config:    cfg,
		input:     ti,
		cwd:       cwd,
		gitBranch: gitBranch,
		streamBuf: &strings.Builder{},
	}
}

func (m Model) Init() tea.Cmd {
	return m.input.Focus()
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.input.SetWidth(m.width - 4)
		return m, nil

	case tea.KeyMsg:
		// Handle permission prompt Y/N
		if m.permPending {
			switch strings.ToLower(msg.String()) {
			case "y":
				m.permPending = false
				m.messages = append(m.messages, chatMessage{role: "system",
					content: fmt.Sprintf("✓ %s approved", m.permToolName)})
				m.config.PermCh <- true
				return m, m.listenForEvents() // continue listening
			case "n", "escape":
				m.permPending = false
				m.messages = append(m.messages, chatMessage{role: "system",
					content: fmt.Sprintf("✗ %s denied", m.permToolName)})
				m.config.PermCh <- false
				return m, m.listenForEvents() // continue listening
			}
			return m, nil // ignore other keys while prompting
		}

		switch msg.String() {
		case "ctrl+c":
			if m.streaming {
				m.session.Cancel()
				return m, nil
			}
			return m, tea.Quit
		case "escape":
			if m.streaming {
				m.session.Cancel()
				return m, nil
			}
		case "ctrl+x":
			// Toggle incognito
			if m.config.Firewall != nil {
				m.incognito = m.config.Firewall.Incognito().Toggle()
				var msg string
				if m.incognito {
					msg = "🔒 incognito ON — no persistence, no learning, no logging"
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

	case permReqMsg:
		m.permPending = true
		m.permToolName = msg.toolName
		m.messages = append(m.messages, chatMessage{role: "system",
			content: fmt.Sprintf("⚠ %s wants to execute. Allow? [y/n]", msg.toolName)})
		m.scrollOffset = 0
		return m, nil

	case streamEventMsg:
		return m.handleStreamEvent(msg.event)

	case turnDoneMsg:
		m.streaming = false
		m.scrollOffset = 0 // snap to bottom on turn complete
		if m.streamBuf.Len() > 0 {
			m.messages = append(m.messages, chatMessage{
				role: m.currentRole, content: m.streamBuf.String(),
			})
			m.streamBuf.Reset()
		}
		if msg.err != nil {
			m.messages = append(m.messages, chatMessage{
				role: "error", content: msg.err.Error(),
			})
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

	case "/clear":
		m.messages = nil
		m.scrollOffset = 0
		return m, nil

	case "/incognito":
		if m.config.Firewall != nil {
			m.incognito = m.config.Firewall.Incognito().Toggle()
			if m.incognito {
				m.messages = append(m.messages, chatMessage{role: "system",
					content: "🔒 incognito mode ON — no persistence, no learning, no content logging"})
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
				b.WriteString("\nAvailable arms:\n")
				for _, arm := range m.config.Router.Arms() {
					marker := "  "
					if string(arm.ID) == status.Provider+"/"+status.Model {
						marker = "→ "
					}
					caps := ""
					if arm.Capabilities.ToolUse {
						caps += "tools "
					}
					if arm.Capabilities.Thinking {
						caps += "thinking "
					}
					if arm.Capabilities.Vision {
						caps += "vision "
					}
					local := ""
					if arm.IsLocal {
						local = " (local)"
					}
					fmt.Fprintf(&b, "%s%s  [%s]%s\n", marker, arm.ID, strings.TrimSpace(caps), local)
				}
			}
			b.WriteString("\nUsage: /model <model-name>")
			m.messages = append(m.messages, chatMessage{role: "system", content: b.String()})
			return m, nil
		}
		if m.config.Engine != nil {
			m.config.Engine.SetModel(args)
			m.messages = append(m.messages, chatMessage{role: "system",
				content: fmt.Sprintf("model switched to: %s", args)})
		}
		return m, nil

	case "/config":
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
		m.messages = append(m.messages, chatMessage{role: "system", content: b.String()})
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

	case "/help":
		m.messages = append(m.messages, chatMessage{role: "system",
			content: "Commands:\n  /clear              clear chat\n  /config             show current config\n  /incognito          toggle incognito (Ctrl+X)\n  /model [name]       list/switch models\n  /permission [mode]  set permission mode (Shift+Tab to cycle)\n  /provider           show current provider\n  /shell              interactive shell (coming soon)\n  /help               show this help\n  /quit               exit gnoma"})
		return m, nil

	default:
		m.messages = append(m.messages, chatMessage{role: "error",
			content: fmt.Sprintf("unknown command: %s (try /help)", command)})
		return m, nil
	}
}

func (m Model) handleStreamEvent(evt stream.Event) (tea.Model, tea.Cmd) {
	switch evt.Type {
	case stream.EventTextDelta:
		if evt.Text != "" {
			m.streamBuf.WriteString(evt.Text)
		}
	case stream.EventThinkingDelta:
		m.streamBuf.WriteString(evt.Text)
	case stream.EventToolCallStart:
		if m.streamBuf.Len() > 0 {
			m.messages = append(m.messages, chatMessage{role: m.currentRole, content: m.streamBuf.String()})
			m.streamBuf.Reset()
		}
	case stream.EventToolCallDone:
		m.messages = append(m.messages, chatMessage{
			role: "tool", content: fmt.Sprintf("⚙ [%s] running...", evt.ToolCallName),
		})
	case stream.EventToolResult:
		m.messages = append(m.messages, chatMessage{
			role: "toolresult", content: evt.ToolOutput,
		})
	}
	return m, m.listenForEvents()
}

func (m Model) listenForEvents() tea.Cmd {
	ch := m.session.Events()
	permReqCh := m.config.PermReqCh

	return func() tea.Msg {
		// Listen for both stream events and permission requests
		if permReqCh != nil {
			select {
			case evt, ok := <-ch:
				if !ok {
					_, err := m.session.TurnResult()
					return turnDoneMsg{err: err}
				}
				return streamEventMsg{event: evt}
			case toolName := <-permReqCh:
				return permReqMsg{toolName: toolName}
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

	// Auto-size textarea based on content
	lines := strings.Count(m.input.Value(), "\n") + 1
	if lines < 1 {
		lines = 1
	}
	if lines > 10 {
		lines = 10
	}
	m.input.SetHeight(lines)

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
	v.MouseMode = tea.MouseModeCellMotion
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

	// Streaming
	if m.streaming && m.streamBuf.Len() > 0 {
		wrapped := wrapText(m.streamBuf.String(), m.width-8)
		for _, line := range strings.Split(wrapped, "\n") {
			lines = append(lines, "    "+line)
		}
	} else if m.streaming {
		lines = append(lines, "    "+sCursor.Render("█"))
	}

	// Join all logical lines then split by newlines
	raw := strings.Join(lines, "\n")
	rawLines := strings.Split(raw, "\n")

	// Hard-wrap each line to terminal width to get accurate physical line count
	var physLines []string
	for _, line := range rawLines {
		// Strip ANSI to measure visible width, but keep original for rendering
		visible := lipgloss.Width(line)
		if visible <= m.width {
			physLines = append(physLines, line)
		} else {
			// Line wraps — split into chunks of terminal width
			// Use simple rune-based splitting (ANSI-aware wrapping is complex,
			// so we just let it wrap naturally and count approximate lines)
			wrappedCount := (visible + m.width - 1) / m.width
			physLines = append(physLines, line) // the line itself
			// Account for the extra wrapped lines
			for i := 1; i < wrappedCount; i++ {
				physLines = append(physLines, "") // placeholder for wrapped overflow
			}
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
	w := m.width - 8

	switch msg.role {
	case "user":
		lines = append(lines, sUserLabel.Render("❯ ")+sUserLabel.Render(msg.content))
		lines = append(lines, "")

	case "assistant":
		wrapped := wrapText(msg.content, w)
		for _, line := range strings.Split(wrapped, "\n") {
			lines = append(lines, "    "+line)
		}
		lines = append(lines, "")

	case "tool":
		lines = append(lines, "    "+sToolOutput.Render(msg.content))

	case "toolresult":
		// Render tool output as indented code block
		for _, line := range strings.Split(msg.content, "\n") {
			lines = append(lines, "      "+sToolResult.Render(line))
		}
		lines = append(lines, "")

	case "system":
		lines = append(lines, "    "+sSystem.Render("• "+msg.content))
		lines = append(lines, "")

	case "error":
		lines = append(lines, "    "+sError.Render("✗ "+msg.content))
		lines = append(lines, "")
	}

	return lines
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

	// Permission pending — flash the line
	if m.permPending {
		lineColor = cRed
		modeLabel = "⚠ " + m.permToolName + " [y/n]"
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
	return "  " + m.input.View()
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

	// Right: stats
	right := sStatusDim.Render(
		fmt.Sprintf("tokens: %d │ turns: %d ", status.TokensUsed, status.TurnCount),
	)

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

func wrapText(text string, width int) string {
	if width <= 0 {
		return text
	}
	var result strings.Builder
	for i, line := range strings.Split(text, "\n") {
		if i > 0 {
			result.WriteByte('\n')
		}
		if len(line) <= width {
			result.WriteString(line)
			continue
		}
		words := strings.Fields(line)
		lineLen := 0
		for _, word := range words {
			if lineLen+len(word)+1 > width && lineLen > 0 {
				result.WriteByte('\n')
				lineLen = 0
			} else if lineLen > 0 {
				result.WriteByte(' ')
				lineLen++
			}
			result.WriteString(word)
			lineLen += len(word)
		}
	}
	return result.String()
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

func detectGitBranch() string {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
