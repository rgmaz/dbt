package theme

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// DefaultName is the built-in theme name.
const DefaultName = "default"

var (
	accent = lipgloss.AdaptiveColor{Light: "#5B21B6", Dark: "#C4B5FD"}
	muted  = lipgloss.AdaptiveColor{Light: "#6B7280", Dark: "#9CA3AF"}
	danger = lipgloss.AdaptiveColor{Light: "#B91C1C", Dark: "#F87171"}
	chrome = lipgloss.AdaptiveColor{Light: "#EDE9FE", Dark: "#2D1B4E"}
	bar    = lipgloss.AdaptiveColor{Light: "#F3F4F6", Dark: "#1F2937"}
)

// Theme is the set of styles used by the TUI. Styles are values, so a theme can
// be passed explicitly to every model without global state.
type Theme struct {
	Name string

	Header       lipgloss.Style
	Sidebar      lipgloss.Style
	Main         lipgloss.Style
	TabActive    lipgloss.Style
	TabInactive  lipgloss.Style
	StatusBar    lipgloss.Style
	StatusKey    lipgloss.Style
	Modal        lipgloss.Style
	ModalTitle   lipgloss.Style
	Error        lipgloss.Style
	Dim          lipgloss.Style
	Null         lipgloss.Style
	Notice       lipgloss.Style
	SectionTitle lipgloss.Style
}

// Default returns the built-in theme.
func Default() Theme {
	return Theme{
		Name: DefaultName,
		Header: lipgloss.NewStyle().
			Bold(true).
			Foreground(accent).
			Background(chrome).
			Padding(0, 1),
		Sidebar: lipgloss.NewStyle().
			Padding(0, 1),
		Main: lipgloss.NewStyle().
			Padding(0, 1),
		TabActive: lipgloss.NewStyle().
			Bold(true).
			Underline(true).
			Foreground(accent),
		TabInactive: lipgloss.NewStyle().
			Foreground(muted),
		StatusBar: lipgloss.NewStyle().
			Foreground(muted).
			Background(bar).
			Padding(0, 1),
		StatusKey: lipgloss.NewStyle().
			Bold(true).
			Foreground(accent),
		Modal: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(accent).
			Padding(0, 2),
		ModalTitle: lipgloss.NewStyle().
			Bold(true).
			Foreground(accent),
		Error: lipgloss.NewStyle().
			Bold(true).
			Foreground(danger),
		Dim: lipgloss.NewStyle().
			Foreground(muted),
		Null: lipgloss.NewStyle().
			Italic(true).
			Foreground(muted),
		Notice: lipgloss.NewStyle().
			Bold(true).
			Foreground(danger).
			Padding(1, 2),
		SectionTitle: lipgloss.NewStyle().
			Bold(true),
	}
}

// New resolves a theme by name. Unknown names fall back to the default theme so
// a typo in the config file never prevents the TUI from starting.
func New(name string) Theme {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", DefaultName:
		return Default()
	default:
		return Default()
	}
}
