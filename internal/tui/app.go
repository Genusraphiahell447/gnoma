package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"somegit.dev/Owlibou/gnoma/internal/session"
	"somegit.dev/Owlibou/gnoma/internal/stream"
)

// streamEventMsg wraps a stream event for the Bubble Tea message system.
type streamEventMsg struct {
	event stream.Event
}

// turnDoneMsg signals that a turn is complete.
type turnDoneMsg struct {
	err error
}

// Model is the Bubble Tea application model.
type Model struct {
	session session.Session
	width   int
	height  int

	// Chat history
	messages []chatMessage
	// Current streaming response
	streaming    bool
	streamBuf    strings.Builder
	currentRole  string

	// Input
	input      string
	inputCursor int

	// Status
	ready bool
	err   error
}

type chatMessage struct {
	role    string // "user", "assistant", "tool", "error"
	content string
}

// New creates a new TUI model.
func New(sess session.Session) Model {
	return Model{
		session: sess,
		ready:   true,
	}
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case streamEventMsg:
		return m.handleStreamEvent(msg.event)

	case turnDoneMsg:
		m.streaming = false
		if m.streamBuf.Len() > 0 {
			m.messages = append(m.messages, chatMessage{
				role:    m.currentRole,
				content: m.streamBuf.String(),
			})
			m.streamBuf.Reset()
		}
		if msg.err != nil {
			m.messages = append(m.messages, chatMessage{
				role:    "error",
				content: msg.err.Error(),
			})
		}
		return m, nil
	}

	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		if m.streaming {
			m.session.Cancel()
			return m, nil
		}
		return m, tea.Quit

	case "enter":
		if m.streaming || strings.TrimSpace(m.input) == "" {
			return m, nil
		}
		return m.submitInput()

	case "backspace":
		if len(m.input) > 0 {
			m.input = m.input[:len(m.input)-1]
		}
		return m, nil

	default:
		// Type characters
		if len(msg.String()) == 1 || msg.String() == " " {
			m.input += msg.String()
		}
		return m, nil
	}
}

func (m Model) submitInput() (tea.Model, tea.Cmd) {
	input := strings.TrimSpace(m.input)
	m.input = ""

	// Handle slash commands
	if strings.HasPrefix(input, "/") {
		return m.handleCommand(input)
	}

	// Add user message to chat
	m.messages = append(m.messages, chatMessage{role: "user", content: input})
	m.streaming = true
	m.currentRole = "assistant"
	m.streamBuf.Reset()

	// Send to session
	if err := m.session.Send(input); err != nil {
		m.messages = append(m.messages, chatMessage{role: "error", content: err.Error()})
		m.streaming = false
		return m, nil
	}

	// Start listening for events
	return m, m.listenForEvents()
}

func (m Model) handleCommand(cmd string) (tea.Model, tea.Cmd) {
	switch {
	case cmd == "/quit" || cmd == "/exit":
		return m, tea.Quit
	case cmd == "/clear":
		m.messages = nil
		return m, nil
	case cmd == "/incognito":
		m.messages = append(m.messages, chatMessage{role: "tool", content: "incognito toggle (not yet wired)"})
		return m, nil
	default:
		m.messages = append(m.messages, chatMessage{role: "error", content: fmt.Sprintf("unknown command: %s", cmd)})
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
		// Show thinking in dimmed text
		m.streamBuf.WriteString(evt.Text)
	case stream.EventToolCallStart:
		// Flush current streaming text
		if m.streamBuf.Len() > 0 {
			m.messages = append(m.messages, chatMessage{role: m.currentRole, content: m.streamBuf.String()})
			m.streamBuf.Reset()
		}
	case stream.EventToolCallDone:
		m.messages = append(m.messages, chatMessage{
			role:    "tool",
			content: fmt.Sprintf("[%s] calling...", evt.ToolCallName),
		})
	}
	return m, m.listenForEvents()
}

func (m Model) listenForEvents() tea.Cmd {
	ch := m.session.Events()
	return func() tea.Msg {
		evt, ok := <-ch
		if !ok {
			// Channel closed — turn is done
			_, err := m.session.TurnResult()
			return turnDoneMsg{err: err}
		}
		return streamEventMsg{event: evt}
	}
}

func (m Model) View() tea.View {
	if m.width == 0 {
		return tea.NewView("loading...")
	}

	// Layout: chat area + input + status bar
	statusHeight := 1
	inputHeight := 3
	chatHeight := m.height - statusHeight - inputHeight

	chat := m.renderChat(chatHeight)
	input := m.renderInput()
	status := m.renderStatus()

	return tea.NewView(lipgloss.JoinVertical(lipgloss.Left, chat, input, status))
}

func (m Model) renderChat(height int) string {
	var lines []string

	for _, msg := range m.messages {
		switch msg.role {
		case "user":
			lines = append(lines, styleUserLabel.Render("you: ")+msg.content)
		case "assistant":
			lines = append(lines, styleAssistantLabel.Render("gnoma: ")+msg.content)
		case "tool":
			lines = append(lines, styleToolOutput.Render("  "+msg.content))
		case "error":
			lines = append(lines, styleError.Render("error: "+msg.content))
		}
	}

	// Show streaming buffer
	if m.streaming && m.streamBuf.Len() > 0 {
		lines = append(lines, styleAssistantLabel.Render("gnoma: ")+m.streamBuf.String()+"▊")
	} else if m.streaming {
		lines = append(lines, styleAssistantLabel.Render("gnoma: ")+"▊")
	}

	if len(lines) == 0 {
		lines = append(lines, styleHint.Render("  Type a message and press Enter. /quit to exit."))
	}

	content := strings.Join(lines, "\n")

	// Scroll to bottom — show last N lines
	contentLines := strings.Split(content, "\n")
	if len(contentLines) > height {
		contentLines = contentLines[len(contentLines)-height:]
	}

	return lipgloss.NewStyle().
		Width(m.width).
		Height(height).
		Render(strings.Join(contentLines, "\n"))
}

func (m Model) renderInput() string {
	prompt := "❯ "
	cursor := ""
	if !m.streaming {
		cursor = "▏"
	}
	content := prompt + m.input + cursor

	return styleInputBorder.
		Width(m.width - 4).
		Render(content)
}

func (m Model) renderStatus() string {
	status := m.session.Status()

	parts := []string{
		styleStatusProvider.Render(fmt.Sprintf(" %s/%s", status.Provider, status.Model)),
		fmt.Sprintf("tokens: %d", status.TokensUsed),
		fmt.Sprintf("turns: %d", status.TurnCount),
	}

	if status.State == session.StateStreaming {
		parts = append(parts, "streaming...")
	}

	return styleStatusBar.
		Width(m.width).
		Render(strings.Join(parts, " │ "))
}
