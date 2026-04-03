package tui

import "charm.land/lipgloss/v2"

var (
	// Colors
	colorPrimary   = lipgloss.Color("#7C3AED") // purple — gnoma brand
	colorSecondary = lipgloss.Color("#10B981") // green
	colorMuted     = lipgloss.Color("#6B7280") // gray
	colorError     = lipgloss.Color("#EF4444") // red
	colorWarning   = lipgloss.Color("#F59E0B") // amber
	colorUser      = lipgloss.Color("#3B82F6") // blue
	colorAssistant = lipgloss.Color("#7C3AED") // purple
	colorTool      = lipgloss.Color("#10B981") // green
	colorIncognito = lipgloss.Color("#F59E0B") // amber

	// Styles
	styleUserLabel = lipgloss.NewStyle().
			Foreground(colorUser).
			Bold(true)

	styleAssistantLabel = lipgloss.NewStyle().
				Foreground(colorAssistant).
				Bold(true)

	styleToolOutput = lipgloss.NewStyle().
			Foreground(colorTool)

	styleStatusBar = lipgloss.NewStyle().
			Background(lipgloss.Color("#1F2937")).
			Foreground(lipgloss.Color("#D1D5DB")).
			Padding(0, 1)

	styleStatusProvider = lipgloss.NewStyle().
				Foreground(colorPrimary).
				Bold(true)

	styleStatusIncognito = lipgloss.NewStyle().
				Foreground(colorIncognito).
				Bold(true)

	styleError = lipgloss.NewStyle().
			Foreground(colorError)

	styleHint = lipgloss.NewStyle().
			Foreground(colorMuted).
			Italic(true)

	styleInputBorder = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorPrimary).
				Padding(0, 1)
)
