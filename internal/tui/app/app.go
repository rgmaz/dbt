package app

import (
	"context"
	"fmt"
	"strings"
	"time"

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

	// Conn is the explicit-target session supplied by the CLI. Picker sessions
	// install their own connection after ConnectResult arrives.
	Conn         db.Conn
	QueryTimeout time.Duration

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

type catalogState struct {
	seq         uint64
	schemasOp   uint64
	relationsOp map[string]uint64
	detailOp    uint64
	pageOp      uint64
	countOp     uint64
	queryOp     uint64
	ctx         context.Context
	cancel      context.CancelFunc
}

// Model is the root Bubble Tea model. It owns global key handling, layout, the
// help/error modals, async connection and catalog state, and the active
// session, and delegates everything else to the active screen.
type Model struct {
	opts  Options
	theme theme.Theme
	keys  keys.Map

	screen screen.Screen

	conn          db.Conn
	connecting    bool
	connectCancel context.CancelFunc
	catalog       catalogState

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
	if opts.QueryTimeout <= 0 {
		opts.QueryTimeout = db.DefaultQueryTimeout
	}

	th := theme.New(opts.Theme)
	km := keys.Default()
	m := Model{
		opts:  opts,
		theme: th,
		keys:  km,
	}
	m.resetCatalog()

	if opts.Picker {
		m.screen = screen.NewPicker(screen.PickerOptions{
			Profiles:   pickerProfiles(opts.Profiles),
			ConfigPath: opts.ConfigPath,
			Theme:      th,
			Keys:       km,
		})
		return m
	}

	m.conn = opts.Conn
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

// Init implements tea.Model. The browser schedules its initial schema load.
func (m Model) Init() tea.Cmd {
	if m.screen == nil {
		return nil
	}
	return m.screen.Init()
}

// Close cancels catalog work and closes the active session. Explicit-target
// sessions are passed in by the CLI, which also keeps an idempotent safety-net
// close.
func (m Model) Close() error {
	if m.catalog.cancel != nil {
		m.catalog.cancel()
	}
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
	case screen.LoadSchemasMsg:
		return m.loadSchemas()
	case screen.LoadRelationsMsg:
		return m.loadRelations(msg.Schema)
	case screen.SchemasLoadedMsg:
		if msg.OpID != m.catalog.schemasOp {
			return m, nil
		}
		return m.forward(msg)
	case screen.RelationsLoadedMsg:
		if m.catalog.relationsOp[msg.Schema] != msg.OpID {
			return m, nil
		}
		return m.forward(msg)
	case screen.CatalogErrMsg:
		return m.catalogError(msg)
	case screen.LoadDetailMsg:
		return m.loadDetail(msg.Ref)
	case screen.DetailLoadedMsg:
		if msg.OpID != m.catalog.detailOp {
			return m, nil
		}
		return m.forward(msg)
	case screen.DetailErrMsg:
		if msg.OpID != m.catalog.detailOp {
			return m, nil
		}
		return m.forward(msg)
	case screen.LoadPageMsg:
		return m.loadPage(msg.Ref, msg.Page, msg.PageSize, msg.Order)
	case screen.PageLoadedMsg:
		if msg.OpID != m.catalog.pageOp {
			return m, nil
		}
		return m.forward(msg)
	case screen.PageErrMsg:
		if msg.OpID != m.catalog.pageOp {
			return m, nil
		}
		return m.forward(msg)
	case screen.LoadCountMsg:
		return m.loadCount(msg.Ref)
	case screen.CountLoadedMsg:
		if msg.OpID != m.catalog.countOp {
			return m, nil
		}
		return m.forward(msg)
	case screen.CountErrMsg:
		if msg.OpID != m.catalog.countOp {
			return m, nil
		}
		return m.forward(msg)
	case screen.RunQueryMsg:
		return m.runQuery(msg.SQL, msg.MaxRows)
	case screen.QueryDoneMsg:
		if msg.OpID != m.catalog.queryOp {
			return m, nil
		}
		return m.forward(msg)
	case screen.ErrModalMsg:
		return m.ShowError(msg.Err), nil
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

// resetCatalog invalidates all catalog operations and installs a fresh
// cancellable context. The sequence counter is never reset so late responses
// from a previous session cannot match a new operation.
func (m *Model) resetCatalog() {
	if m.catalog.cancel != nil {
		m.catalog.cancel()
	}
	m.catalog.ctx, m.catalog.cancel = context.WithCancel(m.opts.Context)
	m.catalog.schemasOp = 0
	m.catalog.relationsOp = make(map[string]uint64)
	m.catalog.detailOp = 0
	m.catalog.pageOp = 0
	m.catalog.countOp = 0
	m.catalog.queryOp = 0
}

// loadSchemas cancels any in-flight catalog work, invalidates relation loads,
// and schedules ListSchemas.
func (m Model) loadSchemas() (tea.Model, tea.Cmd) {
	if m.conn == nil {
		return m, nil
	}

	if m.catalog.cancel != nil {
		m.catalog.cancel()
	}
	m.catalog.ctx, m.catalog.cancel = context.WithCancel(m.opts.Context)
	m.catalog.seq++
	op := m.catalog.seq
	m.catalog.schemasOp = op
	m.catalog.relationsOp = make(map[string]uint64)

	ctx, cancel := context.WithTimeout(m.catalog.ctx, m.opts.QueryTimeout)
	conn := m.conn
	return m, func() tea.Msg {
		defer cancel()
		schemas, err := conn.ListSchemas(ctx)
		if err != nil {
			return screen.CatalogErrMsg{OpID: op, Scope: screen.ScopeSchemas, Err: err}
		}
		return screen.SchemasLoadedMsg{OpID: op, Schemas: schemas}
	}
}

// loadRelations schedules ListRelations for one schema. The response is only
// accepted while its opID remains the latest request for that schema.
func (m Model) loadRelations(schema string) (tea.Model, tea.Cmd) {
	if m.conn == nil || strings.TrimSpace(schema) == "" {
		return m, nil
	}

	base := m.catalog.ctx
	if base == nil {
		base = m.opts.Context
	}
	m.catalog.seq++
	op := m.catalog.seq
	if m.catalog.relationsOp == nil {
		m.catalog.relationsOp = make(map[string]uint64)
	}
	m.catalog.relationsOp[schema] = op

	ctx, cancel := context.WithTimeout(base, m.opts.QueryTimeout)
	conn := m.conn
	return m, func() tea.Msg {
		defer cancel()
		relations, err := conn.ListRelations(ctx, schema)
		if err != nil {
			return screen.CatalogErrMsg{
				OpID:   op,
				Scope:  screen.ScopeRelations,
				Schema: schema,
				Err:    err,
			}
		}
		return screen.RelationsLoadedMsg{OpID: op, Schema: schema, Relations: relations}
	}
}

func (m Model) catalogError(msg screen.CatalogErrMsg) (tea.Model, tea.Cmd) {
	switch msg.Scope {
	case screen.ScopeSchemas:
		if msg.OpID != m.catalog.schemasOp {
			return m, nil
		}
	case screen.ScopeRelations:
		if m.catalog.relationsOp[msg.Schema] != msg.OpID {
			return m, nil
		}
	default:
		return m, nil
	}
	return m.forward(msg)
}

// loadDetail schedules Describe. The detailOp guards stale responses.
func (m Model) loadDetail(ref db.RelationRef) (tea.Model, tea.Cmd) {
	if m.conn == nil {
		return m, nil
	}
	base := m.catalog.ctx
	if base == nil {
		base = m.opts.Context
	}
	m.catalog.seq++
	op := m.catalog.seq
	m.catalog.detailOp = op

	ctx, cancel := context.WithTimeout(base, m.opts.QueryTimeout)
	conn := m.conn
	return m, func() tea.Msg {
		defer cancel()
		detail, err := conn.Describe(ctx, ref)
		if err != nil {
			return screen.DetailErrMsg{OpID: op, Ref: ref, Err: err}
		}
		return screen.DetailLoadedMsg{OpID: op, Ref: ref, Detail: detail}
	}
}

// loadPage schedules Page. pageOp guards stale responses.
func (m Model) loadPage(ref db.RelationRef, page, pageSize int, order []db.Order) (tea.Model, tea.Cmd) {
	if m.conn == nil {
		return m, nil
	}
	if pageSize <= 0 {
		pageSize = 100
	}
	base := m.catalog.ctx
	if base == nil {
		base = m.opts.Context
	}
	m.catalog.seq++
	op := m.catalog.seq
	m.catalog.pageOp = op

	ctx, cancel := context.WithTimeout(base, m.opts.QueryTimeout)
	conn := m.conn
	return m, func() tea.Msg {
		defer cancel()
		limit := pageSize + 1
		result, err := conn.Page(ctx, ref, order, limit, page*pageSize)
		if err != nil {
			return screen.PageErrMsg{OpID: op, Ref: ref, Err: err}
		}
		if result != nil {
			if len(result.Rows) > pageSize {
				result.Rows = result.Rows[:pageSize]
				result.Truncated = true
			}
			result.RowCount = len(result.Rows)
		}
		return screen.PageLoadedMsg{OpID: op, Ref: ref, Page: page, Result: result}
	}
}

// loadCount schedules Count.
func (m Model) loadCount(ref db.RelationRef) (tea.Model, tea.Cmd) {
	if m.conn == nil {
		return m, nil
	}
	base := m.catalog.ctx
	if base == nil {
		base = m.opts.Context
	}
	m.catalog.seq++
	op := m.catalog.seq
	m.catalog.countOp = op

	ctx, cancel := context.WithTimeout(base, m.opts.QueryTimeout)
	conn := m.conn
	return m, func() tea.Msg {
		defer cancel()
		count, err := conn.Count(ctx, ref)
		if err != nil {
			return screen.CountErrMsg{OpID: op, Ref: ref, Err: err}
		}
		return screen.CountLoadedMsg{OpID: op, Ref: ref, Count: count}
	}
}
func (m Model) runQuery(sql string, maxRows int) (tea.Model, tea.Cmd) {
	if m.conn == nil {
		return m, nil
	}
	if maxRows <= 0 {
		maxRows = db.DefaultMaxRows
	}
	base := m.catalog.ctx
	if base == nil {
		base = m.opts.Context
	}
	m.catalog.seq++
	op := m.catalog.seq
	m.catalog.queryOp = op

	ctx, cancel := context.WithTimeout(base, m.opts.QueryTimeout)
	conn := m.conn
	return m, func() tea.Msg {
		defer cancel()
		result, err := conn.Query(ctx, sql, maxRows)
		return screen.QueryDoneMsg{OpID: op, Result: result, Err: err}
	}
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
	m.resetCatalog()
	m = m.SetScreen(m.browser())
	return m, m.screen.Init()
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
	if m.catalog.cancel != nil {
		m.catalog.cancel()
	}
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
	if connection := strings.TrimSpace(db.DSNLabel(m.opts.Connection)); connection != "" {
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
