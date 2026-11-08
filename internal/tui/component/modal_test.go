package component

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/rgmaz/dbt/internal/tui/theme"
)

func TestModalRender(t *testing.T) {
	modal := Modal{
		Title:  "Error",
		Body:   "connection failed\nesc: dismiss",
		Theme:  theme.Default(),
		Danger: true,
	}

	out := modal.Render(80, 24)
	for _, want := range []string{"Error", "connection failed", "esc: dismiss"} {
		if !strings.Contains(out, want) {
			t.Errorf("modal missing %q:\n%s", want, out)
		}
	}
	if width := lipgloss.Width(out); width > 80 {
		t.Errorf("modal width = %d, want <= 80", width)
	}
	if lines := len(strings.Split(out, "\n")); lines > 24 {
		t.Errorf("modal height = %d, want <= 24", lines)
	}
}

func TestModalTinyDimensions(t *testing.T) {
	modal := Modal{Title: "Error", Body: "boom", Theme: theme.Default()}

	for _, size := range []struct{ width, height int }{
		{0, 0},
		{5, 2},
		{10, 3},
		{19, 6},
	} {
		out := modal.Render(size.width, size.height)
		if size.width <= 0 || size.height <= 0 {
			if out != "" {
				t.Errorf("Render(%d, %d) = %q, want empty", size.width, size.height, out)
			}
			continue
		}
		if width := lipgloss.Width(out); width > size.width {
			t.Errorf("Render(%d, %d) width = %d", size.width, size.height, width)
		}
		if lines := len(strings.Split(out, "\n")); lines > size.height {
			t.Errorf("Render(%d, %d) height = %d", size.width, size.height, lines)
		}
	}
}
