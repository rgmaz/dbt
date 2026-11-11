package component

import (
	"strings"

	"github.com/mattn/go-runewidth"

	"github.com/rgmaz/dbt/internal/db"
	"github.com/rgmaz/dbt/internal/tui/theme"
)

// CellModal displays one cell's value in a read-only modal.
type CellModal struct {
	Title string
	Cell  db.Cell
	Theme theme.Theme
}

// Render draws the modal.
func (m CellModal) Render(width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	boxWidth := min(width-2, 76)
	if boxWidth < 20 {
		text := strings.ReplaceAll(m.Title+" — "+m.display(), "\n", " ")
		return runewidth.Truncate(text, width, "…")
	}
	inner := boxWidth - 2
	title := m.Theme.ModalTitle.Render(m.Title)
	panel := m.Theme.Modal.Width(inner).Render(title + "\n\n" + m.display() + "\n\nesc: close")
	return panel
}

func (m CellModal) display() string {
	if m.Cell.Null {
		return m.Theme.Null.Render("NULL")
	}
	text := m.Cell.Text
	if text == "" {
		return "(empty)"
	}
	text = strings.ReplaceAll(text, "\n", "↵\n")
	return text
}
