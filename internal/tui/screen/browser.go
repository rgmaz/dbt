package screen

import (
	"sort"
	"strings"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/rgmaz/dbt/internal/db"
	dbtlog "github.com/rgmaz/dbt/internal/log"
	"github.com/rgmaz/dbt/internal/tui/component"
	"github.com/rgmaz/dbt/internal/tui/keys"
	"github.com/rgmaz/dbt/internal/tui/theme"
)

// LoadSchemasMsg asks the root model to reload the schema list. A refresh also
// invalidates any in-flight relation loads.
type LoadSchemasMsg struct{}

// LoadRelationsMsg asks the root model to load one schema's relations.
type LoadRelationsMsg struct {
	Schema string
}

// CatalogScope identifies which catalog request a response belongs to.
type CatalogScope int

// Catalog request scopes.
const (
	ScopeSchemas CatalogScope = iota
	ScopeRelations
)

// SchemasLoadedMsg is a successful schema-list response.
type SchemasLoadedMsg struct {
	OpID    uint64
	Schemas []db.Schema
}

// RelationsLoadedMsg is a successful relation-list response.
type RelationsLoadedMsg struct {
	OpID      uint64
	Schema    string
	Relations []db.Relation
}

// CatalogErrMsg is a failed catalog request. The root model has already
// discarded stale responses before forwarding it to the browser.
type CatalogErrMsg struct {
	OpID   uint64
	Scope  CatalogScope
	Schema string
	Err    error
}

// DetailLoadedMsg is a successful Describe response.
type DetailLoadedMsg struct {
	OpID   uint64
	Ref    db.RelationRef
	Detail db.RelationDetail
}

// DetailErrMsg is a failed Describe request.
type DetailErrMsg struct {
	OpID uint64
	Ref  db.RelationRef
	Err  error
}

// LoadDetailMsg asks the root model to describe one relation.
type LoadDetailMsg struct {
	Ref db.RelationRef
}

// PageLoadedMsg is a successful Page response.
type PageLoadedMsg struct {
	OpID   uint64
	Ref    db.RelationRef
	Page   int
	Result *db.Result
}

// PageErrMsg is a failed Page request.
type PageErrMsg struct {
	OpID uint64
	Ref  db.RelationRef
	Err  error
}

// LoadPageMsg asks the root model to fetch one page.
type LoadPageMsg struct {
	Ref      db.RelationRef
	Page     int
	PageSize int
	Order    []db.Order
}

// CountLoadedMsg is a Count response.
type CountLoadedMsg struct {
	OpID  uint64
	Ref   db.RelationRef
	Count db.Count
}

// LoadCountMsg asks the root model to fetch a row count.
type LoadCountMsg struct {
	Ref db.RelationRef
}

// CountErrMsg is a failed Count request.
type CountErrMsg struct {
	OpID  uint64
	Ref   db.RelationRef
	Err   error
}

// QueryDoneMsg is a SQL query result.
type QueryDoneMsg struct {
	OpID   uint64
	Result *db.Result
	Err    error
}

// RunQueryMsg asks the root model to execute read-only SQL.
type RunQueryMsg struct {
	SQL     string
	MaxRows int
}

// ErrModalMsg asks the root model to open an error modal.
type ErrModalMsg struct {
	Err error
}

// Tab identifies a browser pane.
type Tab int

// Browser tabs.
const (
	TabData Tab = iota
	TabStructure
	TabSQL
)

// String returns the tab label.
func (t Tab) String() string {
	switch t {
	case TabData:
		return "Data"
	case TabStructure:
		return "Structure"
	case TabSQL:
		return "SQL"
	default:
		return "Unknown"
	}
}

type focusArea int

const (
	focusSidebar focusArea = iota
	focusMain
)

// BrowserOptions configures the browser shell.
type BrowserOptions struct {
	Connection string
	Kind       db.Kind
	Version    string
	ReadOnly   bool
	Theme      theme.Theme
	Keys       keys.Map
}

// Browser is the main browsing shell.
type Browser struct {
	opts BrowserOptions

	width, height int

	tab   Tab
	focus focusArea
	tree  component.Tree

	selected *db.Relation

	// Components for main panes.
	data      component.Grid
	structure component.Structure
	sql       component.SQL

	// MainPane state.
	dataTotal    int

	cellModal *component.CellModal
	mainErr    string

	// Sorting state for the selected relation.
	sortCol         int
	sortDesc        bool
	pageSize        int
	sidebarOverride int
}

// NewBrowser constructs the browser shell.
func NewBrowser(opts BrowserOptions) Browser {
	b := Browser{
		opts:     opts,
		focus:    focusSidebar,
		tree: component.NewTree(component.TreeOptions{
			Theme: opts.Theme,
			Keys:  opts.Keys,
		}),
		data:      component.NewGrid(component.GridOptions{Theme: opts.Theme, Keys: opts.Keys}),
		structure: component.NewStructure(component.StructureOptions{Theme: opts.Theme, Keys: opts.Keys}),
		sql:       component.NewSQL(component.SQLOptions{Theme: opts.Theme, Keys: opts.Keys}),
		sortCol:   -1,
		pageSize:  100,
	}
	return b
}

// Init implements Screen. The browser starts by loading the schema list.
func (b Browser) Init() tea.Cmd { return loadSchemasCmd() }

// ID implements Screen.
func (b Browser) ID() ID { return BrowserScreen }

// CapturesText implements Screen.
func (b Browser) CapturesText() bool {
	if b.sql.CapturesText() {
		return true
	}
	if b.tree.CapturesText() {
		return true
	}
	return false
}

// Tab reports the active tab.
func (b Browser) Tab() Tab { return b.tab }

// Update implements Screen.
func (b Browser) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		b.width = max(msg.Width, 0)
		b.height = max(msg.Height, 0)
		b.tree = b.tree.SetSize(b.sidebarInnerWidth(), b.height)
		b.data = b.data.SetSize(b.mainInnerWidth(), b.height-2)
		b.structure = b.structure.SetSize(b.mainInnerWidth(), b.height-2)
		b.sql = b.sql.SetSize(b.mainInnerWidth(), b.height-2)
	case tea.KeyMsg:
		return b.updateKey(msg)
	case SchemasLoadedMsg:
		b.tree = b.tree.SetSchemas(msg.Schemas)
		return b, b.loadExpandedCmd()
	case RelationsLoadedMsg:
		b.tree = b.tree.SetRelations(msg.Schema, msg.Relations)
	case CatalogErrMsg:
		message := dbtlog.Redact(msg.Err.Error())
		switch msg.Scope {
		case ScopeSchemas:
			b.tree = b.tree.SetSchemasError(message)
		case ScopeRelations:
			b.tree = b.tree.SetRelationsError(msg.Schema, message)
		}
	case DetailLoadedMsg:
		if b.selected != nil && msg.Ref == b.selected.Ref() {
			b.structure = b.structure.SetDetail(msg.Detail)
		}
	case DetailErrMsg:
		if b.selected != nil && msg.Ref == b.selected.Ref() {
			b.structure = b.structure.SetError(dbtlog.Redact(msg.Err.Error()))
		}
	case PageLoadedMsg:
		if b.selected == nil || msg.Ref != b.selected.Ref() {
			break
		}
		page := msg.Page
		gp := component.GridPage{
			Columns:   []db.ResultColumn{},
			Rows:      [][]db.Cell{},
			Page:      page,
			PageSize:  b.pageSize,
			Total:     b.dataTotal,
			Truncated: false,
		}
		if msg.Result != nil {
			gp.Columns = msg.Result.Columns
			gp.Rows = msg.Result.Rows
			gp.Truncated = msg.Result.Truncated
		}
		if msg.Result != nil {
			gp.Truncated = msg.Result.Truncated
		}
		b.data = b.data.SetData(gp)
	case PageErrMsg:
		if b.selected == nil || msg.Ref != b.selected.Ref() {
			break
		}
		b.data = b.data.SetLoading(false)
		b.mainErr = dbtlog.Redact(msg.Err.Error())
	case CountErrMsg:
		if b.selected == nil || msg.Ref != b.selected.Ref() {
			break
		}
		b.mainErr = dbtlog.Redact(msg.Err.Error())
	case CountLoadedMsg:
		if b.selected == nil || msg.Ref != b.selected.Ref() {
			break
		}
		countVal := 0
		if msg.Count.Exact {
			countVal = int(msg.Count.Value)
		} else if msg.Count.Value >= 0 {
			countVal = int(msg.Count.Value)
		}
		b.dataTotal = countVal
		b.data = b.data.SetTotal(countVal)
	case QueryDoneMsg:
		gp := component.GridPage{Page: 0, PageSize: db.DefaultMaxRows}
		if msg.Err != nil {
			b.sql = b.sql.SetError(dbtlog.Redact(msg.Err.Error()))
		} else if msg.Result != nil {
			gp.Columns = msg.Result.Columns
			gp.Rows = msg.Result.Rows
			gp.Truncated = msg.Result.Truncated
			b.sql = b.sql.SetResult(gp)
		} else {
			b.sql = b.sql.SetResult(gp)
		}
	}
	return b, nil
}

func (b Browser) updateKey(msg tea.KeyMsg) (Screen, tea.Cmd) {
	// Tab switching first.
	if !b.tree.CapturesText() && !b.sql.CapturesText() {
		switch {
		case key.Matches(msg, b.opts.Keys.Global.DataTab):
			b.tab = TabData
			return b, nil
		case key.Matches(msg, b.opts.Keys.Global.StructureTab):
			b.tab = TabStructure
			return b, nil
		case key.Matches(msg, b.opts.Keys.Global.SQLTab):
			b.tab = TabSQL
			return b, nil
		}
	}

	if !b.tree.CapturesText() && !b.sql.CapturesText() && key.Matches(msg, b.opts.Keys.Global.Focus) {
		if b.focus == focusSidebar {
			b.focus = focusMain
		} else {
			b.focus = focusSidebar
		}
		return b, nil
	}

	if b.focus != focusSidebar {
		// route to active component
		switch b.tab {
		case TabData:
			return b.handleDataKey(msg)
		case TabStructure:
			b.structure, _ = b.structure.Update(msg)
			return b, nil
		case TabSQL:
			sq, ev := b.sql.Update(msg)
			b.sql = sq
			return b.handleSQLEvent(ev)
		}
		return b, nil
	}

	// sidebar
	if msg.String() == "+" {
		b.sidebarOverride++
		return b, nil
	}
	if msg.String() == "-" {
		b.sidebarOverride--
		return b, nil
	}
	tree, event := b.tree.Update(msg)
	b.tree = tree

	switch event.Kind {
	case component.TreeEventExpandSchema:
		return b, loadRelationsCmd(event.Schema)
	case component.TreeEventOpenRelation:
		rel := event.Relation
		b.selected = &rel
		b.dataTotal = 0
		b.data = b.data.SetLoading(true)
		btab := b
		b.structure = b.structure.SetLoading(true)
		b.sql = b.sql.AddHistory("") // noop-ish
		_ = btab
		cmds := []tea.Cmd{
			loadDetailCmd(rel.Ref()),
			loadCountCmd(rel.Ref()),
			loadPageCmd(rel.Ref(), 0, b.pageSize, nil),
		}
		return b, tea.Batch(cmds...)
	case component.TreeEventRefresh:
		b.selected = nil
		b.tree = b.tree.BeginRefresh()
		return b, loadSchemasCmd()
	}
	return b, nil
}

func (b Browser) handleDataKey(msg tea.KeyMsg) (Screen, tea.Cmd) {
	g, ev := b.data.Update(msg)
	b.data = g
	switch ev.Kind {
	case component.GridEventPage:
		if b.selected == nil {
			return b, nil
		}
		return b, loadPageCmd(b.selected.Ref(), ev.Page, b.pageSize, b.currentOrder())
	case component.GridEventSort:
		if b.selected == nil {
			return b, nil
		}
		b.sortCol = ev.Column
		b.sortDesc = ev.Desc
		return b, loadPageCmd(b.selected.Ref(), 0, b.pageSize, b.currentOrder())
	case component.GridEventCellDetail:
		cm := component.CellModal{Title: b.selected.Ref().String() + "." + ev.Header, Cell: ev.Cell, Theme: b.opts.Theme}
		b.cellModal = &cm
	case component.GridEventCopyCell:
		_ = clipboard.WriteAll(ev.Cell.Text)
	case component.GridEventCopyRow:
		// best-effort: copy first cell of row as tsv placeholder
		_ = ev
	case component.GridEventReload:
		if b.selected == nil {
			return b, nil
		}
		return b, loadPageCmd(b.selected.Ref(), b.data.Page(), b.pageSize, b.currentOrder())
	}
	return b, nil
}

func (b Browser) currentOrder() []db.Order {
	if b.selected == nil || b.sortCol < 0 {
		// default primary key? none here; keep nil
		return nil
	}
	if b.sortCol >= len(b.data.Columns()) {
		return nil
	}
	col := b.data.ColumnName(b.sortCol)
	if col == "" {
		return nil
	}
	return []db.Order{{Column: col, Desc: b.sortDesc}}
}

func (b Browser) handleSQLEvent(ev component.SQLEvent) (Screen, tea.Cmd) {
	switch ev.Kind {
	case component.SQLEventExecute:
		return b, runQueryCmd(ev.SQL, db.DefaultMaxRows)
	case component.SQLEventClear:
		// nothing extra
	case component.SQLEventCancel:
		return b, nil
	}
	return b, nil
}

// Helpers
func (b Browser) loadExpandedCmd() tea.Cmd {
	schemas := b.tree.ExpandedSchemas()
	cmds := make([]tea.Cmd, 0, len(schemas))
	for _, schema := range schemas {
		cmds = append(cmds, loadRelationsCmd(schema))
	}
	switch len(cmds) {
	case 0:
		return nil
	case 1:
		return cmds[0]
	default:
		return tea.Batch(cmds...)
	}
}

func loadSchemasCmd() tea.Cmd {
	return func() tea.Msg { return LoadSchemasMsg{} }
}

func loadRelationsCmd(schema string) tea.Cmd {
	return func() tea.Msg { return LoadRelationsMsg{Schema: schema} }
}

func loadDetailCmd(ref db.RelationRef) tea.Cmd {
	return func() tea.Msg { return LoadDetailMsg{Ref: ref} }
}

func loadCountCmd(ref db.RelationRef) tea.Cmd {
	return func() tea.Msg { return LoadCountMsg{Ref: ref} }
}

func loadPageCmd(ref db.RelationRef, page, pageSize int, order []db.Order) tea.Cmd {
	return func() tea.Msg { return LoadPageMsg{Ref: ref, Page: page, PageSize: pageSize, Order: order} }
}

func runQueryCmd(sql string, maxRows int) tea.Cmd {
	return func() tea.Msg { return RunQueryMsg{SQL: sql, MaxRows: maxRows} }
}

// View
func (b Browser) View() string {
	if b.width <= 0 || b.height <= 0 {
		return ""
	}

	// Handle cell modal overlay in content region: render beneath? Show modal in main.
	sidebarWidth := b.sidebarWidth()
	mainWidth := b.width - sidebarWidth
	if sidebarWidth > 0 {
		mainWidth--
	}

	if sidebarWidth == 0 {
		return b.mainView(mainWidth)
	}
	return lipgloss.JoinHorizontal(
		lipgloss.Top,
		b.sidebarView(sidebarWidth),
		b.mainView(mainWidth),
	)
}

func (b Browser) sidebarWidth() int {
	if b.sidebarOverride != 0 {
		w := 20 + b.sidebarOverride
		if w < 8 {
			w = 8
		}
		if b.width > 0 && w > b.width-20 {
			w = b.width - 20
		}
		if w < 1 {
			w = 1
		}
		return w
	}
	if b.width <= 0 {
		return 0
	}
	width := 20
	if b.width >= 100 {
		width = 24
	}
	if b.width < 60 {
		width = max(b.width/3, 8)
	}
	if b.width-width-1 < 20 {
		return 0
	}
	return width
}

func (b Browser) sidebarInnerWidth() int {
	return max(b.sidebarWidth()-2, 1)
}

func (b Browser) mainInnerWidth() int {
	_ = b.sidebarWidth()
	if b.width <= 0 {
		return 1
	}
	w := b.width - b.sidebarWidth()
	if b.sidebarWidth() > 0 {
		w--
	}
	if w < 1 {
		w = 1
	}
	return w
}

func (b Browser) sidebarView(width int) string {
	style := b.opts.Theme.Pane
	if b.focus == focusSidebar {
		style = b.opts.Theme.PaneFocus
	}
	body := b.tree.View(max(width-2, 1), b.height-2)
	return style.Width(width - 2).Height(b.height - 2).Render(body)
}

func (b Browser) mainView(width int) string {
	if width <= 0 {
		return ""
	}
	style := b.opts.Theme.Pane
	if b.focus == focusMain {
		style = b.opts.Theme.PaneFocus
	}
	titleH := 1
	bodyH := b.height - titleH - 3
	if bodyH < 1 {
		bodyH = 1
	}
	innerW := width - 2
	if innerW < 1 {
		innerW = 1
	}
	tabs := b.tabsView()
	var body string
	if b.cellModal != nil {
		body = b.cellModal.Render(innerW, bodyH)
	} else {
		switch b.tab {
		case TabStructure:
			body = b.structure.View(innerW, bodyH)
		case TabSQL:
			body = b.sql.View(innerW, bodyH)
		default:
			body = b.data.View(innerW, bodyH)
		}
	}
	content := lipgloss.JoinVertical(lipgloss.Left, tabs, body)
	if b.mainErr != "" {
		content = lipgloss.JoinVertical(lipgloss.Left, tabs, b.opts.Theme.Error.Render(runewidth.Truncate("error: "+b.mainErr, width, "…")), body)
	}
	return style.Width(width - 2).Height(b.height - 2).Render(content)
}

func (b Browser) tabsView() string {
	tabs := []struct {
		tab   Tab
		label string
	}{{TabData, "Data"}, {TabStructure, "Structure"}, {TabSQL, "SQL"}}
	parts := make([]string, 0, len(tabs))
	for _, item := range tabs {
		text := " " + item.label + " "
		if item.tab == b.tab {
			parts = append(parts, b.opts.Theme.TabActive.Render(text))
		} else {
			parts = append(parts, b.opts.Theme.TabInactive.Render(text))
		}
	}
	return strings.Join(parts, " ")
}


// Grid helpers used by browser:
// need Data() columns/name. Add methods on Grid via component. Let's add here? They belong component; but add wrappers to avoid import issues.
// We'll add to component.Grid separately later if needed. For now implement locally via stored grid isn't possible to access internals. Use component.Grid public methods needed.
var _ = sort.Strings
var _ = db.Relation{}
var _ = clipboard.WriteAll
var _ = runewidth.StringWidth
var _ = tea.Batch
var _ = lipgloss.JoinVertical
