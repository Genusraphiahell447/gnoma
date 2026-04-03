package tui

import "charm.land/lipgloss/v2"

// Color palette — catppuccin mocha inspired
var (
	cPurple  = lipgloss.Color("#CBA6F7") // mauve
	cBlue    = lipgloss.Color("#89B4FA") // blue
	cGreen   = lipgloss.Color("#A6E3A1") // green
	cRed     = lipgloss.Color("#F38BA8") // red
	cYellow  = lipgloss.Color("#F9E2AF") // yellow
	cText    = lipgloss.Color("#CDD6F4") // text
	cSubtext = lipgloss.Color("#A6ADC8") // subtext0
	cOverlay = lipgloss.Color("#6C7086") // overlay0
	cSurface = lipgloss.Color("#313244") // surface0
	cBase    = lipgloss.Color("#1E1E2E") // base
	cMantle  = lipgloss.Color("#181825") // mantle
)

// Header
var (
	sHeaderBrand = lipgloss.NewStyle().
			Background(cPurple).
			Foreground(cMantle).
			Bold(true).
			Padding(0, 1)

	sHeaderModel = lipgloss.NewStyle().
			Foreground(cGreen).
			Bold(true)

	sHeaderDim = lipgloss.NewStyle().
			Foreground(cOverlay)
)

// Chat
var (
	sUserLabel = lipgloss.NewStyle().
			Foreground(cBlue).
			Bold(true)

	sToolOutput = lipgloss.NewStyle().
			Foreground(cGreen)

	sToolResult = lipgloss.NewStyle().
			Foreground(cOverlay)

	sSystem = lipgloss.NewStyle().
		Foreground(cYellow)

	sError = lipgloss.NewStyle().
		Foreground(cRed)

	sHint = lipgloss.NewStyle().
		Foreground(cOverlay)

	sCursor = lipgloss.NewStyle().
		Foreground(cPurple)
)

// Status bar
var (
	sStatusBar = lipgloss.NewStyle().
			Foreground(cSubtext)

	sStatusHighlight = lipgloss.NewStyle().
				Foreground(cPurple).
				Bold(true)

	sStatusDim = lipgloss.NewStyle().
			Foreground(cOverlay)

	sStatusStreaming = lipgloss.NewStyle().
				Foreground(cYellow).
				Bold(true)

	sStatusBranch = lipgloss.NewStyle().
			Foreground(cGreen)

	sStatusIncognito = lipgloss.NewStyle().
				Foreground(cYellow)

	sLine = lipgloss.NewStyle().
		Foreground(cSurface)
)
