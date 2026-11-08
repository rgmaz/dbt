package screen

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rgmaz/dbt/internal/db"
	"github.com/rgmaz/dbt/internal/tui/keys"
	"github.com/rgmaz/dbt/internal/tui/theme"
)

func TestBrowserShell(t *testing.T) {
	b := newTestBrowser(t, 100, 30)

	view := b.View()
	for _, want := range []string{
		"Schemas", "No schemas loaded", "Data", "Structure", "SQL",
		"No relation selected",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("browser view missing %q:\n%s", want, view)
		}
	}
	if width := lipgloss.Width(view); width > 100 {
		t.Errorf("browser width = %d, want <= 100", width)
	}
	if lines := len(strings.Split(view, "\n")); lines != 30 {
		t.Errorf("browser height = %d, want 30", lines)
	}
}

func TestBrowserTabSwitching(t *testing.T) {
	b := newTestBrowser(t, 100, 30)

	b = updateBrowser(t, b, runeKey('2'))
	if b.Tab() != TabStructure {
		t.Fatalf("tab = %v, want Structure", b.Tab())
	}
	if view := b.View(); !strings.Contains(view, "columns, indexes") {
		t.Errorf("structure view missing metadata hint:\n%s", view)
	}

	b = updateBrowser(t, b, runeKey('3'))
	if b.Tab() != TabSQL {
		t.Fatalf("tab = %v, want SQL", b.Tab())
	}
	if view := b.View(); !strings.Contains(view, "Read-only session") {
		t.Errorf("sql view missing read-only hint:\n%s", view)
	}

	b = updateBrowser(t, b, runeKey('1'))
	if b.Tab() != TabData {
		t.Fatalf("tab = %v, want Data", b.Tab())
	}
}

func TestBrowserIdentity(t *testing.T) {
	b := newTestBrowser(t, 80, 24)
	if b.ID() != BrowserScreen {
		t.Errorf("ID() = %v, want %v", b.ID(), BrowserScreen)
	}
	if b.CapturesText() {
		t.Error("browser must not capture text")
	}
	if b.Init() != nil {
		t.Error("browser Init() should not schedule a command")
	}
}

func TestBrowserNarrowLayout(t *testing.T) {
	b := newTestBrowser(t, 40, 20)
	view := b.View()
	if !strings.Contains(view, "Data") {
		t.Errorf("narrow browser missing tabs:\n%s", view)
	}
	if width := lipgloss.Width(view); width > 40 {
		t.Errorf("narrow browser width = %d, want <= 40", width)
	}
}

func newTestBrowser(t *testing.T, width, height int) Browser {
	t.Helper()

	b := NewBrowser(BrowserOptions{
		Connection: "test.db",
		Kind:       db.KindSQLite,
		Version:    "3.53.4",
		ReadOnly:   true,
		Theme:      theme.Default(),
		Keys:       keys.Default(),
	})
	return updateBrowser(t, b, tea.WindowSizeMsg{Width: width, Height: height})
}

func updateBrowser(t *testing.T, b Browser, msg tea.Msg) Browser {
	t.Helper()

	updated, _ := b.Update(msg)
	next, ok := updated.(Browser)
	if !ok {
		t.Fatalf("Update returned %T, want Browser", updated)
	}
	return next
}

func runeKey(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}
