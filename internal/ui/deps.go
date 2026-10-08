package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"gitlab-tui/internal/gitlab"
)

// depsPopup floats a pipeline's job dependency graph over the page (see
// graph.go for the layout). The highlighted job's lines, and those of
// everything up- and downstream of it, are lit.

type depsPopup struct {
	project string
	pl      gitlab.Pipeline
	sel     string // the highlighted job's name, which survives reloads
	ox, oy  int    // scroll
	follow  bool   // scroll the highlighted job into view
	vw, vh  int    // graph area at last render

	// the layout, kept while the jobs and their needs stay the same
	sig  string
	g    *graphLayout
	jobs []gitlab.Job
}

func needsKey(p string, id int) string { return fmt.Sprintf("needs:%s:%d", p, id) }

// openDeps opens the popup on job sel, or (sel == "") the first failure.
func (a *App) openDeps(p string, pl *gitlab.Pipeline, sel string) tea.Cmd {
	if pl == nil {
		a.setFlash("no pipeline to show dependencies for", true)
		return nil
	}
	dp := &depsPopup{project: p, pl: *pl, sel: sel, follow: true}
	a.overlay = dp
	return dp.refresh(a, false)
}

func (dp *depsPopup) refresh(a *App, force bool) tea.Cmd {
	p, pl := dp.project, dp.pl
	needsAge, jobsAge := 10*time.Minute, 30*time.Second
	if isActive(pl.Status) || dp.active(a) {
		jobsAge = 3 * time.Second
	}
	if force {
		needsAge, jobsAge = 0, 0
	}
	return tea.Batch(
		fetch(a.store, needsKey(p, pl.ID), needsAge, func(ctx ctxT) (map[string][]string, error) {
			iid := pl.IID
			if iid == 0 {
				full, err := a.client.GetPipeline(ctx, p, pl.ID)
				if err != nil {
					return nil, err
				}
				iid = full.IID
			}
			return a.client.JobNeeds(ctx, p, iid)
		}),
		fetch(a.store, jobsKey(p, pl.ID), jobsAge, func(ctx ctxT) ([]gitlab.Job, error) {
			return a.client.ListPipelineJobs(ctx, p, pl.ID)
		}),
	)
}

func (dp *depsPopup) active(a *App) bool {
	jobs, _ := get[[]gitlab.Job](a.store, jobsKey(dp.project, dp.pl.ID))
	for _, j := range jobs {
		if isActive(j.Status) {
			return true
		}
	}
	return false
}

func (dp *depsPopup) debounced(*App, int) tea.Cmd { return nil }
func (dp *depsPopup) passthrough(tea.Msg) tea.Cmd { return nil }

func (dp *depsPopup) help() []kb {
	return []kb{{"hjkl", "move"}, {"enter", "log"}, {"o", "browser"}, {"y", "copy URL"}, {"esc", "close"}}
}

const depsMaxName = 40 // widest a job's name gets before it's cut

func depsDetail(j *gitlab.Job) string {
	s := j.Stage
	if d := elapsed(j.Status, j.StartedAt, j.FinishedAt, j.Duration); d != "" {
		s += " · " + d
	}
	return s
}

// graph lays out the jobs, reusing the last layout while nothing that
// shapes it has changed.
func (dp *depsPopup) graph(a *App) (*graphLayout, []gitlab.Job, error) {
	jobs, je := get[[]gitlab.Job](a.store, jobsKey(dp.project, dp.pl.ID))
	needs, ne := get[map[string][]string](a.store, needsKey(dp.project, dp.pl.ID))
	for _, e := range []*entry{je, ne} {
		if e != nil && e.err != nil && e.val == nil {
			return nil, nil, e.err
		}
	}
	if jobs == nil || needs == nil {
		return nil, nil, nil
	}
	var sorted []gitlab.Job
	for _, g := range groupStages(jobs) {
		sorted = append(sorted, g.jobs...)
	}
	var sig strings.Builder
	for _, j := range sorted {
		sig.WriteString(j.Name + "\x00" + j.Stage + "\x00" + strings.Join(needs[j.Name], "\x00") + "\x01")
	}
	if dp.g != nil && sig.String() == dp.sig {
		dp.jobs = sorted // fresh statuses
		return dp.g, sorted, nil
	}
	idx := map[string]int{}
	for i, j := range sorted {
		idx[j.Name] = i
	}
	deps := make([][]int, len(sorted))
	for i, j := range sorted {
		for _, n := range needs[j.Name] {
			if d, ok := idx[n]; ok {
				deps[i] = append(deps[i], d)
			}
		}
	}
	dp.g = layoutGraph(len(sorted), deps, func(i int) int {
		j := &sorted[i]
		return max(2+min(ansi.StringWidth(dp.name(j)), depsMaxName), 2+ansi.StringWidth(j.Stage)+11)
	})
	dp.sig, dp.jobs = sig.String(), sorted
	return dp.g, sorted, nil
}

func (dp *depsPopup) name(j *gitlab.Job) string {
	if j.IsBridge {
		return "⇢ " + j.Name
	}
	return j.Name
}

// cur is the highlighted job's index, picking one if needed: the first
// failure, else the first running job, else the first job.
func (dp *depsPopup) cur(g *graphLayout, jobs []gitlab.Job) int {
	for i := range jobs {
		if jobs[i].Name == dp.sel {
			return i
		}
	}
	if len(jobs) == 0 {
		return -1
	}
	pick := -1
	for _, want := range []func(j *gitlab.Job) bool{
		func(j *gitlab.Job) bool { return j.Status == "failed" && !j.AllowFailure },
		func(j *gitlab.Job) bool { return isActive(j.Status) },
		func(*gitlab.Job) bool { return true },
	} {
		for _, col := range g.cols {
			for _, s := range col {
				if j := g.slots[s].job; j >= 0 && want(&jobs[j]) {
					pick = j
					break
				}
			}
			if pick >= 0 {
				break
			}
		}
		if pick >= 0 {
			break
		}
	}
	dp.sel = jobs[pick].Name
	return pick
}

// jobsIn lists column c's jobs, top to bottom.
func jobsIn(g *graphLayout, c int) []int {
	var out []int
	if c >= 0 && c < len(g.cols) {
		for _, s := range g.cols[c] {
			if j := g.slots[s].job; j >= 0 {
				out = append(out, j)
			}
		}
	}
	return out
}

func (dp *depsPopup) key(a *App, msg tea.KeyMsg) tea.Cmd {
	// a burst of typed runes (key repeat) can arrive as one message
	if msg.Type == tea.KeyRunes && len(msg.Runes) > 1 && !msg.Paste {
		var cmds []tea.Cmd
		for _, r := range msg.Runes {
			cmds = append(cmds, dp.key(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}))
		}
		return tea.Batch(cmds...)
	}
	key := msg.String()
	switch key {
	case "esc", "q", "D":
		a.overlay = nil
		return nil
	case "ctrl+d", "ctrl+u", "pgdown", "pgup", "ctrl+f", "ctrl+b":
		step := max(1, dp.vh/2)
		if key == "pgdown" || key == "pgup" || key == "ctrl+f" || key == "ctrl+b" {
			step = max(1, dp.vh)
		}
		if key == "ctrl+u" || key == "pgup" || key == "ctrl+b" {
			step = -step
		}
		dp.oy += step
		dp.follow = false
		return nil
	}
	g, jobs, _ := dp.graph(a)
	if g == nil {
		return nil
	}
	ci := dp.cur(g, jobs)
	if ci < 0 {
		return nil
	}
	s := g.slots[ci]
	col := jobsIn(g, s.col)
	at := 0
	for i, j := range col {
		if j == ci {
			at = i
		}
	}
	move := func(j int) tea.Cmd {
		dp.sel, dp.follow = jobs[j].Name, true
		return nil
	}
	switch key {
	case "j", "down", "ctrl+n":
		if at+1 < len(col) {
			return move(col[at+1])
		}
	case "k", "up", "ctrl+p":
		if at > 0 {
			return move(col[at-1])
		}
	case "g", "home":
		return move(col[0])
	case "G", "end":
		return move(col[len(col)-1])
	case "h", "left", "l", "right":
		c := s.col + 1
		if key == "h" || key == "left" {
			c = s.col - 1
		}
		best, dist := -1, 0
		for _, j := range jobsIn(g, c) {
			if d := abs(g.slots[j].pos - s.pos); best < 0 || d < dist {
				best, dist = j, d
			}
		}
		if best >= 0 {
			return move(best)
		}
	case "enter":
		j := jobs[ci]
		if j.IsBridge {
			if ds := j.DownstreamPipeline; ds != nil {
				a.overlay = nil
				return a.push(newPipelineView(a.projRef(ds.ProjectID), ds.ID, ""))
			}
			a.setFlash("trigger job has no downstream pipeline yet", true)
			return nil
		}
		a.overlay = nil
		return a.push(newTraceView(dp.project, j.ID, j.Name))
	case "o":
		j := jobs[ci]
		if j.IsBridge && j.DownstreamPipeline != nil {
			return openBrowser(j.DownstreamPipeline.WebURL)
		}
		return openBrowser(j.WebURL)
	case "y":
		return copyText(jobs[ci].WebURL)
	}
	return nil
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// related marks the jobs up- and downstream of job c (c included in both).
func related(g *graphLayout, c int) (up, down map[int]bool) {
	up, down = map[int]bool{c: true}, map[int]bool{c: true}
	for changed := true; changed; {
		changed = false
		for _, e := range g.edges {
			if up[e.to] && !up[e.from] {
				up[e.from], changed = true, true
			}
			if down[e.from] && !down[e.to] {
				down[e.to], changed = true, true
			}
		}
	}
	return up, down
}

// depsSpan is a job's text on a row of the graph.
type depsSpan struct {
	x0, x1 int
	text   string
	job    int
}

// render sizes the popup to the graph, up to most of the screen.
func (dp *depsPopup) render(a *App, body string, w, h int) string {
	maxW := max(min(w, 50), w*9/10)
	maxH := max(min(h, 12), h*9/10)
	title := fmt.Sprintf("%s Job dependencies · pipeline #%d", ic.pipeline, dp.pl.ID)
	show := func(lines []string, contentW, contentH int) string {
		bw := min(maxW, max(contentW, ansi.StringWidth(title)+2)+4)
		bh := min(maxH, contentH+2)
		for i := range lines {
			lines[i] = fit(lines[i], bw-4)
		}
		return overlayAt(body, a.zone(pane(title, lines, bw, bh, true), zone{}), (w-bw)/2, (h-bh)/2)
	}

	g, jobs, err := dp.graph(a)
	var msg string
	switch {
	case err != nil:
		msg = sErr.Render(err.Error())
	case g == nil:
		msg = sDim.Render(spinnerFrame() + " loading jobs…")
	case len(jobs) == 0:
		msg = sDim.Render("This pipeline has no jobs.")
	}
	if msg != "" {
		return show([]string{msg}, ansi.StringWidth(msg), 1)
	}

	ci := dp.cur(g, jobs)
	up, down := related(g, ci)
	needs, needed := 0, 0
	for _, e := range g.edges {
		if e.to == ci {
			needs++
		}
		if e.from == ci {
			needed++
		}
	}

	// the highlighted job, in full
	j := &jobs[ci]
	head := statusIcon(j.Status, j.AllowFailure) + " " + sBold.Render(sanitize(dp.name(j))) +
		sDim.Render(fmt.Sprintf("  %s · needs %d · needed by %d", depsDetail(j), needs, needed))
	lines := []string{head, ""}

	// scroll
	bw := min(maxW, max(g.w, ansi.StringWidth(head), ansi.StringWidth(title)+2)+4)
	bh := min(maxH, g.h+len(lines)+2)
	dp.vw, dp.vh = bw-4, bh-2-len(lines)
	s := g.slots[ci]
	if dp.follow {
		sx0, sx1 := g.colX[s.col], g.chX[s.col]
		sy0 := s.pos
		if sx1 > dp.ox+dp.vw {
			dp.ox = sx1 - dp.vw
		}
		if sx0 < dp.ox {
			dp.ox = sx0
		}
		if sy0+2 > dp.oy+dp.vh {
			dp.oy = sy0 + 2 - dp.vh
		}
		if sy0 < dp.oy {
			dp.oy = sy0
		}
		dp.follow = false
	}
	dp.ox = max(0, min(dp.ox, g.w-dp.vw))
	dp.oy = max(0, min(dp.oy, g.h-dp.vh))

	// the job's own lines are lit brightest, the rest up- and downstream
	// of it less so
	direct := map[int]bool{}
	hl := func(e int) uint8 {
		ed := g.edges[e]
		switch {
		case ed.from == ci || ed.to == ci:
			return 2
		case (up[ed.from] && up[ed.to]) || (down[ed.from] && down[ed.to]):
			return 1
		}
		return 0
	}
	for _, ed := range g.edges {
		if ed.from == ci {
			direct[ed.to] = true
		}
		if ed.to == ci {
			direct[ed.from] = true
		}
	}
	grid := g.grid(hl, func(i int) int {
		return min(g.colW[g.slots[i].col]-1, ansi.StringWidth(sanitize(dp.name(&jobs[i])))+2)
	})

	// job text, by row
	spans := map[int][]depsSpan{}
	for i := range jobs {
		js := g.slots[i]
		x0, cw := g.colX[js.col], g.colW[js.col]
		jy := js.pos
		jb := &jobs[i]
		name := sanitize(dp.name(jb))
		switch {
		case direct[i]:
			name = sDirect.Render(name)
		case !up[i] && !down[i]:
			name = sDim.Render(name)
		}
		top := fit(statusIcon(jb.Status, jb.AllowFailure)+" "+name, cw-1)
		tw := ansi.StringWidth(top)
		if i == ci {
			top = selectLine(top, tw)
		}
		det := "  " + depsDetail(jb)
		spans[jy] = append(spans[jy], depsSpan{x0, x0 + tw, top, i})
		spans[jy+1] = append(spans[jy+1], depsSpan{x0, x0 + ansi.StringWidth(det), sDim.Render(det), i})
	}

	for _, sp := range spans {
		sort.Slice(sp, func(i, j int) bool { return sp[i].x0 < sp[j].x0 })
	}
	for row := dp.oy; row < min(g.h, dp.oy+dp.vh); row++ {
		var b strings.Builder
		cx, end := dp.ox, dp.ox+dp.vw
		for _, sp := range spans[row] {
			if sp.x1 <= cx || sp.x0 >= end {
				continue
			}
			if sp.x0 > cx {
				b.WriteString(lineString(grid[row], cx, sp.x0))
				cx = sp.x0
			}
			l, r := max(sp.x0, cx), min(sp.x1, end)
			text := sp.text
			if l > sp.x0 || r < sp.x1 {
				text = ansi.Cut(text, l-sp.x0, r-sp.x0)
			}
			b.WriteString(dp.clickJob(a, text, jobs[sp.job].Name))
			cx = r
		}
		b.WriteString(lineString(grid[row], cx, end))
		lines = append(lines, b.String())
	}

	if g.h > dp.vh {
		title += sDim.Render(" " + textProgress(dp.oy, dp.vh, g.h))
	}
	lines[0] = fit(head, dp.vw)
	box := a.zone(pane(title, lines, bw, bh, true), zone{
		scroll: func(dir int) tea.Cmd {
			dp.oy += 3 * dir
			dp.follow = false
			return nil
		},
	})
	return overlayAt(body, box, (w-bw)/2, (h-bh)/2)
}

// clickJob makes a job's text clickable: a click highlights the job, a
// double click opens its log.
func (dp *depsPopup) clickJob(a *App, text, name string) string {
	return a.clickRow(text,
		func() tea.Cmd { dp.sel = name; return nil },
		func() tea.Cmd { return dp.key(a, keyEnter) })
}
