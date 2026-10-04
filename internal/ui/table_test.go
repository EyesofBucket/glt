package ui

import (
	"testing"

	"github.com/charmbracelet/x/ansi"

	"gitlab-tui/internal/gitlab"
)

func TestTableGaps(t *testing.T) {
	rows := [][]string{
		{sOK.Render("ok"), "!1", "short title", "3d"},
		{"x", "!1234", "a much longer title here", "11mo"},
	}
	tb := newTable([]col{{title: "S"}, {title: "MR"}, {title: "TITLE", flex: true}, {title: "AGE", right: true}}, rows, 80)
	if got := ansi.Strip(tb.row(rows[0]...)); got != "ok  !1     short title                 3d" {
		t.Errorf("row 0 = %q", got)
	}
	if got := ansi.Strip(tb.header()); got != "S   MR     TITLE                      AGE" {
		t.Errorf("header = %q", got)
	}

	// too narrow: the flex column shrinks, then optional columns go
	cols := []col{{title: "A"}, {title: "TITLE", flex: true}, {title: "OPT", optional: true}, {title: "END"}}
	rows = [][]string{{"aaaa", "a title that is long enough to shrink", "optional", "end"}}
	tb = newTable(cols, rows, 30)
	if w := tb.width(); w > 30 {
		t.Errorf("width %d > 30", w)
	}
	if tb.shown(2) {
		t.Error("optional column should be dropped")
	}
	if got := ansi.Strip(tb.row(rows[0]...)); got != "aaaa  a title that is lo…  end" {
		t.Errorf("narrow row = %q", got)
	}
}

func TestListValues(t *testing.T) {
	var us []gitlab.User
	for _, n := range []string{"a", "b", "c", "d", "e", "f", "g"} {
		us = append(us, gitlab.User{Username: n})
	}
	got := peopleLines(us)
	if len(got) != 5 || ansi.Strip(got[4]) != "+3 more" || got[0] != "@a" {
		t.Errorf("peopleLines = %q", got)
	}
	if got := peopleLines(us[:2]); len(got) != 2 {
		t.Errorf("two people = %q", got)
	}
	lines := wrapChips([]string{"aaaa", "bbbb", "cccc", "dddd"}, 10)
	if len(lines) != 2 || lines[0] != "aaaa  bbbb" {
		t.Errorf("wrapChips = %q", lines)
	}
}
