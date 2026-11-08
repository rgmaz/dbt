package app

import (
	"context"
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
	Context    context.Context
	Connection string
	Kind       db.Kind
	Version    string
	ReadOnly   bool
	Theme      string

	// Picker selects the connection picker instead of the browser at startup.
	Picker     bool
	Profiles   []Profile
	ConfigPath string
	Open       OpenFunc
}

// Profile is a selectable connection supplied by the CLI. Detail is already
// redacted for display; DSN is used only by OpenFunc.
type Profile struct {
	Name   string
	Driver string
	Kind   db.Kind
	Detail string
	DSN    string
}

// ConnectResult is the outcome of opening a profile.
type ConnectResult struct {
	Conn    db.Conn
	Kind    db.Kind
	Version string
	Label   string
}

// OpenFunc opens a profile and reports its server version. The CLI supplies it
// so the TUI never imports concrete drivers.
type OpenFunc func(context.Context, Profile) (ConnectResult, error)

type connectedMsg struct {
	result ConnectResult
}

type connectFailedMsg struct {
	err error
}

// Model is the root Bubble Tea model. It owns global key handling, layout, the
// help/error modals, async connection state, and the active session, and
// delegates everything else to the active screen.
type Model struct {
	opts  Options
	theme theme.Theme
	keys  keys.Map

	screen screen.Screen

	conn          db.Conn
	connecting    bool
	connectCancel context.CancelFunc

	width, height int
	ready         bool
	help          bool
	errModal      *component.Modal
}

// New constructs the root model. When Options.Picker is set the connection
// picker is active; otherwise the browser shell is active.
func New(opts Options) Model {
	if opts.Context == nil {
		opts.Context = context.Background()
	}

	th := theme.New(opts.Theme)
	km := keys.Default()
	m := Model{
		opts:  opts,
		theme: th,
		keys:  km,
	}

	if opts.Picker {
		m.screen = screen.NewPicker(screen.PickerOptions{
			Profiles:   pickerProfiles(opts.Profiles),
			ConfigPath: opts.ConfigPath,
			Theme:      th,
			Keys:       km,
		})
		return m
	}

	m.screen = m.browser()
	return m
}

func (m Model) browser() screen.Browser {
	return screen.NewBrowser(screen.BrowserOptions{
		Connection: m.opts.Connection,
		Kind:       m.opts.Kind,
		Version:    m.opts.Version,
		ReadOnly:   m.opts.ReadOnly,
		Theme:      m.theme,
		Keys:       m.keys,
	})
}

func pickerProfiles(profiles []Profile) []screen.PickerProfile {
	out := make([]screen.PickerProfile, 0, len(profiles))
	for _, profile := range profiles {
		out = append(out, screen.PickerProfile{
			Name:   profile.Name,
			Driver: profile.Driver,
			Kind:   profile.Kind,
			Detail: profile.Detail,
		})
	}
	return out
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd { return nil }

// Close closes the session opened through the picker. Explicit-target
// connections are owned by the CLI, so this is a no-op in browser mode.
func (m Model) Close() error {
	if m.connectCancel != nil {
		m.connectCancel()
	}
	if m.conn == nil {
		return nil
	}
	return m.conn.Close()
}

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
	case screen.ConnectRequestMsg:
		return m.beginConnect(msg.Profile)
	case connectedMsg:
		return m.connected(msg)
	case connectFailedMsg:
		return m.connectFailed(msg)
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

// beginConnect starts an async connection for the selected profile. It is the
// only place that mutates connection state; database I/O runs in the returned
// tea.Cmd.
func (m Model) beginConnect(profile screen.PickerProfile) (tea.Model, tea.Cmd) {
	if m.connecting || m.opts.Open == nil {
		return m, nil
	}

	appProfile, ok := m.profileByName(profile.Name)
	if !ok {
		return m.ShowError(fmt.Errorf("connection profile %q not found", profile.Name)), nil
	}

	ctx, cancel := context.WithCancel(m.opts.Context)
	m.connecting = true
	m.connectCancel = cancel
	if picker, ok := m.screen.(screen.Picker); ok {
		m.screen = picker.Connecting(profile.Name)
	}

	open := m.opts.Open
	return m, func() tea.Msg {
		result, err := open(ctx, appProfile)
		if err != nil {
			return connectFailedMsg{err: err}
		}
		return connectedMsg{result: result}
	}
}

func (m Model) connected(msg connectedMsg) (tea.Model, tea.Cmd) {
	m.finishConnect()
	if m.conn != nil {
		_ = m.conn.Close()
	}
	m.conn = msg.result.Conn
	m.opts.Connection = msg.result.Label
	m.opts.Kind = msg.result.Kind
	m.opts.Version = msg.result.Version
	m.opts.ReadOnly = true
	return m.SetScreen(m.browser()), nil
}

func (m Model) connectFailed(msg connectFailedMsg) (tea.Model, tea.Cmd) {
	m.finishConnect()
	if picker, ok := m.screen.(screen.Picker); ok {
		m.screen = picker.ClearConnecting()
	}
	return m.ShowError(msg.err), nil
}

func (m *Model) finishConnect() {
	m.connecting = false
	if m.connectCancel != nil {
		m.connectCancel()
		m.connectCancel = nil
	}
}

func (m Model) profileByName(name string) (Profile, bool) {
	for _, profile := range m.opts.Profiles {
		if profile.Name == name {
			return profile, true
		}
	}
	return Profile{}, false
}

func (m Model) quit() (tea.Model, tea.Cmd) {
	if m.connectCancel != nil {
		m.connectCancel()
	}
	return m, tea.Quit
}

func (m Model) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, m.keys.Global.ForceQuit) {
		return m.quit()
	}

	if m.help {
		switch {
		case key.Matches(msg, m.keys.Global.Help), key.Matches(msg, m.keys.Global.Back):
			m.help = false
		case key.Matches(msg, m.keys.Global.Quit):
			return m.quit()
		}
		return m, nil
	}

	if m.errModal != nil {
		switch {
		case key.Matches(msg, m.keys.Global.Back):
			m.errModal = nil
		case key.Matches(msg, m.keys.Global.Quit):
			return m.quit()
		}
		return m, nil
	}

	if !m.capturesText() {
		switch {
		case key.Matches(msg, m.keys.Global.Help):
			m.help = true
			return m, nil
		case key.Matches(msg, m.keys.Global.Quit):
			return m.quit()
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
	if m.pickerActive() {
		parts = append(parts, "picker")
	}
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
	var hint string
	if m.pickerActive() {
		hint = strings.Join([]string{
			m.theme.StatusKey.Render("j/k") + " move",
			m.theme.StatusKey.Render("enter") + " connect",
			m.theme.StatusKey.Render("?") + " help",
			m.theme.StatusKey.Render("q") + " quit",
		}, " · ")
	} else {
		hint = strings.Join([]string{
			m.theme.StatusKey.Render("?") + " help",
			m.theme.StatusKey.Render("q") + " quit",
			m.theme.StatusKey.Render("tab") + " focus",
			m.theme.StatusKey.Render("1/2/3") + " tabs",
		}, " · ")
	}
	return m.theme.StatusBar.Width(m.width).Render(hint)
}

func (m Model) pickerActive() bool {
	return m.screen != nil && m.screen.ID() == screen.PickerScreen
}

func contentHeight(height int) int {
	return max(height-chromeHeight, 0)
}
