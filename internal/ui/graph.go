package ui

import (
	"math"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Job dependency graph layout.
//
// Jobs go in columns by how deep their needs run, as in GitLab's "job
// dependencies" view. Lines never run through a job: each job has one
// line, leaving from its name, which forks off to every job that needs
// it. Where it has to skip columns, it passes through them in a slot of
// its own (a "carrier"), shared by everything it's heading to.
//
// Between two columns, each forking line has its own vertical lane. Lines
// meet only where a line forks, or where lines join on their way into a
// job that needs several. Anywhere else that a horizontal line meets a
// vertical one they cross without connecting, and the crossing is drawn
// with the vertical line unbroken.

// line directions, combined into a cell's mask
const (
	dUp uint8 = 1 << iota
	dDown
	dLeft
	dRight
)

type gslot struct {
	job   int // index of the job, or -1 for a carrier
	src   int // the job whose line it carries
	col   int
	pos   int // order in the column while ordering, then its top row
	preds []int
	succs []int
}

// height is how many rows a slot takes: a job's name and details, or a
// carrier's line.
func (s *gslot) height() int {
	if s.job < 0 {
		return 1
	}
	return 2
}

type gedge struct{ from, to int } // jobs: to needs from

// gnet is a slot's line through the channel right of its column.
type gnet struct {
	slot  int
	ch    int
	y0    int   // the row it leaves on
	dests []int // slots it reaches
	lane  int   // x of its vertical, or -1 when it runs straight
}

type graphLayout struct {
	slots   []gslot // jobs first, at their own index
	edges   []gedge
	carrier map[[2]int]int // (job, column): the slot carrying its line
	cols    [][]int        // slots per column, top to bottom
	nets    []gnet
	colX    []int
	colW    []int
	chX     []int // channel right of each column
	chW     []int
	w, h    int
}

// layoutGraph lays out n jobs; needs[i] lists the jobs job i needs, and
// width(i) is how wide job i's text is. Jobs start in their given order
// within a column, so pass them sorted.
func layoutGraph(n int, needs [][]int, width func(i int) int) *graphLayout {
	g := &graphLayout{carrier: map[[2]int]int{}}
	if n == 0 {
		return g
	}

	// columns: one past the deepest need
	layer := make([]int, n)
	state := make([]int8, n) // 0 new, 1 visiting, 2 done
	var depth func(i int) int
	depth = func(i int) int {
		if state[i] == 2 {
			return layer[i]
		}
		if state[i] == 1 {
			return 0 // a cycle; GitLab doesn't allow them
		}
		state[i] = 1
		l := 0
		for _, d := range needs[i] {
			l = max(l, depth(d)+1)
		}
		layer[i], state[i] = l, 2
		return l
	}
	ncols := 0
	for i := 0; i < n; i++ {
		ncols = max(ncols, depth(i)+1)
	}

	for i := 0; i < n; i++ {
		g.slots = append(g.slots, gslot{job: i, src: i, col: layer[i]})
	}
	for to := 0; to < n; to++ {
		seen := map[int]bool{}
		for _, from := range needs[to] {
			if seen[from] || layer[from] >= layer[to] {
				continue
			}
			seen[from] = true
			g.edges = append(g.edges, gedge{from, to})
		}
	}
	// carriers, and the links between slots in neighbouring columns
	linked := map[[2]int]bool{}
	link := func(a, b int) {
		if !linked[[2]int{a, b}] {
			linked[[2]int{a, b}] = true
			g.slots[a].succs = append(g.slots[a].succs, b)
			g.slots[b].preds = append(g.slots[b].preds, a)
		}
	}
	for _, e := range g.edges {
		prev := e.from
		for c := layer[e.from] + 1; c < layer[e.to]; c++ {
			k := [2]int{e.from, c}
			s, ok := g.carrier[k]
			if !ok {
				s = len(g.slots)
				g.slots = append(g.slots, gslot{job: -1, src: e.from, col: c})
				g.carrier[k] = s
			}
			link(prev, s)
			prev = s
		}
		link(prev, e.to)
	}

	g.cols = make([][]int, ncols)
	for s := range g.slots {
		c := g.slots[s].col
		g.cols[c] = append(g.cols[c], s)
	}
	g.order()
	g.align()

	// geometry
	g.colX, g.colW = make([]int, ncols), make([]int, ncols)
	g.chX, g.chW = make([]int, ncols), make([]int, ncols)
	for c, col := range g.cols {
		w := 8
		for _, s := range col {
			if j := g.slots[s].job; j >= 0 {
				w = max(w, width(j)+1)
			}
		}
		g.colW[c] = w
	}
	// route; where two lines can't help sharing a row, move the job one
	// arrives at down and try again
	var lanes []int
	for try := 0; ; try++ {
		g.nets = g.nets[:0]
		for s := range g.slots {
			sl := &g.slots[s]
			if len(sl.succs) > 0 {
				g.nets = append(g.nets, gnet{slot: s, ch: sl.col, y0: sl.pos, dests: sl.succs, lane: -1})
			}
		}
		var clash int
		lanes, clash = g.assignLanes(ncols)
		if clash < 0 || try == 50 {
			break
		}
		g.nudge(clash)
	}
	for _, col := range g.cols {
		for _, s := range col {
			g.h = max(g.h, g.slots[s].pos+g.slots[s].height())
		}
	}
	x := 0
	for c := 0; c < ncols; c++ {
		g.colX[c] = x
		x += g.colW[c]
		g.chX[c] = x
		if c < ncols-1 {
			g.chW[c] = max(4, lanes[c]+2)
			x += g.chW[c]
		}
	}
	g.w = x
	for i := range g.nets {
		if g.nets[i].lane >= 0 {
			g.nets[i].lane += g.chX[g.nets[i].ch] + 1
		}
	}
	return g
}

// order arranges each column to cut down on crossing lines: a few sweeps
// of the barycenter heuristic, keeping the best arrangement seen.
func (g *graphLayout) order() {
	g.place()
	for c := 1; c < len(g.cols); c++ {
		g.sortCol(c, true)
	}
	best := g.crossings()
	saved := g.snapshot()
	for it := 0; it < 8 && best > 0; it++ {
		if it%2 == 0 {
			for c := 1; c < len(g.cols); c++ {
				g.sortCol(c, true)
			}
		} else {
			for c := len(g.cols) - 2; c >= 0; c-- {
				g.sortCol(c, false)
			}
		}
		if n := g.crossings(); n < best {
			best, saved = n, g.snapshot()
		}
	}
	g.cols = saved
	g.place()
}

func (g *graphLayout) place() {
	for _, col := range g.cols {
		for p, s := range col {
			g.slots[s].pos = p
		}
	}
}

func (g *graphLayout) snapshot() [][]int {
	out := make([][]int, len(g.cols))
	for c, col := range g.cols {
		out[c] = append([]int(nil), col...)
	}
	return out
}

// sortCol orders column c by the mean position of each slot's neighbours
// in the column before it (byPreds) or after it.
func (g *graphLayout) sortCol(c int, byPreds bool) {
	col := g.cols[c]
	key := make(map[int]float64, len(col))
	for _, s := range col {
		nb := g.slots[s].succs
		if byPreds {
			nb = g.slots[s].preds
		}
		if len(nb) == 0 {
			key[s] = float64(g.slots[s].pos)
			continue
		}
		sum := 0
		for _, o := range nb {
			sum += g.slots[o].pos
		}
		key[s] = float64(sum) / float64(len(nb))
		if g.slots[s].job < 0 {
			key[s] += 0.25 // carriers go below jobs they tie with
		}
	}
	sort.SliceStable(col, func(i, j int) bool { return key[col[i]] < key[col[j]] })
	for p, s := range col {
		g.slots[s].pos = p
	}
}

// crossings counts pairs of links between neighbouring columns that cross.
func (g *graphLayout) crossings() int {
	n := 0
	for c := 0; c+1 < len(g.cols); c++ {
		type link struct{ a, b int }
		var ls []link
		for _, s := range g.cols[c] {
			for _, t := range g.slots[s].succs {
				ls = append(ls, link{g.slots[s].pos, g.slots[t].pos})
			}
		}
		for i := range ls {
			for j := i + 1; j < len(ls); j++ {
				if (ls[i].a-ls[j].a)*(ls[i].b-ls[j].b) < 0 {
					n++
				}
			}
		}
	}
	return n
}

// align gives slots rows, keeping each column's order but leaving gaps
// where that lets a slot sit level with what it connects to, so lines run
// straight. Each sweep fits a column as closely as it can to its
// neighbours' rows; carriers pull hardest, so a line skipping columns runs
// level with where it comes from. The last sweep runs downstream.
func (g *graphLayout) align() {
	for _, col := range g.cols {
		y := 0
		for _, s := range col {
			g.slots[s].pos = y
			y += g.slots[s].height()
		}
	}
	for it := 0; it < 7; it++ {
		if it%2 == 0 {
			for c := 1; c < len(g.cols); c++ {
				g.alignCol(c, true)
			}
		} else {
			for c := len(g.cols) - 2; c >= 0; c-- {
				g.alignCol(c, false)
			}
		}
	}
	top := math.MaxInt
	for s := range g.slots {
		top = min(top, g.slots[s].pos)
	}
	for s := range g.slots {
		g.slots[s].pos -= top
	}
}

// alignCol places column c's slots as near as their order allows to the
// mean row of their neighbours before (byPreds) or after them: a weighted
// isotonic regression, with the slots above each one taken off its row so
// that "in order" means "not overlapping".
func (g *graphLayout) alignCol(c int, byPreds bool) {
	col := g.cols[c]
	want := make([]float64, len(col))
	weight := make([]float64, len(col))
	off := 0
	for i, s := range col {
		sl := &g.slots[s]
		nb := sl.succs
		if byPreds {
			nb = sl.preds
		}
		y := float64(sl.pos)
		if len(nb) > 0 {
			sum := 0
			for _, o := range nb {
				sum += g.slots[o].pos
			}
			y = float64(sum) / float64(len(nb))
		}
		want[i] = y - float64(off)
		weight[i] = 1
		if sl.job < 0 {
			weight[i] = 4
		}
		off += sl.height()
	}
	fit := isotonic(want, weight)
	off = 0
	for i, s := range col {
		g.slots[s].pos = int(math.Round(fit[i])) + off
		off += g.slots[s].height()
	}
}

// isotonic is the non-decreasing sequence closest to want (weighted least
// squares), by pooling adjacent violators.
func isotonic(want, weight []float64) []float64 {
	type block struct {
		v, w float64
		n    int
	}
	var bs []block
	for i := range want {
		bs = append(bs, block{want[i], weight[i], 1})
		for len(bs) > 1 && bs[len(bs)-2].v > bs[len(bs)-1].v {
			a, b := bs[len(bs)-2], bs[len(bs)-1]
			bs = append(bs[:len(bs)-2], block{(a.v*a.w + b.v*b.w) / (a.w + b.w), a.w + b.w, a.n + b.n})
		}
	}
	out := make([]float64, 0, len(want))
	for _, b := range bs {
		for k := 0; k < b.n; k++ {
			out = append(out, b.v)
		}
	}
	return out
}

// span is the rows a net's vertical covers.
func (g *graphLayout) span(n *gnet) (lo, hi int) {
	lo, hi = n.y0, n.y0
	for _, d := range n.dests {
		lo, hi = min(lo, g.slots[d].pos), max(hi, g.slots[d].pos)
	}
	return lo, hi
}

func (g *graphLayout) straight(n *gnet) bool {
	return len(n.dests) == 1 && g.slots[n.dests[0]].pos == n.y0
}

// arrives reports whether net n reaches a slot on row y.
func (g *graphLayout) arrives(n *gnet, y int) bool {
	for _, d := range n.dests {
		if g.slots[d].pos == y {
			return true
		}
	}
	return false
}

// nudge moves slot s down a row, and the slots below it as far as they
// need to go to make room.
func (g *graphLayout) nudge(s int) {
	sl := &g.slots[s]
	sl.pos++
	col := g.cols[sl.col]
	for i := 1; i < len(col); i++ {
		a, b := &g.slots[col[i-1]], &g.slots[col[i]]
		b.pos = max(b.pos, a.pos+a.height())
	}
}

// assignLanes gives each net that doesn't run straight a lane in its
// channel and returns how many lanes each channel needs.
//
// A net leaving on a row where another arrives must sit left of it, or
// the two would share that row's line; past that, lanes are ordered to
// cross as little as they can. When two nets each leave where the other
// arrives, no order works: clash is then a slot to move off the row, or
// -1 when there's none.
func (g *graphLayout) assignLanes(ncols int) (lanes []int, clash int) {
	lanes, clash = make([]int, ncols), -1
	for c := 0; c < ncols-1; c++ {
		var idx []int
		for i := range g.nets {
			if n := &g.nets[i]; n.ch == c && !g.straight(n) {
				idx = append(idx, i)
			}
		}
		sort.SliceStable(idx, func(a, b int) bool { return g.nets[idx[a]].y0 < g.nets[idx[b]].y0 })
		before := func(a, b int) bool { // a must be left of b
			return a != b && g.arrives(&g.nets[b], g.nets[a].y0)
		}
		// take the topmost net that nothing left has to precede, or if
		// they all have something, the one with the least
		var order []int
		left := append([]int(nil), idx...)
		for len(left) > 0 {
			pick, fewest := 0, -1
			for i, a := range left {
				n := 0
				for _, b := range left {
					if before(b, a) {
						n++
					}
				}
				if fewest < 0 || n < fewest {
					pick, fewest = i, n
				}
			}
			order = append(order, left[pick])
			left = append(left[:pick], left[pick+1:]...)
		}
		for l, i := range order {
			g.nets[i].lane = l
		}
		g.untangle(order, before)
		lanes[c] = len(order)
		for l, a := range order {
			for _, b := range order[:l] {
				if clash < 0 && before(a, b) {
					// b arrives where a leaves, from the wrong side
					for _, d := range g.nets[b].dests {
						if g.slots[d].pos == g.nets[a].y0 {
							clash = d
						}
					}
				}
			}
		}
	}
	return lanes, clash
}

// untangle swaps neighbouring lanes while that leaves fewer crossings.
func (g *graphLayout) untangle(order []int, before func(a, b int) bool) {
	if len(order) > 60 {
		return // too slow to be worth it, and too dense to help much
	}
	swap := func(l int) {
		a, b := order[l], order[l+1]
		g.nets[a].lane, g.nets[b].lane = l+1, l
		order[l], order[l+1] = b, a
	}
	best := g.laneCrossings(order)
	for pass := 0; pass < 20 && best > 0; pass++ {
		improved := false
		for l := 0; l+1 < len(order); l++ {
			if before(order[l], order[l+1]) {
				continue
			}
			swap(l)
			if c := g.laneCrossings(order); c < best {
				best, improved = c, true
			} else {
				swap(l)
			}
		}
		if !improved {
			return
		}
	}
}

// laneCrossings counts where a net's horizontal runs over another's
// vertical within one channel.
func (g *graphLayout) laneCrossings(order []int) int {
	n := 0
	for _, i := range order {
		a := &g.nets[i]
		for _, j := range order {
			b := &g.nets[j]
			if i == j {
				continue
			}
			lo, hi := g.span(b)
			over := func(y int) bool { return y > lo && y < hi && y != b.y0 && !g.arrives(b, y) }
			if b.lane < a.lane && over(a.y0) {
				n++
			}
			if b.lane > a.lane {
				for _, d := range a.dests {
					if over(g.slots[d].pos) {
						n++
					}
				}
			}
		}
	}
	return n
}

// gpart is one line's share of a cell.
type gpart struct {
	mask uint8
	lvl  [4]uint8 // highlight level of each direction, by bit index
}

type gcell []gpart

// level reports how a cell's directions in bits are highlighted: the
// highest level among them.
func (c gcell) level(bits uint8) uint8 {
	var l uint8
	for _, p := range c {
		for i := range 4 {
			if bits&p.mask&(1<<i) != 0 {
				l = max(l, p.lvl[i])
			}
		}
	}
	return l
}

// grid draws the lines. hl gives each edge's highlight level (0 for none),
// and label(j) is how wide job j's name is, so its line can start after it.
func (g *graphLayout) grid(hl func(e int) uint8, label func(j int) int) [][]gcell {
	grid := make([][]gcell, g.h)
	for y := range grid {
		grid[y] = make([]gcell, g.w)
	}
	put := func(x, y int, mask uint8, lvl [4]uint8) {
		if y >= 0 && y < g.h && x >= 0 && x < g.w && mask != 0 {
			grid[y][x] = append(grid[y][x], gpart{mask, lvl})
		}
	}
	horiz := func(x0, x1, y int, l uint8) {
		var lvl [4]uint8
		lvl[2], lvl[3] = l, l // dLeft, dRight
		for x := x0; x <= x1; x++ {
			put(x, y, dLeft|dRight, lvl)
		}
	}

	// how highlighted each link is: a link from a slot to the next takes
	// the highest level of the edges running along it
	lit := map[[2]int]uint8{}
	mark := func(a, b int, l uint8) { lit[[2]int{a, b}] = max(lit[[2]int{a, b}], l) }
	for e, ed := range g.edges {
		l := hl(e)
		if l == 0 {
			continue
		}
		prev := ed.from
		for c := g.slots[ed.from].col + 1; c < g.slots[ed.to].col; c++ {
			s := g.carrier[[2]int{ed.from, c}]
			mark(prev, s, l)
			prev = s
		}
		mark(prev, ed.to, l)
	}

	for i := range g.nets {
		n := &g.nets[i]
		s := &g.slots[n.slot]
		var out uint8 // the line out takes its brightest branch's level
		for _, d := range n.dests {
			out = max(out, lit[[2]int{n.slot, d}])
		}
		// out of its slot: after the job's name, or right through a carrier
		x0 := g.colX[s.col]
		if s.job >= 0 {
			x0 += label(s.job) + 1
		}
		horiz(x0, g.chX[s.col]-1, n.y0, out)
		end := g.chX[n.ch] + g.chW[n.ch] - 1
		if g.straight(n) {
			horiz(g.chX[n.ch], end, n.y0, out)
			continue
		}
		horiz(g.chX[n.ch], n.lane-1, n.y0, out)
		// the vertical: each stretch takes the level of the branches it
		// leads to
		lo, hi := g.span(n)
		up := make([]uint8, hi-lo+1)   // level of the stretch above row lo+i
		down := make([]uint8, hi-lo+1) // and below it
		dest := map[int]uint8{}        // rows it reaches: their level
		for _, d := range n.dests {
			y, l := g.slots[d].pos, lit[[2]int{n.slot, d}]
			if _, ok := dest[y]; !ok || l > dest[y] {
				dest[y] = l
			}
			for r := min(n.y0, y); r < max(n.y0, y); r++ {
				down[r-lo] = max(down[r-lo], l)
				up[r+1-lo] = max(up[r+1-lo], l)
			}
		}
		for y := lo; y <= hi; y++ {
			var m uint8
			var lvl [4]uint8
			if y > lo {
				m |= dUp
				lvl[0] = up[y-lo]
			}
			if y < hi {
				m |= dDown
				lvl[1] = down[y-lo]
			}
			if y == n.y0 {
				m |= dLeft
				lvl[2] = out
			}
			if l, ok := dest[y]; ok {
				m |= dRight
				lvl[3] = l
			}
			put(n.lane, y, m, lvl)
		}
		for y, l := range dest {
			horiz(n.lane+1, end, y, l)
		}
	}
	return grid
}

var boxGlyphs = map[uint8]rune{
	dUp | dDown:                  '│',
	dLeft | dRight:               '─',
	dDown | dRight:               '╭',
	dDown | dLeft:                '╮',
	dUp | dRight:                 '╰',
	dUp | dLeft:                  '╯',
	dUp | dDown | dRight:         '├',
	dUp | dDown | dLeft:          '┤',
	dLeft | dRight | dDown:       '┬',
	dLeft | dRight | dUp:         '┴',
	dUp:                          '│',
	dDown:                        '│',
	dLeft:                        '─',
	dRight:                       '─',
	dUp | dDown | dLeft | dRight: '┼',
}

// glyph is the character for a cell and its highlight level. Where lines
// only pass straight through each other they cross without joining: the
// vertical one is drawn over the horizontal one, unless the horizontal
// one is highlighted more. Anywhere a line turns or forks, they join.
func (c gcell) glyph() (rune, uint8) {
	const h, v = dLeft | dRight, dUp | dDown
	var mask uint8
	turns := false
	for _, p := range c {
		mask |= p.mask
		if p.mask&h != 0 && p.mask&v != 0 {
			turns = true
		}
	}
	if mask == h|v && !turns {
		if lh, lv := c.level(h), c.level(v); lh > lv {
			return '─', lh
		} else {
			return '│', lv
		}
	}
	if r, ok := boxGlyphs[mask]; ok {
		return r, c.level(mask)
	}
	return ' ', 0
}

// lineStyles colour lines by highlight level: plain, indirectly related to
// the selected job, directly related.
var lineStyles = []*lipgloss.Style{&sBorder, &sIndirect, &sDirect}

// lineString renders cells [x0, x1) of a grid row.
func lineString(row []gcell, x0, x1 int) string {
	var b strings.Builder
	var run []rune
	var runLvl uint8
	flush := func() {
		if len(run) == 0 {
			return
		}
		if strings.TrimSpace(string(run)) == "" {
			b.WriteString(string(run))
		} else {
			b.WriteString(lineStyles[runLvl].Render(string(run)))
		}
		run = run[:0]
	}
	for x := x0; x < x1 && x < len(row); x++ {
		r, l := row[x].glyph()
		if l != runLvl {
			flush()
			runLvl = l
		}
		run = append(run, r)
	}
	flush()
	return b.String()
}
