package screen

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rgmaz/dbt/internal/db"
	"github.com/rgmaz/dbt/internal/tui/keys"
	"github.com/rgmaz/dbt/internal/tui/theme"
)

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

// BrowserOptions configures the browser shell.
type BrowserOptions struct {
	Connection string
	Kind       db.Kind
	Version    string
	ReadOnly   bool
	Theme      theme.Theme
	Keys       keys.Map
}

// Browser is the main browsing shell. Task 8 provides the layout and tab
// routing; later tasks fill the sidebar, grid, structure pane, and editor.
type Browser struct {
	opts          BrowserOptions
	width, height int
	tab           Tab
}

// NewBrowser constructs the browser shell.
func NewBrowser(opts BrowserOptions) Browser {
	return Browser{opts: opts}
}

// Init implements Screen.
func (b Browser) Init() tea.Cmd { return nil }

// ID implements Screen.
func (b Browser) ID() ID { return BrowserScreen }

// CapturesText implements Screen.
func (b Browser) CapturesText() bool { return false }

// Tab reports the active tab.
func (b Browser) Tab() Tab { return b.tab }

// Update implements Screen.
func (b Browser) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		b.width = max(msg.Width, 0)
		b.height = max(msg.Height, 0)
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, b.opts.Keys.Global.DataTab):
			b.tab = TabData
		case key.Matches(msg, b.opts.Keys.Global.StructureTab):
			b.tab = TabStructure
		case key.Matches(msg, b.opts.Keys.Global.SQLTab):
			b.tab = TabSQL
		}
	}
	return b, nil
}

// View implements Screen.
func (b Browser) View() string {
	if b.width <= 0 || b.height <= 0 {
		return ""
	}

	sidebarWidth := 20
	if b.width >= 100 {
		sidebarWidth = 24
	}
	if b.width < 60 {
		sidebarWidth = max(b.width/3, 8)
	}
	if b.width-sidebarWidth-1 < 20 {
		sidebarWidth = 0
	}

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

func (b Browser) sidebarView(width int) string {
	body := b.opts.Theme.SectionTitle.Render("Schemas") + "\n\n" +
		b.opts.Theme.Dim.Render("No schemas loaded")
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
		return "No relation selected.\n\nSelect a relation to inspect columns, indexes, and foreign keys."
	case TabSQL:
		return "Read-only session.\n\nThe SQL editor is not available yet."
	default:
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
