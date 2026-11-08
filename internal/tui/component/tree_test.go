package component

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rgmaz/dbt/internal/db"
	"github.com/rgmaz/dbt/internal/tui/keys"
	"github.com/rgmaz/dbt/internal/tui/theme"
)

func TestTreeSchemasAndExpansion(t *testing.T) {
	tree := testTree(24, 12).SetSchemas([]db.Schema{{Name: "public"}})

	view := tree.View(24, 12)
	for _, want := range []string{"Schemas", "public", "/ filter"} {
		if !strings.Contains(view, want) {
			t.Errorf("tree view missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "Loading schemas") {
		t.Errorf("tree still loading after SetSchemas:\n%s", view)
	}

	tree, event := updateTree(t, tree, runeKey('l'))
	if event.Kind != TreeEventExpandSchema || event.Schema != "public" {
		t.Fatalf("expand event = %+v, want public expand", event)
	}
	if view := tree.View(24, 12); !strings.Contains(view, "loading") {
		t.Errorf("expanded schema missing loading status:\n%s", view)
	}

	tree = tree.SetRelations("public", []db.Relation{
		{Schema: "public", Name: "users", Kind: db.RelationTable},
		{Schema: "public", Name: "orders", Kind: db.RelationView},
	})
	view = tree.View(24, 12)
	for _, want := range []string{"public", "users", "table", "orders", "view"} {
		if !strings.Contains(view, want) {
			t.Errorf("expanded tree missing %q:\n%s", want, view)
		}
	}

	tree, _ = updateTree(t, tree, runeKey('j'))
	if relation, ok := tree.SelectedRelation(); !ok || relation.Name != "users" {
		t.Fatalf("selection after j = %+v, %v, want users", relation, ok)
	}

	tree, _ = updateTree(t, tree, runeKey('G'))
	if relation, ok := tree.SelectedRelation(); !ok || relation.Name != "orders" {
		t.Fatalf("selection after G = %+v, %v, want orders", relation, ok)
	}

	tree, _ = updateTree(t, tree, runeKey('h'))
	if view := tree.View(24, 12); strings.Contains(view, "users") {
		t.Errorf("h did not collapse the parent schema:\n%s", view)
	}
	if relation, ok := tree.SelectedRelation(); ok {
		t.Errorf("collapsed tree still selects relation %+v", relation)
	}

	tree, _ = updateTree(t, tree, runeKey('l'))
	if view := tree.View(24, 12); !strings.Contains(view, "users") {
		t.Errorf("l did not re-expand the loaded schema:\n%s", view)
	}
}

func TestTreeOpenRelation(t *testing.T) {
	tree := expandedTree(t)
	tree, _ = updateTree(t, tree, runeKey('j'))

	_, event := updateTree(t, tree, tea.KeyMsg{Type: tea.KeyEnter})
	if event.Kind != TreeEventOpenRelation {
		t.Fatalf("enter event = %+v, want open relation", event)
	}
	if event.Relation.Name != "users" {
		t.Errorf("opened relation = %q, want users", event.Relation.Name)
	}
}

func TestTreeFilter(t *testing.T) {
	tree := expandedTree(t)
	tree = tree.SetRelations("analytics", []db.Relation{
		{Schema: "analytics", Name: "events", Kind: db.RelationTable},
	})

	tree, _ = updateTree(t, tree, runeKey('/'))
	if !tree.CapturesText() {
		t.Fatal("filter mode did not capture text")
	}
	for _, r := range "user" {
		tree, _ = updateTree(t, tree, runeKey(r))
	}

	view := tree.View(24, 16)
	for _, want := range []string{"public", "users", "/user_"} {
		if !strings.Contains(view, want) {
			t.Errorf("filtered view missing %q:\n%s", want, view)
		}
	}
	for _, unwanted := range []string{"orders", "analytics", "events"} {
		if strings.Contains(view, unwanted) {
			t.Errorf("filtered view contains %q:\n%s", unwanted, view)
		}
	}

	tree, _ = updateTree(t, tree, runeKey('q'))
	if !tree.CapturesText() {
		t.Fatal("q left filter mode")
	}
	if view := tree.View(24, 16); !strings.Contains(view, "/userq_") {
		t.Errorf("filter text missing q:\n%s", view)
	}

	tree, _ = updateTree(t, tree, tea.KeyMsg{Type: tea.KeyBackspace})
	tree, _ = updateTree(t, tree, tea.KeyMsg{Type: tea.KeyEnter})
	if tree.CapturesText() {
		t.Fatal("enter did not leave filter mode")
	}
	if view := tree.View(24, 16); strings.Contains(view, "orders") {
		t.Errorf("applied filter did not hide orders:\n%s", view)
	}

	tree, _ = updateTree(t, tree, tea.KeyMsg{Type: tea.KeyEsc})
	view = tree.View(24, 16)
	for _, want := range []string{"orders", "analytics"} {
		if !strings.Contains(view, want) {
			t.Errorf("cleared filter missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "events") {
		t.Errorf("cleared filter left a collapsed schema expanded:\n%s", view)
	}
}

func TestTreeFilterRestoresExpansion(t *testing.T) {
	tree := testTree(24, 16).SetSchemas([]db.Schema{{Name: "public"}, {Name: "analytics"}})
	tree = tree.SetRelations("public", []db.Relation{
		{Schema: "public", Name: "users", Kind: db.RelationTable},
	})
	tree = tree.SetRelations("analytics", []db.Relation{
		{Schema: "analytics", Name: "events", Kind: db.RelationTable},
	})

	// Expand public only.
	tree, _ = updateTree(t, tree, runeKey('l'))
	tree = tree.SetRelations("public", []db.Relation{
		{Schema: "public", Name: "users", Kind: db.RelationTable},
	})

	tree, _ = updateTree(t, tree, runeKey('/'))
	for _, r := range "event" {
		tree, _ = updateTree(t, tree, runeKey(r))
	}
	if view := tree.View(24, 16); !strings.Contains(view, "events") {
		t.Errorf("filter did not reveal matching analytics relation:\n%s", view)
	}

	tree, _ = updateTree(t, tree, tea.KeyMsg{Type: tea.KeyEsc})
	if view := tree.View(24, 16); strings.Contains(view, "events") {
		t.Errorf("cleared filter left analytics expanded:\n%s", view)
	}
}

func TestTreeRefresh(t *testing.T) {
	tree := expandedTree(t)
	tree, event := updateTree(t, tree, runeKey('r'))
	if event.Kind != TreeEventRefresh {
		t.Fatalf("refresh event = %+v, want refresh", event)
	}

	tree = tree.BeginRefresh()
	if view := tree.View(24, 12); !strings.Contains(view, "refreshing") {
		t.Errorf("refresh state missing status:\n%s", view)
	}

	tree = tree.SetSchemas([]db.Schema{{Name: "public"}, {Name: "analytics"}})
	if got := tree.ExpandedSchemas(); len(got) != 1 || got[0] != "public" {
		t.Fatalf("expanded schemas after refresh = %v, want [public]", got)
	}
	view := tree.View(24, 12)
	if strings.Contains(view, "users") {
		t.Errorf("refresh kept stale relation data:\n%s", view)
	}
	if !strings.Contains(view, "loading") {
		t.Errorf("refresh did not mark expanded schema loading:\n%s", view)
	}
}

func TestTreeErrors(t *testing.T) {
	tree := testTree(40, 12).SetSchemas([]db.Schema{{Name: "public"}})
	tree = tree.SetSchemasError("database unavailable")
	view := tree.View(40, 12)
	for _, want := range []string{"database unavailable", "r retry", "public"} {
		if !strings.Contains(view, want) {
			t.Errorf("schema error view missing %q:\n%s", want, view)
		}
	}

	tree = tree.SetRelationsError("public", "permission denied")
	view = tree.View(40, 12)
	for _, want := range []string{"public", "permission denied"} {
		if !strings.Contains(view, want) {
			t.Errorf("relation error view missing %q:\n%s", want, view)
		}
	}

	_, event := updateTree(t, tree, runeKey('l'))
	if event.Kind != TreeEventExpandSchema || event.Schema != "public" {
		t.Errorf("retry event = %+v, want public expand", event)
	}
}

func TestTreeEmptyAndBounds(t *testing.T) {
	tree := testTree(16, 8)
	if view := tree.View(16, 8); !strings.Contains(view, "Loading schemas") {
		t.Errorf("initial tree missing loading status:\n%s", view)
	}

	tree = tree.SetSchemas(nil)
	if view := tree.View(16, 8); !strings.Contains(view, "No schemas found") {
		t.Errorf("empty tree missing empty status:\n%s", view)
	}

	relations := make([]db.Relation, 0, 12)
	for i := 0; i < 12; i++ {
		relations = append(relations, db.Relation{
			Schema: "public",
			Name:   "relation-" + string(rune('a'+i)),
			Kind:   db.RelationTable,
		})
	}
	tree = tree.SetSchemas([]db.Schema{{Name: "public"}})
	tree = tree.SetRelations("public", relations)
	tree, _ = updateTree(t, tree, runeKey('l'))
	tree = tree.SetRelations("public", relations)
	tree, _ = updateTree(t, tree, runeKey('G'))

	view := tree.View(16, 8)
	lines := strings.Split(view, "\n")
	if len(lines) > 8 {
		t.Errorf("tree height = %d, want <= 8", len(lines))
	}
	for _, line := range lines {
		if width := lipgloss.Width(line); width > 16 {
			t.Errorf("tree line width = %d, want <= 16: %q", width, line)
		}
	}
	if !strings.Contains(view, "relation-l") {
		t.Errorf("scrolled view missing last relation:\n%s", view)
	}
}

func expandedTree(t *testing.T) Tree {
	t.Helper()

	tree := testTree(24, 16).SetSchemas([]db.Schema{{Name: "public"}, {Name: "analytics"}})
	tree, event := updateTree(t, tree, runeKey('l'))
	if event.Kind != TreeEventExpandSchema || event.Schema != "public" {
		t.Fatalf("expand event = %+v, want public expand", event)
	}
	return tree.SetRelations("public", []db.Relation{
		{Schema: "public", Name: "users", Kind: db.RelationTable},
		{Schema: "public", Name: "orders", Kind: db.RelationView},
	})
}

func testTree(width, height int) Tree {
	return NewTree(TreeOptions{Theme: theme.Default(), Keys: keys.Default()}).SetSize(width, height)
}

func updateTree(t *testing.T, tree Tree, msg tea.Msg) (Tree, TreeEvent) {
	t.Helper()

	key, ok := msg.(tea.KeyMsg)
	if !ok {
		t.Fatalf("message = %T, want tea.KeyMsg", msg)
	}
	return tree.Update(key)
}

func runeKey(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}
