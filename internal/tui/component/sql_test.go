package component

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/rgmaz/dbt/internal/db"
	"github.com/rgmaz/dbt/internal/tui/keys"
	"github.com/rgmaz/dbt/internal/tui/theme"
)

func newSQL(width, height int) SQL {
	s := NewSQL(SQLOptions{Theme: theme.Default(), Keys: keys.Default()})
	return s.SetSize(width, height)
}

func TestSQLConstruction(t *testing.T) {
	s := newSQL(80, 24)
	if s.CapturesText() {
		t.Error("should not capture text before editing")
	}
	view := s.View(80, 24)
	if !strings.Contains(view, "SQL") {
		t.Errorf("sql view missing header:\n%s", view)
	}
}

func TestSQLEditorEnterEdit(t *testing.T) {
	s := newSQL(80, 24)
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	if !s.CapturesText() {
		t.Fatal("did not enter editing mode")
	}
	s.editor.SetValue("select 1")
	if s.EditorText() != "select 1" {
		t.Fatalf("editor text = %q", s.EditorText())
	}
	s.editing = false
	s.editor.Blur()
	if s.CapturesText() {
		t.Fatal("still capturing after blur")
	}
}

func TestSQLClear(t *testing.T) {
	s := newSQL(80, 24)
	s.editor.SetValue("select 1")
	s, ev := s.Update(tea.KeyMsg{Type: tea.KeyCtrlL})
	if ev.Kind != SQLEventClear {
		t.Fatalf("clear event = %+v", ev)
	}
	if s.EditorText() != "" {
		t.Errorf("editor not cleared: %q", s.EditorText())
	}
}

func TestSQLHistory(t *testing.T) {
	s := newSQL(80, 24)
	s = s.AddHistory("select 1")
	s = s.AddHistory("select 2")
	s, ev := s.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	if ev.Kind != SQLEventHistory {
		t.Fatalf("history event = %+v", ev)
	}
	if s.EditorText() != "select 2" {
		t.Errorf("history load = %q, want select 2", s.EditorText())
	}
}

func TestSQLExecuteKey(t *testing.T) {
	s := newSQL(80, 24)
	s, _ = s.Update(runeKey('i'))
	s.editor.SetValue("select 1")

	s, ev := s.Update(tea.KeyMsg{Type: tea.KeyCtrlJ})
	if ev.Kind != SQLEventExecute {
		t.Fatalf("event = %+v, want execute", ev)
	}
	if ev.SQL != "select 1" {
		t.Errorf("execute SQL = %q, want %q", ev.SQL, "select 1")
	}
	if !s.CapturesText() {
		t.Error("execute should keep the editor focused")
	}
}

func TestSQLRunningState(t *testing.T) {
	s := newSQL(80, 24)
	s = s.SetRunning(true)
	view := s.View(80, 24)
	if !strings.Contains(view, "running") {
		t.Errorf("running state missing:\n%s", view)
	}
	s = s.SetResult(GridPage{Page: 0, PageSize: 100, Rows: [][]db.Cell{{{Text: "1"}}}, Columns: []db.ResultColumn{{Name: "result"}}})
	view = s.View(80, 24)
	if !strings.Contains(view, "result") {
		t.Errorf("result missing:\n%s", view)
	}
}

func TestSQLBounded(t *testing.T) {
	s := newSQL(80, 24)
	view := s.View(80, 24)
	if w := runewidthMaxWidth(view); w > 80 {
		t.Errorf("sql width=%d > 80", w)
	}
	if lines := strings.Count(view, "\n") + 1; lines != 24 {
		t.Errorf("sql lines=%d != 24", lines)
	}
}
