package ui

import (
	"strings"
	"testing"
	"time"
)

// plainGraph draws a layout as text: job names on their rows, lines
// unstyled.
func plainGraph(g *graphLayout, names []string) string {
	grid := g.grid(func(int) uint8 { return 0 }, func(j int) int { return len(names[j]) })
	rows := make([][]rune, g.h)
	for y := range rows {
		rows[y] = make([]rune, g.w)
		for x := range rows[y] {
			rows[y][x], _ = grid[y][x].glyph()
		}
	}
	for j, name := range names {
		s := g.slots[j]
		copy(rows[s.pos][g.colX[s.col]:], []rune(name))
		copy(rows[s.pos+1][g.colX[s.col]:], []rune(strings.Repeat("·", len(name))))
	}
	var b strings.Builder
	for _, r := range rows {
		b.WriteString(strings.TrimRight(string(r), " ") + "\n")
	}
	return b.String()
}

func layoutNames(names []string, needs map[string][]string) *graphLayout {
	idx := map[string]int{}
	for i, n := range names {
		idx[n] = i
	}
	deps := make([][]int, len(names))
	for i, n := range names {
		for _, d := range needs[n] {
			deps[i] = append(deps[i], idx[d])
		}
	}
	return layoutGraph(len(names), deps, func(i int) int { return len(names[i]) })
}

// checkGraph checks that lines keep off jobs' text, that slots in a column
// don't overlap, and that every line leaves from a job's name row.
func checkGraph(t *testing.T, g *graphLayout, names []string) {
	t.Helper()
	grid := g.grid(func(int) uint8 { return 0 }, func(j int) int { return len(names[j]) })
	for j, name := range names {
		s := g.slots[j]
		for x := g.colX[s.col]; x < g.colX[s.col]+len(name); x++ {
			for _, y := range []int{s.pos, s.pos + 1} {
				if len(grid[y][x]) != 0 {
					t.Errorf("a line runs over %s at %d,%d", name, x, y)
				}
			}
		}
		// the details row is clear all the way across the column
		for x := g.colX[s.col]; x < g.chX[s.col]; x++ {
			if len(grid[s.pos+1][x]) != 0 {
				t.Errorf("a line leaves %s's details row at %d", name, x)
			}
		}
	}
	for c, col := range g.cols {
		for i := 1; i < len(col); i++ {
			a, b := &g.slots[col[i-1]], &g.slots[col[i]]
			if b.pos < a.pos+a.height() {
				t.Errorf("column %d: slots %d and %d overlap", c, col[i-1], col[i])
			}
		}
	}
}

func TestGraphLayout(t *testing.T) {
	names := []string{"init", "setup", "base: [a]", "base: [b]", "base: [c]", "build", "scan", "trivy: [a]", "trivy: [b]"}
	g := layoutNames(names, map[string][]string{
		"setup":      {"init"},
		"base: [a]":  {"setup"},
		"base: [b]":  {"setup"},
		"base: [c]":  {"setup"},
		"build":      {"base: [a]", "setup"},
		"scan":       {"build", "setup"},
		"trivy: [a]": {"base: [a]", "base: [b]", "base: [c]", "setup"},
		"trivy: [b]": {"base: [a]", "base: [b]", "base: [c]", "setup"},
	})
	t.Log("\n" + plainGraph(g, names))
	checkGraph(t, g, names)

	col := func(n string) int {
		for i, x := range names {
			if x == n {
				return g.slots[i].col
			}
		}
		return -1
	}
	for n, want := range map[string]int{"init": 0, "setup": 1, "base: [b]": 2, "build": 3, "scan": 4, "trivy: [a]": 3} {
		if got := col(n); got != want {
			t.Errorf("%s in column %d, want %d", n, got, want)
		}
	}
	if len(g.edges) != 16 {
		t.Errorf("%d edges, want 16", len(g.edges))
	}
	// setup's line skips columns once per column, however many jobs it
	// goes on to
	if n := len(g.slots) - len(names); n != 2 {
		t.Errorf("%d carriers, want 2 (setup through columns 2 and 3)", n)
	}
}

func TestGraphFork(t *testing.T) {
	names := []string{"a", "b", "c", "d", "e", "f"}
	g := layoutNames(names, map[string][]string{
		"b": {"a"}, "c": {"b"}, "d": {"c", "a"}, "e": {"a", "b"}, "f": {"a", "c", "e"},
	})
	t.Log("\n" + plainGraph(g, names))
	checkGraph(t, g, names)
	// one net per slot with lines out
	seen := map[int]bool{}
	for _, n := range g.nets {
		if seen[n.slot] {
			t.Errorf("slot %d has two lines", n.slot)
		}
		seen[n.slot] = true
	}
}

// A big stage-based pipeline (every job needs the whole stage before it)
// lays out quickly.
func TestGraphLarge(t *testing.T) {
	const stages, per = 10, 15
	n := stages * per
	needs := make([][]int, n)
	for i := per; i < n; i++ {
		for d := (i/per - 1) * per; d < (i/per)*per; d++ {
			needs[i] = append(needs[i], d)
		}
	}
	start := time.Now()
	g := layoutGraph(n, needs, func(int) int { return 20 })
	if d := time.Since(start); d > time.Second {
		t.Errorf("layout took %v", d)
	}
	if len(g.edges) != (stages-1)*per*per {
		t.Errorf("%d edges", len(g.edges))
	}
}

// No line leaves a slot on a row where another line arrives from its left:
// the two would run together.
func TestGraphNoSharedRows(t *testing.T) {
	names := []string{"init", "setup", "base: [a]", "base: [b]", "base: [c]", "build", "trivy: [a]", "trivy: [b]"}
	g := layoutNames(names, map[string][]string{
		"setup":      {"init"},
		"base: [a]":  {"setup"},
		"base: [b]":  {"setup"},
		"base: [c]":  {"setup"},
		"build":      {"base: [a]", "setup"},
		"trivy: [a]": {"base: [a]", "base: [b]", "base: [c]", "setup"},
		"trivy: [b]": {"base: [a]", "base: [b]", "base: [c]", "setup"},
	})
	t.Log("\n" + plainGraph(g, names))
	for i := range g.nets {
		a := &g.nets[i]
		for j := range g.nets {
			b := &g.nets[j]
			if i != j && a.ch == b.ch && !g.straight(a) && !g.straight(b) && b.lane < a.lane && g.arrives(b, a.y0) {
				t.Errorf("slot %d leaves on row %d across slot %d's line arriving there", a.slot, a.y0, b.slot)
			}
		}
	}
}

// A selected job's own lines are lit brighter than the ones further up-
// and downstream.
func TestGraphHighlightLevels(t *testing.T) {
	names := []string{"a", "b", "c", "d"}
	g := layoutNames(names, map[string][]string{"b": {"a"}, "c": {"b"}, "d": {"c"}})
	// select c: b→c and c→d are direct, a→b indirect
	levels := map[[2]int]uint8{{0, 1}: 1, {1, 2}: 2, {2, 3}: 2}
	grid := g.grid(func(e int) uint8 { ed := g.edges[e]; return levels[[2]int{ed.from, ed.to}] },
		func(j int) int { return len(names[j]) })
	// a chain runs straight along one row: check between each pair of jobs
	for pair, want := range levels {
		from, to := g.slots[pair[0]], g.slots[pair[1]]
		for x := g.colX[from.col] + len(names[pair[0]]) + 1; x < g.colX[to.col]; x++ {
			if _, got := grid[from.pos][x].glyph(); got != want {
				t.Errorf("%s→%s at x=%d: level %d, want %d", names[pair[0]], names[pair[1]], x, got, want)
			}
		}
	}
}
