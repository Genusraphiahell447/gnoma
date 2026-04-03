package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
	"somegit.dev/Owlibou/gnoma/internal/engine"
	"somegit.dev/Owlibou/gnoma/internal/permission"
	"somegit.dev/Owlibou/gnoma/internal/security"
	"somegit.dev/Owlibou/gnoma/internal/session"
	"somegit.dev/Owlibou/gnoma/internal/stream"
)

const version = "v0.1.0-dev"

type streamEventMsg struct{ event stream.Event }
type turnDoneMsg struct{ err error }

type chatMessage struct {
	role    string
	content string
}

// Config holds optional dependencies for TUI features.
type Config struct {
	Firewall    *security.Firewall    // for incognito toggle
	Engine      *engine.Engine        // for model switching
	Permissions *permission.Checker   // for mode switching
}

type Model struct {
	session session.Session
	config  Config
	width   int
	height  int

	messages    []chatMessage
	streaming   bool
	streamBuf   strings.Builder
	currentRole string

	input        textinput.Model
	cwd          string
	gitBranch    string
	scrollOffset int
	incognito    bool
}

func New(sess session.Session, cfg Config) Model {
	ti := textinput.New()
	ti.Placeholder = ""
	ti.Prompt = "❯ "
	ti.Focus()
	ti.SetWidth(80)

	cwd, _ := os.Getwd()
	gitBranch := detectGitBranch()

	return Model{
		session:   sess,
		config:    cfg,
		input:     ti,
		cwd:       cwd,
		gitBranch: gitBranch,
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
				m.messages = append(m.messages, chatMessage{role: "system",
					content: fmt.Sprintf("permission mode: %s", next)})
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
			m.messages = append(m.messages, chatMessage{role: "system",
				content: fmt.Sprintf("current model: %s/%s\nUsage: /model <model-name>", status.Provider, status.Model)})
			return m, nil
		}
		if m.config.Engine != nil {
			m.config.Engine.SetModel(args)
			m.messages = append(m.messages, chatMessage{role: "system",
				content: fmt.Sprintf("model switched to: %s", args)})
		}
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
		m.messages = append(m.messages, chatMessage{role: "system",
			content: fmt.Sprintf("permission mode: %s", mode)})
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
			content: "Commands:\n  /clear            clear chat\n  /incognito        toggle incognito mode\n  /model <name>     switch model\n  /provider <name>  show/switch provider\n  /help             show this help\n  /quit             exit gnoma"})
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
	return func() tea.Msg {
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
	// Get mode color
	lineColor := cSurface // default dim
	modeLabel := ""

	if m.config.Permissions != nil {
		mode := m.config.Permissions.Mode()
		lineColor = ModeColor(mode)
		modeLabel = string(mode)
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

	// Center: cwd + git branch + perm mode
	dir := filepath.Base(m.cwd)
	centerParts := []string{"📁 " + dir}
	if m.config.Permissions != nil {
		mode := string(m.config.Permissions.Mode())
		centerParts = append(centerParts, sStatusDim.Render(" 🛡 "+mode))
	}
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

func detectGitBranch() string {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
