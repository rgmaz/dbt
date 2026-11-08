package keys

import (
	"testing"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

func TestDefaultMatchesDocumentedKeys(t *testing.T) {
	km := Default()

	tests := []struct {
		name string
		bind key.Binding
		msg  tea.KeyMsg
	}{
		{"help", km.Global.Help, runeKey('?')},
		{"quit", km.Global.Quit, runeKey('q')},
		{"force quit", km.Global.ForceQuit, tea.KeyMsg{Type: tea.KeyCtrlC}},
		{"back", km.Global.Back, tea.KeyMsg{Type: tea.KeyEsc}},
		{"data tab", km.Global.DataTab, runeKey('1')},
		{"structure tab", km.Global.StructureTab, runeKey('2')},
		{"sql tab", km.Global.SQLTab, runeKey('3')},
		{"sidebar down", km.Sidebar.Down, runeKey('j')},
		{"data next page", km.Data.NextPage, runeKey('n')},
		{"sql history", km.SQL.History, tea.KeyMsg{Type: tea.KeyCtrlR}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !key.Matches(tt.msg, tt.bind) {
				t.Errorf("%s binding did not match %v", tt.name, tt.msg)
			}
		})
	}
}

func TestGroupsHaveHelpTextAndNoDuplicates(t *testing.T) {
	groups := Default().Groups()
	if len(groups) == 0 {
		t.Fatal("Groups() returned no groups")
	}

	for _, group := range groups {
		if group.Title == "" {
			t.Error("group has empty title")
		}
		if len(group.Bindings) == 0 {
			t.Errorf("group %q has no bindings", group.Title)
		}

		seen := make(map[string]bool)
		for _, bind := range group.Bindings {
			help := bind.Help()
			if help.Key == "" || help.Desc == "" {
				t.Errorf("group %q has binding with empty help: %+v", group.Title, help)
			}
			for _, sequence := range bind.Keys() {
				if seen[sequence] {
					t.Errorf("group %q has duplicate key %q", group.Title, sequence)
				}
				seen[sequence] = true
			}
		}
	}
}

func TestShortHelp(t *testing.T) {
	short := Default().ShortHelp()
	if len(short) == 0 {
		t.Fatal("ShortHelp() returned no bindings")
	}
	for _, bind := range short {
		if !bind.Enabled() {
			t.Errorf("short help binding %q is disabled", bind.Help().Key)
		}
	}
}

func runeKey(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}
