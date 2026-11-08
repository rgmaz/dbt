package component

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/rgmaz/dbt/internal/db"
	"github.com/rgmaz/dbt/internal/tui/keys"
	"github.com/rgmaz/dbt/internal/tui/theme"
)

func sampleDetail() db.RelationDetail {
	def := "now()"
	return db.RelationDetail{
		Relation: db.Relation{Schema: "public", Name: "users", Kind: db.RelationTable},
		Columns: []db.Column{
			{Name: "id", DataType: "bigint", Nullable: false, IsPrimaryKey: true},
			{Name: "email", DataType: "text", Nullable: true, Default: &def},
			{Name: "note", DataType: "text", Nullable: true, IsGenerated: true, Comment: "freeform"},
		},
		Indexes: []db.Index{
			{Name: "users_pkey", Columns: []string{"id"}, Primary: true, Unique: true, Method: "btree"},
		},
		ForeignKeys: []db.ForeignKey{
			{Name: "users_team_fkey", Columns: []string{"team_id"}, RefSchema: "public", RefTable: "teams", RefColumns: []string{"id"}, OnDelete: "CASCADE", OnUpdate: "NO ACTION"},
		},
		DDL:          "CREATE TABLE public.users (\n  id bigint PRIMARY KEY,\n  email text\n)",
		DDLSupported: true,
	}
}

func newStructure(width, height int) Structure {
	s := NewStructure(StructureOptions{Theme: theme.Default(), Keys: keys.Default()})
	s = s.SetSize(width, height)
	return s.SetDetail(sampleDetail())
}

func TestStructureRendersMetadata(t *testing.T) {
	s := newStructure(120, 40)
	view := s.View(120, 40)
	for _, want := range []string{"public.users", "Columns", "email", "Indexes", "users_pkey", "btree", "Foreign keys", "users_team_fkey", "CASCADE", "DDL", "CREATE TABLE"} {
		if !strings.Contains(view, want) {
			t.Errorf("structure view missing %q:\n%s", want, view)
		}
	}
}

func TestStructureEmptyAndLoading(t *testing.T) {
	s := NewStructure(StructureOptions{Theme: theme.Default(), Keys: keys.Default()}).SetSize(80, 24)
	view := s.View(80, 24)
	if !strings.Contains(view, "No relation selected") {
		t.Errorf("empty structure missing placeholder:\n%s", view)
	}
	s2 := NewStructure(StructureOptions{Theme: theme.Default(), Keys: keys.Default()}).SetSize(80, 24)
	s2 = s2.SetLoading(true)
	view2 := s2.View(80, 24)
	if !strings.Contains(view2, "Loading") {
		t.Errorf("loading structure missing Loading:\n%s", view2)
	}
}

func TestStructureDDLFallback(t *testing.T) {
	d := sampleDetail()
	d.DDLSupported = false
	s := NewStructure(StructureOptions{Theme: theme.Default(), Keys: keys.Default()}).SetSize(80, 24)
	s = s.SetDetail(d)
	view := s.View(80, 24)
	if !strings.Contains(view, "not available") {
		t.Errorf("structure view missing DDL fallback:\n%s", view)
	}
}

func TestStructureBounded(t *testing.T) {
	s := newStructure(80, 24)
	view := s.View(80, 24)
	if w := runewidthMaxWidth(view); w > 80 {
		t.Errorf("structure width=%d > 80", w)
	}
	if lines := strings.Count(view, "\n") + 1; lines != 24 {
		t.Errorf("structure lines=%d != 24", lines)
	}
}

func TestStructureGolden(t *testing.T) {
	for _, size := range []struct {
		w, h int
		name string
	}{
		{80, 24, "structure_80x24.golden"},
		{120, 40, "structure_120x40.golden"},
	} {
		s := newStructure(size.w, size.h)
		goldenCheck(t, s.View(size.w, size.h), size.name)
	}
}

func TestStructureUpdateScroll(t *testing.T) {
	s := newStructure(80, 24)
	s, ev := s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	_ = ev
	// just ensure no panic
	if s.View(80, 24) == "" {
		t.Fatal("view empty")
	}
}
