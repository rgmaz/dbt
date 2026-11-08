package component

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"

	"github.com/rgmaz/dbt/internal/tui/keys"
	"github.com/rgmaz/dbt/internal/tui/theme"
)

// SQLEventKind identifies an action requested by the SQL panel.
type SQLEventKind int

// SQL panel actions.
const (
	SQLEventNone SQLEventKind = iota
	SQLEventExecute
	SQLEventCancel
	SQLEventClear
	SQLEventHistory
)

// SQLEvent tells the browser how to react to an SQL key.
type SQLEvent struct {
	Kind SQLEventKind
	SQL  string
}

// SQLOptions configures the SQL panel.
type SQLOptions struct {
	Theme theme.Theme
	Keys  keys.Map
}

// SQL is a read-only SQL editor with a result grid and session history.
type SQL struct {
	opts SQLOptions

	width, height int

	editor     textarea.Model
	editing    bool

	result Grid
	history []string
	histIdx int

	running bool
	err     string

	lastSQL string
}

// NewSQL constructs an idle SQL panel.
func NewSQL(opts SQLOptions) SQL {
	t := textarea.New()
	t.Placeholder = "select ..."
	t.CharLimit = 0
	t.ShowLineNumbers = false
	t.KeyMap = textarea.DefaultKeyMap
	s := SQL{
		opts:    opts,
		editor:  t,
		result:  NewGrid(GridOptions(opts)),
		histIdx: -1,
	}
	return s
}

// SetSize updates the render area and resizes the editor/grid.
func (s SQL) SetSize(width, height int) SQL {
	s.width = max(width, 0)
	s.height = max(height, 0)
	editorH := 6
	if s.height < 12 {
		editorH = 3
	}
	if editorH > s.height-2 {
		editorH = s.height - 2
		if editorH < 1 {
			editorH = 1
		}
	}
	s.editor.SetWidth(max(width-2, 1))
	s.editor.SetHeight(editorH)
	s.result = s.result.SetSize(width, max(s.height-editorH-1, 1))
	return s
}

// SetResult installs a query result.
func (s SQL) SetResult(page GridPage) SQL {
	s.result = s.result.SetData(page)
	s.running = false
	s.err = page.Error
	return s
}

// SetRunning marks execution state.
func (s SQL) SetRunning(running bool) SQL {
	s.running = running
	return s
}

// SetError records an execution error.
func (s SQL) SetError(message string) SQL {
	s.running = false
	s.result = s.result.SetLoading(false)
	s.err = message
	return s
}

// AddHistory appends a successful statement.
func (s SQL) AddHistory(sql string) SQL {
	sql = strings.TrimSpace(sql)
	if sql == "" {
		return s
	}
	s.history = append(s.history, sql)
	s.histIdx = len(s.history) - 1
	return s
}

// EditorText reports the current SQL text.
func (s SQL) EditorText() string { return s.editor.Value() }

// Init satisfies component consumers.
func (s SQL) Init() tea.Cmd { return nil }

// CapturesText reports whether the editor owns text input.
func (s SQL) CapturesText() bool { return s.editing }

// View renders the panel.
func (s SQL) View(width, height int) string {
	if width > 0 || height > 0 {
		if width > 0 {
			s.width = width
		}
		if height > 0 {
			s.height = height
		}
	}
	if width <= 0 || height <= 0 {
		return ""
	}
	s = s.SetSize(width, height)

	status := s.statusLine(width)
	// Render editor.
	editorView := s.editor.View()
	if !s.editing {
		editorView = s.opts.Theme.Dim.Render(runewidth.Truncate(editorView, width, "…"))
	}

	resultView := s.result.View(width, max(s.height-6, 1))

	// Build raw lines.
	raw := editorView + "\n" + status + "\n" + resultView
	if s.err != "" {
		raw += "\n" + s.opts.Theme.Error.Render(runewidth.Truncate("error: "+s.err, width, "…"))
	}
	if s.running {
		raw += "\n" + s.opts.Theme.Dim.Render(runewidth.Truncate("running…", width, "…"))
	}
	lines := strings.Split(raw, "\n")
	out := make([]string, 0, height)
	for i := 0; i < height; i++ {
		if i < len(lines) {
			out = append(out, runewidth.Truncate(lines[i], width, "…"))
		} else {
			out = append(out, "")
		}
	}
	return strings.Join(out, "\n")
}

func (s SQL) statusLine(width int) string {
	mode := "normal"
	if s.editing {
		mode = "insert"
	}
	hint := "i edit · esc normal · ctrl+enter run · ctrl+l clear · ctrl+r history"
	text := "SQL (" + mode + ")  " + hint
	return s.opts.Theme.StatusBar.Render(runewidth.Truncate(text, width, "…"))
}

// Update handles keys.
func (s SQL) Update(msg tea.KeyMsg) (SQL, SQLEvent) {
	if s.editing {
		// editor-focused
		switch {
		case key.Matches(msg, s.opts.Keys.SQL.Normal):
			s.editing = false
			s.editor.Blur()
			return s, SQLEvent{}
		case key.Matches(msg, s.opts.Keys.SQL.Execute):
			text := s.editor.Value()
			s.lastSQL = text
			s.running = true
			return s, SQLEvent{Kind: SQLEventExecute, SQL: text}
		case key.Matches(msg, s.opts.Keys.SQL.Clear):
			s.editor.Reset()
			s.result = s.result.SetData(GridPage{})
			s.err = ""
			return s, SQLEvent{Kind: SQLEventClear}
		case key.Matches(msg, s.opts.Keys.SQL.History):
			// cycle history into editor
			if len(s.history) > 0 {
				if s.histIdx < 0 {
					s.histIdx = len(s.history) - 1
				}
				s.editor.SetValue(s.history[s.histIdx])
				s.histIdx--
				if s.histIdx < 0 {
					s.histIdx = len(s.history) - 1
				}
			}
			return s, SQLEvent{Kind: SQLEventHistory}
		}
		// let textarea handle text input
		var cmd tea.Cmd
		s.editor, cmd = s.editor.Update(msg)
		_ = cmd
		return s, SQLEvent{}
	}

	// normal mode
	switch {
	case key.Matches(msg, s.opts.Keys.SQL.Edit):
		s.editing = true
		s.editor.Focus()
		return s, SQLEvent{}
	case key.Matches(msg, s.opts.Keys.SQL.Normal):
		// esc in normal mode does nothing extra
	case key.Matches(msg, s.opts.Keys.SQL.Execute):
		text := s.editor.Value()
		if strings.TrimSpace(text) != "" {
			s.lastSQL = text
			s.running = true
			return s, SQLEvent{Kind: SQLEventExecute, SQL: text}
		}
	case key.Matches(msg, s.opts.Keys.SQL.Clear):
		s.editor.Reset()
		s.result = s.result.SetData(GridPage{})
		s.err = ""
		return s, SQLEvent{Kind: SQLEventClear}
	case key.Matches(msg, s.opts.Keys.SQL.History):
		if len(s.history) > 0 {
			if s.histIdx < 0 {
				s.histIdx = len(s.history) - 1
			}
			s.editor.SetValue(s.history[s.histIdx])
			s.histIdx--
			if s.histIdx < 0 {
				s.histIdx = len(s.history) - 1
			}
			return s, SQLEvent{Kind: SQLEventHistory}
		}
	case key.Matches(msg, s.opts.Keys.Global.ForceQuit):
		// leave cancel to root; emit cancel
		return s, SQLEvent{Kind: SQLEventCancel}
	}
	// delegate non-edit keys (e.g. grid navigation when results focused)
	g, ev := s.result.Update(msg)
	s.result = g
	if ev.Kind != GridEventNone {
		// unlikely but harmless
		_ = ev
	}
	return s, SQLEvent{}
}

