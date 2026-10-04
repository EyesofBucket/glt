package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// chooser is a telescope-style finder over a list of choices, drawn as an
// overlay: prompt at the bottom, best match just above it. In multi mode
// tab (or a click) toggles and enter saves; in single mode enter (or a
// double click) picks the highlighted choice.
type chooser struct {
	title  string // results pane title
	prompt string // prompt pane title
	multi  bool
	// items returns the choices (and whether they're still loading).
	items func(a *App) ([]choice, bool)
	load  func(a *App, force bool) tea.Cmd
	// exclusive, if set, lists choices that turning on id turns off.
	exclusive func(on map[string]bool, id string) []string
	// done is called with the chosen IDs (sorted in multi mode).
	done func(a *App, chosen []string) tea.Cmd
	// hover, if set, is called as the highlighted choice changes (for live
	// previews); cancel is called when the chooser is dismissed.
	hover  func(a *App, id string)
	cancel func(a *App)
	// initial is the choice to start on (single mode).
	initial string

	back    overlay // overlay to return to when closed
	hovered string
	started bool

	orig, on map[string]bool
	input    textinput.Model
	cursor   int
	offset   int
	rows     []choiceRow
	height   int // result rows shown at last render, for paging
}

type choice struct {
	id     string
	text   string // matched against the query
	prefix string // styled, before the text (e.g. a colour dot)
	desc   string // plain, shown dimmed after the text
	order  int    // tie-break when there's no query (lower first)
}

type choiceRow struct {
	c   choice
	pos []int
}

func (a *App) openChooser(ch *chooser, selected []string) tea.Cmd {
	ti := textinput.New()
	ti.Prompt = sActive.Render(ic.caret) + " "
	ti.Placeholder = "filter"
	focus := ti.Focus()
	ch.input = ti
	ch.orig, ch.on = map[string]bool{}, map[string]bool{}
	for _, id := range selected {
		ch.orig[id], ch.on[id] = true, true
	}
	ch.back = a.overlay
	a.overlay = ch
	return tea.Batch(focus, ch.refresh(a, false))
}

func (ch *chooser) refresh(a *App, force bool) tea.Cmd {
	if ch.load == nil {
		return nil
	}
	return ch.load(a, force)
}

func (ch *chooser) debounced(*App, int) tea.Cmd { return nil }

func (ch *chooser) passthrough(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	ch.input, cmd = ch.input.Update(msg)
	return cmd
}

func (ch *chooser) help() []kb {
	if ch.multi {
		return []kb{{"tab", "toggle"}, {"enter", "save"}, {"^n/^p", "move"}, {"esc", "cancel"}}
	}
	return []kb{{"enter", "choose"}, {"^n/^p", "move"}, {"esc", "cancel"}}
}

// recompute filters and orders the choices: with no query, originally
// selected ones come first (nearest the prompt), then by order; with a
// query, best match first.
func (ch *chooser) recompute(a *App) {
	all, _ := ch.items(a)
	q := strings.TrimSpace(ch.input.Value())
	type scored struct {
		choiceRow
		score int
	}
	seen := map[string]bool{}
	var res []scored
	for _, c := range all {
		if seen[c.id] {
			continue
		}
		seen[c.id] = true
		if q == "" {
			res = append(res, scored{choiceRow{c: c}, 0})
		} else if sc, pos, ok := fuzzyMatch(q, c.text); ok {
			res = append(res, scored{choiceRow{c, pos}, sc})
		}
	}
	sort.SliceStable(res, func(i, j int) bool {
		ri, rj := res[i], res[j]
		if q != "" {
			return ri.score > rj.score
		}
		if ch.orig[ri.c.id] != ch.orig[rj.c.id] {
			return ch.orig[ri.c.id]
		}
		return ri.c.order < rj.c.order
	})
	ch.rows = ch.rows[:0]
	for _, r := range res {
		ch.rows = append(ch.rows, r.choiceRow)
	}
	if !ch.started && len(ch.rows) > 0 {
		ch.started = true
		for i, r := range ch.rows {
			if r.c.id == ch.initial && ch.initial != "" {
				ch.cursor = i
			}
		}
	}
	ch.cursor = max(0, min(ch.cursor, len(ch.rows)-1))
}

// moved tells the hover hook about the highlighted choice.
func (ch *chooser) moved(a *App) {
	if ch.hover == nil || ch.cursor >= len(ch.rows) {
		return
	}
	if id := ch.rows[ch.cursor].c.id; id != ch.hovered {
		ch.hovered = id
		ch.hover(a, id)
	}
}

func (ch *chooser) close(a *App) {
	a.overlay = ch.back
}

// page is the number of result rows on screen (at least 2).
func (ch *chooser) page() int { return max(2, ch.height) }

func (ch *chooser) toggle(id string) {
	if ch.on[id] {
		delete(ch.on, id)
		return
	}
	if ch.exclusive != nil {
		for _, other := range ch.exclusive(ch.on, id) {
			delete(ch.on, other)
		}
	}
	ch.on[id] = true
}

// diff lists what was turned on and off, sorted.
func (ch *chooser) diff() (add, remove []string) {
	for id := range ch.on {
		if !ch.orig[id] {
			add = append(add, id)
		}
	}
	for id := range ch.orig {
		if !ch.on[id] {
			remove = append(remove, id)
		}
	}
	sort.Strings(add)
	sort.Strings(remove)
	return add, remove
}

func (ch *chooser) finish(a *App) tea.Cmd {
	ch.close(a)
	var chosen []string
	if ch.multi {
		for id := range ch.on {
			chosen = append(chosen, id)
		}
		sort.Strings(chosen)
	} else if ch.cursor < len(ch.rows) {
		chosen = []string{ch.rows[ch.cursor].c.id}
	} else {
		return nil
	}
	return ch.done(a, chosen)
}

func (ch *chooser) key(a *App, msg tea.KeyMsg) tea.Cmd {
	cmd := ch.key_(a, msg)
	if a.overlay == ch {
		ch.recompute(a)
		ch.moved(a)
	}
	return cmd
}

func (ch *chooser) key_(a *App, msg tea.KeyMsg) tea.Cmd {
	ch.recompute(a)
	n := len(ch.rows)
	move := func(d int) tea.Cmd {
		ch.cursor = max(0, min(n-1, ch.cursor+d))
		return nil
	}
	switch msg.String() {
	case "esc":
		ch.close(a)
		if ch.cancel != nil {
			ch.cancel(a)
		}
		return nil
	case "up", "ctrl+p", "ctrl+k":
		return move(1)
	case "down", "ctrl+n", "ctrl+j":
		return move(-1)
	case "pgup", "ctrl+b":
		return move(ch.page())
	case "pgdown", "ctrl+f":
		return move(-ch.page())
	case "ctrl+u":
		return move(ch.page() / 2)
	case "ctrl+d":
		return move(-ch.page() / 2)
	case "tab", "shift+tab":
		if !ch.multi {
			return nil
		}
		if ch.cursor < n {
			ch.toggle(ch.rows[ch.cursor].c.id)
		}
		if msg.String() == "tab" {
			return move(1)
		}
		return move(-1)
	case "enter":
		return ch.finish(a)
	}
	before := ch.input.Value()
	var cmd tea.Cmd
	ch.input, cmd = ch.input.Update(msg)
	if ch.input.Value() != before {
		ch.cursor, ch.offset = 0, 0
	}
	return cmd
}

func (ch *chooser) render(a *App, body string, w, h int) string {
	ch.recompute(a)
	bw := min(w-4, max(50, w*6/10))
	bh := min(h-2, max(12, h*7/10))
	x, y := (w-bw)/2, (h-bh)/2
	promptH := 3
	resH := bh - promptH
	rows := resH - 2
	ch.height = rows
	inner := bw - 4

	if ch.cursor < ch.offset {
		ch.offset = ch.cursor
	}
	if ch.cursor >= ch.offset+rows {
		ch.offset = ch.cursor - rows + 1
	}
	lines := make([]string, max(rows, 0))
	for i := 0; i < rows; i++ {
		idx := ch.offset + i
		if idx >= len(ch.rows) {
			break
		}
		r := ch.rows[idx]
		sel := func() tea.Cmd {
			ch.cursor = idx
			ch.moved(a)
			if ch.multi {
				ch.toggle(r.c.id)
			}
			return nil
		}
		open := func() tea.Cmd {
			if ch.multi {
				return nil
			}
			return ch.finish(a)
		}
		lines[rows-1-i] = a.clickRow(ch.renderRow(r, inner, idx == ch.cursor), sel, open)
	}
	if len(ch.rows) == 0 && rows > 0 {
		msg := "no matches"
		if _, loading := ch.items(a); loading {
			msg = spinnerFrame() + " loading…"
		}
		lines[rows-1] = sDim.Render(msg)
	}
	results := pane(ch.title, lines, bw, resH, false)

	all, _ := ch.items(a)
	count := fmt.Sprintf("%d / %d", len(ch.rows), len(all))
	if ch.multi {
		add, remove := ch.diff()
		count = fmt.Sprintf("%d selected", len(ch.on))
		if len(add)+len(remove) > 0 {
			count += fmt.Sprintf(" (+%d −%d)", len(add), len(remove))
		}
	}
	count = sDim.Render(count)
	ch.input.Width = max(5, inner-ansi.StringWidth(count)-6)
	in := ch.input.View()
	promptLine := in + strings.Repeat(" ", max(1, inner-ansi.StringWidth(in)-ansi.StringWidth(count))) + count
	prompt := pane(ch.prompt, []string{promptLine}, bw, promptH, true)

	return overlayAt(body, a.zone(results+"\n"+prompt, zone{}), x, y)
}

func (ch *chooser) renderRow(r choiceRow, w int, selected bool) string {
	left := "  "
	if selected {
		left = sActive.Render(ic.caret) + " "
	}
	if ch.multi {
		box := sDim.Render(ic.unchecked)
		if ch.on[r.c.id] {
			box = sOK.Render(ic.checked)
		}
		left += box + " "
	}
	if r.c.prefix != "" {
		left += r.c.prefix + " "
	}
	text := r.c.text
	if len(r.pos) > 0 {
		text = highlight(text, r.pos, func(s string) string { return s })
	}
	left += text
	if ch.multi && ch.orig[r.c.id] != ch.on[r.c.id] {
		left += sWarn.Render(" *")
	}
	if d := strings.TrimSpace(r.c.desc); d != "" {
		left += "  " + sDim.Render(strings.Join(strings.Fields(d), " "))
	}
	line := pad(left, w)
	if selected {
		return selectLine(line, w)
	}
	return line
}
