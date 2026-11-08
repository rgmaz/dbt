package screen

import (
	"errors"
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
		"Schemas", "Loading schemas…", "Data", "Structure", "SQL",
		"No data",
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
	if view := b.View(); !strings.Contains(view, "i edit") {
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

	cmd := b.Init()
	if cmd == nil {
		t.Fatal("browser Init() should schedule a schema load")
	}
	if _, ok := cmd().(LoadSchemasMsg); !ok {
		t.Errorf("Init() returned %T, want LoadSchemasMsg", cmd())
	}
}

func TestBrowserSchemaLoadAndExpand(t *testing.T) {
	b := newTestBrowser(t, 100, 30)

	b, cmd := sendBrowser(t, b, SchemasLoadedMsg{Schemas: []db.Schema{{Name: "public"}}})
	if cmd != nil {
		t.Errorf("unexpected command after schema load: %T", cmd())
	}
	if view := b.View(); !strings.Contains(view, "public") {
		t.Errorf("browser missing loaded schema:\n%s", view)
	}

	b, cmd = sendBrowser(t, b, runeKey('l'))
	if cmd == nil {
		t.Fatal("expanding a schema did not schedule a relation load")
	}
	request, ok := cmd().(LoadRelationsMsg)
	if !ok {
		t.Fatalf("expand command returned %T, want LoadRelationsMsg", request)
	}
	if request.Schema != "public" {
		t.Errorf("relation request schema = %q, want public", request.Schema)
	}

	b, _ = sendBrowser(t, b, RelationsLoadedMsg{
		Schema: "public",
		Relations: []db.Relation{
			{Schema: "public", Name: "users", Kind: db.RelationTable},
			{Schema: "public", Name: "orders", Kind: db.RelationView},
		},
	})
	view := b.View()
	for _, want := range []string{"public", "users", "orders", "table", "view"} {
		if !strings.Contains(view, want) {
			t.Errorf("browser tree missing %q:\n%s", want, view)
		}
	}

	b, _ = sendBrowser(t, b, runeKey('j'))
	b, _ = sendBrowser(t, b, tea.KeyMsg{Type: tea.KeyEnter})
	if view := b.View(); !strings.Contains(view, "Loading") {
		t.Errorf("opened relation not shown in main pane:\n%s", view)
	}
}

func TestBrowserFilterCapturesText(t *testing.T) {
	b := newTestBrowser(t, 100, 30)
	b, _ = sendBrowser(t, b, SchemasLoadedMsg{Schemas: []db.Schema{{Name: "public"}}})

	b, _ = sendBrowser(t, b, runeKey('/'))
	if !b.CapturesText() {
		t.Fatal("filter mode did not capture text")
	}

	b, _ = sendBrowser(t, b, runeKey('q'))
	if !b.CapturesText() {
		t.Fatal("q left filter mode")
	}
	if view := b.View(); !strings.Contains(view, "/q_") {
		t.Errorf("filter input missing q:\n%s", view)
	}

	b, _ = sendBrowser(t, b, tea.KeyMsg{Type: tea.KeyEnter})
	if b.CapturesText() {
		t.Fatal("enter did not leave filter mode")
	}
}

func TestBrowserTabFocus(t *testing.T) {
	b := newTestBrowser(t, 100, 30)
	b, _ = sendBrowser(t, b, SchemasLoadedMsg{Schemas: []db.Schema{{Name: "public"}}})

	if b.focus != focusSidebar {
		t.Fatalf("initial focus = %v, want sidebar", b.focus)
	}

	b = updateBrowser(t, b, tea.KeyMsg{Type: tea.KeyTab})
	if b.focus != focusMain {
		t.Fatalf("focus after tab = %v, want main", b.focus)
	}

	b = updateBrowser(t, b, runeKey('l'))
	if view := b.View(); strings.Contains(view, "loading") {
		t.Errorf("sidebar key fired while main was focused:\n%s", view)
	}

	b = updateBrowser(t, b, tea.KeyMsg{Type: tea.KeyTab})
	if b.focus != focusSidebar {
		t.Fatalf("focus after second tab = %v, want sidebar", b.focus)
	}
}

func TestBrowserCatalogErrorRedacted(t *testing.T) {
	b := newTestBrowser(t, 100, 30)

	b, _ = sendBrowser(t, b, CatalogErrMsg{
		Scope: ScopeSchemas,
		Err:   errors.New("connection failed: password=secret"),
	})
	view := b.View()
	for _, want := range []string{"connection", "r retry"} {
		if !strings.Contains(view, want) {
			t.Errorf("catalog error missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "password=secret") {
		t.Errorf("catalog error leaked a password:\n%s", view)
	}
}

func TestBrowserRefreshReloadsExpandedSchemas(t *testing.T) {
	b := newTestBrowser(t, 100, 30)
	b, _ = sendBrowser(t, b, SchemasLoadedMsg{Schemas: []db.Schema{{Name: "public"}}})
	b, _ = sendBrowser(t, b, runeKey('l'))
	b, _ = sendBrowser(t, b, RelationsLoadedMsg{
		Schema:    "public",
		Relations: []db.Relation{{Schema: "public", Name: "users", Kind: db.RelationTable}},
	})

	b, cmd := sendBrowser(t, b, runeKey('r'))
	if cmd == nil {
		t.Fatal("refresh did not schedule a schema load")
	}
	if _, ok := cmd().(LoadSchemasMsg); !ok {
		t.Errorf("refresh command returned %T, want LoadSchemasMsg", cmd())
	}
	if view := b.View(); !strings.Contains(view, "refreshing") {
		t.Errorf("refresh state missing status:\n%s", view)
	}

	_, cmd = sendBrowser(t, b, SchemasLoadedMsg{Schemas: []db.Schema{{Name: "public"}}})
	if cmd == nil {
		t.Fatal("refresh did not reload expanded relations")
	}
	request, ok := cmd().(LoadRelationsMsg)
	if !ok {
		t.Fatalf("refresh relation command returned %T, want LoadRelationsMsg", request)
	}
	if request.Schema != "public" {
		t.Errorf("refresh relation schema = %q, want public", request.Schema)
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

func sendBrowser(t *testing.T, b Browser, msg tea.Msg) (Browser, tea.Cmd) {
	t.Helper()

	updated, cmd := b.Update(msg)
	next, ok := updated.(Browser)
	if !ok {
		t.Fatalf("Update returned %T, want Browser", updated)
	}
	return next, cmd
}

func updateBrowser(t *testing.T, b Browser, msg tea.Msg) Browser {
	t.Helper()

	next, _ := sendBrowser(t, b, msg)
	return next
}

func runeKey(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}
