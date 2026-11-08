package component

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"

	"github.com/rgmaz/dbt/internal/db"
	"github.com/rgmaz/dbt/internal/tui/keys"
	"github.com/rgmaz/dbt/internal/tui/theme"
)

// GridEventKind identifies an action requested by the data grid.
type GridEventKind int

// Data grid actions.
const (
	GridEventNone GridEventKind = iota
	GridEventMove
	GridEventPage
	GridEventSort
	GridEventCellDetail
	GridEventCopyCell
	GridEventCopyRow
	GridEventReload
)

// GridEvent tells the browser how to react to a grid key. The grid performs no
// database I/O; the browser translates these events into queries.
type GridEvent struct {
	Kind   GridEventKind
	Page   int
	Column int
	Desc   bool
	Cell   db.Cell
	Header string
	Row    int
}

// GridPage carries one page of driver data.
type GridPage struct {
	Columns   []db.ResultColumn
	Rows      [][]db.Cell
	Page      int // 0-based page index
	PageSize  int
	Total     int
	Truncated bool // true when another page may exist
	Loading   bool
	Error     string
}

// GridOptions configures the data grid.
type GridOptions struct {
	Theme theme.Theme
	Keys  keys.Map
}

// Grid is the scrollable/pagable data grid. Pure state only.
type Grid struct {
	opts GridOptions

	width, height int

	columns   []db.ResultColumn
	rows      [][]db.Cell
	total     int
	page      int
	pageSize  int
	truncated bool

	cursorRow, cursorCol int
	scrollX              int

	sortCol  int // -1 when no sort
	sortDesc bool

	loading bool
	err     string

	// widths cached per column set.
	widths []int
}

// NewGrid constructs an empty grid.
func NewGrid(opts GridOptions) Grid {
	return Grid{
		opts:     opts,
		pageSize: 100,
		sortCol:  -1,
	}
}

// SetSize updates the render area.
func (g Grid) SetSize(width, height int) Grid {
	g.width = max(width, 0)
	g.height = max(height, 0)
	g.clampScrollX()
	g.clampCursor()
	return g
}

// SetPageSize updates the page size.
func (g Grid) SetPageSize(size int) Grid {
	if size <= 0 {
		size = 100
	}
	g.pageSize = size
	return g
}

// SetData installs a new page of rows.
func (g Grid) SetData(page GridPage) Grid {
	if page.PageSize > 0 {
		g.pageSize = page.PageSize
	}
	g.columns = page.Columns
	g.rows = page.Rows
	g.total = page.Total
	g.page = page.Page
	g.truncated = page.Truncated
	g.loading = page.Loading
	g.err = page.Error
	g.cursorRow = 0
	g.cursorCol = min(g.cursorCol, max(len(g.columns)-1, 0))
	if g.sortCol >= len(g.columns) {
		g.sortCol = -1
	}
	g.computeWidths()
	g.clampScrollX()
	g.clampCursor()
	return g
}

// SetLoading marks the grid as loading while keeping current data.
func (g Grid) SetLoading(loading bool) Grid {
	g.loading = loading
	return g
}

// Init satisfies component consumers; grid starts empty.
func (g Grid) Init() tea.Cmd { return nil }

// CapturesText reports whether the grid owns text input (it never does).
func (g Grid) CapturesText() bool { return false }

// ID not needed at component level.

// Update handles one grid key and reports any request the browser must perform.
func (g Grid) Update(msg tea.KeyMsg) (Grid, GridEvent) {
	if g.err != "" {
		switch {
		case key.Matches(msg, g.opts.Keys.Global.Back):
			g.err = ""
			return g, GridEvent{}
		}
	}

	switch {
	case key.Matches(msg, g.opts.Keys.Data.Up):
		g.move(-1, 0)
	case key.Matches(msg, g.opts.Keys.Data.Down):
		g.move(1, 0)
	case key.Matches(msg, g.opts.Keys.Data.Left):
		g.move(0, -1)
	case key.Matches(msg, g.opts.Keys.Data.Right):
		g.move(0, 1)
	case key.Matches(msg, g.opts.Keys.Data.HalfPageUp):
		g.halfPage(-1)
	case key.Matches(msg, g.opts.Keys.Data.HalfPageDown):
		g.halfPage(1)
	case key.Matches(msg, g.opts.Keys.Data.NextPage):
		if g.hasNextPage() {
			g.page++
			return g, GridEvent{Kind: GridEventPage, Page: g.page}
		}
	case key.Matches(msg, g.opts.Keys.Data.PrevPage):
		if g.page > 0 {
			g.page--
			return g, GridEvent{Kind: GridEventPage, Page: g.page}
		}
	case key.Matches(msg, g.opts.Keys.Data.Sort):
		g.toggleSort()
		if g.sortCol < 0 {
			g.page = 0
			return g, GridEvent{Kind: GridEventSort, Column: -1, Desc: false, Page: 0}
		}
		g.page = 0
		return g, GridEvent{Kind: GridEventSort, Column: g.sortCol, Desc: g.sortDesc, Page: 0}
	case key.Matches(msg, g.opts.Keys.Data.CellDetail):
		if cell, header, ok := g.focusedCell(); ok {
			return g, GridEvent{Kind: GridEventCellDetail, Cell: cell, Header: header, Row: g.cursorRow}
		}
	case key.Matches(msg, g.opts.Keys.Data.CopyCell):
		if cell, _, ok := g.focusedCell(); ok {
			return g, GridEvent{Kind: GridEventCopyCell, Cell: cell, Row: g.cursorRow}
		}
	case key.Matches(msg, g.opts.Keys.Data.CopyRow):
		return g, GridEvent{Kind: GridEventCopyRow, Row: g.cursorRow, Page: g.page}
	case key.Matches(msg, g.opts.Keys.Data.Reload):
		return g, GridEvent{Kind: GridEventReload, Page: g.page}
	}
	// why: unused but kept for explicit symmetry in key handling.
	g.scrollXToCursor()
	return g, GridEvent{}
}

// move adjusts cursorRow/cursorCol, clamping and ensuring visible area.
func (g *Grid) move(dRow, dCol int) {
	rows := len(g.rows)
	if rows == 0 {
		return
	}
	cols := len(g.columns)
	if cols == 0 {
		return
	}
	g.cursorRow = clampInt(g.cursorRow+dRow, 0, rows-1)
	g.cursorCol = clampInt(g.cursorCol+dCol, 0, cols-1)
}

func (g *Grid) halfPage(dir int) {
	rows := len(g.rows)
	if rows == 0 || dir == 0 {
		return
	}
	visible := g.visibleRows()
	if visible <= 0 {
		visible = 1
	}
	delta := dir * (visible / 2)
	if delta == 0 {
		if dir > 0 {
			delta = 1
		} else {
			delta = -1
		}
	}
	g.move(delta, 0)
}

func (g *Grid) toggleSort() {
	if len(g.columns) == 0 {
		return
	}
	if g.sortCol != g.cursorCol {
		g.sortCol = g.cursorCol
		g.sortDesc = false
		return
	}
	if !g.sortDesc {
		g.sortDesc = true
		return
	}
	g.sortCol = -1
	g.sortDesc = false
}

func (g Grid) focusedCell() (db.Cell, string, bool) {
	if g.cursorRow < 0 || g.cursorRow >= len(g.rows) {
		return db.Cell{}, "", false
	}
	if g.cursorCol < 0 || g.cursorCol >= len(g.columns) {
		return db.Cell{}, "", false
	}
	row := g.rows[g.cursorRow]
	if g.cursorCol >= len(row) {
		return db.Cell{}, "", false
	}
	return row[g.cursorCol], g.columns[g.cursorCol].Name, true
}

func (g Grid) hasNextPage() bool {
	if g.truncated {
		return true
	}
	if g.total > 0 && (g.page+1)*g.pageSize < g.total {
		return true
	}
	if len(g.rows) == g.pageSize && len(g.rows) > 0 {
		return true
	}
	return false
}

func (g *Grid) computeWidths() {
	if len(g.columns) == 0 {
		g.widths = nil
		return
	}
	out := make([]int, len(g.columns))
	for i, col := range g.columns {
		w := runewidth.StringWidth(col.Name)
		for _, row := range g.rows {
			if i >= len(row) {
				continue
			}
			cell := row[i]
			text := cell.Text
			if cell.Null {
				text = "NULL"
			}
			cw := runewidth.StringWidth(text)
			if cw > w {
				w = cw
			}
		}
		w = clampInt(w+2, 4, 48)
		out[i] = w
	}
	g.widths = out
}

func (g Grid) visibleRows() int {
	// chrome: 2 lines (header + status) → state in view()
	if g.height <= 0 {
		return 0
	}
	visible := g.height - 1 // header
	if visible < 1 {
		visible = 1
	}
	return visible
}

func (g *Grid) clampCursor() {
	if len(g.rows) == 0 {
		g.cursorRow = 0
	}
	if g.cursorRow >= len(g.rows) && len(g.rows) > 0 {
		g.cursorRow = len(g.rows) - 1
	}
	if g.cursorCol < 0 {
		g.cursorCol = 0
	}
	if g.cursorCol >= len(g.columns) && len(g.columns) > 0 {
		g.cursorCol = len(g.columns) - 1
	}
}

func (g *Grid) scrollXToCursor() {
	if len(g.widths) == 0 {
		return
	}
	// Compute left position of cursor column.
	x := 0
	for i := 0; i < g.cursorCol; i++ {
		x += g.widths[i] + 1
	}
	// find visible window start (scrollX is column index).
	for g.scrollX > g.cursorCol {
		g.scrollX--
	}
	for g.scrollX < g.cursorCol {
		// Visible width consumed by columns scrollX..cursorCol inclusive.
		used := 0
		for i := g.scrollX; i <= g.cursorCol && i < len(g.widths); i++ {
			used += g.widths[i] + 1
		}
		if used <= g.availableWidth() {
			break
		}
		g.scrollX++
	}
	g.clampScrollX()
}

func (g Grid) availableWidth() int {
	if g.width <= 0 {
		return 80
	}
	return g.width
}

func (g *Grid) clampScrollX() {
	if g.scrollX < 0 {
		g.scrollX = 0
	}
	if g.scrollX > len(g.columns) {
		g.scrollX = len(g.columns)
	}
}

// View renders the grid.
func (g Grid) View(width, height int) string {
	if width > 0 {
		g.width = width
	}
	if height > 0 {
		g.height = height
	}
	if width <= 0 || height <= 0 {
		return ""
	}

	lines := make([]string, 0, height)
	// Header.
	header := g.renderHeader(width)
	lines = append(lines, header)
	visibleRows := height - 2
	if visibleRows < 1 {
		visibleRows = 1
	}

	if g.err != "" {
		for i := 1; i < height; i++ {
			lines = append(lines, g.opts.Theme.Error.Render(runewidth.Truncate("error: "+g.err, width, "…")))
		}
	} else if g.loading {
		for i := 1; i < height; i++ {
			if i == 1 {
				lines = append(lines, g.opts.Theme.Dim.Render(runewidth.Truncate("Loading…", width, "…")))
			} else {
				lines = append(lines, "")
			}
		}
	} else if len(g.columns) == 0 {
		lines = append(lines, g.opts.Theme.Dim.Render(runewidth.Truncate("No data", width, "…")))
		for i := len(lines); i < height; i++ {
			lines = append(lines, "")
		}
	} else {
		for r := 0; r < visibleRows; r++ {
			if r >= len(g.rows) {
				lines = append(lines, "")
				continue
			}
			row := g.renderRow(r, width)
			lines = append(lines, row)
		}
		// highlight cursor row by re-rendering? Already embedded below via renderRow.
		// Replace last rendered cursor row with active styling.
	}

	if len(lines) > height {
		lines = lines[:height]
	}
	for len(lines) < height {
		lines = append(lines, "")
	}

	// Status line on bottom.
	status := g.statusLine(width)
	lines[height-1] = status
	return strings.Join(lines, "\n")
}

func (g Grid) renderHeader(width int) string {
	if len(g.columns) == 0 {
		return ""
	}
	cells := make([]string, 0, len(g.columns))
	for i := g.scrollX; i < len(g.columns); i++ {
		col := g.columns[i]
		w := g.widths[i]
		name := runewidth.Truncate(col.Name, w-2, "…")
		padded := padRight(name, w)
		style := g.opts.Theme.SectionTitle
		if g.sortCol == i {
			indicator := " ▲"
			if g.sortDesc {
				indicator = " ▼"
			}
			padded = runewidth.Truncate(col.Name+indicator, w, "…")
			padded = padRight(padded, w)
		}
		cells = append(cells, style.Render(runewidth.Truncate(padded, w, "…")))
		if i < len(g.columns)-1 {
			cells = append(cells, "  ")
		}
	}
	line := strings.Join(cells, "")
	if runewidth.StringWidth(line) > width {
		line = runewidth.Truncate(line, width, "…")
	}
	return line
}

func (g Grid) renderRow(rowIdx int, width int) string {
	if rowIdx < 0 || rowIdx >= len(g.rows) {
		return ""
	}
	row := g.rows[rowIdx]
	cells := make([]string, 0, len(g.columns))
	for i := g.scrollX; i < len(g.columns); i++ {
		w := g.widths[i]
		text := ""
		relig := db.Cell{}
		if i < len(row) {
			relig = row[i]
		}
		if relig.Null {
			text = g.opts.Theme.Null.Render(padRight("NULL", w))
		} else {
			text = padRight(runewidth.Truncate(relig.Text, w-2, "…"), w)
		}
		if g.cursorRow == rowIdx && g.cursorCol == i {
			text = g.opts.Theme.TabActive.Render(runewidth.Truncate(text, w, "…"))
			text = padRight(text, w)
		}
		cells = append(cells, runewidth.Truncate(text, w, "…"))
		if i < len(g.columns)-1 {
			cells = append(cells, "  ")
		}
	}
	line := strings.Join(cells, "")
	if runewidth.StringWidth(line) > width {
		line = runewidth.Truncate(line, width, "…")
	}
	// Ensure ASCII fit.
	return line
}

func (g Grid) statusLine(width int) string {
	status := ""
	if g.total > 0 {
		end := (g.page + 1) * g.pageSize
		if end > g.total {
			end = g.total
		}
		start := g.page*g.pageSize + 1
		if len(g.rows) == 0 {
			start = 0
		}
		status = "rows " + strconv.Itoa(start) + "–" + strconv.Itoa(end) + " of " + strconv.Itoa(g.total)
	} else if len(g.rows) > 0 {
		status = "rows " + strconv.Itoa(g.page*g.pageSize+1) + "–" + strconv.Itoa(g.page*g.pageSize+len(g.rows))
	}
	hint := "h/j/k/l move · n/p page · s sort · Enter detail · ?: help"
	text := hint
	if status != "" {
		text = status + "  ·  " + hint
	}
	if g.err != "" {
		text = "error: " + g.err
	}
	return g.opts.Theme.StatusBar.Render(runewidth.Truncate(text, width, "…"))
}

// Page returns the current page index.
func (g Grid) Page() int { return g.page }

// Columns returns the current columns.
func (g Grid) Columns() []db.ResultColumn { return g.columns }

// ColumnName returns one column's name.
func (g Grid) ColumnName(i int) string {
	if i < 0 || i >= len(g.columns) {
		return ""
	}
	return g.columns[i].Name
}

// SetTotal updates the total row count without replacing rows.
func (g Grid) SetTotal(total int) Grid {
	g.total = total
	return g
}
