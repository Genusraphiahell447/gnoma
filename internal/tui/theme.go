package tui

import "charm.land/lipgloss/v2"

var (
	// Colors
	colorPrimary    = lipgloss.Color("#A78BFA") // light purple
	colorUser       = lipgloss.Color("#60A5FA") // light blue
	colorAssistant  = lipgloss.Color("#A78BFA") // light purple
	colorTool       = lipgloss.Color("#34D399") // green
	colorError      = lipgloss.Color("#F87171") // red
	colorMuted      = lipgloss.Color("#6B7280") // gray
	colorStreaming   = lipgloss.Color("#FBBF24") // amber
	colorStatusBg   = lipgloss.Color("#1E1E2E") // dark bg

	// Chat styles
	styleUserLabel = lipgloss.NewStyle().
			Foreground(colorUser).
			Bold(true)

	styleUserText = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#E5E7EB"))

	styleAssistantLabel = lipgloss.NewStyle().
				Foreground(colorAssistant).
				Bold(true)

	styleToolOutput = lipgloss.NewStyle().
			Foreground(colorTool).
			Italic(true)

	styleError = lipgloss.NewStyle().
			Foreground(colorError)

	styleHint = lipgloss.NewStyle().
			Foreground(colorMuted)

	styleCursor = lipgloss.NewStyle().
			Foreground(colorStreaming)

	styleSeperator = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#374151"))

	// Status bar
	styleStatusBar = lipgloss.NewStyle().
			Background(colorStatusBg).
			Foreground(lipgloss.Color("#9CA3AF"))

	styleStatusProvider = lipgloss.NewStyle().
				Background(colorStatusBg).
				Foreground(colorPrimary).
				Bold(true)

	styleStatusStreaming = lipgloss.NewStyle().
				Background(colorStatusBg).
				Foreground(colorStreaming).
				Bold(true)
)
