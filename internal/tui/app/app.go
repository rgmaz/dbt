package app

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/rgmaz/dbt/internal/db"
	dbtlog "github.com/rgmaz/dbt/internal/log"
	"github.com/rgmaz/dbt/internal/tui/component"
	"github.com/rgmaz/dbt/internal/tui/keys"
	"github.com/rgmaz/dbt/internal/tui/screen"
	"github.com/rgmaz/dbt/internal/tui/theme"
)

// Minimum terminal size before the shell shows a resize notice.
const (
	MinWidth  = 80
	MinHeight = 24
)

const chromeHeight = 2 // header + status bar

// Options configures the root model.
type Options struct {
	Connection string
	Kind       db.Kind
	Version    string
	ReadOnly   bool
	Theme      string
}

// Model is the root Bubble Tea model. It owns global key handling, layout, and
// the help/error modals, and delegates everything else to the active screen.
type Model struct {
	opts  Options
	theme theme.Theme
	keys  keys.Map

	screen screen.Screen

	width, height int
	ready         bool
	help          bool
	errModal      *component.Modal
}

// New constructs the root model with the browser shell active.
func New(opts Options) Model {
	th := theme.New(opts.Theme)
	km := keys.Default()
	browser := screen.NewBrowser(screen.BrowserOptions{
		Connection: opts.Connection,
		Kind:       opts.Kind,
		Version:    opts.Version,
		ReadOnly:   opts.ReadOnly,
		Theme:      th,
		Keys:       km,
	})

	return Model{
		opts:   opts,
		theme:  th,
		keys:   km,
		screen: browser,
	}
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd { return nil }

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = max(msg.Width, 0)
		m.height = max(msg.Height, 0)
		m.ready = true
		return m.forward(tea.WindowSizeMsg{Width: m.width, Height: contentHeight(m.height)})
	case tea.KeyMsg:
		return m.updateKey(msg)
	default:
		return m.forward(msg)
	}
}

// View implements tea.Model.
func (m Model) View() string {
	if !m.ready {
		return "Initializing…"
	}
	if m.width < MinWidth || m.height < MinHeight {
		notice := m.theme.Notice.Render(fmt.Sprintf(
			"Terminal too small: %d×%d (minimum %d×%d)",
			m.width, m.height, MinWidth, MinHeight,
		))
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, notice)
	}

	contentH := contentHeight(m.height)
	var content string
	switch {
	case m.help:
		content = component.HelpView(m.keys, m.theme, m.width, contentH)
	case m.errModal != nil:
		content = m.errModal.Render(m.width, contentH)
	case m.screen != nil:
		content = m.screen.View()
	}

	return lipgloss.JoinVertical(lipgloss.Left, m.headerView(), content, m.statusView())
}

// SetScreen routes to a new screen and resizes it to the current content area.
func (m Model) SetScreen(s screen.Screen) Model {
	m.screen = s
	if m.ready && s != nil {
		updated, _ := s.Update(tea.WindowSizeMsg{Width: m.width, Height: contentHeight(m.height)})
		m.screen = updated
	}
	return m
}

// ShowError opens the error modal with a redacted message.
func (m Model) ShowError(err error) Model {
	if err == nil {
		m.errModal = nil
		return m
	}
	m.help = false
	m.errModal = &component.Modal{
		Title:  "Error",
		Body:   dbtlog.Redact(err.Error()) + "\n\nesc: dismiss",
		Theme:  m.theme,
		Danger: true,
	}
	return m
}

func (m Model) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, m.keys.Global.ForceQuit) {
		return m, tea.Quit
	}

	if m.help {
		switch {
		case key.Matches(msg, m.keys.Global.Help), key.Matches(msg, m.keys.Global.Back):
			m.help = false
		case key.Matches(msg, m.keys.Global.Quit):
			return m, tea.Quit
		}
		return m, nil
	}

	if m.errModal != nil {
		switch {
		case key.Matches(msg, m.keys.Global.Back):
			m.errModal = nil
		case key.Matches(msg, m.keys.Global.Quit):
			return m, tea.Quit
		}
		return m, nil
	}

	if !m.capturesText() {
		switch {
		case key.Matches(msg, m.keys.Global.Help):
			m.help = true
			return m, nil
		case key.Matches(msg, m.keys.Global.Quit):
			return m, tea.Quit
		}
	}

	return m.forward(msg)
}

func (m Model) forward(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.screen == nil {
		return m, nil
	}
	updated, cmd := m.screen.Update(msg)
	m.screen = updated
	return m, cmd
}

func (m Model) capturesText() bool {
	return m.screen != nil && m.screen.CapturesText()
}

func (m Model) headerView() string {
	parts := []string{"dbt"}
	if connection := strings.TrimSpace(m.opts.Connection); connection != "" {
		parts = append(parts, connection)
	}
	if m.opts.Kind.Valid() {
		parts = append(parts, string(m.opts.Kind))
	}
	if version := strings.TrimSpace(m.opts.Version); version != "" {
		parts = append(parts, version)
	}
	if m.opts.ReadOnly {
		parts = append(parts, "read-only")
	}

	text := runewidth.Truncate(strings.Join(parts, " · "), max(m.width-2, 1), "…")
	return m.theme.Header.Width(m.width).Render(text)
}

func (m Model) statusView() string {
	hint := strings.Join([]string{
		m.theme.StatusKey.Render("?") + " help",
		m.theme.StatusKey.Render("q") + " quit",
		m.theme.StatusKey.Render("tab") + " focus",
		m.theme.StatusKey.Render("1/2/3") + " tabs",
	}, " · ")
	return m.theme.StatusBar.Width(m.width).Render(hint)
}

func contentHeight(height int) int {
	return max(height-chromeHeight, 0)
}
