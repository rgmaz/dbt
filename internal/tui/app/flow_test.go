package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/rgmaz/dbt/internal/db"
	"github.com/rgmaz/dbt/internal/db/dbtest"
	"github.com/rgmaz/dbt/internal/tui/screen"
)

// TestEndToEndBrowserFlow drives the same path a user takes: pick a profile,
// load the catalog, open a relation, page, sort, and run SQL.
func TestEndToEndBrowserFlow(t *testing.T) {
	m, conn := flowModel(t, 100, 30)

	if m.screen.ID() != screen.PickerScreen {
		t.Fatalf("screen = %v, want picker", m.screen.ID())
	}

	m = connectAndOpenUsers(t, m)

	view := m.View()
	for _, want := range []string{"local-pg", "postgres", "read-only", "rows 1–100 of 250", "user1@example.com"} {
		if !strings.Contains(view, want) {
			t.Errorf("browser after open missing %q:\n%s", want, view)
		}
	}
	for _, want := range []string{
		"ListSchemas",
		"ListRelations public",
		"Describe public.users",
		"Count public.users",
		"Page public.users limit=101 offset=0 order=-",
	} {
		if !hasCall(conn.Calls(), want) {
			t.Errorf("missing driver call %q in %v", want, conn.Calls())
		}
	}

	// Focus the grid and move to the next page.
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyTab})
	m, cmd := mustCmd(t, m, runeKey('n'))
	m = drain(t, m, cmd)
	if view := m.View(); !strings.Contains(view, "rows 101–200 of 250") {
		t.Errorf("next page not rendered:\n%s", view)
	}
	if !hasCall(conn.Calls(), "Page public.users limit=101 offset=100 order=-") {
		t.Errorf("next page call missing: %v", conn.Calls())
	}

	// Sort the id column: ascending, descending, then unsorted.
	for _, want := range []string{"order=id:asc", "order=id:desc", "order=-"} {
		m, cmd = mustCmd(t, m, runeKey('s'))
		m = drain(t, m, cmd)
		if !hasCall(conn.Calls(), "Page public.users limit=101 offset=0 "+want) {
			t.Errorf("sort call %q missing: %v", want, conn.Calls())
		}
	}

	// Run a query through the SQL editor.
	m, _ = send(t, m, runeKey('3'))
	m, _ = send(t, m, runeKey('i'))
	for _, r := range "select 1" {
		m, _ = send(t, m, runeKey(r))
	}
	m, cmd = mustCmd(t, m, tea.KeyMsg{Type: tea.KeyCtrlJ})
	m = drain(t, m, cmd)

	if view := m.View(); !strings.Contains(view, "ok") {
		t.Errorf("query result not rendered:\n%s", view)
	}
	if !hasCall(conn.Calls(), "Query select 1") {
		t.Errorf("query call missing: %v", conn.Calls())
	}

	// The executed statement is recalled from session history.
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyCtrlL})
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyCtrlR})
	if view := m.View(); !strings.Contains(view, "select 1") {
		t.Errorf("query history did not restore the statement:\n%s", view)
	}
}

// TestConcurrentPageResponsesKeepLatest exercises the stale-response guard with
// genuinely concurrent driver calls; run under -race this covers the async path.
func TestConcurrentPageResponsesKeepLatest(t *testing.T) {
	m, _ := flowModel(t, 100, 30)
	m = connectAndOpenUsers(t, m)
	ref := db.Relation{Schema: "public", Name: "users"}.Ref()

	cmds := make([]tea.Cmd, 3)
	for page := range cmds {
		m, cmds[page] = send(t, m, screen.LoadPageMsg{Ref: ref, Page: page, PageSize: 100})
		if cmds[page] == nil {
			t.Fatalf("page %d did not schedule a command", page)
		}
	}

	msgs := make([]tea.Msg, len(cmds))
	var wg sync.WaitGroup
	for i, cmd := range cmds {
		wg.Add(1)
		go func() {
			defer wg.Done()
			msgs[i] = cmd()
		}()
	}
	wg.Wait()

	// Earlier responses must be discarded even when they arrive in order.
	for _, msg := range msgs[:2] {
		m, _ = send(t, m, msg)
	}
	if view := m.View(); !strings.Contains(view, "rows 1–100 of 250") {
		t.Errorf("stale page response replaced the grid:\n%s", view)
	}

	m, _ = send(t, m, msgs[2])
	if view := m.View(); !strings.Contains(view, "rows 201–250 of 250") {
		t.Errorf("latest page response was discarded:\n%s", view)
	}
}

// TestBrowserFlowGoldens captures the fully loaded browser at the two sizes the
// design calls out.
func TestBrowserFlowGoldens(t *testing.T) {
	for _, tc := range []struct {
		name   string
		width  int
		height int
	}{
		{"browser_80x24.golden", 80, 24},
		{"browser_120x40.golden", 120, 40},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := flowModel(t, tc.width, tc.height)
			m = connectAndOpenUsers(t, m)
			goldenCheck(t, m.View(), tc.name)
		})
	}
}

// flowModel returns a picker model backed by the deterministic fake connection.
func flowModel(t *testing.T, width, height int) (Model, *dbtest.FakeConn) {
	t.Helper()

	conn := dbtest.NewFakeConn()
	m := New(Options{
		Context: context.Background(),
		Picker:  true,
		Profiles: []Profile{{
			Name:   "local-pg",
			Driver: "postgres",
			Kind:   db.KindPostgres,
			Detail: "postgres://app@localhost:5432/app",
			DSN:    "postgres://app@localhost:5432/app",
		}},
		ConfigPath:   "/home/user/.config/dbt/config.toml",
		QueryTimeout: time.Second,
		Open: func(_ context.Context, profile Profile) (ConnectResult, error) {
			return ConnectResult{
				Conn:    conn,
				Kind:    db.KindPostgres,
				Version: "16.0 (fake)",
				Label:   profile.Name,
			}, nil
		},
	})
	return resize(t, m, width, height), conn
}

// connectAndOpenUsers walks the picker, catalog, and relation open flow.
func connectAndOpenUsers(t *testing.T, m Model) Model {
	t.Helper()

	m, cmd := send(t, m, screen.ConnectRequestMsg{Profile: screen.PickerProfile{Name: "local-pg"}})
	m = drain(t, m, cmd)
	if m.screen.ID() != screen.BrowserScreen {
		t.Fatalf("screen = %v, want browser", m.screen.ID())
	}
	if view := m.View(); !strings.Contains(view, "public") {
		t.Fatalf("catalog did not load:\n%s", view)
	}

	m, cmd = mustCmd(t, m, runeKey('l'))
	m = drain(t, m, cmd)
	if view := m.View(); !strings.Contains(view, "users") {
		t.Fatalf("relations did not load:\n%s", view)
	}

	m, _ = send(t, m, runeKey('j'))
	m, cmd = mustCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	return drain(t, m, cmd)
}

// mustCmd sends msg and fails if it does not schedule a command.
func mustCmd(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	next, cmd := send(t, m, msg)
	if cmd == nil {
		t.Fatalf("%v did not schedule a command", msg)
	}
	return next, cmd
}

// drain runs commands to completion, feeding every resulting message back into
// the model. Batches are expanded, and follow-up commands are queued.
func drain(t *testing.T, m Model, cmds ...tea.Cmd) Model {
	t.Helper()

	queue := append([]tea.Cmd(nil), cmds...)
	for steps := 0; len(queue) > 0; steps++ {
		if steps > 500 {
			t.Fatal("command queue did not settle")
		}
		cmd := queue[0]
		queue = queue[1:]
		if cmd == nil {
			continue
		}
		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok {
			queue = append(queue, batch...)
			continue
		}
		var next tea.Cmd
		m, next = send(t, m, msg)
		queue = append(queue, next)
	}
	return m
}

func goldenCheck(t *testing.T, got, name string) {
	t.Helper()

	dir := filepath.Join("testdata", "golden")
	path := filepath.Join(dir, name)
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		_ = os.MkdirAll(dir, 0o755)
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		_ = os.MkdirAll(dir, 0o755)
		_ = os.WriteFile(path, []byte(got), 0o644)
		t.Fatalf("missing golden file %s (created); re-run", path)
	}
	if got != string(want) {
		t.Errorf("golden mismatch %s", path)
	}
}
