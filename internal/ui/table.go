package ui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// col describes a table column. Columns are sized to their widest cell
// (title included) and separated by exactly colGap.
type col struct {
	title    string
	max      int  // cap on the width; 0 for none
	flex     bool // shrinks (down to minFlex) when the table is too wide
	optional bool // dropped, rightmost first, if shrinking isn't enough
	right    bool // right-aligned
}

const (
	colGap     = "  "
	minFlex    = 12
	minComfort = 30
)

type table struct {
	cols   []col
	widths []int // 0 means the column is hidden
}

// newTable lays out rows (cells may contain ANSI styling) within w cells.
func newTable(cols []col, rows [][]string, w int) *table {
	ws := make([]int, len(cols))
	for i, c := range cols {
		ws[i] = ansi.StringWidth(c.title)
	}
	for _, r := range rows {
		for i := 0; i < len(r) && i < len(cols); i++ {
			ws[i] = max(ws[i], ansi.StringWidth(r[i]))
		}
	}
	for i, c := range cols {
		if c.max > 0 {
			ws[i] = min(ws[i], c.max)
		}
	}
	t := &table{cols: cols, widths: ws}
	natural := append([]int(nil), ws...)
	shrink := func(floor int) {
		for i, c := range cols {
			if over := t.width() - w; over > 0 && c.flex && ws[i] > floor {
				ws[i] -= min(over, ws[i]-floor)
			}
		}
	}
	// squeeze flexible columns to a comfortable width, then drop optional
	// columns, then squeeze further; give back any room a drop freed up
	shrink(minComfort)
	for i := len(cols) - 1; i >= 0 && t.width() > w; i-- {
		if cols[i].optional {
			ws[i] = 0
		}
	}
	shrink(minFlex)
	for i, c := range cols {
		if spare := w - t.width(); spare > 0 && c.flex && ws[i] < natural[i] {
			ws[i] += min(spare, natural[i]-ws[i])
		}
	}
	return t
}

// width is the total width of a row.
func (t *table) width() int {
	n, total := 0, 0
	for _, w := range t.widths {
		if w > 0 {
			total += w
			n++
		}
	}
	return total + max(0, n-1)*len(colGap)
}

func (t *table) row(cells ...string) string {
	var parts []string
	for i, w := range t.widths {
		if w == 0 {
			continue
		}
		cell := ""
		if i < len(cells) {
			cell = cells[i]
		}
		if t.cols[i].right {
			cell = fit(cell, w)
			cell = strings.Repeat(" ", w-ansi.StringWidth(cell)) + cell
		} else {
			cell = pad(cell, w)
		}
		parts = append(parts, cell)
	}
	return strings.TrimRight(strings.Join(parts, colGap), " ")
}

// header renders the column titles.
func (t *table) header() string {
	titles := make([]string, len(t.cols))
	for i, c := range t.cols {
		titles[i] = c.title
	}
	return sHeader.Render(t.row(titles...))
}

// shown reports whether column i survived layout.
func (t *table) shown(i int) bool { return t.widths[i] > 0 }
