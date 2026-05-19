package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	xansi "github.com/charmbracelet/x/ansi"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"somegit.dev/Owlibou/gnoma/internal/message"
	"somegit.dev/Owlibou/gnoma/internal/session"
)

func (m Model) View() tea.View {
	if m.width == 0 {
		return tea.NewView("")
	}

	status := m.renderStatus()
	input := m.renderInput()
	topLine, bottomLine := m.renderSeparators()

	// Suggestion dropdown — rendered between topLine and input.
	suggestions := ""
	if len(m.suggestions) > 0 {
		suggestions = m.renderSuggestions()
	}
	suggestH := lipgloss.Height(suggestions)

	// Fixed: status bar + separator + input + separator + suggestions = bottom area
	statusH := lipgloss.Height(status)
	inputH := lipgloss.Height(input)
	chatH := m.height - statusH - inputH - 2 - suggestH

	chat := m.renderChat(chatH)

	// Show "new content below" indicator when scrolled up during streaming
	if m.scrollOffset > 0 && m.streaming {
		indicator := lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true).Render("  ⬇ new content below")
		topLine = indicator + topLine[len(indicator):]
	}

	parts := []string{chat, topLine}
	if suggestions != "" {
		parts = append(parts, suggestions)
	}
	parts = append(parts, input, bottomLine, status)

	v := tea.NewView(lipgloss.JoinVertical(lipgloss.Left, parts...))
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

	// Settings panel (/config)
	if m.configPanelOpen {
		lines = append(lines, m.renderConfigPanel(m.width)...)
	}

	// Transient: session resume picker
	if m.resumePending && len(m.resumeSessions) > 0 {
		lines = append(lines, "")
		lines = append(lines, sSystem.Render("  Sessions  ↑↓ · Enter to load · Esc to cancel"))
		lines = append(lines, "")
		for i, s := range m.resumeSessions {
			age := time.Since(s.UpdatedAt).Truncate(time.Minute)
			label := s.ID
			if s.Title != "" {
				label = s.Title
				if len(label) > 40 {
					label = label[:40] + "…"
				}
			}
			row := fmt.Sprintf("%-42s  %s/%s  %d turns  %s ago",
				label, s.Provider, s.Model, s.TurnCount, age)
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
		maxShow := 3 // collapsed by default
		if m.expandOutput {
			maxShow = len(resultLines)
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

	case "cost":
		lines = append(lines, sHint.Render(indent+msg.content))
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

	bottomLine := lineStyle.Render(strings.Repeat("─", m.width))

	return topLine, bottomLine
}

func (m Model) renderInput() string {
	view := m.input.View()
	// Ghost text only when there's no dropdown (dropdown handles completion when visible)
	if m.suggestion != "" && len(m.suggestions) == 0 {
		rest := strings.TrimPrefix(m.suggestion, m.input.Value())
		if rest != "" {
			ghost := lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render(rest + " (tab)")
			view += ghost
		}
	}
	return view
}

func (m Model) renderStatus() string {
	status := m.session.Status()

	// Left: provider + model + incognito
	provModel := fmt.Sprintf(" %s/%s", status.Provider, status.Model)
	if m.incognito {
		provModel += " " + sStatusIncognito.Render("🔒")
	}
	if !status.ToolsAvailable {
		provModel += " " + sStatusDim.Render("text-only")
	}
	left := sStatusHighlight.Render(provModel) + renderSLMBadge(m.config.SLM)

	// Center: cwd + git branch
	dir := filepath.Base(m.cwd)
	centerParts := []string{"📁 " + dir}
	if m.gitBranch != "" {
		centerParts = append(centerParts, sStatusBranch.Render(" "+m.gitBranch))
	}
	center := sStatusDim.Render(strings.Join(centerParts, ""))

	// Right: context bar + tokens + turns
	right := renderContextBar(status) + sStatusDim.Render(fmt.Sprintf(" │ turns: %d ", status.TurnCount))

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

// renderContextBar draws a compact [████░░░░] 45% progress bar for the context window.
func renderContextBar(s session.Status) string {
	pct := s.TokenPercent
	if pct <= 0 && s.TokensUsed == 0 {
		return sStatusDim.Render("ctx: —")
	}

	const barWidth = 8
	filled := (pct * barWidth) / 100
	if filled > barWidth {
		filled = barWidth
	}
	empty := barWidth - filled

	var barColor lipgloss.Style
	switch s.TokenState {
	case "critical":
		barColor = lipgloss.NewStyle().Foreground(cRed).Bold(true)
	case "warning":
		barColor = lipgloss.NewStyle().Foreground(cYellow)
	default:
		barColor = lipgloss.NewStyle().Foreground(lipgloss.Color("42")) // green
	}
	dimBlock := sStatusDim

	bar := barColor.Render(strings.Repeat("█", filled)) + dimBlock.Render(strings.Repeat("░", empty))
	label := fmt.Sprintf(" %d%%", pct)

	var labelStyle lipgloss.Style
	switch s.TokenState {
	case "critical":
		labelStyle = lipgloss.NewStyle().Foreground(cRed).Bold(true)
	case "warning":
		labelStyle = lipgloss.NewStyle().Foreground(cYellow)
	default:
		labelStyle = sStatusDim
	}
	return "[" + bar + "]" + labelStyle.Render(label)
}

// renderSLMBadge produces a short " · slm: <model> [tools]" badge for the
// status bar's left side. Returns "" when SLM is disabled or unconfigured.
func renderSLMBadge(info SLMInfo) string {
	if !info.Enabled {
		return ""
	}
	if !info.Active {
		return sStatusDim.Render(" · slm: ✗")
	}
	label := " · slm: " + info.Model
	if info.Tools {
		label += " ⚙"
	}
	return sStatusDim.Render(label)
}

// formatTurnUsage produces a compact token summary for a single turn.
func formatTurnUsage(u message.Usage) string {
	parts := []string{fmt.Sprintf("in: %d", u.InputTokens), fmt.Sprintf("out: %d", u.OutputTokens)}
	if u.CacheReadTokens > 0 {
		parts = append(parts, fmt.Sprintf("cache: %d", u.CacheReadTokens))
	}
	return strings.Join(parts, " · ")
}

// renderSuggestions renders the slash-command autocomplete dropdown.
func (m Model) renderSuggestions() string {
	const maxVisible = 6

	sCmd := lipgloss.NewStyle().Foreground(cPurple).Bold(true)
	sDesc := lipgloss.NewStyle().Foreground(cSubtext)
	sSelectedCmd := lipgloss.NewStyle().
		Background(cSurface).
		Foreground(cPurple).
		Bold(true)
	sSelectedDesc := lipgloss.NewStyle().
		Background(cSurface).
		Foreground(cText)

	// Determine visible window around selected item
	start := 0
	end := len(m.suggestions)
	if end > maxVisible {
		start = m.suggIdx - maxVisible/2
		if start < 0 {
			start = 0
		}
		if start+maxVisible > len(m.suggestions) {
			start = len(m.suggestions) - maxVisible
		}
		end = start + maxVisible
	}

	// Measure widest command name for alignment
	maxCmdW := 0
	for _, s := range m.suggestions[start:end] {
		if len(s.name) > maxCmdW {
			maxCmdW = len(s.name)
		}
	}

	innerW := m.width - 6
	if innerW < 40 {
		innerW = 40
	}

	var bodyLines []string
	for i, entry := range m.suggestions[start:end] {
		idx := start + i
		pad := strings.Repeat(" ", maxCmdW-len(entry.name)+1)
		desc := entry.desc
		// Truncate desc to fit
		maxDescW := innerW - maxCmdW - 2
		if maxDescW > 0 && len(desc) > maxDescW {
			desc = desc[:maxDescW-1] + "…"
		}
		if idx == m.suggIdx {
			line := sSelectedCmd.Render(entry.name) + sSelectedDesc.Render(pad+desc)
			// Pad to fill width
			lineW := lipgloss.Width(line)
			if lineW < innerW {
				line += sSelectedDesc.Render(strings.Repeat(" ", innerW-lineW))
			}
			bodyLines = append(bodyLines, line)
		} else {
			bodyLines = append(bodyLines, sCmd.Render(entry.name)+sDesc.Render(pad+desc))
		}
	}
	if len(m.suggestions) > maxVisible {
		extra := len(m.suggestions) - maxVisible
		bodyLines = append(bodyLines, sHint.Render(fmt.Sprintf("  +%d more", extra)))
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(cSurface).
		Padding(0, 1).
		Width(innerW + 2).
		Render(strings.Join(bodyLines, "\n"))

	return box
}

// renderConfigPanel renders the interactive /config settings overlay.
func (m Model) renderConfigPanel(width int) []string {
	rtr := m.config.Router
	perm := m.config.Permissions

	// Build the three setting rows
	type row struct{ label, value string }
	rows := make([]row, 3)

	// Row 0: Model
	modelVal := "none"
	if rtr != nil {
		forced := rtr.ForcedArm()
		if forced != "" {
			modelVal = string(forced)
		} else {
			arms := configPanelArms(rtr.Arms())
			if len(arms) > 0 {
				modelVal = string(arms[0].ID) + "  (press Enter to select)"
			} else {
				modelVal = "none discovered"
			}
		}
	}
	rows[0] = row{"Model", modelVal}

	// Row 1: Permission
	permVal := "—"
	if perm != nil {
		permVal = string(perm.Mode())
	}
	rows[1] = row{"Permission", permVal}

	// Row 2: Incognito
	incogVal := "Off"
	if m.incognito {
		incogVal = "On"
	}
	rows[2] = row{"Incognito", incogVal}

	// Measure widest label for alignment
	maxLabel := 0
	for _, r := range rows {
		if len(r.label) > maxLabel {
			maxLabel = len(r.label)
		}
	}

	sSelected := lipgloss.NewStyle().
		Background(cTeal).
		Foreground(cMantle).
		Bold(true)
	sItem := lipgloss.NewStyle().Foreground(cText)
	sLabel := lipgloss.NewStyle().Foreground(cSubtext)

	innerW := width - 8 // border(2) + padding(2) each side = 8
	if innerW < 30 {
		innerW = 30
	}

	var bodyLines []string
	for i, r := range rows {
		labelPad := strings.Repeat(" ", maxLabel-len(r.label))
		if i == m.configSelected {
			line := fmt.Sprintf("%s%s: %s", r.label, labelPad, r.value)
			// Pad to full inner width so the highlight fills the row
			if lipgloss.Width(line) < innerW {
				line += strings.Repeat(" ", innerW-lipgloss.Width(line))
			}
			bodyLines = append(bodyLines, sSelected.Render(line))
		} else {
			bodyLines = append(bodyLines, sLabel.Render(r.label+labelPad+": ")+sItem.Render(r.value))
		}
	}
	bodyLines = append(bodyLines, "")
	bodyLines = append(bodyLines, sHint.Render("↑↓ Navigate  Enter Select/Toggle  Esc Exit"))

	body := strings.Join(bodyLines, "\n")

	titleStyle := lipgloss.NewStyle().Foreground(cTeal).Bold(true)
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(cTeal).
		Padding(0, 1).
		Width(innerW + 2) // +2 for padding

	box := boxStyle.Render(titleStyle.Render("Settings") + "\n\n" + body)

	return []string{"", box, ""}
}

// wrapText word-wraps text at word boundaries, preserving existing newlines.
// Uses ANSI-aware wrapping so lipgloss-styled text is measured correctly.
func wrapText(text string, width int) string {
	if width <= 0 {
		return text
	}
	return xansi.Wordwrap(text, width, "")
}
