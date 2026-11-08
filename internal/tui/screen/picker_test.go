package screen

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rgmaz/dbt/internal/db"
	"github.com/rgmaz/dbt/internal/tui/keys"
	"github.com/rgmaz/dbt/internal/tui/theme"
)

func TestPickerListAndNavigation(t *testing.T) {
	p := newTestPicker(t, 100, 30, 3)

	view := p.View()
	for _, want := range []string{"Connections", "local-pg", "postgres", "notes", "sqlite"} {
		if !strings.Contains(view, want) {
			t.Errorf("picker view missing %q:\n%s", want, view)
		}
	}
	if width := lipgloss.Width(view); width > 100 {
		t.Errorf("picker width = %d, want <= 100", width)
	}

	p = updatePicker(t, p, runeKey('j'))
	if p.Cursor() != 1 {
		t.Fatalf("cursor after j = %d, want 1", p.Cursor())
	}

	p = updatePicker(t, p, runeKey('k'))
	p = updatePicker(t, p, runeKey('k'))
	if p.Cursor() != 0 {
		t.Errorf("cursor after k,k = %d, want 0", p.Cursor())
	}

	p = updatePicker(t, p, runeKey('G'))
	if p.Cursor() != 2 {
		t.Errorf("cursor after G = %d, want 2", p.Cursor())
	}

	p = updatePicker(t, p, runeKey('g'))
	if p.Cursor() != 0 {
		t.Errorf("cursor after g = %d, want 0", p.Cursor())
	}
}

func TestPickerEnterEmitsConnectRequest(t *testing.T) {
	p := newTestPicker(t, 100, 30, 3)
	p = updatePicker(t, p, runeKey('j'))

	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter did not schedule a command")
	}

	msg := cmd()
	request, ok := msg.(ConnectRequestMsg)
	if !ok {
		t.Fatalf("command returned %T, want ConnectRequestMsg", msg)
	}
	if request.Profile.Name != "notes" {
		t.Errorf("selected profile = %q, want notes", request.Profile.Name)
	}
	if request.Profile.Kind != db.KindSQLite {
		t.Errorf("selected kind = %q, want sqlite", request.Profile.Kind)
	}
}

func TestPickerEmptyDoesNotConnect(t *testing.T) {
	p := newTestPicker(t, 80, 24, 0)

	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Error("enter on empty picker should not schedule a command")
	}
	if view := p.View(); !strings.Contains(view, "No connections configured") {
		t.Errorf("empty picker missing onboarding hint:\n%s", view)
	}
}

func TestPickerScrolling(t *testing.T) {
	p := newTestPicker(t, 80, 10, 30)

	p = updatePicker(t, p, runeKey('G'))
	if p.Cursor() != 29 {
		t.Fatalf("cursor = %d, want 29", p.Cursor())
	}
	if p.offset == 0 {
		t.Error("offset did not scroll for a long list")
	}
	if lines := len(strings.Split(p.View(), "\n")); lines > 10 {
		t.Errorf("picker height = %d, want <= 10", lines)
	}

	p = updatePicker(t, p, runeKey('g'))
	if p.offset != 0 {
		t.Errorf("offset after g = %d, want 0", p.offset)
	}
}

func TestPickerOnboarding(t *testing.T) {
	p := NewPicker(PickerOptions{
		ConfigPath: "/home/user/.config/dbt/config.toml",
		Theme:      theme.Default(),
		Keys:       keys.Default(),
	})
	p = updatePicker(t, p, tea.WindowSizeMsg{Width: 80, Height: 24})

	view := p.View()
	for _, want := range []string{
		"No connections configured",
		"/home/user/.config/dbt/config.toml",
		"[[connection]]",
		`driver = "postgres"`,
		"dbt postgres://...",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("onboarding missing %q:\n%s", want, view)
		}
	}
	if width := lipgloss.Width(view); width > 80 {
		t.Errorf("onboarding width = %d, want <= 80", width)
	}
}

func TestPickerConnectingState(t *testing.T) {
	p := newTestPicker(t, 100, 30, 3).Connecting("local-pg")

	if view := p.View(); !strings.Contains(view, "Connecting to local-pg") {
		t.Errorf("connecting state not rendered:\n%s", view)
	}

	before := p.Cursor()
	p = updatePicker(t, p, runeKey('j'))
	if p.Cursor() != before {
		t.Error("navigation should be ignored while connecting")
	}
	if _, cmd := p.Update(tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil {
		t.Error("enter should be ignored while connecting")
	}

	p = p.ClearConnecting()
	if view := p.View(); strings.Contains(view, "Connecting to") {
		t.Errorf("connecting state not cleared:\n%s", view)
	}
}

func TestPickerIdentity(t *testing.T) {
	p := newTestPicker(t, 80, 24, 1)
	if p.ID() != PickerScreen {
		t.Errorf("ID() = %v, want %v", p.ID(), PickerScreen)
	}
	if p.CapturesText() {
		t.Error("picker must not capture text")
	}
	if p.Init() != nil {
		t.Error("picker Init() should not schedule a command")
	}
}

func TestPickerNarrowLayout(t *testing.T) {
	p := newTestPicker(t, 40, 20, 3)
	if width := lipgloss.Width(p.View()); width > 40 {
		t.Errorf("narrow picker width = %d, want <= 40", width)
	}
}

func newTestPicker(t *testing.T, width, height, count int) Picker {
	t.Helper()

	profiles := make([]PickerProfile, 0, count)
	for i := 0; i < count; i++ {
		profile := PickerProfile{
			Name:   "local-pg",
			Driver: "postgres",
			Detail: "postgres://app@localhost:5432/app",
			Kind:   db.KindPostgres,
		}
		if i == 1 {
			profile = PickerProfile{
				Name:   "notes",
				Driver: "sqlite",
				Detail: "/home/user/notes.db",
				Kind:   db.KindSQLite,
			}
		}
		if i > 1 {
			profile.Name = fmt.Sprintf("extra-%d", i)
			profile.Detail = "postgres://app@localhost:5432/extra"
		}
		profiles = append(profiles, profile)
	}

	p := NewPicker(PickerOptions{
		Profiles:   profiles,
		ConfigPath: "/home/user/.config/dbt/config.toml",
		Theme:      theme.Default(),
		Keys:       keys.Default(),
	})
	return updatePicker(t, p, tea.WindowSizeMsg{Width: width, Height: height})
}

func updatePicker(t *testing.T, p Picker, msg tea.Msg) Picker {
	t.Helper()

	updated, _ := p.Update(msg)
	next, ok := updated.(Picker)
	if !ok {
		t.Fatalf("Update returned %T, want Picker", updated)
	}
	return next
}
