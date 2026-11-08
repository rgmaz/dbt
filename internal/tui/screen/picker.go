package screen

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"

	"github.com/rgmaz/dbt/internal/db"
	"github.com/rgmaz/dbt/internal/tui/keys"
	"github.com/rgmaz/dbt/internal/tui/theme"
)

const maxProfileNameWidth = 20

// PickerProfile is one selectable connection. Detail is already redacted by the
// caller so the picker never renders credentials.
type PickerProfile struct {
	Name   string
	Driver string
	Detail string
	Kind   db.Kind
}

// ConnectRequestMsg asks the root model to open the selected profile. The
// picker only emits this message; it never performs database I/O itself.
type ConnectRequestMsg struct {
	Profile PickerProfile
}

// PickerOptions configures the connection picker.
type PickerOptions struct {
	Profiles   []PickerProfile
	ConfigPath string
	Theme      theme.Theme
	Keys       keys.Map
}

// Picker lists configured connection profiles. Selection is handed to the root
// model, which owns async connection and session lifecycle.
type Picker struct {
	opts          PickerOptions
	width, height int
	cursor        int
	offset        int
	connecting    string
}

// NewPicker constructs the connection picker.
func NewPicker(opts PickerOptions) Picker {
	return Picker{opts: opts}
}

// Init implements Screen.
func (p Picker) Init() tea.Cmd { return nil }

// ID implements Screen.
func (p Picker) ID() ID { return PickerScreen }

// CapturesText implements Screen.
func (p Picker) CapturesText() bool { return false }

// Cursor reports the selected profile index.
func (p Picker) Cursor() int { return p.cursor }

// Connecting marks the picker as connecting to the named profile.
func (p Picker) Connecting(name string) Picker {
	p.connecting = name
	return p
}

// ClearConnecting clears the connecting state.
func (p Picker) ClearConnecting() Picker {
	p.connecting = ""
	return p
}

// Update implements Screen.
func (p Picker) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width = max(msg.Width, 0)
		p.height = max(msg.Height, 0)
		p.clampOffset()
	case tea.KeyMsg:
		if p.connecting != "" {
			return p, nil
		}
		switch {
		case key.Matches(msg, p.opts.Keys.Sidebar.Up):
			p.move(-1)
		case key.Matches(msg, p.opts.Keys.Sidebar.Down):
			p.move(1)
		case key.Matches(msg, p.opts.Keys.Sidebar.Top):
			p.cursor = 0
			p.clampOffset()
		case key.Matches(msg, p.opts.Keys.Sidebar.Bottom):
			p.cursor = max(len(p.opts.Profiles)-1, 0)
			p.clampOffset()
		case key.Matches(msg, p.opts.Keys.Sidebar.Enter):
			return p, p.selectCmd()
		}
	}
	return p, nil
}

// View implements Screen.
func (p Picker) View() string {
	if p.width <= 0 || p.height <= 0 {
		return ""
	}

	innerWidth := max(p.width-2, 1) // Main has one column of horizontal padding.
	lines := []string{p.opts.Theme.SectionTitle.Render("Connections"), ""}
	if len(p.opts.Profiles) == 0 {
		lines = append(lines, p.onboarding(innerWidth)...)
	} else {
		lines = append(lines, p.listView(innerWidth)...)
		if p.connecting != "" {
			lines = append(lines, "", p.opts.Theme.StatusKey.Render(
				runewidth.Truncate("Connecting to "+p.connecting+"…", innerWidth, "…"),
			))
		}
	}

	if len(lines) > p.height {
		lines = lines[:p.height]
	}

	body := strings.Join(lines, "\n")
	return p.opts.Theme.Main.Width(p.width).Height(p.height).Render(body)
}

func (p Picker) selectCmd() tea.Cmd {
	if len(p.opts.Profiles) == 0 {
		return nil
	}
	profile := p.opts.Profiles[p.cursor]
	return func() tea.Msg {
		return ConnectRequestMsg{Profile: profile}
	}
}

func (p *Picker) move(delta int) {
	if len(p.opts.Profiles) == 0 {
		return
	}
	p.cursor = clampInt(p.cursor+delta, 0, len(p.opts.Profiles)-1)
	p.clampOffset()
}

func (p Picker) visibleRows() int {
	return max(p.height-4, 1)
}

func (p *Picker) clampOffset() {
	if p.cursor < p.offset {
		p.offset = p.cursor
	}
	visible := p.visibleRows()
	if p.cursor >= p.offset+visible {
		p.offset = p.cursor - visible + 1
	}
	p.offset = max(p.offset, 0)
	maxOffset := max(len(p.opts.Profiles)-visible, 0)
	if p.offset > maxOffset {
		p.offset = maxOffset
	}
}

func (p Picker) listView(width int) []string {
	nameWidth := 0
	for _, profile := range p.opts.Profiles {
		nameWidth = max(nameWidth, runewidth.StringWidth(profile.Name))
	}
	nameWidth = min(nameWidth, maxProfileNameWidth)
	nameWidth = max(nameWidth, 4)

	visible := p.visibleRows()
	start := p.offset
	end := min(start+visible, len(p.opts.Profiles))
	rows := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		profile := p.opts.Profiles[i]
		marker := "  "
		if i == p.cursor {
			marker = "> "
		}
		line := fmt.Sprintf("%s%s  %-8s  %s",
			marker,
			padRight(profile.Name, nameWidth),
			profile.Driver,
			profile.Detail,
		)
		line = runewidth.Truncate(line, width, "…")
		if i == p.cursor {
			line = p.opts.Theme.TabActive.Render(line)
		}
		rows = append(rows, line)
	}
	return rows
}

func (p Picker) onboarding(width int) []string {
	path := strings.TrimSpace(p.opts.ConfigPath)
	if path == "" {
		path = "$XDG_CONFIG_HOME/dbt/config.toml"
	}

	lines := []string{
		"No connections configured.",
		"",
		"Add a [[connection]] block to:",
		"  " + path,
		"",
		"Example:",
		"",
		"  [[connection]]",
		`  name   = "local-pg"`,
		`  driver = "postgres"`,
		`  dsn    = "postgres://app@localhost:5432/app?sslmode=disable"`,
		"",
		"Or pass a DSN directly:",
		"  dbt postgres://...",
		"",
		"q quit",
	}
	for i, line := range lines {
		lines[i] = runewidth.Truncate(line, width, "…")
	}
	return lines
}

func padRight(text string, width int) string {
	padding := width - runewidth.StringWidth(text)
	if padding <= 0 {
		return text
	}
	return text + strings.Repeat(" ", padding)
}

func clampInt(value, low, high int) int {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}
