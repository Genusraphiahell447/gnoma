package tui

import (
	"image/color"

	"charm.land/lipgloss/v2"
	"somegit.dev/Owlibou/gnoma/internal/permission"
)

// Color palette — catppuccin mocha inspired
var (
	cPurple  = lipgloss.Color("#CBA6F7") // mauve
	cBlue    = lipgloss.Color("#89B4FA") // blue
	cGreen   = lipgloss.Color("#A6E3A1") // green
	cRed     = lipgloss.Color("#F38BA8") // red
	cYellow  = lipgloss.Color("#F9E2AF") // yellow
	cPeach   = lipgloss.Color("#FAB387") // peach
	cTeal    = lipgloss.Color("#94E2D5") // teal
	cText    = lipgloss.Color("#CDD6F4") // text
	cSubtext = lipgloss.Color("#A6ADC8") // subtext0
	cOverlay = lipgloss.Color("#6C7086") // overlay0
	cSurface = lipgloss.Color("#313244") // surface0
	cBase    = lipgloss.Color("#1E1E2E") // base
	cMantle  = lipgloss.Color("#181825") // mantle
)

// Permission mode colors — each mode has a distinct color
var modeColors = map[permission.Mode]color.Color{
	permission.ModeBypass:      cGreen,  // green = all allowed
	permission.ModeDefault:     cBlue,   // blue = prompting
	permission.ModePlan:        cTeal,   // teal = read-only
	permission.ModeAcceptEdits: cPurple, // purple = edits ok
	permission.ModeAuto:        cPeach,  // peach = smart
	permission.ModeDeny:        cRed,    // red = locked down
}

// ModeColor returns the color for a permission mode.
func ModeColor(mode permission.Mode) color.Color {
	if c, ok := modeColors[mode]; ok {
		return c
	}
	return cOverlay
}

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

	styleAssistantLabel = lipgloss.NewStyle().
				Foreground(cPurple).
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

	sDiffAdd = lipgloss.NewStyle().
		Foreground(cGreen)

	sDiffRemove = lipgloss.NewStyle().
			Foreground(cRed)

	sText = lipgloss.NewStyle().
		Foreground(cText)

	sThinkingLabel = lipgloss.NewStyle().
			Foreground(cOverlay).
			Italic(true)

	sThinkingBody = lipgloss.NewStyle().
			Foreground(cOverlay).
			Italic(true)
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
)
