package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rgmaz/dbt/internal/db"
	"github.com/rgmaz/dbt/internal/db/dbtest"
	"github.com/rgmaz/dbt/internal/tui/screen"
)

func TestShellView(t *testing.T) {
	m := resize(t, testModel(), 100, 30)
	view := m.View()

	for _, want := range []string{
		"dbt", "test.db", "sqlite", "3.53.4", "read-only",
		"Data", "Structure", "SQL", "Schemas", "help", "quit", "tabs",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("shell missing %q:\n%s", want, view)
		}
	}
	if width := lipgloss.Width(view); width > 100 {
		t.Errorf("shell width = %d, want <= 100", width)
	}
	if lines := len(strings.Split(view, "\n")); lines != 30 {
		t.Errorf("shell height = %d, want 30", lines)
	}
}

func TestHelpToggle(t *testing.T) {
	m := resize(t, testModel(), 100, 30)

	m, _ = send(t, m, runeKey('?'))
	if !strings.Contains(m.View(), "Keys") {
		t.Errorf("help overlay not shown:\n%s", m.View())
	}

	m, _ = send(t, m, runeKey('?'))
	if strings.Contains(m.View(), "Keys") {
		t.Errorf("help overlay did not close:\n%s", m.View())
	}

	m, _ = send(t, m, runeKey('?'))
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if strings.Contains(m.View(), "Keys") {
		t.Errorf("esc did not close help:\n%s", m.View())
	}
}

func TestQuitKeys(t *testing.T) {
	for _, tt := range []struct {
		name string
		msg  tea.KeyMsg
	}{
		{"q", runeKey('q')},
		{"ctrl+c", tea.KeyMsg{Type: tea.KeyCtrlC}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := resize(t, testModel(), 100, 30)
			_, cmd := send(t, m, tt.msg)
			if cmd == nil {
				t.Fatal("expected quit command")
			}
			if _, ok := cmd().(tea.QuitMsg); !ok {
				t.Fatalf("command returned %T, want tea.QuitMsg", cmd())
			}
		})
	}
}

func TestQuitFromHelp(t *testing.T) {
	m := resize(t, testModel(), 100, 30)
	m, _ = send(t, m, runeKey('?'))
	_, cmd := send(t, m, runeKey('q'))
	if cmd == nil {
		t.Fatal("q should quit while help is open")
	}
}

func TestErrorModal(t *testing.T) {
	m := resize(t, testModel(), 100, 30)
	m = m.ShowError(errors.New("connection failed: password=secret"))

	view := m.View()
	for _, want := range []string{"Error", "connection failed", "esc: dismiss"} {
		if !strings.Contains(view, want) {
			t.Errorf("error modal missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "password=secret") {
		t.Errorf("error modal leaked a password:\n%s", view)
	}

	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if strings.Contains(m.View(), "connection failed") {
		t.Errorf("esc did not dismiss error:\n%s", m.View())
	}
}

func TestResizeNotice(t *testing.T) {
	for _, size := range []struct{ width, height int }{
		{79, 24},
		{80, 23},
	} {
		m := resize(t, testModel(), size.width, size.height)
		if view := m.View(); !strings.Contains(view, "Terminal too small") {
			t.Errorf("%dx%d missing resize notice:\n%s", size.width, size.height, view)
		}
	}

	m := resize(t, testModel(), 80, 24)
	if view := m.View(); strings.Contains(view, "Terminal too small") {
		t.Errorf("80x24 should render the shell:\n%s", view)
	}
}

func TestTabForwarding(t *testing.T) {
	m := resize(t, testModel(), 100, 30)

	m, _ = send(t, m, runeKey('2'))
	if view := m.View(); !strings.Contains(view, "columns, indexes") {
		t.Errorf("structure tab not forwarded:\n%s", view)
	}

	m, _ = send(t, m, runeKey('3'))
	if view := m.View(); !strings.Contains(view, "Read-only session") {
		t.Errorf("sql tab not forwarded:\n%s", view)
	}
}

func TestCapturesTextSuppressesGlobals(t *testing.T) {
	fake := &fakeScreen{capture: true}
	m := testModel()
	m = m.SetScreen(fake)
	m = resize(t, m, 100, 30)

	if fake.width != 100 || fake.height != 28 {
		t.Errorf("screen resized to %dx%d, want 100x28", fake.width, fake.height)
	}

	_, cmd := send(t, m, runeKey('q'))
	if cmd != nil {
		t.Error("q should be forwarded while text is captured, not quit")
	}

	m, _ = send(t, m, runeKey('?'))
	if m.help {
		t.Error("? should be forwarded while text is captured, not open help")
	}

	_, cmd = send(t, m, tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("ctrl+c must quit even while text is captured")
	}
}

func TestPickerStartup(t *testing.T) {
	m := pickerModel(func(context.Context, Profile) (ConnectResult, error) {
		return ConnectResult{}, nil
	})
	m = resize(t, m, 100, 30)

	if m.screen.ID() != screen.PickerScreen {
		t.Fatalf("screen = %v, want picker", m.screen.ID())
	}
	view := m.View()
	for _, want := range []string{"dbt · picker", "Connections", "local-pg", "postgres", "enter connect"} {
		if !strings.Contains(view, want) {
			t.Errorf("picker shell missing %q:\n%s", want, view)
		}
	}
}

func TestPickerOnboarding(t *testing.T) {
	m := New(Options{
		Picker:     true,
		ConfigPath: "/home/user/.config/dbt/config.toml",
		Open: func(context.Context, Profile) (ConnectResult, error) {
			return ConnectResult{}, nil
		},
	})
	m = resize(t, m, 80, 24)

	view := m.View()
	for _, want := range []string{
		"No connections configured",
		"[[connection]]",
		"/home/user/.config/dbt/config.toml",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("onboarding shell missing %q:\n%s", want, view)
		}
	}
}

func TestPickerConnectSuccess(t *testing.T) {
	conn := dbtest.NewFakeConn()
	m := pickerModel(func(_ context.Context, profile Profile) (ConnectResult, error) {
		return ConnectResult{
			Conn:    conn,
			Kind:    db.KindPostgres,
			Version: "16.0 (fake)",
			Label:   profile.Name,
		}, nil
	})
	m = resize(t, m, 100, 30)

	m, cmd := send(t, m, screen.ConnectRequestMsg{Profile: screen.PickerProfile{Name: "local-pg"}})
	if cmd == nil {
		t.Fatal("connect request did not schedule a command")
	}
	if !m.connecting {
		t.Error("connecting state not set")
	}

	m, loadCmd := send(t, m, cmd())
	if m.screen.ID() != screen.BrowserScreen {
		t.Fatalf("screen = %v, want browser", m.screen.ID())
	}
	if m.connecting {
		t.Error("connecting state not cleared")
	}
	if loadCmd == nil {
		t.Fatal("post-connect did not schedule a schema load")
	}
	load, ok := loadCmd().(screen.LoadSchemasMsg)
	if !ok {
		t.Fatalf("post-connect command returned %T, want LoadSchemasMsg", loadCmd())
	}

	m, schemaCmd := send(t, m, load)
	if schemaCmd == nil {
		t.Fatal("post-connect schema load did not schedule database work")
	}
	loaded, ok := schemaCmd().(screen.SchemasLoadedMsg)
	if !ok {
		t.Fatalf("schema load returned %T, want SchemasLoadedMsg", schemaCmd())
	}
	m, _ = send(t, m, loaded)

	view := m.View()
	for _, want := range []string{"local-pg", "postgres", "16.0 (fake)", "read-only", "Data", "public"} {
		if !strings.Contains(view, want) {
			t.Errorf("browser after connect missing %q:\n%s", want, view)
		}
	}

	if err := m.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if !hasCall(conn.Calls(), "Close") {
		t.Error("session was not closed")
	}
}

func TestPickerConnectFailure(t *testing.T) {
	m := pickerModel(func(context.Context, Profile) (ConnectResult, error) {
		return ConnectResult{}, errors.New("connection refused")
	})
	m = resize(t, m, 100, 30)

	m, cmd := send(t, m, screen.ConnectRequestMsg{Profile: screen.PickerProfile{Name: "local-pg"}})
	if cmd == nil {
		t.Fatal("connect request did not schedule a command")
	}
	m, _ = send(t, m, cmd())

	view := m.View()
	for _, want := range []string{"Error", "connection refused", "esc: dismiss"} {
		if !strings.Contains(view, want) {
			t.Errorf("error modal missing %q:\n%s", want, view)
		}
	}
	if m.screen.ID() != screen.PickerScreen {
		t.Errorf("screen = %v, want picker after failure", m.screen.ID())
	}
	if m.connecting {
		t.Error("connecting state not cleared after failure")
	}

	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	_, retry := send(t, m, screen.ConnectRequestMsg{Profile: screen.PickerProfile{Name: "local-pg"}})
	if retry == nil {
		t.Error("retry after failure should schedule a command")
	}
}

func TestPickerConnectIgnoresDuplicate(t *testing.T) {
	m := pickerModel(func(context.Context, Profile) (ConnectResult, error) {
		return ConnectResult{}, nil
	})
	m = resize(t, m, 100, 30)

	m, cmd := send(t, m, screen.ConnectRequestMsg{Profile: screen.PickerProfile{Name: "local-pg"}})
	if cmd == nil {
		t.Fatal("connect request did not schedule a command")
	}
	if _, duplicate := send(t, m, screen.ConnectRequestMsg{Profile: screen.PickerProfile{Name: "local-pg"}}); duplicate != nil {
		t.Error("duplicate connect request scheduled a command")
	}
}

func TestPickerConnectCancelledOnQuit(t *testing.T) {
	var got context.Context
	m := pickerModel(func(ctx context.Context, _ Profile) (ConnectResult, error) {
		got = ctx
		return ConnectResult{}, ctx.Err()
	})
	m = resize(t, m, 100, 30)

	m, cmd := send(t, m, screen.ConnectRequestMsg{Profile: screen.PickerProfile{Name: "local-pg"}})
	if cmd == nil {
		t.Fatal("connect request did not schedule a command")
	}
	if _, quitCmd := send(t, m, runeKey('q')); quitCmd == nil {
		t.Fatal("q did not schedule quit")
	}

	_ = cmd()
	if got == nil {
		t.Fatal("open function was not called")
	}
	if !errors.Is(got.Err(), context.Canceled) {
		t.Errorf("connect context error = %v, want context.Canceled", got.Err())
	}
}

func TestCatalogInitialLoad(t *testing.T) {
	conn := dbtest.NewFakeConn()
	m := resize(t, connModel(conn), 100, 30)

	initCmd := m.Init()
	if initCmd == nil {
		t.Fatal("Init() did not schedule a schema load")
	}
	load, ok := initCmd().(screen.LoadSchemasMsg)
	if !ok {
		t.Fatalf("Init() command returned %T, want LoadSchemasMsg", initCmd())
	}

	m, cmd := send(t, m, load)
	if cmd == nil {
		t.Fatal("schema load did not schedule database work")
	}
	loaded, ok := cmd().(screen.SchemasLoadedMsg)
	if !ok {
		t.Fatalf("schema load returned %T, want SchemasLoadedMsg", cmd())
	}
	if loaded.OpID == 0 {
		t.Error("schema load op ID must be non-zero")
	}

	m, _ = send(t, m, loaded)
	if view := m.View(); !strings.Contains(view, "public") {
		t.Errorf("browser missing loaded schema:\n%s", view)
	}
	if !hasCall(conn.Calls(), "ListSchemas") {
		t.Error("ListSchemas was not called")
	}
}

func TestCatalogExpandRelations(t *testing.T) {
	conn := dbtest.NewFakeConn()
	m := loadSchemas(t, resize(t, connModel(conn), 100, 30))

	m, cmd := send(t, m, runeKey('l'))
	if cmd == nil {
		t.Fatal("expanding a schema did not schedule a relation load")
	}
	request, ok := cmd().(screen.LoadRelationsMsg)
	if !ok {
		t.Fatalf("expand command returned %T, want LoadRelationsMsg", cmd())
	}
	if request.Schema != "public" {
		t.Errorf("relation request schema = %q, want public", request.Schema)
	}

	m, cmd = send(t, m, request)
	if cmd == nil {
		t.Fatal("relation request did not schedule database work")
	}
	loaded, ok := cmd().(screen.RelationsLoadedMsg)
	if !ok {
		t.Fatalf("relation load returned %T, want RelationsLoadedMsg", cmd())
	}
	m, _ = send(t, m, loaded)

	view := m.View()
	for _, want := range []string{"public", "users", "orders"} {
		if !strings.Contains(view, want) {
			t.Errorf("browser missing loaded relation %q:\n%s", want, view)
		}
	}
}

func TestCatalogDiscardsStaleSchemaResponse(t *testing.T) {
	conn := dbtest.NewFakeConn()
	m := resize(t, connModel(conn), 100, 30)

	m, cmd := send(t, m, screen.LoadSchemasMsg{})
	first, ok := cmd().(screen.SchemasLoadedMsg)
	if !ok {
		t.Fatalf("first schema load returned %T", cmd())
	}

	conn.SetSchemas([]db.Schema{{Name: "analytics"}})
	m, cmd = send(t, m, screen.LoadSchemasMsg{})
	second, ok := cmd().(screen.SchemasLoadedMsg)
	if !ok {
		t.Fatalf("second schema load returned %T", cmd())
	}
	if second.OpID == first.OpID {
		t.Fatalf("schema op ID did not advance: %d", second.OpID)
	}

	m, _ = send(t, m, first)
	if view := m.View(); strings.Contains(view, "public") {
		t.Errorf("stale schema response was rendered:\n%s", view)
	}

	m, _ = send(t, m, second)
	if view := m.View(); !strings.Contains(view, "analytics") {
		t.Errorf("current schema response was discarded:\n%s", view)
	}
}

func TestCatalogDiscardsStaleRelationResponse(t *testing.T) {
	conn := dbtest.NewFakeConn()
	m := loadSchemas(t, resize(t, connModel(conn), 100, 30))

	m, startCmd := send(t, m, runeKey('l'))
	start, ok := startCmd().(screen.LoadRelationsMsg)
	if !ok {
		t.Fatalf("expand command returned %T", startCmd())
	}
	m, dbCmd := send(t, m, start)
	first, ok := dbCmd().(screen.RelationsLoadedMsg)
	if !ok {
		t.Fatalf("first relation load returned %T", dbCmd())
	}

	conn.SetRelations("public", []db.Relation{
		{Schema: "public", Name: "fresh", Kind: db.RelationTable},
	})
	m, dbCmd = send(t, m, screen.LoadRelationsMsg{Schema: "public"})
	second, ok := dbCmd().(screen.RelationsLoadedMsg)
	if !ok {
		t.Fatalf("second relation load returned %T", dbCmd())
	}
	if second.OpID == first.OpID {
		t.Fatalf("relation op ID did not advance: %d", second.OpID)
	}

	m, _ = send(t, m, first)
	if view := m.View(); strings.Contains(view, "users") {
		t.Errorf("stale relation response was rendered:\n%s", view)
	}

	m, _ = send(t, m, second)
	if view := m.View(); !strings.Contains(view, "fresh") {
		t.Errorf("current relation response was discarded:\n%s", view)
	}
}

func TestCatalogRefreshDiscardsInFlightRelations(t *testing.T) {
	conn := dbtest.NewFakeConn()
	m := loadSchemas(t, resize(t, connModel(conn), 100, 30))

	m, startCmd := send(t, m, runeKey('l'))
	start, ok := startCmd().(screen.LoadRelationsMsg)
	if !ok {
		t.Fatalf("expand command returned %T", startCmd())
	}
	m, dbCmd := send(t, m, start)
	relations, ok := dbCmd().(screen.RelationsLoadedMsg)
	if !ok {
		t.Fatalf("relation load returned %T", dbCmd())
	}

	m, refreshCmd := send(t, m, runeKey('r'))
	if refreshCmd == nil {
		t.Fatal("refresh did not schedule a schema load")
	}
	refresh, ok := refreshCmd().(screen.LoadSchemasMsg)
	if !ok {
		t.Fatalf("refresh command returned %T, want LoadSchemasMsg", refreshCmd())
	}
	m, dbCmd = send(t, m, refresh)
	schemas, ok := dbCmd().(screen.SchemasLoadedMsg)
	if !ok {
		t.Fatalf("refreshed schema load returned %T", dbCmd())
	}

	// The relation response belongs to the pre-refresh generation.
	m, _ = send(t, m, relations)
	if view := m.View(); strings.Contains(view, "users") {
		t.Errorf("refresh kept a stale relation response:\n%s", view)
	}

	m, _ = send(t, m, schemas)
	if view := m.View(); !strings.Contains(view, "public") {
		t.Errorf("refreshed schema response was discarded:\n%s", view)
	}
}

func TestCatalogErrorInlineRedacted(t *testing.T) {
	conn := dbtest.NewFakeConn()
	conn.SetError(errors.New("connection failed: password=secret"))
	m := resize(t, connModel(conn), 100, 30)

	m, cmd := send(t, m, screen.LoadSchemasMsg{})
	if cmd == nil {
		t.Fatal("schema load did not schedule database work")
	}
	failed, ok := cmd().(screen.CatalogErrMsg)
	if !ok {
		t.Fatalf("failed schema load returned %T, want CatalogErrMsg", cmd())
	}
	m, _ = send(t, m, failed)

	view := m.View()
	for _, want := range []string{"error", "r retry"} {
		if !strings.Contains(view, want) {
			t.Errorf("inline catalog error missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "password=secret") {
		t.Errorf("inline catalog error leaked a password:\n%s", view)
	}
}

func TestCatalogDiscardsStaleError(t *testing.T) {
	conn := dbtest.NewFakeConn()
	m := resize(t, connModel(conn), 100, 30)

	m, cmd := send(t, m, screen.LoadSchemasMsg{})
	stale, ok := cmd().(screen.SchemasLoadedMsg)
	if !ok {
		t.Fatalf("schema load returned %T", cmd())
	}
	m, _ = send(t, m, screen.LoadSchemasMsg{})
	if m.catalog.schemasOp == stale.OpID {
		t.Fatal("schema op ID did not advance")
	}

	m, _ = send(t, m, screen.CatalogErrMsg{
		OpID:  stale.OpID,
		Scope: screen.ScopeSchemas,
		Err:   errors.New("stale failure"),
	})
	if view := m.View(); strings.Contains(view, "stale failure") {
		t.Errorf("stale catalog error was rendered:\n%s", view)
	}
}

func TestCatalogWithoutConnectionIsNoop(t *testing.T) {
	m := resize(t, testModel(), 100, 30)
	if _, cmd := send(t, m, screen.LoadSchemasMsg{}); cmd != nil {
		t.Errorf("schema load without a connection returned %T", cmd())
	}
	if _, cmd := send(t, m, screen.LoadRelationsMsg{Schema: "public"}); cmd != nil {
		t.Errorf("relation load without a connection returned %T", cmd())
	}
}

func TestQuitCancelsCatalog(t *testing.T) {
	conn := dbtest.NewFakeConn()
	m := resize(t, connModel(conn), 100, 30)
	m, _ = send(t, m, screen.LoadSchemasMsg{})

	ctx := m.catalog.ctx
	if ctx == nil || ctx.Err() != nil {
		t.Fatalf("catalog context = %v, want active", ctx)
	}

	if _, cmd := send(t, m, runeKey('q')); cmd == nil {
		t.Fatal("q did not schedule quit")
	}
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Errorf("catalog context error = %v, want context.Canceled", ctx.Err())
	}
}

func TestCloseCancelsCatalogAndClosesConn(t *testing.T) {
	conn := dbtest.NewFakeConn()
	m := connModel(conn)
	m, _ = send(t, m, screen.LoadSchemasMsg{})
	ctx := m.catalog.ctx

	if err := m.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if ctx == nil || !errors.Is(ctx.Err(), context.Canceled) {
		t.Errorf("Close() did not cancel catalog context: %v", ctx)
	}
	if !hasCall(conn.Calls(), "Close") {
		t.Error("Close() did not close the explicit session")
	}
}

func connModel(conn db.Conn) Model {
	return New(Options{
		Context:      context.Background(),
		Connection:   "fake.db",
		Kind:         db.KindPostgres,
		Version:      "16.0 (fake)",
		ReadOnly:     true,
		Conn:         conn,
		QueryTimeout: time.Second,
	})
}

// loadSchemas drives a full initial schema load through the root model.
func loadSchemas(t *testing.T, m Model) Model {
	t.Helper()

	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init() did not schedule a schema load")
	}
	msg, ok := cmd().(screen.LoadSchemasMsg)
	if !ok {
		t.Fatalf("Init() command returned %T, want LoadSchemasMsg", cmd())
	}

	m, dbCmd := send(t, m, msg)
	if dbCmd == nil {
		t.Fatal("schema load did not schedule database work")
	}
	loaded, ok := dbCmd().(screen.SchemasLoadedMsg)
	if !ok {
		t.Fatalf("schema load returned %T, want SchemasLoadedMsg", dbCmd())
	}
	m, _ = send(t, m, loaded)
	return m
}

func pickerModel(open OpenFunc) Model {
	return New(Options{
		Context: context.Background(),
		Picker:  true,
		Profiles: []Profile{{
			Name:   "local-pg",
			Driver: "postgres",
			Kind:   db.KindPostgres,
			Detail: "postgres://app@localhost:5432/app",
			DSN:    "postgres://app@localhost:5432/app",
		}},
		ConfigPath: "/home/user/.config/dbt/config.toml",
		Open:       open,
	})
}

func hasCall(calls []string, want string) bool {
	for _, call := range calls {
		if call == want {
			return true
		}
	}
	return false
}

func testModel() Model {
	return New(Options{
		Connection: "test.db",
		Kind:       db.KindSQLite,
		Version:    "3.53.4",
		ReadOnly:   true,
	})
}

func resize(t *testing.T, m Model, width, height int) Model {
	t.Helper()

	updated, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	next, ok := updated.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want Model", updated)
	}
	return next
}

func send(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()

	updated, cmd := m.Update(msg)
	next, ok := updated.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want Model", updated)
	}
	return next, cmd
}

func runeKey(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

type fakeScreen struct {
	capture       bool
	width, height int
}

func (f *fakeScreen) Init() tea.Cmd { return nil }

func (f *fakeScreen) Update(msg tea.Msg) (screen.Screen, tea.Cmd) {
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		f.width = size.Width
		f.height = size.Height
	}
	return f, nil
}

func (f *fakeScreen) View() string { return "fake" }

func (f *fakeScreen) ID() screen.ID { return screen.BrowserScreen }

func (f *fakeScreen) CapturesText() bool { return f.capture }
