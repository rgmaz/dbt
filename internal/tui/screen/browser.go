package screen

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

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

// Browser is the main browsing shell. It owns sidebar presentation state and
// delegates all database work to the root model through messages.
type Browser struct {
	opts          BrowserOptions
	width, height int
	tab           Tab
	focus         focusArea
	tree          component.Tree
	selected      *db.Relation
}

// NewBrowser constructs the browser shell.
func NewBrowser(opts BrowserOptions) Browser {
	return Browser{
		opts:  opts,
		focus: focusSidebar,
		tree: component.NewTree(component.TreeOptions{
			Theme: opts.Theme,
			Keys:  opts.Keys,
		}),
	}
}

// Init implements Screen. The browser starts by loading the schema list.
func (b Browser) Init() tea.Cmd { return loadSchemasCmd() }

// ID implements Screen.
func (b Browser) ID() ID { return BrowserScreen }

// CapturesText implements Screen.
func (b Browser) CapturesText() bool { return b.tree.CapturesText() }

// Tab reports the active tab.
func (b Browser) Tab() Tab { return b.tab }

// Update implements Screen.
func (b Browser) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		b.width = max(msg.Width, 0)
		b.height = max(msg.Height, 0)
		b.tree = b.tree.SetSize(b.sidebarInnerWidth(), b.height)
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
	}
	return b, nil
}

func (b Browser) updateKey(msg tea.KeyMsg) (Screen, tea.Cmd) {
	if !b.tree.CapturesText() {
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

	if !b.tree.CapturesText() && key.Matches(msg, b.opts.Keys.Global.Focus) {
		if b.focus == focusSidebar {
			b.focus = focusMain
		} else {
			b.focus = focusSidebar
		}
		return b, nil
	}

	if b.focus != focusSidebar {
		return b, nil
	}

	tree, event := b.tree.Update(msg)
	b.tree = tree

	switch event.Kind {
	case component.TreeEventExpandSchema:
		return b, loadRelationsCmd(event.Schema)
	case component.TreeEventOpenRelation:
		relation := event.Relation
		b.selected = &relation
	case component.TreeEventRefresh:
		b.selected = nil
		b.tree = b.tree.BeginRefresh()
		return b, loadSchemasCmd()
	}
	return b, nil
}

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

// View implements Screen.
func (b Browser) View() string {
	if b.width <= 0 || b.height <= 0 {
		return ""
	}

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
		b.separator(),
		b.mainView(mainWidth),
	)
}

func (b Browser) sidebarWidth() int {
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

func (b Browser) sidebarView(width int) string {
	body := b.tree.View(max(width-2, 1), b.height)
	return b.opts.Theme.Sidebar.Width(width).Height(b.height).Render(body)
}

func (b Browser) mainView(width int) string {
	body := lipgloss.JoinVertical(lipgloss.Left, b.tabsView(), "", b.bodyView())
	return b.opts.Theme.Main.Width(width).Height(b.height).Render(body)
}

func (b Browser) tabsView() string {
	tabs := []struct {
		tab   Tab
		label string
	}{
		{TabData, "Data"},
		{TabStructure, "Structure"},
		{TabSQL, "SQL"},
	}

	parts := make([]string, 0, len(tabs))
	for _, item := range tabs {
		text := " " + item.label + " "
		if item.tab == b.tab {
			parts = append(parts, b.opts.Theme.TabActive.Render("["+text+"]"))
		} else {
			parts = append(parts, b.opts.Theme.TabInactive.Render(text))
		}
	}
	return strings.Join(parts, " ")
}

func (b Browser) bodyView() string {
	switch b.tab {
	case TabStructure:
		if b.selected != nil {
			return fmt.Sprintf(
				"%s · %s\n\nSelect a relation to inspect columns, indexes, and foreign keys.",
				b.selected.Ref(), b.selected.Kind,
			)
		}
		return "No relation selected.\n\nSelect a relation to inspect columns, indexes, and foreign keys."
	case TabSQL:
		return "Read-only session.\n\nThe SQL editor is not available yet."
	default:
		if b.selected != nil {
			return fmt.Sprintf(
				"%s · %s\n\nThe data grid is not available yet.",
				b.selected.Ref(), b.selected.Kind,
			)
		}
		return "No relation selected.\n\nSelect a relation to browse its rows."
	}
}

func (b Browser) separator() string {
	lines := make([]string, b.height)
	for i := range lines {
		lines[i] = b.opts.Theme.Dim.Render("│")
	}
	return strings.Join(lines, "\n")
}
