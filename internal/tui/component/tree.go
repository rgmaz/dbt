package component

import (
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"

	"github.com/rgmaz/dbt/internal/db"
	"github.com/rgmaz/dbt/internal/tui/keys"
	"github.com/rgmaz/dbt/internal/tui/theme"
)

// TreeEventKind identifies an action requested by the schema tree.
type TreeEventKind int

// Schema tree actions.
const (
	TreeEventNone TreeEventKind = iota
	TreeEventExpandSchema
	TreeEventOpenRelation
	TreeEventRefresh
)

// TreeEvent tells the browser how to react to a sidebar key. The tree itself
// performs no I/O; the browser translates these events into database requests.
type TreeEvent struct {
	Kind     TreeEventKind
	Schema   string
	Relation db.Relation
}

// TreeOptions configures the schema tree.
type TreeOptions struct {
	Theme theme.Theme
	Keys  keys.Map
}

type schemaNode struct {
	schema    db.Schema
	expanded  bool
	relations []db.Relation
	loaded    bool
	loading   bool
	err       string
}

type treeRowKind int

const (
	treeRowSchema treeRowKind = iota
	treeRowRelation
	treeRowStatus
)

type treeRow struct {
	kind       treeRowKind
	key        string
	schema     string
	relation   db.Relation
	text       string
	selectable bool
	expanded   bool
	loading    bool
	isError    bool
}

// Tree is the schema explorer's pure state. It owns cursor, expansion, filter,
// and scroll state, but never touches a database connection.
type Tree struct {
	opts TreeOptions

	schemas []schemaNode
	cursor  int
	offset  int
	width   int
	height  int

	filter      string
	filtering   bool
	refreshing  bool
	schemasErr  string
	loadingTree bool
}

// NewTree constructs an empty tree that reports an initial schema load.
func NewTree(opts TreeOptions) Tree {
	return Tree{opts: opts, loadingTree: true}
}

// SetSize updates the render area used for scrolling.
func (t Tree) SetSize(width, height int) Tree {
	t.width = max(width, 0)
	t.height = max(height, 0)
	t.clampOffset()
	return t
}

// SetSchemas replaces the schema list while preserving expansion and the
// selected row where possible. Relation data is cleared because a refresh
// always reloads relations for expanded schemas.
func (t Tree) SetSchemas(schemas []db.Schema) Tree {
	key := t.cursorKey()
	old := make(map[string]schemaNode, len(t.schemas))
	for _, node := range t.schemas {
		old[node.schema.Name] = node
	}

	nodes := make([]schemaNode, 0, len(schemas))
	for _, schema := range schemas {
		node := schemaNode{schema: schema}
		if prev, ok := old[schema.Name]; ok {
			node.expanded = prev.expanded
		}
		if node.expanded {
			node.loading = true
		}
		nodes = append(nodes, node)
	}

	t.schemas = nodes
	t.loadingTree = false
	t.refreshing = false
	t.schemasErr = ""
	t.restoreCursor(key)
	return t
}

// SetSchemasError records a failed schema-list load. Existing schemas are kept
// so a transient refresh failure does not wipe the explorer.
func (t Tree) SetSchemasError(message string) Tree {
	t.loadingTree = false
	t.refreshing = false
	t.schemasErr = message
	t.clampCursor()
	return t
}

// SetRelations installs a schema's relations after a successful load.
func (t Tree) SetRelations(schema string, relations []db.Relation) Tree {
	key := t.cursorKey()
	t.schemas = slices.Clone(t.schemas)
	for i := range t.schemas {
		if t.schemas[i].schema.Name != schema {
			continue
		}
		t.schemas[i].relations = slices.Clone(relations)
		t.schemas[i].loaded = true
		t.schemas[i].loading = false
		t.schemas[i].err = ""
		break
	}
	t.restoreCursor(key)
	return t
}

// SetRelationsError records a failed relation load and leaves the schema open
// so the user can retry with enter or l.
func (t Tree) SetRelationsError(schema, message string) Tree {
	key := t.cursorKey()
	t.schemas = slices.Clone(t.schemas)
	for i := range t.schemas {
		if t.schemas[i].schema.Name != schema {
			continue
		}
		t.schemas[i].loading = false
		t.schemas[i].loaded = false
		t.schemas[i].err = message
		t.schemas[i].expanded = true
		break
	}
	t.restoreCursor(key)
	return t
}

// BeginRefresh marks the tree as reloading while keeping current data visible.
func (t Tree) BeginRefresh() Tree {
	t.refreshing = true
	t.loadingTree = true
	t.schemasErr = ""
	return t
}

// ExpandedSchemas returns the schemas whose relations should be loaded after a
// schema-list refresh.
func (t Tree) ExpandedSchemas() []string {
	out := make([]string, 0, len(t.schemas))
	for _, node := range t.schemas {
		if node.expanded {
			out = append(out, node.schema.Name)
		}
	}
	return out
}

// SelectedRelation reports the relation under the cursor, if any.
func (t Tree) SelectedRelation() (db.Relation, bool) {
	rows := t.rows()
	if len(rows) == 0 || t.cursor < 0 || t.cursor >= len(rows) {
		return db.Relation{}, false
	}
	row := rows[t.cursor]
	if row.kind != treeRowRelation {
		return db.Relation{}, false
	}
	return row.relation, true
}

// CapturesText reports whether the tree is accepting filter text. While true,
// single-letter global bindings must not fire.
func (t Tree) CapturesText() bool { return t.filtering }

// Update handles one sidebar key and reports any request the browser must
// perform. Filtering is handled first so q, ?, and / become literal text.
func (t Tree) Update(msg tea.KeyMsg) (Tree, TreeEvent) {
	if t.filtering {
		return t.updateFilter(msg), TreeEvent{}
	}

	switch {
	case msg.Type == tea.KeyEsc:
		if t.filter != "" {
			key := t.cursorKey()
			t.filter = ""
			t.restoreCursor(key)
		}
		return t, TreeEvent{}
	case key.Matches(msg, t.opts.Keys.Sidebar.Filter):
		t.filtering = true
		return t, TreeEvent{}
	case key.Matches(msg, t.opts.Keys.Sidebar.Refresh):
		return t, TreeEvent{Kind: TreeEventRefresh}
	case key.Matches(msg, t.opts.Keys.Sidebar.Down):
		return t.move(1), TreeEvent{}
	case key.Matches(msg, t.opts.Keys.Sidebar.Up):
		return t.move(-1), TreeEvent{}
	case key.Matches(msg, t.opts.Keys.Sidebar.Top):
		return t.jump(true), TreeEvent{}
	case key.Matches(msg, t.opts.Keys.Sidebar.Bottom):
		return t.jump(false), TreeEvent{}
	case key.Matches(msg, t.opts.Keys.Sidebar.Right):
		return t.expand()
	case key.Matches(msg, t.opts.Keys.Sidebar.Left):
		return t.collapse(), TreeEvent{}
	case key.Matches(msg, t.opts.Keys.Sidebar.Enter):
		return t.open()
	default:
		return t, TreeEvent{}
	}
}

func (t Tree) updateFilter(msg tea.KeyMsg) Tree {
	key := t.cursorKey()
	switch msg.Type {
	case tea.KeyEsc:
		t.filtering = false
		t.filter = ""
	case tea.KeyEnter:
		t.filtering = false
	case tea.KeyBackspace:
		runes := []rune(t.filter)
		if len(runes) > 0 {
			t.filter = string(runes[:len(runes)-1])
		}
	case tea.KeyCtrlU:
		t.filter = ""
	case tea.KeySpace:
		t.filter += " "
	case tea.KeyRunes:
		t.filter += string(msg.Runes)
	}
	t.restoreCursor(key)
	return t
}

func (t Tree) move(delta int) Tree {
	rows := t.rows()
	if len(rows) == 0 {
		return t
	}

	index := t.cursor
	if index < 0 || index >= len(rows) {
		index = 0
	}
	step := 1
	if delta < 0 {
		step = -1
		delta = -delta
	}

	for n := 0; n < delta; n++ {
		next := index + step
		for next >= 0 && next < len(rows) && !rows[next].selectable {
			next += step
		}
		if next < 0 || next >= len(rows) {
			break
		}
		index = next
	}

	t.cursor = index
	t.clampOffset()
	return t
}

func (t Tree) jump(top bool) Tree {
	rows := t.rows()
	if len(rows) == 0 {
		return t
	}
	if top {
		for i, row := range rows {
			if row.selectable {
				t.cursor = i
				t.clampOffset()
				return t
			}
		}
		return t
	}
	for i := len(rows) - 1; i >= 0; i-- {
		if rows[i].selectable {
			t.cursor = i
			t.clampOffset()
			return t
		}
	}
	return t
}

func (t Tree) expand() (Tree, TreeEvent) {
	rows := t.rows()
	if len(rows) == 0 {
		return t, TreeEvent{}
	}
	row := rows[t.cursor]
	if row.kind != treeRowSchema {
		return t, TreeEvent{}
	}
	return t.expandSchema(row.schema)
}

func (t Tree) expandSchema(schema string) (Tree, TreeEvent) {
	index := t.schemaIndex(schema)
	if index < 0 {
		return t, TreeEvent{}
	}

	key := t.cursorKey()
	t.schemas = slices.Clone(t.schemas)
	node := &t.schemas[index]

	if !node.loaded {
		node.expanded = true
		if !node.loading {
			node.loading = true
			t.restoreCursor(key)
			return t, TreeEvent{Kind: TreeEventExpandSchema, Schema: schema}
		}
		t.restoreCursor(key)
		return t, TreeEvent{}
	}

	if !node.expanded {
		node.expanded = true
		t.restoreCursor(key)
		return t, TreeEvent{}
	}

	// Already expanded with data: move to the first child relation.
	rows := t.rows()
	for i := t.cursor + 1; i < len(rows); i++ {
		if rows[i].kind == treeRowSchema {
			break
		}
		if rows[i].kind == treeRowRelation && rows[i].schema == schema {
			t.cursor = i
			t.clampOffset()
			break
		}
	}
	return t, TreeEvent{}
}

func (t Tree) collapse() Tree {
	rows := t.rows()
	if len(rows) == 0 {
		return t
	}
	row := rows[t.cursor]

	switch row.kind {
	case treeRowRelation:
		index := t.schemaIndex(row.schema)
		if index < 0 {
			return t
		}
		t.schemas = slices.Clone(t.schemas)
		t.schemas[index].expanded = false
		for i, candidate := range t.rows() {
			if candidate.kind == treeRowSchema && candidate.schema == row.schema {
				t.cursor = i
				break
			}
		}
		t.clampOffset()
	case treeRowSchema:
		index := t.schemaIndex(row.schema)
		if index >= 0 && t.schemas[index].expanded {
			t.schemas = slices.Clone(t.schemas)
			t.schemas[index].expanded = false
			t.clampCursor()
		}
	}
	return t
}

func (t Tree) open() (Tree, TreeEvent) {
	rows := t.rows()
	if len(rows) == 0 {
		return t, TreeEvent{}
	}
	row := rows[t.cursor]

	switch row.kind {
	case treeRowRelation:
		return t, TreeEvent{Kind: TreeEventOpenRelation, Relation: row.relation}
	case treeRowSchema:
		index := t.schemaIndex(row.schema)
		if index >= 0 && t.schemas[index].expanded {
			return t.collapse(), TreeEvent{}
		}
		return t.expandSchema(row.schema)
	default:
		return t, TreeEvent{}
	}
}

func (t Tree) schemaIndex(schema string) int {
	for i := range t.schemas {
		if t.schemas[i].schema.Name == schema {
			return i
		}
	}
	return -1
}

func (t *Tree) restoreCursor(key string) {
	if key != "" {
		for i, row := range t.rows() {
			if row.selectable && row.key == key {
				t.cursor = i
				t.clampOffset()
				return
			}
		}
	}
	t.clampCursor()
}

func (t Tree) cursorKey() string {
	rows := t.rows()
	if len(rows) == 0 || t.cursor < 0 || t.cursor >= len(rows) {
		return ""
	}
	row := rows[t.cursor]
	if !row.selectable {
		return ""
	}
	return row.key
}

func (t *Tree) clampCursor() {
	rows := t.rows()
	if len(rows) == 0 {
		t.cursor = 0
		t.offset = 0
		return
	}

	t.cursor = clampInt(t.cursor, 0, len(rows)-1)
	if rows[t.cursor].selectable {
		t.clampOffset()
		return
	}

	for i := t.cursor; i < len(rows); i++ {
		if rows[i].selectable {
			t.cursor = i
			t.clampOffset()
			return
		}
	}
	for i := t.cursor; i >= 0; i-- {
		if rows[i].selectable {
			t.cursor = i
			t.clampOffset()
			return
		}
	}
	t.cursor = 0
	t.clampOffset()
}

func (t *Tree) clampOffset() {
	visible := t.visibleRows()
	if visible <= 0 {
		t.offset = 0
		return
	}
	rows := t.rows()
	if t.cursor < t.offset {
		t.offset = t.cursor
	}
	if t.cursor >= t.offset+visible {
		t.offset = t.cursor - visible + 1
	}
	t.offset = clampInt(t.offset, 0, max(len(rows)-visible, 0))
}

func (t Tree) visibleRows() int { return max(t.height-2, 0) }

// View renders the sidebar contents inside width x height cells.
func (t Tree) View(width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}

	t.width = width
	t.height = height
	t.clampOffset()

	lines := make([]string, 0, height)
	lines = append(lines, t.opts.Theme.SectionTitle.Render(runewidth.Truncate("Schemas", width, "…")))
	lines = append(lines, t.filterLine(width))

	rows := t.rows()
	visible := t.visibleRows()
	start := min(t.offset, max(len(rows)-visible, 0))
	end := min(start+visible, len(rows))
	for i := start; i < end; i++ {
		text := runewidth.Truncate(t.rowText(rows[i]), width, "…")
		switch {
		case i == t.cursor && rows[i].selectable:
			text = t.opts.Theme.TabActive.Render(text)
		case rows[i].kind == treeRowStatus && rows[i].isError:
			text = t.opts.Theme.Error.Render(text)
		case rows[i].kind == treeRowStatus:
			text = t.opts.Theme.Dim.Render(text)
		}
		lines = append(lines, text)
	}

	if len(lines) > height {
		lines = lines[:height]
	}
	return strings.Join(lines, "\n")
}

func (t Tree) filterLine(width int) string {
	if t.filtering {
		return runewidth.Truncate("/"+t.filter+"_", width, "…")
	}
	if t.filter != "" {
		return t.opts.Theme.Dim.Render(runewidth.Truncate("/"+t.filter+"  esc clear", width, "…"))
	}
	return t.opts.Theme.Dim.Render("/ filter")
}

func (t Tree) rowText(row treeRow) string {
	switch row.kind {
	case treeRowSchema:
		marker := "▸"
		if row.expanded {
			marker = "▾"
		}
		text := marker + " " + row.text
		if row.loading {
			text += " …"
		}
		return text
	case treeRowRelation:
		return "  " + row.text + "  " + relationKindLabel(row.relation.Kind)
	default:
		return row.text
	}
}

func (t Tree) rows() []treeRow {
	query := strings.ToLower(strings.TrimSpace(t.filter))
	rows := make([]treeRow, 0, len(t.schemas)*3)

	if t.schemasErr != "" {
		rows = append(rows,
			treeRow{kind: treeRowStatus, text: "error: " + t.schemasErr, isError: true},
			treeRow{kind: treeRowStatus, text: "r retry"},
		)
	}
	if t.refreshing {
		rows = append(rows, treeRow{kind: treeRowStatus, text: "refreshing…"})
	}
	if t.loadingTree && len(t.schemas) == 0 && t.schemasErr == "" {
		rows = append(rows, treeRow{kind: treeRowStatus, text: "Loading schemas…"})
	}

	for _, node := range t.schemas {
		schemaMatch := query == "" || strings.Contains(strings.ToLower(node.schema.Name), query)
		matchedRelations := make([]db.Relation, 0, len(node.relations))
		for _, relation := range node.relations {
			if query == "" || schemaMatch || strings.Contains(strings.ToLower(relation.Name), query) {
				matchedRelations = append(matchedRelations, relation)
			}
		}
		if query != "" && !schemaMatch && len(matchedRelations) == 0 {
			continue
		}

		expanded := node.expanded || query != ""
		rows = append(rows, treeRow{
			kind:       treeRowSchema,
			key:        "schema:" + node.schema.Name,
			schema:     node.schema.Name,
			text:       node.schema.Name,
			selectable: true,
			expanded:   expanded,
			loading:    node.loading,
		})
		if !expanded {
			continue
		}
		if node.loading {
			rows = append(rows, treeRow{kind: treeRowStatus, text: "  loading…"})
		}
		if node.err != "" {
			rows = append(rows, treeRow{kind: treeRowStatus, text: "  error: " + node.err, isError: true})
		}

		relations := node.relations
		if query != "" && !schemaMatch {
			relations = matchedRelations
		}
		for _, relation := range relations {
			rows = append(rows, treeRow{
				kind:       treeRowRelation,
				key:        "relation:" + relation.Schema + "." + relation.Name,
				schema:     relation.Schema,
				relation:   relation,
				text:       relation.Name,
				selectable: true,
			})
		}
		if node.loaded && len(node.relations) == 0 && query == "" {
			rows = append(rows, treeRow{kind: treeRowStatus, text: "  (empty)"})
		}
	}

	if len(rows) == 0 && !t.loadingTree && t.schemasErr == "" {
		if t.filter != "" {
			rows = append(rows, treeRow{kind: treeRowStatus, text: "No matches"})
		} else {
			rows = append(rows, treeRow{kind: treeRowStatus, text: "No schemas found"})
		}
	}
	return rows
}

func relationKindLabel(kind db.RelationKind) string {
	switch kind {
	case db.RelationTable:
		return "table"
	case db.RelationView:
		return "view"
	case db.RelationMaterializedView:
		return "matview"
	case db.RelationForeignTable:
		return "foreign"
	default:
		return string(kind)
	}
}

func clampInt(value, low, high int) int {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}
