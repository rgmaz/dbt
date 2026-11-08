package component

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/rgmaz/dbt/internal/tui/keys"
	"github.com/rgmaz/dbt/internal/tui/theme"
)

func TestHelpView(t *testing.T) {
	out := HelpView(keys.Default(), theme.Default(), 100, 80)

	for _, want := range []string{"Keys", "Global", "Sidebar", "Data", "Structure", "SQL", "help", "quit"} {
		if !strings.Contains(out, want) {
			t.Errorf("help missing %q:\n%s", want, out)
		}
	}
	if width := lipgloss.Width(out); width > 100 {
		t.Errorf("help width = %d, want <= 100", width)
	}
	if lines := len(strings.Split(out, "\n")); lines > 80 {
		t.Errorf("help height = %d, want <= 80", lines)
	}
}

func TestHelpViewFitsMinimumTerminal(t *testing.T) {
	out := HelpView(keys.Default(), theme.Default(), 80, 24)

	for _, want := range []string{"Global", "Sidebar", "Data", "Structure", "SQL"} {
		if !strings.Contains(out, want) {
			t.Errorf("80x24 help missing group %q:\n%s", want, out)
		}
	}
	if width := lipgloss.Width(out); width > 80 {
		t.Errorf("help width = %d, want <= 80", width)
	}
	if lines := len(strings.Split(out, "\n")); lines > 24 {
		t.Errorf("help height = %d, want <= 24", lines)
	}
}

func TestPadRight(t *testing.T) {
	tests := []struct {
		text  string
		width int
		want  string
	}{
		{"ab", 4, "ab  "},
		{"abcd", 4, "abcd"},
		{"abcde", 4, "abcde"},
		{"日", 4, "日  "},
	}
	for _, tt := range tests {
		if got := padRight(tt.text, tt.width); got != tt.want {
			t.Errorf("padRight(%q, %d) = %q, want %q", tt.text, tt.width, got, tt.want)
		}
	}
}
