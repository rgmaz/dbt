package keys

import "github.com/charmbracelet/bubbles/key"

// Group is a titled collection of bindings used by the help overlay.
type Group struct {
	Title    string
	Bindings []key.Binding
}

// Map is the complete keymap for the application. Screens read the subset they
// handle, while the root model owns global bindings.
type Map struct {
	Global    Global
	Sidebar   Sidebar
	Data      Data
	Structure Structure
	SQL       SQL
}

// Global bindings are available from every screen that does not capture text.
type Global struct {
	Help         key.Binding
	Quit         key.Binding
	ForceQuit    key.Binding
	Back         key.Binding
	Focus        key.Binding
	DataTab      key.Binding
	StructureTab key.Binding
	SQLTab       key.Binding
}

// Sidebar bindings control the schema tree.
type Sidebar struct {
	Up      key.Binding
	Down    key.Binding
	Left    key.Binding
	Right   key.Binding
	Top     key.Binding
	Bottom  key.Binding
	Filter  key.Binding
	Enter   key.Binding
	Refresh key.Binding
}

// Data bindings control the result grid.
type Data struct {
	Up           key.Binding
	Down         key.Binding
	Left         key.Binding
	Right        key.Binding
	HalfPageDown key.Binding
	HalfPageUp   key.Binding
	NextPage     key.Binding
	PrevPage     key.Binding
	Sort         key.Binding
	CellDetail   key.Binding
	CopyCell     key.Binding
	CopyRow      key.Binding
	Reload       key.Binding
}

// Structure bindings control the metadata pane.
type Structure struct {
	Up       key.Binding
	Down     key.Binding
	PageDown key.Binding
	PageUp   key.Binding
	Top      key.Binding
	Bottom   key.Binding
}

// SQL bindings control the query editor.
type SQL struct {
	Edit    key.Binding
	Normal  key.Binding
	Execute key.Binding
	History key.Binding
	Clear   key.Binding
}

// Default returns the documented keymap.
func Default() Map {
	return Map{
		Global: Global{
			Help:         binding([]string{"?"}, "?", "help"),
			Quit:         binding([]string{"q"}, "q", "quit"),
			ForceQuit:    binding([]string{"ctrl+c"}, "ctrl+c", "force quit"),
			Back:         binding([]string{"esc"}, "esc", "close"),
			Focus:        binding([]string{"tab"}, "tab", "focus"),
			DataTab:      binding([]string{"1"}, "1", "data"),
			StructureTab: binding([]string{"2"}, "2", "structure"),
			SQLTab:       binding([]string{"3"}, "3", "sql"),
		},
		Sidebar: Sidebar{
			Up:      binding([]string{"k"}, "k", "up"),
			Down:    binding([]string{"j"}, "j", "down"),
			Left:    binding([]string{"h"}, "h", "collapse"),
			Right:   binding([]string{"l"}, "l", "expand"),
			Top:     binding([]string{"g"}, "g", "top"),
			Bottom:  binding([]string{"G"}, "G", "bottom"),
			Filter:  binding([]string{"/"}, "/", "filter"),
			Enter:   binding([]string{"enter"}, "enter", "open"),
			Refresh: binding([]string{"r"}, "r", "refresh"),
		},
		Data: Data{
			Up:           binding([]string{"k"}, "k", "up"),
			Down:         binding([]string{"j"}, "j", "down"),
			Left:         binding([]string{"h"}, "h", "left"),
			Right:        binding([]string{"l"}, "l", "right"),
			HalfPageDown: binding([]string{"ctrl+d"}, "ctrl+d", "half down"),
			HalfPageUp:   binding([]string{"ctrl+u"}, "ctrl+u", "half up"),
			NextPage:     binding([]string{"n"}, "n", "next page"),
			PrevPage:     binding([]string{"p"}, "p", "prev page"),
			Sort:         binding([]string{"s"}, "s", "sort"),
			CellDetail:   binding([]string{"enter"}, "enter", "detail"),
			CopyCell:     binding([]string{"y"}, "y", "copy cell"),
			CopyRow:      binding([]string{"Y"}, "Y", "copy row"),
			Reload:       binding([]string{"r"}, "r", "reload"),
		},
		Structure: Structure{
			Up:       binding([]string{"k"}, "k", "up"),
			Down:     binding([]string{"j"}, "j", "down"),
			PageDown: binding([]string{"ctrl+d"}, "ctrl+d", "page down"),
			PageUp:   binding([]string{"ctrl+u"}, "ctrl+u", "page up"),
			Top:      binding([]string{"g"}, "g", "top"),
			Bottom:   binding([]string{"G"}, "G", "bottom"),
		},
		SQL: SQL{
			Edit:    binding([]string{"i"}, "i", "edit"),
			Normal:  binding([]string{"esc"}, "esc", "normal"),
			Execute: binding([]string{"ctrl+enter"}, "ctrl+enter", "execute"),
			History: binding([]string{"ctrl+r"}, "ctrl+r", "history"),
			Clear:   binding([]string{"ctrl+l"}, "ctrl+l", "clear"),
		},
	}
}

// Groups returns the keymap grouped for the help overlay.
func (m Map) Groups() []Group {
	return []Group{
		{Title: "Global", Bindings: []key.Binding{
			m.Global.Help, m.Global.Quit, m.Global.ForceQuit, m.Global.Back,
			m.Global.Focus, m.Global.DataTab, m.Global.StructureTab, m.Global.SQLTab,
		}},
		{Title: "Sidebar", Bindings: []key.Binding{
			m.Sidebar.Up, m.Sidebar.Down, m.Sidebar.Left, m.Sidebar.Right,
			m.Sidebar.Top, m.Sidebar.Bottom, m.Sidebar.Filter, m.Sidebar.Enter,
			m.Sidebar.Refresh,
		}},
		{Title: "Data", Bindings: []key.Binding{
			m.Data.Up, m.Data.Down, m.Data.Left, m.Data.Right,
			m.Data.HalfPageDown, m.Data.HalfPageUp, m.Data.NextPage, m.Data.PrevPage,
			m.Data.Sort, m.Data.CellDetail, m.Data.CopyCell, m.Data.CopyRow, m.Data.Reload,
		}},
		{Title: "Structure", Bindings: []key.Binding{
			m.Structure.Up, m.Structure.Down, m.Structure.PageDown, m.Structure.PageUp,
			m.Structure.Top, m.Structure.Bottom,
		}},
		{Title: "SQL", Bindings: []key.Binding{
			m.SQL.Edit, m.SQL.Normal, m.SQL.Execute, m.SQL.History, m.SQL.Clear,
		}},
	}
}

// ShortHelp returns the most important bindings for a single-line hint.
func (m Map) ShortHelp() []key.Binding {
	return []key.Binding{m.Global.Help, m.Global.Quit, m.Global.ForceQuit}
}

func binding(keys []string, helpKey, desc string) key.Binding {
	return key.NewBinding(
		key.WithKeys(keys...),
		key.WithHelp(helpKey, desc),
	)
}
