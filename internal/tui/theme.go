package tui

import (
	"image/color"
	"strings"
	"sync/atomic"

	"charm.land/lipgloss/v2"
	"somegit.dev/Owlibou/gnoma/internal/permission"
)

// Theme represents a custom color palette for the TUI.
type Theme struct {
	Name    string
	Purple  color.Color
	Blue    color.Color
	Green   color.Color
	Red     color.Color
	Yellow  color.Color
	Peach   color.Color
	Teal    color.Color
	Text    color.Color
	Subtext color.Color
	Overlay color.Color
	Surface color.Color
	Mantle  color.Color
}

// Predefined themes
var Themes = []Theme{
	{
		Name:    "catppuccin",
		Purple:  lipgloss.Color("#CBA6F7"),
		Blue:    lipgloss.Color("#89B4FA"),
		Green:   lipgloss.Color("#A6E3A1"),
		Red:     lipgloss.Color("#F38BA8"),
		Yellow:  lipgloss.Color("#F9E2AF"),
		Peach:   lipgloss.Color("#FAB387"),
		Teal:    lipgloss.Color("#94E2D5"),
		Text:    lipgloss.Color("#CDD6F4"),
		Subtext: lipgloss.Color("#A6ADC8"),
		Overlay: lipgloss.Color("#6C7086"),
		Surface: lipgloss.Color("#313244"),
		Mantle:  lipgloss.Color("#181825"),
	},
	{
		Name:    "nord",
		Purple:  lipgloss.Color("#B48EAD"),
		Blue:    lipgloss.Color("#81A1C1"),
		Green:   lipgloss.Color("#A3BE8C"),
		Red:     lipgloss.Color("#BF616A"),
		Yellow:  lipgloss.Color("#EBCB8B"),
		Peach:   lipgloss.Color("#D08770"),
		Teal:    lipgloss.Color("#88C0D0"),
		Text:    lipgloss.Color("#D8DEE9"),
		Subtext: lipgloss.Color("#E5E9F0"),
		Overlay: lipgloss.Color("#4C566A"),
		Surface: lipgloss.Color("#3B4252"),
		Mantle:  lipgloss.Color("#2E3440"),
	},
	{
		Name:    "gruvbox",
		Purple:  lipgloss.Color("#d3869b"),
		Blue:    lipgloss.Color("#83a598"),
		Green:   lipgloss.Color("#b8bb26"),
		Red:     lipgloss.Color("#fb4934"),
		Yellow:  lipgloss.Color("#fabd2f"),
		Peach:   lipgloss.Color("#fe8019"),
		Teal:    lipgloss.Color("#8ec07c"),
		Text:    lipgloss.Color("#ebdbb2"),
		Subtext: lipgloss.Color("#a89984"),
		Overlay: lipgloss.Color("#928374"),
		Surface: lipgloss.Color("#3c3836"),
		Mantle:  lipgloss.Color("#282828"),
	},
	{
		Name:    "monokai",
		Purple:  lipgloss.Color("#ae81ff"),
		Blue:    lipgloss.Color("#66d9ef"),
		Green:   lipgloss.Color("#a6e22e"),
		Red:     lipgloss.Color("#f92672"),
		Yellow:  lipgloss.Color("#e6db74"),
		Peach:   lipgloss.Color("#fd971f"),
		Teal:    lipgloss.Color("#a1efe4"),
		Text:    lipgloss.Color("#f8f8f2"),
		Subtext: lipgloss.Color("#cfcfc2"),
		Overlay: lipgloss.Color("#75715e"),
		Surface: lipgloss.Color("#272822"),
		Mantle:  lipgloss.Color("#1e1f1c"),
	},
	{
		Name:    "solarized_light",
		Purple:  lipgloss.Color("#6c71c4"),
		Blue:    lipgloss.Color("#268bd2"),
		Green:   lipgloss.Color("#859900"),
		Red:     lipgloss.Color("#dc322f"),
		Yellow:  lipgloss.Color("#b58900"),
		Peach:   lipgloss.Color("#cb4b16"),
		Teal:    lipgloss.Color("#2aa198"),
		Text:    lipgloss.Color("#586e75"),
		Subtext: lipgloss.Color("#657b83"),
		Overlay: lipgloss.Color("#93a1a1"),
		Surface: lipgloss.Color("#eee8d5"),
		Mantle:  lipgloss.Color("#fdf6e3"),
	},
}

// themeStyles is the immutable snapshot of the active palette and the
// pre-built lipgloss styles derived from it. ApplyTheme builds a fresh
// snapshot and stores it atomically; readers Load() the pointer once and
// see a coherent view, so no mutex is needed even if rendering ever moves
// off the bubbletea event-loop goroutine.
type themeStyles struct {
	name string

	cPurple  color.Color
	cBlue    color.Color
	cGreen   color.Color
	cRed     color.Color
	cYellow  color.Color
	cPeach   color.Color
	cTeal    color.Color
	cText    color.Color
	cSubtext color.Color
	cOverlay color.Color
	cSurface color.Color
	cMantle  color.Color

	modeColors map[permission.Mode]color.Color

	sHeaderBrand        lipgloss.Style
	sHeaderModel        lipgloss.Style
	sHeaderDim          lipgloss.Style
	sUserLabel          lipgloss.Style
	styleAssistantLabel lipgloss.Style
	sToolOutput         lipgloss.Style
	sToolResult         lipgloss.Style
	sSystem             lipgloss.Style
	sError              lipgloss.Style
	sHint               lipgloss.Style
	sCursor             lipgloss.Style
	sDiffAdd            lipgloss.Style
	sDiffRemove         lipgloss.Style
	sText               lipgloss.Style
	sThinkingLabel      lipgloss.Style
	sThinkingBody       lipgloss.Style
	sStatusBar          lipgloss.Style
	sStatusHighlight    lipgloss.Style
	sStatusDim          lipgloss.Style
	sStatusStreaming    lipgloss.Style
	sStatusBranch       lipgloss.Style
	sStatusIncognito    lipgloss.Style
}

var activeStyles atomic.Pointer[themeStyles]

// theme returns the currently-active style snapshot. The returned pointer
// must be treated as read-only; ApplyTheme never mutates an existing
// snapshot in place.
func theme() *themeStyles {
	return activeStyles.Load()
}

// ModeColor returns the color for a permission mode under the active theme.
func ModeColor(mode permission.Mode) color.Color {
	t := theme()
	if c, ok := t.modeColors[mode]; ok {
		return c
	}
	return t.cOverlay
}

// Initialize with catppuccin on package load.
func init() {
	ApplyTheme("catppuccin")
}

// ApplyTheme builds a fresh themeStyles snapshot for the named theme and
// atomically swaps it in as the active one. Concurrent reads via theme()
// see either the previous snapshot or the new one — never a half-built
// state. Returns false if name does not match a known theme.
func ApplyTheme(name string) bool {
	var src *Theme
	for i := range Themes {
		tName := strings.ReplaceAll(strings.ToLower(Themes[i].Name), "_", "-")
		sName := strings.ReplaceAll(strings.ToLower(name), "_", "-")
		if tName == sName {
			src = &Themes[i]
			break
		}
	}
	if src == nil {
		return false
	}

	t := &themeStyles{
		name:     src.Name,
		cPurple:  src.Purple,
		cBlue:    src.Blue,
		cGreen:   src.Green,
		cRed:     src.Red,
		cYellow:  src.Yellow,
		cPeach:   src.Peach,
		cTeal:    src.Teal,
		cText:    src.Text,
		cSubtext: src.Subtext,
		cOverlay: src.Overlay,
		cSurface: src.Surface,
		cMantle:  src.Mantle,
	}

	t.modeColors = map[permission.Mode]color.Color{
		permission.ModeBypass:      t.cGreen,
		permission.ModeDefault:     t.cBlue,
		permission.ModePlan:        t.cTeal,
		permission.ModeAcceptEdits: t.cPurple,
		permission.ModeAuto:        t.cPeach,
		permission.ModeDeny:        t.cRed,
	}

	t.sHeaderBrand = lipgloss.NewStyle().Background(t.cPurple).Foreground(t.cMantle).Bold(true).Padding(0, 1)
	t.sHeaderModel = lipgloss.NewStyle().Foreground(t.cGreen).Bold(true)
	t.sHeaderDim = lipgloss.NewStyle().Foreground(t.cOverlay)
	t.sUserLabel = lipgloss.NewStyle().Foreground(t.cBlue).Bold(true)
	t.styleAssistantLabel = lipgloss.NewStyle().Foreground(t.cPurple).Bold(true)
	t.sToolOutput = lipgloss.NewStyle().Foreground(t.cGreen)
	t.sToolResult = lipgloss.NewStyle().Foreground(t.cOverlay)
	t.sSystem = lipgloss.NewStyle().Foreground(t.cYellow)
	t.sError = lipgloss.NewStyle().Foreground(t.cRed)
	t.sHint = lipgloss.NewStyle().Foreground(t.cOverlay)
	t.sCursor = lipgloss.NewStyle().Foreground(t.cPurple)
	t.sDiffAdd = lipgloss.NewStyle().Foreground(t.cGreen)
	t.sDiffRemove = lipgloss.NewStyle().Foreground(t.cRed)
	t.sText = lipgloss.NewStyle().Foreground(t.cText)
	t.sThinkingLabel = lipgloss.NewStyle().Foreground(t.cOverlay).Italic(true)
	t.sThinkingBody = lipgloss.NewStyle().Foreground(t.cOverlay).Italic(true)
	t.sStatusBar = lipgloss.NewStyle().Foreground(t.cSubtext)
	t.sStatusHighlight = lipgloss.NewStyle().Foreground(t.cPurple).Bold(true)
	t.sStatusDim = lipgloss.NewStyle().Foreground(t.cOverlay)
	t.sStatusStreaming = lipgloss.NewStyle().Foreground(t.cYellow).Bold(true)
	t.sStatusBranch = lipgloss.NewStyle().Foreground(t.cGreen)
	t.sStatusIncognito = lipgloss.NewStyle().Foreground(t.cYellow)

	activeStyles.Store(t)
	return true
}
