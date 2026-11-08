package app

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rgmaz/dbt/internal/db"
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
