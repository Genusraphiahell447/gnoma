package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
	"somegit.dev/Owlibou/gnoma/internal/session"
	"somegit.dev/Owlibou/gnoma/internal/stream"
)

type streamEventMsg struct{ event stream.Event }
type turnDoneMsg struct{ err error }

type chatMessage struct {
	role    string // "user", "assistant", "tool", "error"
	content string
}

// Model is the Bubble Tea application model.
type Model struct {
	session session.Session
	width   int
	height  int

	messages    []chatMessage
	streaming   bool
	streamBuf   strings.Builder
	currentRole string

	input textinput.Model
	err   error
}

func New(sess session.Session) Model {
	ti := textinput.New()
	ti.Placeholder = "Type a message... (Enter to send, Ctrl+C to quit)"
	ti.Prompt = "❯ "
	ti.Focus()
	ti.SetWidth(80)

	return Model{
		session: sess,
		input:   ti,
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
		m.input.SetWidth(m.width - 6)
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			if m.streaming {
				m.session.Cancel()
				return m, nil
			}
			return m, tea.Quit

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

	case streamEventMsg:
		return m.handleStreamEvent(msg.event)

	case turnDoneMsg:
		m.streaming = false
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

	// Forward to textinput
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

func (m Model) submitInput(input string) (tea.Model, tea.Cmd) {
	// Slash commands
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
	switch {
	case cmd == "/quit" || cmd == "/exit" || cmd == "/q":
		return m, tea.Quit
	case cmd == "/clear":
		m.messages = nil
		return m, nil
	case cmd == "/incognito":
		m.messages = append(m.messages, chatMessage{
			role: "tool", content: "  incognito mode toggled (wiring pending)",
		})
		return m, nil
	default:
		m.messages = append(m.messages, chatMessage{
			role: "error", content: fmt.Sprintf("unknown command: %s", cmd),
		})
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
			m.messages = append(m.messages, chatMessage{
				role: m.currentRole, content: m.streamBuf.String(),
			})
			m.streamBuf.Reset()
		}
	case stream.EventToolCallDone:
		m.messages = append(m.messages, chatMessage{
			role: "tool", content: fmt.Sprintf("  [%s] executing...", evt.ToolCallName),
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

func (m Model) View() tea.View {
	if m.width == 0 {
		return tea.NewView("")
	}

	statusH := 1
	inputH := 1
	separatorH := 1
	chatH := m.height - statusH - inputH - separatorH - 1

	chat := m.renderChat(chatH)
	separator := styleSeperator.Width(m.width).Render(strings.Repeat("─", m.width))
	input := m.renderInput()
	status := m.renderStatus()

	return tea.NewView(lipgloss.JoinVertical(lipgloss.Left,
		chat,
		separator,
		input,
		status,
	))
}

func (m Model) renderChat(height int) string {
	var lines []string

	for _, msg := range m.messages {
		switch msg.role {
		case "user":
			lines = append(lines, styleUserLabel.Render("  ❯ ")+styleUserText.Render(msg.content))
		case "assistant":
			wrapped := wrapText(msg.content, m.width-6)
			for i, line := range strings.Split(wrapped, "\n") {
				if i == 0 {
					lines = append(lines, styleAssistantLabel.Render("  ◆ ")+line)
				} else {
					lines = append(lines, "    "+line)
				}
			}
		case "tool":
			lines = append(lines, styleToolOutput.Render(msg.content))
		case "error":
			lines = append(lines, styleError.Render("  ✗ "+msg.content))
		}
		lines = append(lines, "") // blank line between messages
	}

	// Streaming buffer
	if m.streaming && m.streamBuf.Len() > 0 {
		wrapped := wrapText(m.streamBuf.String(), m.width-6)
		first := true
		for _, line := range strings.Split(wrapped, "\n") {
			if first {
				lines = append(lines, styleAssistantLabel.Render("  ◆ ")+line)
				first = false
			} else {
				lines = append(lines, "    "+line)
			}
		}
		lines = append(lines, styleCursor.Render("    ▊"))
	} else if m.streaming {
		lines = append(lines, styleAssistantLabel.Render("  ◆ ")+styleCursor.Render("▊"))
	}

	// Empty state
	if len(lines) == 0 {
		lines = append(lines, "")
		lines = append(lines, styleHint.Render("    gnoma — provider-agnostic coding assistant"))
		lines = append(lines, "")
		lines = append(lines, styleHint.Render("    Type a message and press Enter."))
		lines = append(lines, styleHint.Render("    /quit to exit, /clear to reset, Ctrl+C to cancel."))
	}

	// Scroll to bottom
	allLines := strings.Split(strings.Join(lines, "\n"), "\n")
	if len(allLines) > height {
		allLines = allLines[len(allLines)-height:]
	}

	return lipgloss.NewStyle().
		Width(m.width).
		Height(height).
		Render(strings.Join(allLines, "\n"))
}

func (m Model) renderInput() string {
	return "  " + m.input.View()
}

func (m Model) renderStatus() string {
	status := m.session.Status()

	left := styleStatusProvider.Render(
		fmt.Sprintf(" %s/%s", status.Provider, status.Model),
	)

	right := fmt.Sprintf("tokens: %d │ turns: %d ", status.TokensUsed, status.TurnCount)

	if m.streaming {
		right = styleStatusStreaming.Render("● streaming ") + "│ " + right
	}

	// Pad middle
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 0 {
		gap = 0
	}
	middle := strings.Repeat(" ", gap)

	return styleStatusBar.Width(m.width).Render(left + middle + right)
}

func wrapText(text string, width int) string {
	if width <= 0 {
		return text
	}
	var result strings.Builder
	for _, line := range strings.Split(text, "\n") {
		if len(line) <= width {
			if result.Len() > 0 {
				result.WriteByte('\n')
			}
			result.WriteString(line)
			continue
		}
		// Simple word wrap
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
		if result.Len() > 0 && !strings.HasSuffix(result.String(), "\n") {
			// don't add extra newline
		}
	}
	return result.String()
}
