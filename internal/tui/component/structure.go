package component

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"

	"github.com/rgmaz/dbt/internal/db"
	"github.com/rgmaz/dbt/internal/tui/keys"
	"github.com/rgmaz/dbt/internal/tui/theme"
)

// StructureEventKind identifies an action requested by the structure pane.
type StructureEventKind int

// Structure pane actions.
const (
	StructureEventNone StructureEventKind = iota
	StructureEventRefresh
)

// StructureEvent tells the browser how to react to a structure key.
type StructureEvent struct {
	Kind StructureEventKind
}

// StructureOptions configures the structure pane.
type StructureOptions struct {
	Theme theme.Theme
	Keys  keys.Map
}

// Structure is the read-only metadata pane. Pure state only.
type Structure struct {
	opts StructureOptions

	width, height int

	detail  db.RelationDetail
	hasData bool
	loading bool
	err     string

	viewport viewport.Model
	ready    bool

	lines []string // rendered detail lines (rebuilt on SetDetail/SetSize)
}

// NewStructure constructs an empty structure pane.
func NewStructure(opts StructureOptions) Structure {
	vp := viewport.New(0, 0)
	return Structure{
		opts:     opts,
		viewport: vp,
	}
}

// SetSize updates the render area and rebuilds the viewport.
func (s Structure) SetSize(width, height int) Structure {
	s.width = max(width, 0)
	s.height = max(height, 0)
	s.viewport.Width = max(s.width, 0)
	s.viewport.Height = max(s.height-1, 1)
	s.ready = true
	s.rebuild()
	return s
}

// SetDetail installs metadata.
func (s Structure) SetDetail(detail db.RelationDetail) Structure {
	s.detail = detail
	s.hasData = true
	s.loading = false
	s.err = ""
	s.rebuild()
	return s
}

// SetLoading marks the pane as loading.
func (s Structure) SetLoading(loading bool) Structure {
	s.loading = loading
	if loading {
		s.err = ""
	}
	s.rebuild()
	return s
}

// SetError records a failed Describe.
func (s Structure) SetError(message string) Structure {
	s.loading = false
	s.err = message
	s.rebuild()
	return s
}

// Init satisfies component consumers.
func (s Structure) Init() tea.Cmd { return nil }

// CapturesText reports the pane owns no text input.
func (s Structure) CapturesText() bool { return false }

// View renders the structure pane.
func (s Structure) View(width, height int) string {
	if width > 0 && (s.width != width || s.height != height) {
		s = s.SetSize(width, height)
	}
	if width <= 0 || height <= 0 {
		return ""
	}
	if !s.ready {
		s = s.SetSize(width, height)
	}

	if s.err != "" {
		line := s.opts.Theme.Error.Render(runewidth.Truncate("error: "+s.err, width, "…"))
		out := []string{line}
		for i := 1; i < height; i++ {
			out = append(out, "")
		}
		return strings.Join(out, "\n")
	}
	if s.loading {
		line := s.opts.Theme.Dim.Render(runewidth.Truncate("Loading…", width, "…"))
		out := []string{line}
		for i := 1; i < height; i++ {
			out = append(out, "")
		}
		return strings.Join(out, "\n")
	}
	if !s.hasData {
		line := s.opts.Theme.Dim.Render(runewidth.Truncate("No relation selected. Select a relation to inspect columns, indexes, and foreign keys.", width, "…"))
		out := []string{line}
		for i := 1; i < height; i++ {
			out = append(out, "")
		}
		return strings.Join(out, "\n")
	}
	// Use viewport for scrolling.
	s.viewport.Width = width
	s.viewport.Height = height
	s.viewport.SetContent(strings.Join(s.lines, "\n"))
	return s.viewport.View()
}

// Update handles scrolling keys.
func (s Structure) Update(msg tea.KeyMsg) (Structure, StructureEvent) {
	if s.err != "" {
		if msg.String() == "esc" {
			s.err = ""
			return s, StructureEvent{}
		}
	}
	if key.Matches(msg, s.opts.Keys.Structure.Up) {
		s.viewport.ScrollUp(1)
	} else if key.Matches(msg, s.opts.Keys.Structure.Down) {
		s.viewport.ScrollDown(1)
	} else if key.Matches(msg, s.opts.Keys.Structure.PageUp) {
		s.viewport.PageUp()
	} else if key.Matches(msg, s.opts.Keys.Structure.PageDown) {
		s.viewport.PageDown()
	} else if key.Matches(msg, s.opts.Keys.Structure.Top) {
		s.viewport.GotoTop()
	} else if key.Matches(msg, s.opts.Keys.Structure.Bottom) {
		s.viewport.GotoBottom()
	}
	return s, StructureEvent{}
}

// rebuild constructs the detail lines.
func (s *Structure) rebuild() {
	if s.width <= 0 {
		return
	}
	width := s.width
	if !s.hasData {
		s.lines = nil
		return
	}
	d := s.detail
	lines := make([]string, 0, 64)
	title := fmt.Sprintf("%s · %s", d.Relation.Ref(), d.Relation.Kind)
	lines = append(lines, s.opts.Theme.SectionTitle.Render(runewidth.Truncate(title, width, "…")))
	lines = append(lines, "")

	// Columns
	lines = append(lines, s.opts.Theme.SectionTitle.Render(runewidth.Truncate("Columns", width, "…")))
	if len(d.Columns) == 0 {
		lines = append(lines, s.opts.Theme.Dim.Render("  (none)"))
	}
	for _, c := range d.Columns {
		marks := []string{}
		if c.IsPrimaryKey {
			marks = append(marks, "PK")
		}
		if c.IsGenerated {
			marks = append(marks, "generated")
		}
		if !c.Nullable {
			marks = append(marks, "NOT NULL")
		}
		suffix := ""
		if len(marks) > 0 {
			suffix = "  " + strings.Join(marks, ", ")
		}
		def := ""
		if c.Default != nil {
			def = " DEFAULT " + *c.Default
		}
		line := c.Name + "  " + c.DataType + def + suffix
		lines = append(lines, runewidth.Truncate(line, width, "…"))
		if c.Comment != "" {
			lines = append(lines, s.opts.Theme.Dim.Render(runewidth.Truncate("  "+c.Comment, width, "…")))
		}
	}
	lines = append(lines, "")

	// Indexes
	lines = append(lines, s.opts.Theme.SectionTitle.Render(runewidth.Truncate("Indexes", width, "…")))
	if len(d.Indexes) == 0 {
		lines = append(lines, s.opts.Theme.Dim.Render("  (none)"))
	}
	for _, idx := range d.Indexes {
		flags := []string{}
		if idx.Primary {
			flags = append(flags, "primary")
		}
		if idx.Unique {
			flags = append(flags, "unique")
		}
		if idx.Method != "" {
			flags = append(flags, idx.Method)
		}
		suffix := ""
		if len(flags) > 0 {
			suffix = "  (" + strings.Join(flags, ", ") + ")"
		}
		line := idx.Name + suffix
		lines = append(lines, runewidth.Truncate(line, width, "…"))
		if len(idx.Columns) > 0 {
			cols := strings.Join(idx.Columns, ", ")
			lines = append(lines, s.opts.Theme.Dim.Render(runewidth.Truncate("  "+cols, width, "…")))
		}
	}
	lines = append(lines, "")

	// Foreign keys
	lines = append(lines, s.opts.Theme.SectionTitle.Render(runewidth.Truncate("Foreign keys", width, "…")))
	if len(d.ForeignKeys) == 0 {
		lines = append(lines, s.opts.Theme.Dim.Render("  (none)"))
	}
	for _, fk := range d.ForeignKeys {
		line := fk.Name + ": " + strings.Join(fk.Columns, ", ") + " → " + fk.RefSchema + "." + fk.RefTable + "(" + strings.Join(fk.RefColumns, ", ") + ")"
		lines = append(lines, runewidth.Truncate(line, width, "…"))
		if fk.OnDelete != "" || fk.OnUpdate != "" {
			lines = append(lines, s.opts.Theme.Dim.Render(runewidth.Truncate("  on delete "+fk.OnDelete+", on update "+fk.OnUpdate, width, "…")))
		}
	}
	lines = append(lines, "")

	// DDL
	lines = append(lines, s.opts.Theme.SectionTitle.Render(runewidth.Truncate("DDL", width, "…")))
	if !d.DDLSupported {
		lines = append(lines, s.opts.Theme.Dim.Render("  DDL is not available for this driver"))
	} else if strings.TrimSpace(d.DDL) == "" {
		lines = append(lines, s.opts.Theme.Dim.Render("  (no DDL)"))
	} else {
		for _, dl := range strings.Split(d.DDL, "\n") {
			lines = append(lines, runewidth.Truncate(dl, width, "…"))
		}
	}

	s.lines = lines
	s.viewport.SetContent(strings.Join(lines, "\n"))
}
