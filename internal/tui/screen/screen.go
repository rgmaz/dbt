package screen

import tea "github.com/charmbracelet/bubbletea"

// ID identifies a routable screen.
type ID int

// Screen identifiers.
const (
	BrowserScreen ID = iota
	PickerScreen
)

// String returns a stable name for logs and tests.
func (id ID) String() string {
	switch id {
	case BrowserScreen:
		return "browser"
	case PickerScreen:
		return "picker"
	default:
		return "unknown"
	}
}

// Screen is a routable TUI screen. The root model owns global key handling and
// delegates everything else to the active screen.
type Screen interface {
	Init() tea.Cmd
	Update(tea.Msg) (Screen, tea.Cmd)
	View() string
	ID() ID

	// CapturesText reports whether the screen is in a text-entry mode where
	// single-letter global bindings must not fire.
	CapturesText() bool
}
