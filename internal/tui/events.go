package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"somegit.dev/Owlibou/gnoma/internal/message"
	"somegit.dev/Owlibou/gnoma/internal/stream"
)

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
	case stream.EventRouting:
		content := fmt.Sprintf("routed → %s (task: %s", evt.RoutingModel, evt.RoutingTask)
		if evt.RoutingClassifier != "" {
			content += ", by: " + evt.RoutingClassifier
		}
		content += ")"
		m.messages = append(m.messages, chatMessage{role: "cost", content: content})
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
					turn, err := m.session.TurnResult()
					var usage message.Usage
					if turn != nil {
						usage = turn.Usage
					}
					return turnDoneMsg{err: err, usage: usage}
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
			turn, err := m.session.TurnResult()
			var usage message.Usage
			if turn != nil {
				usage = turn.Usage
			}
			return turnDoneMsg{err: err, usage: usage}
		}
		return streamEventMsg{event: evt}
	}
}
