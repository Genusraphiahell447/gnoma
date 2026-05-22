package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
	var modelInfo string
	if m.config.SLM.Active {
		modelInfo = fmt.Sprintf("router (slm: %s)", m.config.SLM.Model)
	} else {
		modelInfo = fmt.Sprintf("%s/%s", status.Provider, status.Model)
	}

	lines = append(lines,
		theme().sHeaderBrand.Render(" gnoma ")+"  "+theme().sHeaderDim.Render("gnoma "+version),
		"    "+theme().sHeaderModel.Render(modelInfo)+
			theme().sHeaderDim.Render(" · ")+theme().sHeaderDim.Render(m.shortCwd()),
		"",
	)

	if len(m.messages) == 0 && !m.streaming {
		lines = append(lines,
			theme().sHint.Render("    Type a message and press Enter."),
			theme().sHint.Render("    /help for commands, Ctrl+C to cancel or quit."),
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
		lines = append(lines, "  "+theme().sToolOutput.Render(fmt.Sprintf("⚙ [%s] running...", name)))
	}

	// Transient: permission prompt (disappear when approved/denied)
	if m.permPending {
		lines = append(lines, m.renderPermissionPending(m.width)...)
	}

	// Settings panel (/config)
	if m.configPanelOpen {
		lines = append(lines, m.renderConfigPanel(m.width)...)
	}

	// Model Picker
	if m.modelPickerOpen {
		lines = append(lines, m.renderModelPicker(m.width)...)
	}

	// Provider Picker
	if m.providerPickerOpen {
		lines = append(lines, m.renderProviderPicker(m.width)...)
	}

	// Profile Picker
	if m.profilePickerOpen {
		lines = append(lines, m.renderProfilePicker(m.width)...)
	}

	// Skills Picker
	if m.skillsPickerOpen {
		lines = append(lines, m.renderSkillsPicker(m.width)...)
	}

	// Plugins Picker
	if m.pluginsPickerOpen {
		lines = append(lines, m.renderPluginsPicker(m.width)...)
	}

	// Theme Picker
	if m.themePickerOpen {
		lines = append(lines, m.renderThemePicker(m.width)...)
	}

	// Help Picker
	if m.helpPickerOpen {
		lines = append(lines, m.renderHelpPicker(m.width)...)
	}

	// Transient: session resume picker
	if m.resumePending && len(m.resumeSessions) > 0 {
		lines = append(lines, m.renderResumePicker(m.width)...)
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
					lines = append(lines, theme().sThinkingLabel.Render("◇ ")+theme().sThinkingBody.Render(line))
				} else {
					lines = append(lines, theme().sThinkingBody.Render("  "+line))
				}
			}
			if !m.expandOutput && len(thinkLines) > liveThinkMax {
				lines = append(lines, theme().sHint.Render(fmt.Sprintf("  +%d lines (ctrl+o to expand)", len(thinkLines)-liveThinkMax)))
			}
		}
		if m.streamBuf.Len() > 0 {
			// Regular text content — strip model artifacts before display
			liveText := sanitizeAssistantText(m.streamBuf.String())
			for i, line := range strings.Split(wrapText(liveText, maxWidth), "\n") {
				if i == 0 {
					lines = append(lines, theme().styleAssistantLabel.Render("◆ ")+line)
				} else {
					lines = append(lines, "  "+line)
				}
			}
		} else if m.thinkingBuf.Len() == 0 {
			lines = append(lines, theme().styleAssistantLabel.Render("◆ ")+theme().sCursor.Render("█"))
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
				lines = append(lines, theme().sUserLabel.Render("❯ ")+theme().sUserLabel.Render(line))
			} else {
				lines = append(lines, theme().sUserLabel.Render(indent+line))
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
				lines = append(lines, theme().sThinkingLabel.Render("◇ ")+theme().sThinkingBody.Render(line))
			} else {
				lines = append(lines, theme().sThinkingBody.Render(indent+line))
			}
		}
		if !m.expandOutput && len(msgLines) > thinkingMaxLines {
			remaining := len(msgLines) - thinkingMaxLines
			lines = append(lines, theme().sHint.Render(indent+fmt.Sprintf("+%d lines (ctrl+o to expand)", remaining)))
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
				lines = append(lines, theme().styleAssistantLabel.Render("◆ ")+line)
			} else {
				lines = append(lines, indent+line)
			}
		}
		lines = append(lines, "")

	case "tool":
		maxW := m.width - len([]rune(indent))
		for _, line := range strings.Split(wrapText(msg.content, maxW), "\n") {
			lines = append(lines, indent+theme().sToolOutput.Render(line))
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
				lines = append(lines, indent+indent+theme().sHint.Render(
					fmt.Sprintf("+%d lines (ctrl+o to expand)", remaining)))
				break
			}
			// Wrap this logical line into sub-lines, then diff-color each sub-line
			for _, subLine := range strings.Split(wrapText(line, maxW), "\n") {
				trimmed := strings.TrimSpace(subLine)
				if strings.HasPrefix(trimmed, "+") && !strings.HasPrefix(trimmed, "++") && len(trimmed) > 1 {
					lines = append(lines, indent+indent+theme().sDiffAdd.Render(subLine))
				} else if strings.HasPrefix(trimmed, "-") && !strings.HasPrefix(trimmed, "--") && len(trimmed) > 1 {
					lines = append(lines, indent+indent+theme().sDiffRemove.Render(subLine))
				} else {
					lines = append(lines, indent+indent+theme().sToolResult.Render(subLine))
				}
			}
		}
		lines = append(lines, "")

	case "system":
		maxW := m.width - 4 // "• "(2) + indent(2)
		for i, line := range strings.Split(wrapText(msg.content, maxW), "\n") {
			if i == 0 {
				lines = append(lines, theme().sSystem.Render("• "+line))
			} else {
				lines = append(lines, theme().sSystem.Render(indent+line))
			}
		}
		lines = append(lines, "")

	case "error":
		maxW := m.width - 2 // "✗ " = 2
		for _, line := range strings.Split(wrapText(msg.content, maxW), "\n") {
			lines = append(lines, theme().sError.Render("✗ "+line))
		}
		lines = append(lines, "")

	case "cost":
		lines = append(lines, theme().sHint.Render(indent+msg.content))
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
		lines = append(lines, theme().sStatusStreaming.Render(header))
	} else {
		header := fmt.Sprintf("● %d elf", len(m.elfOrder))
		if len(m.elfOrder) != 1 {
			header += "s"
		}
		header += " completed"
		lines = append(lines, theme().sToolOutput.Render(header))
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
		line := theme().sToolOutput.Render(branch+" ") + theme().sText.Render(desc)
		if len(statsStr) > 0 {
			line += theme().sToolResult.Render(statsStr)
		}
		lines = append(lines, line)

		// Activity sub-line
		var activity string
		if p.Done {
			if p.Error != "" {
				activity = theme().sError.Render("Error: " + p.Error)
			} else {
				dur := p.Duration.Round(time.Millisecond)
				activity = theme().sToolOutput.Render(fmt.Sprintf("Done (%s)", dur))
			}
		} else {
			activity = p.Activity
			if activity == "" {
				activity = "working…"
			}
			activity = theme().sToolResult.Render(activity)
		}
		// Wrap activity so long error/path strings don't overflow the terminal.
		actPrefix := childPrefix + "└─ "
		actMaxW := m.width - len([]rune(actPrefix))
		actLines := strings.Split(wrapText(activity, actMaxW), "\n")
		for j, al := range actLines {
			if j == 0 {
				lines = append(lines, theme().sToolResult.Render(actPrefix)+al)
			} else {
				lines = append(lines, theme().sToolResult.Render(childPrefix+"   ")+al)
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
	lineColor := theme().cSurface // default dim
	modeLabel := ""

	if m.config.Permissions != nil {
		mode := m.config.Permissions.Mode()
		lineColor = ModeColor(mode)
	}

	// Incognito adds amber overlay
	if m.incognito {
		lineColor = theme().cYellow
	}

	// Permission pending — flash the line with command summary
	if m.permPending {
		lineColor = theme().cRed
		hint := shortPermHint(m.permToolName, m.permArgs)
		modeLabel = "⚠ " + hint + " [y/n]"
	}

	lineStyle := lipgloss.NewStyle().Foreground(lineColor)
	labelStyle := lipgloss.NewStyle().Foreground(lineColor).Bold(true)

	// Top line: ─── with mode label on right if any (like permission pending hint)
	var topLine string
	if modeLabel != "" {
		label := " " + modeLabel + " "
		labelW := lipgloss.Width(labelStyle.Render(label))
		lineW := m.width - labelW
		if lineW < 4 {
			lineW = 4
		}
		leftW := lineW - 2
		rightW := 2
		topLine = lineStyle.Render(strings.Repeat("─", leftW)) +
			labelStyle.Render(label) +
			lineStyle.Render(strings.Repeat("─", rightW))
	} else {
		topLine = lineStyle.Render(strings.Repeat("─", m.width))
	}

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
	var provModel string
	if m.config.SLM.Active {
		provModel = " router"
	} else {
		provModel = fmt.Sprintf(" %s/%s", status.Provider, status.Model)
	}
	if m.incognito {
		provModel += " " + theme().sStatusIncognito.Render("🔒")
	}
	if !status.ToolsAvailable {
		provModel += " " + theme().sStatusDim.Render("text-only")
	}

	modeStr := ""
	if m.config.Permissions != nil {
		mode := m.config.Permissions.Mode()
		modeColor := ModeColor(mode)
		modeStyle := lipgloss.NewStyle().
			Background(modeColor).
			Foreground(theme().cMantle).
			Bold(true).
			Padding(0, 1)
		modeStr = " " + modeStyle.Render(strings.ToUpper(string(mode)))
	}

	left := theme().sStatusHighlight.Render(provModel) + modeStr + renderSLMBadge(m.config.SLM) + renderProfileBadge(m.config.Profile)

	// Center: cwd + git branch
	dir := filepath.Base(m.cwd)
	centerParts := []string{"📁 " + dir}
	if m.gitBranch != "" {
		centerParts = append(centerParts, theme().sStatusBranch.Render(" "+m.gitBranch))
	}
	center := theme().sStatusDim.Render(strings.Join(centerParts, ""))

	// Right: context bar + tokens + turns
	right := renderContextBar(status) + theme().sStatusDim.Render(fmt.Sprintf(" │ turns: %d ", status.TurnCount))

	if m.quitHint {
		right = lipgloss.NewStyle().Foreground(theme().cRed).Bold(true).Render("ctrl+c to quit ") + theme().sStatusDim.Render("│ ") + right
	}
	if m.copyMode {
		right = lipgloss.NewStyle().Foreground(theme().cYellow).Bold(true).Render("✂ COPY ") + theme().sStatusDim.Render("│ ") + right
	}
	if m.streaming {
		right = theme().sStatusStreaming.Render("● streaming ") + theme().sStatusDim.Render("│ ") + right
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
	return theme().sStatusBar.Width(m.width).Render(bar)
}

// renderContextBar draws a compact [████░░░░] 45% progress bar for the context window.
func renderContextBar(s session.Status) string {
	pct := s.TokenPercent
	if pct <= 0 && s.TokensUsed == 0 {
		return theme().sStatusDim.Render("ctx: —")
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
		barColor = lipgloss.NewStyle().Foreground(theme().cRed).Bold(true)
	case "warning":
		barColor = lipgloss.NewStyle().Foreground(theme().cYellow)
	default:
		barColor = lipgloss.NewStyle().Foreground(lipgloss.Color("42")) // green
	}
	dimBlock := theme().sStatusDim

	bar := barColor.Render(strings.Repeat("█", filled)) + dimBlock.Render(strings.Repeat("░", empty))
	label := fmt.Sprintf(" %d%%", pct)

	var labelStyle lipgloss.Style
	switch s.TokenState {
	case "critical":
		labelStyle = lipgloss.NewStyle().Foreground(theme().cRed).Bold(true)
	case "warning":
		labelStyle = lipgloss.NewStyle().Foreground(theme().cYellow)
	default:
		labelStyle = theme().sStatusDim
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
		return theme().sStatusDim.Render(" · slm: ✗")
	}
	label := " · slm: " + info.Model
	if info.Tools {
		label += " ⚙"
	}
	return theme().sStatusDim.Render(label)
}

// renderProfileBadge produces " · profile: <name>" for the status bar's
// left side. Returns "" when profile mode is not engaged so legacy
// single-config installations don't carry a "profile: default" badge
// that adds noise without information.
func renderProfileBadge(info ProfileInfo) string {
	if !info.Active || info.Name == "" {
		return ""
	}
	return theme().sStatusDim.Render(" · profile: " + info.Name)
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

	sCmd := lipgloss.NewStyle().Foreground(theme().cPurple).Bold(true)
	sDesc := lipgloss.NewStyle().Foreground(theme().cSubtext)
	sSelectedCmd := lipgloss.NewStyle().
		Background(theme().cSurface).
		Foreground(theme().cPurple).
		Bold(true)
	sSelectedDesc := lipgloss.NewStyle().
		Background(theme().cSurface).
		Foreground(theme().cText)

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
		bodyLines = append(bodyLines, theme().sHint.Render(fmt.Sprintf("  +%d more", extra)))
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme().cSurface).
		Padding(0, 1).
		Width(innerW + 2).
		Render(strings.Join(bodyLines, "\n"))

	return box
}

// renderConfigPanel renders the interactive /config settings overlay.
func (m Model) renderConfigPanel(width int) []string {
	perm := m.config.Permissions

	status := m.session.Status()

	// Build the setting rows
	type row struct{ label, value string }
	var rows []row

	for _, setting := range m.getActiveSettings() {
		switch setting {
		case "provider":
			provVal := "none"
			if status.Provider != "" {
				provVal = status.Provider + "  (press Enter to switch)"
			}
			rows = append(rows, row{"Provider", provVal})
		case "model":
			modelVal := "none"
			if status.Model != "" {
				modelVal = status.Model + "  (press Enter to switch)"
			}
			rows = append(rows, row{"Model", modelVal})
		case "permission":
			permVal := "—"
			if perm != nil {
				permVal = string(perm.Mode())
			}
			rows = append(rows, row{"Permission", permVal})
		case "incognito":
			incogVal := "Off"
			if m.incognito {
				incogVal = "On"
			}
			rows = append(rows, row{"Incognito", incogVal})
		}
	}

	// Measure widest label for alignment
	maxLabel := 0
	for _, r := range rows {
		if len(r.label) > maxLabel {
			maxLabel = len(r.label)
		}
	}

	sSelected := lipgloss.NewStyle().
		Background(theme().cTeal).
		Foreground(theme().cMantle).
		Bold(true)
	sItem := lipgloss.NewStyle().Foreground(theme().cText)
	sLabel := lipgloss.NewStyle().Foreground(theme().cSubtext)

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
	bodyLines = append(bodyLines, theme().sHint.Render("↑↓ Navigate  Enter Select/Toggle  Esc Exit"))

	body := strings.Join(bodyLines, "\n")

	titleStyle := lipgloss.NewStyle().Foreground(theme().cTeal).Bold(true)
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme().cTeal).
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

func (m Model) renderPermissionPending(width int) []string {
	innerW := width - 8
	if innerW < 40 {
		innerW = 40
	}

	var bodyLines []string
	bodyLines = append(bodyLines, lipgloss.NewStyle().Foreground(theme().cRed).Bold(true).Render("Permission Requested"))
	bodyLines = append(bodyLines, "")
	bodyLines = append(bodyLines, fmt.Sprintf("Tool: %s", lipgloss.NewStyle().Foreground(theme().cPurple).Bold(true).Render(m.permToolName)))
	bodyLines = append(bodyLines, "")

	var details []string
	switch m.permToolName {
	case "bash":
		var a struct{ Command string }
		if json.Unmarshal(m.permArgs, &a) == nil && a.Command != "" {
			details = append(details, "Command to run:")
			cmdLines := strings.Split(wrapText(a.Command, innerW-4), "\n")
			for _, cl := range cmdLines {
				details = append(details, "  "+theme().sText.Render(cl))
			}
		}
	case "fs.write", "fs_write":
		var a struct {
			Path    string `json:"file_path"`
			Content string `json:"content"`
		}
		if json.Unmarshal(m.permArgs, &a) == nil && a.Path != "" {
			details = append(details, fmt.Sprintf("Write to: %s", theme().sStatusBranch.Render(a.Path)))
			if a.Content != "" {
				details = append(details, "Preview:")
				previewLines := strings.Split(diffPreviewWrite(a.Content), "\n")
				for _, pl := range previewLines {
					details = append(details, "  "+pl)
				}
			}
		}
	case "fs.edit", "fs_edit":
		var a struct {
			Path      string `json:"file_path"`
			OldString string `json:"old_string"`
			NewString string `json:"new_string"`
		}
		if json.Unmarshal(m.permArgs, &a) == nil && a.Path != "" {
			details = append(details, fmt.Sprintf("Edit: %s", theme().sStatusBranch.Render(a.Path)))
			previewLines := strings.Split(diffPreviewEdit(a.OldString, a.NewString), "\n")
			for _, pl := range previewLines {
				details = append(details, "  "+pl)
			}
		}
	default:
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, m.permArgs, "  ", "  "); err == nil {
			details = append(details, "Arguments:")
			for _, line := range strings.Split(pretty.String(), "\n") {
				details = append(details, "  "+theme().sText.Render(line))
			}
		} else if len(m.permArgs) > 0 {
			details = append(details, "Arguments:", "  "+theme().sText.Render(string(m.permArgs)))
		}
	}

	for _, line := range details {
		bodyLines = append(bodyLines, strings.Split(wrapText(line, innerW), "\n")...)
	}

	bodyLines = append(bodyLines, "")
	bodyLines = append(bodyLines, theme().sHint.Render("y Approve  /  n Deny  /  Esc Cancel"))

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme().cRed).
		Padding(1, 2).
		Width(innerW + 4)

	box := boxStyle.Render(strings.Join(bodyLines, "\n"))
	return []string{"", box, ""}
}

func (m Model) renderModelPicker(width int) []string {
	if m.config.Router == nil {
		return []string{"", "No models configured", ""}
	}
	arms := m.config.Router.Arms()
	sort.Slice(arms, func(i, j int) bool {
		return string(arms[i].ID) < string(arms[j].ID)
	})

	innerW := width - 8
	if innerW < 40 {
		innerW = 40
	}

	titleStyle := lipgloss.NewStyle().Foreground(theme().cTeal).Bold(true)
	sSelected := lipgloss.NewStyle().
		Background(theme().cTeal).
		Foreground(theme().cMantle).
		Bold(true)
	sItem := lipgloss.NewStyle().Foreground(theme().cText)

	var bodyLines []string
	for i, arm := range arms {
		tag := ""
		if arm.IsCLIAgent {
			tag = " [CLI]"
		} else if arm.IsLocal {
			tag = " [Local]"
		}

		lineText := fmt.Sprintf(" %s%s", arm.ID, tag)
		if i == m.pickerSelected {
			if lipgloss.Width(lineText) < innerW {
				lineText += strings.Repeat(" ", innerW-lipgloss.Width(lineText))
			}
			bodyLines = append(bodyLines, sSelected.Render(lineText))
		} else {
			bodyLines = append(bodyLines, sItem.Render(lineText))
		}
	}

	bodyLines = append(bodyLines, "")
	bodyLines = append(bodyLines, theme().sHint.Render("↑/↓ Navigate  /  Enter Select  /  Esc Exit"))

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme().cTeal).
		Padding(0, 1).
		Width(innerW + 2)

	box := boxStyle.Render(titleStyle.Render("Select Model") + "\n\n" + strings.Join(bodyLines, "\n"))
	return []string{"", box, ""}
}

func (m Model) renderProviderPicker(width int) []string {
	providers := m.getAvailableProviders()
	innerW := width - 8
	if innerW < 40 {
		innerW = 40
	}

	titleStyle := lipgloss.NewStyle().Foreground(theme().cTeal).Bold(true)
	sSelected := lipgloss.NewStyle().
		Background(theme().cTeal).
		Foreground(theme().cMantle).
		Bold(true)
	sItem := lipgloss.NewStyle().Foreground(theme().cText)

	var bodyLines []string
	if len(providers) == 0 {
		bodyLines = append(bodyLines, theme().sHint.Render(" No providers configured"))
	} else {
		for i, prov := range providers {
			activeTag := ""
			status := m.session.Status()
			if prov == status.Provider {
				activeTag = " (active)"
			}
			lineText := fmt.Sprintf(" %s%s", prov, activeTag)
			if i == m.pickerSelected {
				if lipgloss.Width(lineText) < innerW {
					lineText += strings.Repeat(" ", innerW-lipgloss.Width(lineText))
				}
				bodyLines = append(bodyLines, sSelected.Render(lineText))
			} else {
				bodyLines = append(bodyLines, sItem.Render(lineText))
			}
		}
	}

	bodyLines = append(bodyLines, "")
	bodyLines = append(bodyLines, theme().sHint.Render("↑/↓ Navigate  /  Enter Select  /  Esc Exit"))

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme().cTeal).
		Padding(0, 1).
		Width(innerW + 2)

	box := boxStyle.Render(titleStyle.Render("Select Provider") + "\n\n" + strings.Join(bodyLines, "\n"))
	return []string{"", box, ""}
}

func (m Model) renderProfilePicker(width int) []string {
	profiles := m.config.ProfileNames
	innerW := width - 8
	if innerW < 40 {
		innerW = 40
	}

	titleStyle := lipgloss.NewStyle().Foreground(theme().cTeal).Bold(true)
	sSelected := lipgloss.NewStyle().
		Background(theme().cTeal).
		Foreground(theme().cMantle).
		Bold(true)
	sItem := lipgloss.NewStyle().Foreground(theme().cText)

	var bodyLines []string
	if len(profiles) == 0 {
		bodyLines = append(bodyLines, theme().sHint.Render(" No profiles configured"))
	} else {
		for i, name := range profiles {
			lineText := " " + name
			if name == m.config.Profile.Name {
				lineText += " (active)"
			}
			if i == m.pickerSelected {
				if lipgloss.Width(lineText) < innerW {
					lineText += strings.Repeat(" ", innerW-lipgloss.Width(lineText))
				}
				bodyLines = append(bodyLines, sSelected.Render(lineText))
			} else {
				bodyLines = append(bodyLines, sItem.Render(lineText))
			}
		}
	}

	bodyLines = append(bodyLines, "")
	bodyLines = append(bodyLines, theme().sHint.Render("↑/↓ Navigate  /  Enter Select  /  Esc Exit"))

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme().cTeal).
		Padding(0, 1).
		Width(innerW + 2)

	box := boxStyle.Render(titleStyle.Render("Select Profile") + "\n\n" + strings.Join(bodyLines, "\n"))
	return []string{"", box, ""}
}

func (m Model) renderSkillsPicker(width int) []string {
	var names []string
	if m.config.Skills != nil {
		names = m.config.Skills.Names()
		sort.Strings(names)
	}

	innerW := width - 8
	if innerW < 40 {
		innerW = 40
	}

	titleStyle := lipgloss.NewStyle().Foreground(theme().cTeal).Bold(true)
	sSelected := lipgloss.NewStyle().
		Background(theme().cTeal).
		Foreground(theme().cMantle).
		Bold(true)
	sItem := lipgloss.NewStyle().Foreground(theme().cText)

	var bodyLines []string
	if len(names) == 0 {
		bodyLines = append(bodyLines, theme().sHint.Render(" No skills registered"))
	} else {
		for i, name := range names {
			lineText := " /" + name
			if i == m.pickerSelected {
				if lipgloss.Width(lineText) < innerW {
					lineText += strings.Repeat(" ", innerW-lipgloss.Width(lineText))
				}
				bodyLines = append(bodyLines, sSelected.Render(lineText))
			} else {
				bodyLines = append(bodyLines, sItem.Render(lineText))
			}
		}
	}

	bodyLines = append(bodyLines, "")
	bodyLines = append(bodyLines, theme().sHint.Render("↑/↓ Navigate  /  Enter Select  /  Esc Exit"))

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme().cTeal).
		Padding(0, 1).
		Width(innerW + 2)

	box := boxStyle.Render(titleStyle.Render("Select Skill") + "\n\n" + strings.Join(bodyLines, "\n"))
	return []string{"", box, ""}
}

func (m Model) renderPluginsPicker(width int) []string {
	plugins := m.config.PluginInfos
	innerW := width - 8
	if innerW < 40 {
		innerW = 40
	}

	titleStyle := lipgloss.NewStyle().Foreground(theme().cTeal).Bold(true)
	sSelected := lipgloss.NewStyle().
		Background(theme().cTeal).
		Foreground(theme().cMantle).
		Bold(true)
	sItem := lipgloss.NewStyle().Foreground(theme().cText)
	sDisabled := lipgloss.NewStyle().Foreground(theme().cOverlay)

	var bodyLines []string
	if len(plugins) == 0 {
		bodyLines = append(bodyLines, theme().sHint.Render(" No plugins installed"))
	} else {
		for i, p := range plugins {
			status := "enabled"
			if !p.Enabled {
				status = "disabled"
			}
			lineText := fmt.Sprintf(" %s (%s, v%s) [%s]", p.Name, p.Scope, p.Version, status)
			if i == m.pickerSelected {
				if lipgloss.Width(lineText) < innerW {
					lineText += strings.Repeat(" ", innerW-lipgloss.Width(lineText))
				}
				bodyLines = append(bodyLines, sSelected.Render(lineText))
			} else {
				if p.Enabled {
					bodyLines = append(bodyLines, sItem.Render(lineText))
				} else {
					bodyLines = append(bodyLines, sDisabled.Render(lineText))
				}
			}
		}
	}

	bodyLines = append(bodyLines, "")
	bodyLines = append(bodyLines, theme().sHint.Render("↑/↓ Navigate  /  Esc Exit"))

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme().cTeal).
		Padding(0, 1).
		Width(innerW + 2)

	box := boxStyle.Render(titleStyle.Render("Plugins") + "\n\n" + strings.Join(bodyLines, "\n"))
	return []string{"", box, ""}
}

func (m Model) renderThemePicker(width int) []string {
	themes := []string{"catppuccin", "nord", "gruvbox", "monokai", "solarized-light"}
	innerW := width - 8
	if innerW < 40 {
		innerW = 40
	}

	titleStyle := lipgloss.NewStyle().Foreground(theme().cTeal).Bold(true)
	sSelected := lipgloss.NewStyle().
		Background(theme().cTeal).
		Foreground(theme().cMantle).
		Bold(true)
	sItem := lipgloss.NewStyle().Foreground(theme().cText)

	var bodyLines []string
	for i, name := range themes {
		lineText := " " + name
		if strings.EqualFold(name, theme().name) || (name == "solarized-light" && strings.EqualFold(theme().name, "solarized_light")) {
			lineText += " (active)"
		}
		if i == m.pickerSelected {
			if lipgloss.Width(lineText) < innerW {
				lineText += strings.Repeat(" ", innerW-lipgloss.Width(lineText))
			}
			bodyLines = append(bodyLines, sSelected.Render(lineText))
		} else {
			bodyLines = append(bodyLines, sItem.Render(lineText))
		}
	}

	bodyLines = append(bodyLines, "")
	bodyLines = append(bodyLines, theme().sHint.Render("↑/↓ Navigate  /  Enter Select  /  Esc Exit"))

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme().cTeal).
		Padding(0, 1).
		Width(innerW + 2)

	box := boxStyle.Render(titleStyle.Render("Select Theme") + "\n\n" + strings.Join(bodyLines, "\n"))
	return []string{"", box, ""}
}

func (m Model) renderHelpPicker(width int) []string {
	innerW := width - 8
	if innerW < 40 {
		innerW = 40
	}

	titleStyle := lipgloss.NewStyle().Foreground(theme().cPurple).Bold(true)
	sCmd := lipgloss.NewStyle().Foreground(theme().cPurple).Bold(true)
	sDesc := lipgloss.NewStyle().Foreground(theme().cText)
	sHeader := lipgloss.NewStyle().Foreground(theme().cSubtext).Underline(true).Bold(true)

	var bodyLines []string
	bodyLines = append(bodyLines, sHeader.Render("Slash Commands"))
	maxCmdW := 12
	for _, cmd := range builtinCommands {
		pad := strings.Repeat(" ", maxCmdW-len(cmd.name)+1)
		desc := cmd.desc
		descLines := strings.Split(wrapText(desc, innerW-maxCmdW-2), "\n")
		for j, dl := range descLines {
			if j == 0 {
				bodyLines = append(bodyLines, " "+sCmd.Render(cmd.name)+pad+sDesc.Render(dl))
			} else {
				bodyLines = append(bodyLines, " "+strings.Repeat(" ", maxCmdW+1)+sDesc.Render(dl))
			}
		}
	}

	bodyLines = append(bodyLines, "")
	bodyLines = append(bodyLines, sHeader.Render("Keyboard Shortcuts"))
	shortcuts := []struct{ key, desc string }{
		{"Ctrl+C", "quit / cancel streaming"},
		{"Ctrl+O", "toggle expanded tool output display"},
		{"Ctrl+Y", "copy latest assistant response to clipboard"},
		{"Ctrl+J / Shift+Enter", "insert newline in prompt"},
		{"Tab", "accept autocomplete suggestion"},
		{"Esc", "close active menu / picker"},
		{"Up / Down", "navigate history (on empty prompt) or menus"},
	}

	maxKeyW := 21
	for _, s := range shortcuts {
		pad := strings.Repeat(" ", maxKeyW-len(s.key)+1)
		descLines := strings.Split(wrapText(s.desc, innerW-maxKeyW-2), "\n")
		for j, dl := range descLines {
			if j == 0 {
				bodyLines = append(bodyLines, " "+sCmd.Render(s.key)+pad+sDesc.Render(dl))
			} else {
				bodyLines = append(bodyLines, " "+strings.Repeat(" ", maxKeyW+1)+sDesc.Render(dl))
			}
		}
	}

	bodyLines = append(bodyLines, "")
	bodyLines = append(bodyLines, theme().sHint.Render("Esc / Enter to close help menu"))

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme().cPurple).
		Padding(0, 1).
		Width(innerW + 2)

	box := boxStyle.Render(titleStyle.Render("Help & Keyboard Shortcuts") + "\n\n" + strings.Join(bodyLines, "\n"))
	return []string{"", box, ""}
}

func (m Model) renderResumePicker(width int) []string {
	sessions := m.resumeSessions
	innerW := width - 8
	if innerW < 40 {
		innerW = 40
	}

	titleStyle := lipgloss.NewStyle().Foreground(theme().cTeal).Bold(true)
	sSelected := lipgloss.NewStyle().
		Background(theme().cTeal).
		Foreground(theme().cMantle).
		Bold(true)
	sItem := lipgloss.NewStyle().Foreground(theme().cText)

	var bodyLines []string
	if len(sessions) == 0 {
		bodyLines = append(bodyLines, theme().sHint.Render(" No saved sessions found"))
	} else {
		for i, s := range sessions {
			title := s.Title
			if title == "" {
				title = "(untitled session)"
			}
			dateStr := s.UpdatedAt.Format("2006-01-02 15:04")
			lineText := fmt.Sprintf(" %s (%s, turns: %d) - %s", title, s.Model, s.TurnCount, dateStr)
			if i == m.resumeSelected {
				if lipgloss.Width(lineText) < innerW {
					lineText += strings.Repeat(" ", innerW-lipgloss.Width(lineText))
				}
				bodyLines = append(bodyLines, sSelected.Render(lineText))
			} else {
				bodyLines = append(bodyLines, sItem.Render(lineText))
			}
		}
	}

	bodyLines = append(bodyLines, "")
	bodyLines = append(bodyLines, theme().sHint.Render("↑/↓ Navigate  /  Enter Select  /  Esc Exit"))

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme().cTeal).
		Padding(0, 1).
		Width(innerW + 2)

	box := boxStyle.Render(titleStyle.Render("Resume Session") + "\n\n" + strings.Join(bodyLines, "\n"))
	return []string{"", box, ""}
}
