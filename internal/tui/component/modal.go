package component

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/rgmaz/dbt/internal/tui/theme"
)

const maxModalWidth = 76

// Modal is a centered bordered panel used for help and error messages.
type Modal struct {
	Title  string
	Body   string
	Theme  theme.Theme
	Danger bool
}

// Render draws the modal centered inside the given width and height. Small
// dimensions degrade to a truncated one-line message instead of panicking.
func (m Modal) Render(width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}

	boxWidth := min(width-2, maxModalWidth)
	if boxWidth < 20 {
		text := strings.ReplaceAll(m.Title+" — "+m.Body, "\n", " ")
		return runewidth.Truncate(text, width, "…")
	}

	innerWidth := boxWidth - 2 // account for the rounded border
	title := m.Theme.ModalTitle
	if m.Danger {
		title = m.Theme.Error
	}
	body := title.Render(m.Title) + "\n\n" + m.Body
	box := m.Theme.Modal.Width(innerWidth).Render(body)
	box = clampHeight(box, height)

	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

func clampHeight(rendered string, height int) string {
	lines := strings.Split(rendered, "\n")
	if len(lines) <= height {
		return rendered
	}
	if height <= 0 {
		return ""
	}
	lines = lines[:height]
	return strings.Join(lines, "\n")
}
