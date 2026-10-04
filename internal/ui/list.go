package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// listState is cursor/scroll/filter bookkeeping shared by the list views.
type listState struct {
	cursor, offset int
	height         int // rows visible at last render
	filter         textinput.Model
	filtering      bool
	query          string
}

func newListState() listState {
	ti := textinput.New()
	ti.Prompt = "/"
	return listState{filter: ti}
}

// nav handles movement keys. It returns true if the key was consumed and
// whether the cursor moved.
func (l *listState) nav(key string, total int) (handled, moved bool) {
	prev := l.cursor
	page := max(1, l.height)
	switch key {
	case "j", "down", "ctrl+n":
		l.cursor++
	case "k", "up", "ctrl+p":
		l.cursor--
	case "g", "home":
		l.cursor = 0
	case "G", "end":
		l.cursor = total - 1
	case "ctrl+d":
		l.cursor += page / 2
	case "ctrl+u":
		l.cursor -= page / 2
	case "pgdown", "ctrl+f", " ":
		l.cursor += page
	case "pgup", "ctrl+b":
		l.cursor -= page
	default:
		return false, false
	}
	l.clamp(total)
	return true, l.cursor != prev
}

// clickRow makes row i clickable: a click moves the cursor there (calling
// moved), a double click opens it.
func (l *listState) clickRow(a *App, line string, i int, moved func() tea.Cmd, open func() tea.Cmd) string {
	return a.clickRow(line, func() tea.Cmd {
		if l.cursor == i {
			return nil
		}
		l.cursor = i
		return moved()
	}, open)
}

func (l *listState) clamp(total int) {
	if l.cursor >= total {
		l.cursor = total - 1
	}
	if l.cursor < 0 {
		l.cursor = 0
	}
}

// window returns the [start,end) slice of rows to draw in h lines.
func (l *listState) window(total, h int) (int, int) {
	l.height = h
	l.clamp(total)
	if h <= 0 {
		return 0, 0
	}
	if l.cursor < l.offset {
		l.offset = l.cursor
	}
	if l.cursor >= l.offset+h {
		l.offset = l.cursor - h + 1
	}
	if l.offset > max(0, total-h) {
		l.offset = max(0, total-h)
	}
	return l.offset, min(total, l.offset+h)
}

func (l *listState) startFilter() tea.Cmd {
	l.filtering = true
	l.filter.SetValue(l.query)
	l.filter.CursorEnd()
	return l.filter.Focus()
}

// filterKey feeds a key to the filter input. It reports whether the query
// changed.
func (l *listState) filterKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	switch msg.String() {
	case "esc":
		l.filtering = false
		l.filter.Blur()
		changed := l.query != ""
		l.query = ""
		return nil, changed
	case "enter":
		l.filtering = false
		l.filter.Blur()
		return nil, false
	}
	var cmd tea.Cmd
	l.filter, cmd = l.filter.Update(msg)
	changed := l.filter.Value() != l.query
	l.query = l.filter.Value()
	if changed {
		l.cursor, l.offset = 0, 0
	}
	return cmd, changed
}

func (l *listState) filterLine() string {
	if l.filtering {
		return l.filter.View()
	}
	if l.query != "" {
		return sDim.Render("/" + l.query + "  (esc on / to clear)")
	}
	return ""
}

// matches is a case-insensitive, all-words substring match.
func matches(query string, fields ...string) bool {
	if query == "" {
		return true
	}
	hay := strings.ToLower(strings.Join(fields, " "))
	for _, w := range strings.Fields(strings.ToLower(query)) {
		if !strings.Contains(hay, w) {
			return false
		}
	}
	return true
}
