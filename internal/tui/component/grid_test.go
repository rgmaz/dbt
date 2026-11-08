package component

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"

	"github.com/rgmaz/dbt/internal/db"
	"github.com/rgmaz/dbt/internal/tui/keys"
	"github.com/rgmaz/dbt/internal/tui/theme"
)

func samplePage() GridPage {
	return GridPage{
		Columns: []db.ResultColumn{
			{Name: "id", DatabaseType: "bigint"},
			{Name: "email", DatabaseType: "text"},
		},
		Rows: [][]db.Cell{
			{{Text: "1"}, {Text: "a@x.com"}},
			{{Text: "2"}, {Text: "b@x.com"}},
			{{Text: "3"}, {Null: true}},
		},
		Page:      0,
		PageSize:  2,
		Total:     10,
		Truncated: true,
	}
}

func newGrid(width, height int) Grid {
	g := NewGrid(GridOptions{Theme: theme.Default(), Keys: keys.Default()})
	g = g.SetSize(width, height)
	return g.SetData(samplePage())
}

func TestGridRenderIsBounded(t *testing.T) {
	g := newGrid(80, 24)
	for _, c := range []struct{ w, h int }{{80, 24}, {120, 40}} {
		view := g.View(c.w, c.h)
		if width := runewidthMaxWidth(view); width > c.w {
			t.Errorf("%dx%d width=%d > %d", c.w, c.h, width, c.w)
		}
		if lines := strings.Count(view, "\n") + 1; lines != c.h {
			t.Errorf("%dx%d lines=%d != %d", c.w, c.h, lines, c.h)
		}
	}
}

func TestGridCursorMovement(t *testing.T) {
	g := newGrid(80, 24)
	g, _ = updateGrid(t, g, runeKey('j'))
	if g.CursorRow() != 1 {
		t.Fatalf("after j CursorRow=%d", g.CursorRow())
	}
	g, _ = updateGrid(t, g, runeKey('l'))
	if g.CursorCol() != 1 {
		t.Fatalf("after l CursorCol=%d", g.CursorCol())
	}
	g, _ = updateGrid(t, g, runeKey('k'))
	if g.CursorRow() != 0 {
		t.Fatalf("after k CursorRow=%d", g.CursorRow())
	}
}

func TestGridPaging(t *testing.T) {
	g := newGrid(80, 24)
	g, ev := updateGrid(t, g, runeKey('n'))
	if ev.Kind != GridEventPage || ev.Page != 1 {
		t.Fatalf("next page event = %+v", ev)
	}
	g, ev = updateGrid(t, g, runeKey('p'))
	if ev.Kind != GridEventPage || ev.Page != 0 {
		t.Fatalf("prev page event = %+v", ev)
	}
	// cannot go below 0
	g2, ev := updateGrid(t, g, runeKey('p'))
	if ev.Kind != 0 {
		t.Fatalf("page below range fired event %+v", ev)
	}
	_ = g2
}

func TestGridSortToggle(t *testing.T) {
	g := newGrid(80, 24)
	g = g.SetCursor(0, 0)
	g, ev := updateGrid(t, g, runeKey('s'))
	if ev.Kind != GridEventSort || ev.Column != 0 || ev.Desc {
		t.Fatalf("first sort event = %+v", ev)
	}
	g, ev = updateGrid(t, g, runeKey('s'))
	if ev.Kind != GridEventSort || ev.Column != 0 || !ev.Desc {
		t.Fatalf("second sort event = %+v", ev)
	}
	g, ev = updateGrid(t, g, runeKey('s'))
	if ev.Kind != GridEventSort || ev.Column != -1 {
		t.Fatalf("third sort event = %+v", ev)
	}
}

func TestGridCellDetail(t *testing.T) {
	g := newGrid(80, 24)
	g = g.SetCursor(0, 0)
	g, ev := updateGrid(t, g, tea.KeyMsg{Type: tea.KeyEnter})
	if ev.Kind != GridEventCellDetail || ev.Header != "id" {
		t.Fatalf("cell detail event = %+v", ev)
	}
}

func TestGridEmptyState(t *testing.T) {
	g := NewGrid(GridOptions{Theme: theme.Default(), Keys: keys.Default()}).SetSize(80, 24)
	g = g.SetData(GridPage{Page: 0, PageSize: 100})
	view := g.View(80, 24)
	if !strings.Contains(view, "No data") {
		t.Errorf("empty grid missing 'No data':\n%s", view)
	}
	g = NewGrid(GridOptions{Theme: theme.Default(), Keys: keys.Default()}).SetSize(80, 24)
	g = g.SetData(GridPage{Loading: true, Page: 0, PageSize: 100})
	view = g.View(80, 24)
	if !strings.Contains(view, "Loading") {
		t.Errorf("loading grid missing 'Loading':\n%s", view)
	}
}

func TestGridGolden(t *testing.T) {
	for _, size := range []struct {
		w, h int
		name string
	}{
		{80, 24, "grid_80x24.golden"},
		{120, 40, "grid_120x40.golden"},
	} {
		g := newGrid(size.w, size.h)
		goldenCheck(t, g.View(size.w, size.h), size.name)
	}
}

func TestCellModalRender(t *testing.T) {
	cm := CellModal{Title: "users.email", Cell: db.Cell{Text: "a@x.com"}, Theme: theme.Default()}
	view := cm.Render(80, 24)
	if !strings.Contains(view, "a@x.com") {
		t.Errorf("cell modal missing value:\n%s", view)
	}
	cm2 := CellModal{Title: "users.created", Cell: db.Cell{Null: true}, Theme: theme.Default()}
	view2 := cm2.Render(80, 24)
	if !strings.Contains(view2, "NULL") {
		t.Errorf("cell modal missing NULL:\n%s", view2)
	}
}

func (g *Grid) SetCursor(row, col int) Grid {
	g.cursorRow = row
	g.cursorCol = col
	g.clampCursor()
	return *g
}

func (g Grid) CursorRow() int { return g.cursorRow }
func (g Grid) CursorCol() int { return g.cursorCol }

func updateGrid(t *testing.T, g Grid, msg tea.Msg) (Grid, GridEvent) {
	t.Helper()
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		t.Fatalf("message = %T, want tea.KeyMsg", msg)
	}
	return g.Update(key)
}

func goldenCheck(t *testing.T, got, name string) {
	t.Helper()
	dir := filepath.Join("testdata", "golden")
	path := filepath.Join(dir, name)
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		_ = os.MkdirAll(dir, 0o755)
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		_ = os.MkdirAll(dir, 0o755)
		_ = os.WriteFile(path, []byte(got), 0o644)
		t.Fatalf("missing golden file %s (created); re-run", path)
	}
	if got != string(want) {
		t.Errorf("golden mismatch %s", path)
	}
}

func runewidthMaxWidth(view string) int {
	max := 0
	for _, line := range strings.Split(view, "\n") {
		if w := runewidth.StringWidth(line); w > max {
			max = w
		}
	}
	return max
}
