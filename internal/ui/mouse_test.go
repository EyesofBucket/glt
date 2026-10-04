package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"gitlab-tui/internal/config"
)

func testApp(w, h int) *App { return &App{w: w, h: h} }

func TestZonesAndClick(t *testing.T) {
	a := testApp(40, 3)
	clicked := ""
	row := func(name string) string {
		return a.clickRow(sOK.Render(name), func() tea.Cmd { clicked = name; return nil },
			func() tea.Cmd { clicked = name + "!"; return nil })
	}
	frame := "header\n  " + row("alpha") + "   " + row("beta") + "\nfooter"
	out := a.finishFrame(frame)
	if strings.Contains(out, zoneOpen) || strings.Contains(out, zoneClose) {
		t.Fatalf("markers left in output: %q", out)
	}
	if got := ansi.Strip(strings.Split(out, "\n")[1]); got != "  alpha   beta" {
		t.Fatalf("row = %q", got)
	}
	a.click(3, 1)
	if clicked != "alpha" {
		t.Fatalf("clicked %q, want alpha", clicked)
	}
	a.click(10, 1)
	if clicked != "beta" {
		t.Fatalf("clicked %q, want beta", clicked)
	}
	a.click(10, 1)
	if clicked != "beta!" {
		t.Fatalf("double click gave %q", clicked)
	}
	clicked = ""
	a.click(8, 1) // gap between zones
	if clicked != "" {
		t.Fatalf("gap click hit %q", clicked)
	}
}

func TestSelectionWrapsWithinPane(t *testing.T) {
	left := pane("L", []string{"one two", "three"}, 16, 4, true)
	right := pane("R", []string{"right side", "more"}, 16, 4, false)
	a := testApp(32, 4)
	a.finishFrame(joinHoriz(left, right))

	area := a.paneAt(5, 1)
	if area != (rect{2, 1, 13, 2}) {
		t.Fatalf("left pane area = %+v", area)
	}
	if r := a.paneAt(20, 2); r != (rect{18, 1, 29, 2}) {
		t.Fatalf("right pane area = %+v", r)
	}
	// drag from "two" on row 1 down to the end of "three" on row 2
	s := &selection{area: area, ax: 6, ay: 1, ex: 6, ey: 2, dragging: true}
	if got := s.text(a.ms.frame); got != "two\nthree" {
		t.Fatalf("selection = %q", got)
	}
	// dragging backwards selects the same text
	s = &selection{area: area, ax: 6, ay: 2, ex: 6, ey: 1, dragging: true}
	if got := s.text(a.ms.frame); got != "two\nthree" {
		t.Fatalf("reverse selection = %q", got)
	}
}

func TestMouseDragCopiesAndClears(t *testing.T) {
	a := testApp(20, 2)
	a.finishFrame("hello world\nsecond line")
	a.mouse(tea.MouseMsg{X: 6, Y: 0, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	a.mouse(tea.MouseMsg{X: 5, Y: 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion})
	if a.ms.sel == nil || !a.ms.sel.dragging {
		t.Fatal("expected an active selection")
	}
	hl := a.finishFrame("hello world\nsecond line")
	for i, l := range strings.Split(hl, "\n") {
		if w := ansi.StringWidth(l); w < 11 {
			t.Errorf("highlighted line %d width %d", i, w)
		}
	}
	if got := a.ms.sel.text(a.ms.frame); got != "world\nsecond" {
		t.Fatalf("selection = %q", got)
	}
	if cmd := a.mouse(tea.MouseMsg{X: 5, Y: 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease}); cmd == nil {
		t.Fatal("release should copy")
	}
	if a.ms.sel != nil {
		t.Fatal("selection should clear on release")
	}
}

func TestHighlightCellsKeepsWidth(t *testing.T) {
	l := sOK.Render("abc") + "def" + sErr.Render("ghi")
	out := highlightCells(l, 2, 5)
	if ansi.Strip(out) != "abcdefghi" || ansi.StringWidth(out) != 9 {
		t.Fatalf("got %q", out)
	}
}

func TestQuitFromHome(t *testing.T) {
	a := &App{ctx: &config.Context{Project: "g/p"}}
	a.stack = []view{newDashboard()}
	if !a.atHome() {
		t.Error("dashboard should be home")
	}
	a.stack = append(a.stack, newProjectView("g/p"))
	if !a.atHome() {
		t.Error("the cwd project's page on the dashboard should be home")
	}
	a.stack[1] = newProjectView("other/p")
	if a.atHome() {
		t.Error("another project's page isn't home")
	}
	a.stack = []view{newDashboard(), newProjectView("g/p"), newMRList("g/p")}
	if a.atHome() {
		t.Error("the MR list isn't home")
	}
}
