package component

import (
	"strings"

	"github.com/mattn/go-runewidth"

	"github.com/rgmaz/dbt/internal/tui/keys"
	"github.com/rgmaz/dbt/internal/tui/theme"
)

const (
	helpGap         = 2
	maxHelpKeyWidth = 10
)

type helpLine struct {
	title bool
	text  string
}

// HelpView renders the grouped keymap as a centered help modal. Groups are
// packed into balanced columns so the full keymap fits an 80x24 terminal.
func HelpView(km keys.Map, th theme.Theme, width, height int) string {
	groups := km.Groups()
	keyWidth := 0
	for _, group := range groups {
		for _, bind := range group.Bindings {
			keyWidth = max(keyWidth, runewidth.StringWidth(bind.Help().Key))
		}
	}
	keyWidth = min(keyWidth, maxHelpKeyWidth)
	keyWidth = max(keyWidth, 4)

	blocks := make([][]helpLine, len(groups))
	for i, group := range groups {
		lines := []helpLine{{title: true, text: group.Title}}
		for _, bind := range group.Bindings {
			help := bind.Help()
			lines = append(lines, helpLine{
				text: padRight(help.Key, keyWidth) + " " + help.Desc,
			})
		}
		blocks[i] = lines
	}

	innerWidth := min(width-2, maxModalWidth) - 6
	columns := helpColumnCount(innerWidth)
	packed := packColumns(blocks, columns)
	body := renderColumns(packed, th, innerWidth, columns)

	return Modal{Title: "Keys", Body: body, Theme: th}.Render(width, height)
}

func helpColumnCount(innerWidth int) int {
	switch {
	case innerWidth >= 60:
		return 3
	case innerWidth >= 36:
		return 2
	default:
		return 1
	}
}

func packColumns(blocks [][]helpLine, columns int) [][]helpLine {
	packed := make([][]helpLine, columns)
	heights := make([]int, columns)

	for _, block := range blocks {
		target := 0
		for i := 1; i < len(packed); i++ {
			if heights[i] < heights[target] {
				target = i
			}
		}
		if heights[target] > 0 {
			packed[target] = append(packed[target], helpLine{})
			heights[target]++
		}
		packed[target] = append(packed[target], block...)
		heights[target] += len(block)
	}
	return packed
}

func renderColumns(columns [][]helpLine, th theme.Theme, innerWidth, count int) string {
	if count == 0 {
		return ""
	}

	columnWidth := (innerWidth - helpGap*(count-1)) / count
	if columnWidth < 8 {
		columnWidth = 8
	}

	rendered := make([][]string, count)
	maxLines := 0
	for i, lines := range columns {
		rendered[i] = make([]string, 0, len(lines))
		for _, line := range lines {
			text := runewidth.Truncate(line.text, columnWidth, "…")
			text = padRight(text, columnWidth)
			if line.title {
				text = th.ModalTitle.Render(text)
			}
			rendered[i] = append(rendered[i], text)
		}
		maxLines = max(maxLines, len(rendered[i]))
	}

	rows := make([]string, maxLines)
	for row := 0; row < maxLines; row++ {
		parts := make([]string, count)
		for col := 0; col < count; col++ {
			if row < len(rendered[col]) {
				parts[col] = rendered[col][row]
			} else {
				parts[col] = strings.Repeat(" ", columnWidth)
			}
		}
		rows[row] = strings.Join(parts, strings.Repeat(" ", helpGap))
	}
	return strings.Join(rows, "\n")
}

func padRight(text string, width int) string {
	padding := width - runewidth.StringWidth(text)
	if padding <= 0 {
		return text
	}
	return text + strings.Repeat(" ", padding)
}
