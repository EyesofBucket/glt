package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// Mouse support.
//
// Clickable regions ("zones") are marked while rendering by wrapping text in
// private CSI sequences that every width/truncation helper treats as
// zero-width. View() then scans the finished frame, records where each zone
// landed and strips the markers, so views never need to know their own
// screen offsets.
//
// Dragging selects text like a terminal does, but confined to the pane the
// drag started in: a multi-line selection wraps at the pane's borders
// instead of running across neighbouring panes. Releasing copies the
// selection and clears it.

type zone struct {
	click  func(double bool) tea.Cmd // nil: swallow clicks
	scroll func(dir int) tea.Cmd     // nil: fall through to the view
	layer  int                       // 1 for the picker overlay
}

type zoneSeg struct {
	id, y, x0, x1 int // x1 inclusive
}

type rect struct{ x0, y0, x1, y1 int } // inclusive

func (r rect) has(x, y int) bool { return x >= r.x0 && x <= r.x1 && y >= r.y0 && y <= r.y1 }

type selection struct {
	area     rect // the pane the selection is confined to
	ax, ay   int  // anchor (press position)
	ex, ey   int  // extent (current drag position)
	dragging bool
}

type mouseState struct {
	zones []zone
	segs  []zoneSeg
	layer int      // layer for zones registered now
	frame []string // last rendered frame, markers stripped
	sel   *selection

	lastClickAt time.Time
	lastClickID int
}

const (
	zoneOpen  = ";7z"
	zoneClose = ";8z"
)

// zone makes s (possibly multi-line) clickable/scrollable.
func (a *App) zone(s string, z zone) string {
	z.layer = a.ms.layer
	a.ms.zones = append(a.ms.zones, z)
	id := strconv.Itoa(len(a.ms.zones))
	open, close := "\x1b["+id+zoneOpen, "\x1b["+id+zoneClose
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = open + l + close
	}
	return strings.Join(lines, "\n")
}

// clickRow wraps a list row: a click selects it, a double click opens it.
func (a *App) clickRow(line string, sel func() tea.Cmd, open func() tea.Cmd) string {
	return a.zone(line, zone{click: func(double bool) tea.Cmd {
		cmd := sel()
		if double {
			return tea.Batch(cmd, open())
		}
		return cmd
	}})
}

var keyEnter = tea.KeyMsg{Type: tea.KeyEnter}

// finishFrame records zone positions, strips their markers and draws the
// selection.
func (a *App) finishFrame(frame string) string {
	lines := strings.Split(frame, "\n")
	a.ms.segs = a.ms.segs[:0]
	for y, l := range lines {
		lines[y] = a.scanLine(l, y)
	}
	a.ms.frame = append([]string(nil), lines...)
	if s := a.ms.sel; s != nil && s.dragging {
		for y := s.area.y0; y <= s.area.y1 && y < len(lines); y++ {
			if x0, x1, ok := s.span(y); ok {
				lines[y] = highlightCells(lines[y], x0, x1)
			}
		}
	}
	if themePaint != nil {
		for y, l := range lines {
			lines[y] = themePaint.line(l, a.w)
		}
	}
	return strings.Join(lines, "\n")
}

func (a *App) scanLine(l string, y int) string {
	if !strings.Contains(l, "\x1b[") {
		return l
	}
	var out strings.Builder
	open := map[int]int{}
	x := 0
	var state byte
	for rest := l; len(rest) > 0; {
		seq, w, n, ns := ansi.DecodeSequence(rest, state, nil)
		state, rest = ns, rest[n:]
		if w == 0 && strings.HasPrefix(seq, "\x1b[") {
			if id, ok := strings.CutSuffix(seq[2:], zoneOpen); ok {
				if n, err := strconv.Atoi(id); err == nil {
					open[n] = x
					continue
				}
			}
			if id, ok := strings.CutSuffix(seq[2:], zoneClose); ok {
				if n, err := strconv.Atoi(id); err == nil {
					if x0, ok := open[n]; ok && x > x0 {
						a.ms.segs = append(a.ms.segs, zoneSeg{n, y, x0, x - 1})
					}
					delete(open, n)
					continue
				}
			}
		}
		out.WriteString(seq)
		x += w
	}
	for id, x0 := range open {
		if x > x0 {
			a.ms.segs = append(a.ms.segs, zoneSeg{id, y, x0, x - 1})
		}
	}
	return out.String()
}

// zonesAt lists zones under (x, y), innermost (narrowest) first, limited to
// the topmost layer that's showing.
func (a *App) zonesAt(x, y int) []int {
	layer := 0
	if a.overlay != nil {
		layer = 1
	}
	var hits []zoneSeg
	for _, s := range a.ms.segs {
		if s.y == y && x >= s.x0 && x <= s.x1 && s.id <= len(a.ms.zones) && a.ms.zones[s.id-1].layer == layer {
			i := len(hits)
			for i > 0 && hits[i-1].x1-hits[i-1].x0 > s.x1-s.x0 {
				i--
			}
			hits = append(hits[:i], append([]zoneSeg{s}, hits[i:]...)...)
		}
	}
	ids := make([]int, len(hits))
	for i, h := range hits {
		ids[i] = h.id
	}
	return ids
}

func (a *App) mouse(msg tea.MouseMsg) tea.Cmd {
	x, y := msg.X, msg.Y
	switch msg.Button {
	case tea.MouseButtonWheelUp, tea.MouseButtonWheelDown:
		if msg.Action != tea.MouseActionPress {
			return nil
		}
		dir := 1
		if msg.Button == tea.MouseButtonWheelUp {
			dir = -1
		}
		return a.wheel(x, y, dir)
	}

	switch msg.Action {
	case tea.MouseActionPress:
		if msg.Button != tea.MouseButtonLeft {
			return nil
		}
		a.ms.sel = &selection{area: a.paneAt(x, y), ax: x, ay: y, ex: x, ey: y}
	case tea.MouseActionMotion:
		s := a.ms.sel
		if s == nil {
			return nil
		}
		s.ex = max(s.area.x0, min(s.area.x1, x))
		s.ey = max(s.area.y0, min(s.area.y1, y))
		if s.ex != s.ax || s.ey != s.ay {
			s.dragging = true
		}
	case tea.MouseActionRelease:
		s := a.ms.sel
		a.ms.sel = nil
		if s == nil {
			return nil
		}
		if s.dragging {
			text := s.text(a.ms.frame)
			n := strings.Count(text, "\n") + 1
			label := fmt.Sprintf("copied %d characters", len([]rune(text)))
			if n > 1 {
				label = fmt.Sprintf("copied %d lines", n)
			}
			return copyTextAs(text, label)
		}
		return a.click(s.ax, s.ay)
	}
	return nil
}

func (a *App) click(x, y int) tea.Cmd {
	ids := a.zonesAt(x, y)
	if len(ids) == 0 {
		// clicking outside an overlay dismisses it
		if a.overlay != nil {
			return a.overlay.key(a, tea.KeyMsg{Type: tea.KeyEsc})
		}
		return nil
	}
	if a.modal != nil && a.modal.input == nil {
		return nil
	}
	id := ids[0]
	double := id == a.ms.lastClickID && time.Since(a.ms.lastClickAt) < 400*time.Millisecond
	a.ms.lastClickID, a.ms.lastClickAt = id, time.Now()
	if double {
		a.ms.lastClickID = 0 // a third click starts over
	}
	if z := a.ms.zones[id-1]; z.click != nil {
		return z.click(double)
	}
	return nil
}

func (a *App) wheel(x, y, dir int) tea.Cmd {
	for _, id := range a.zonesAt(x, y) {
		if z := a.ms.zones[id-1]; z.scroll != nil {
			return z.scroll(dir)
		}
	}
	if a.modal != nil {
		return nil
	}
	k := tea.KeyMsg{Type: tea.KeyDown}
	if dir < 0 {
		k.Type = tea.KeyUp
	}
	if a.overlay != nil {
		return a.overlay.key(a, k)
	}
	var cmds []tea.Cmd
	for range 3 {
		cmds = append(cmds, a.top().key(a, k))
	}
	return tea.Batch(cmds...)
}

// paneAt finds the bordered pane around (x, y); outside any pane it's the
// whole screen.
func (a *App) paneAt(x, y int) rect {
	full := rect{0, 0, max(0, a.w-1), max(0, a.h-1)}
	grid := make([][]string, len(a.ms.frame))
	for i, l := range a.ms.frame {
		grid[i] = cells(l)
	}
	at := func(x, y int) string {
		if y < 0 || y >= len(grid) || x < 0 || x >= len(grid[y]) {
			return ""
		}
		return grid[y][x]
	}
	if at(x, y) == "│" {
		return full
	}
	for l := x - 1; l >= 0; l-- {
		if at(l, y) != "│" {
			continue
		}
		r := x + 1
		for ; r < a.w && at(r, y) != "│"; r++ {
		}
		if at(r, y) != "│" {
			return full
		}
		top := y - 1
		for top >= 0 && at(l, top) == "│" && at(r, top) == "│" {
			top--
		}
		bot := y + 1
		for bot < len(grid) && at(l, bot) == "│" && at(r, bot) == "│" {
			bot++
		}
		if at(l, top) == "╭" && at(r, top) == "╮" && at(l, bot) == "╰" && at(r, bot) == "╯" && r-l > 4 {
			// inside the one-cell padding pane() puts within the border
			return rect{l + 2, top + 1, r - 2, bot - 1}
		}
		return full
	}
	return full
}

// cells splits a rendered line into screen cells; the trailing cells of a
// wide character are "".
func cells(l string) []string {
	var out []string
	var state byte
	for rest := l; len(rest) > 0; {
		seq, w, n, ns := ansi.DecodeSequence(rest, state, nil)
		state, rest = ns, rest[n:]
		if w == 0 {
			continue
		}
		out = append(out, seq)
		for i := 1; i < w; i++ {
			out = append(out, "")
		}
	}
	return out
}

// span is the selected column range on row y: the selection runs like text
// from the earlier end to the later one, wrapping at the pane's edges.
func (s *selection) span(y int) (x0, x1 int, ok bool) {
	sx, sy, ex, ey := s.ax, s.ay, s.ex, s.ey
	if ey < sy || (ey == sy && ex < sx) {
		sx, sy, ex, ey = ex, ey, sx, sy
	}
	if y < sy || y > ey {
		return 0, 0, false
	}
	x0, x1 = s.area.x0, s.area.x1
	if y == sy {
		x0 = sx
	}
	if y == ey {
		x1 = ex
	}
	return x0, x1, x0 <= x1
}

func (s *selection) text(frame []string) string {
	var out []string
	for y := s.area.y0; y <= s.area.y1 && y < len(frame); y++ {
		x0, x1, ok := s.span(y)
		if !ok {
			continue
		}
		c := cells(frame[y])
		var b strings.Builder
		for x := x0; x <= x1 && x < len(c); x++ {
			b.WriteString(c[x])
		}
		out = append(out, strings.TrimRight(b.String(), " "))
	}
	return strings.Join(out, "\n")
}

// highlightCells shows columns x0..x1 of a rendered line in reverse video.
func highlightCells(l string, x0, x1 int) string {
	w := ansi.StringWidth(l)
	if w <= x1 {
		l += strings.Repeat(" ", x1+1-w)
	}
	plain := ansi.Strip(ansi.Cut(l, x0, x1+1))
	return ansi.Truncate(l, x0, "") + "\x1b[0;7m" + plain + "\x1b[0m" + ansi.TruncateLeft(l, x1+1, "")
}
