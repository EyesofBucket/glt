package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"gitlab-tui/internal/gitlab"
)

type traceView struct {
	project string
	job     int
	name    string

	top     int // first visible display row
	xoff    int
	height  int
	follow  bool
	wrap    bool
	numbers bool
	stamps  bool

	search    textinput.Model
	searching bool
	query     string
	hits      []int // source line indexes matching query
	hitVer    int

	// wrap cache: display rows and the source line each came from
	wrapped []string
	wrapSrc []int
	wrapN   int // source lines consumed
	wrapW   int
}

func newTraceView(project string, job int, name string) *traceView {
	ti := textinput.New()
	ti.Prompt = "/"
	return &traceView{project: project, job: job, name: name, follow: true, numbers: true, search: ti}
}

// NewTrace opens a job log directly.
func NewTrace(project string, job int) view { return newTraceView(project, job, "") }

func jobKey(p string, id int) string { return fmt.Sprintf("job:%s:%d", p, id) }

func (v *traceView) title() string {
	if v.name != "" {
		return v.name
	}
	return fmt.Sprintf("job %d", v.job)
}

func (v *traceView) capturing() bool { return v.searching }

func (v *traceView) proj() string { return v.project }

func (v *traceView) jobInfo(a *App) (*gitlab.Job, *entry) {
	return get[*gitlab.Job](a.store, jobKey(v.project, v.job))
}

func (v *traceView) refresh(a *App, force bool) tea.Cmd {
	p, id := v.project, v.job
	j, _ := v.jobInfo(a)
	if j != nil && v.name == "" {
		v.name = j.Name
	}
	age := 3 * time.Second
	if j != nil && !isActive(j.Status) && j.Status != "manual" {
		age = time.Minute
	}
	if force {
		age = 0
	}
	cmds := []tea.Cmd{fetch(a.store, jobKey(p, id), age, func(ctx ctxT) (*gitlab.Job, error) { return a.client.GetJob(ctx, p, id) })}
	t := a.store.traces[traceKey(p, id)]
	tAge, ok := traceMaxAge(j, t)
	if j == nil {
		tAge, ok = 0, true // don't wait for job info before the first fetch
	}
	if ok || force {
		if force {
			tAge = 0
		}
		cmds = append(cmds, fetchTrace(a.store, a.client, p, id, tAge))
	}
	return tea.Batch(cmds...)
}

func (v *traceView) help() []kb {
	return []kb{{"f", "follow"}, {"w", "wrap"}, {"/ n N", "search"}, {"[ ]", "sections"}, {"T", "timestamps"}, {"r", "retry"}, {"p", "play"}, {"x", "cancel"}, {"o", "browser"}}
}

func (v *traceView) trace(a *App) *traceState { return a.store.traces[traceKey(v.project, v.job)] }

// rows returns the number of display rows, and a function to fetch one.
func (v *traceView) rowCount(t *traceState) int {
	if t == nil {
		return 0
	}
	if v.wrap {
		return len(v.wrapped)
	}
	return t.lineCount()
}

func (v *traceView) srcLine(t *traceState, row int) int {
	if v.wrap {
		if row < len(v.wrapSrc) {
			return v.wrapSrc[row]
		}
		return -1
	}
	return row
}

func (v *traceView) displayRow(t *traceState, src int) int {
	if !v.wrap {
		return src
	}
	for i, s := range v.wrapSrc {
		if s >= src {
			return i
		}
	}
	return len(v.wrapSrc) - 1
}

func (v *traceView) gutterW(t *traceState) int {
	if t == nil {
		return 0
	}
	w := 0
	if v.numbers {
		w += len(fmt.Sprint(t.lineCount())) + 1
	}
	if v.stamps && t.hasTS {
		w += 9
	}
	return w
}

func (v *traceView) ensureWrap(t *traceState, w int) {
	if !v.wrap || t == nil {
		return
	}
	cw := max(10, w-v.gutterW(t))
	if cw != v.wrapW || v.wrapN > len(t.lines) {
		v.wrapped, v.wrapSrc, v.wrapN, v.wrapW = nil, nil, 0, cw
	}
	// drop the previously-wrapped partial tail line
	for len(v.wrapSrc) > 0 && v.wrapSrc[len(v.wrapSrc)-1] >= v.wrapN {
		v.wrapped, v.wrapSrc = v.wrapped[:len(v.wrapped)-1], v.wrapSrc[:len(v.wrapSrc)-1]
	}
	add := func(i int, l string) {
		for _, part := range strings.Split(ansi.Hardwrap(l, cw, true), "\n") {
			v.wrapped = append(v.wrapped, part)
			v.wrapSrc = append(v.wrapSrc, i)
		}
	}
	for ; v.wrapN < len(t.lines); v.wrapN++ {
		add(v.wrapN, t.lines[v.wrapN])
	}
	for i, l := range t.tail {
		add(len(t.lines)+i, l)
	}
}

func (v *traceView) runSearch(t *traceState) {
	v.hits = v.hits[:0]
	if v.query == "" || t == nil {
		return
	}
	q := strings.ToLower(v.query)
	for i := 0; i < t.lineCount(); i++ {
		if strings.Contains(strings.ToLower(ansi.Strip(t.line(i))), q) {
			v.hits = append(v.hits, i)
		}
	}
	v.hitVer = t.version
}

func (v *traceView) jumpHit(t *traceState, dir int) bool {
	if t != nil && v.hitVer != t.version {
		v.runSearch(t)
	}
	if len(v.hits) == 0 {
		return false
	}
	cur := v.srcLine(t, v.top+v.height/3)
	if dir > 0 {
		for _, h := range v.hits {
			if h > cur {
				v.centre(t, h)
				return true
			}
		}
		v.centre(t, v.hits[0])
	} else {
		for i := len(v.hits) - 1; i >= 0; i-- {
			if v.hits[i] < cur {
				v.centre(t, v.hits[i])
				return true
			}
		}
		v.centre(t, v.hits[len(v.hits)-1])
	}
	return true
}

func (v *traceView) centre(t *traceState, src int) {
	v.follow = false
	v.top = max(0, v.displayRow(t, src)-v.height/3)
}

func (v *traceView) key(a *App, msg tea.KeyMsg) tea.Cmd {
	t := v.trace(a)
	if v.searching {
		switch msg.String() {
		case "esc":
			v.searching = false
			v.search.Blur()
			return nil
		case "enter":
			v.searching = false
			v.search.Blur()
			v.query = v.search.Value()
			v.runSearch(t)
			if !v.jumpHit(t, 1) && v.query != "" {
				a.setFlash("no matches for "+v.query, true)
			}
			return nil
		}
		var cmd tea.Cmd
		v.search, cmd = v.search.Update(msg)
		return cmd
	}

	page := max(1, v.height)
	total := v.rowCount(t)
	scroll := func(n int) {
		v.top += n
		v.follow = false
	}
	switch msg.String() {
	case "j", "down":
		scroll(1)
	case "k", "up":
		scroll(-1)
	case "ctrl+d":
		scroll(page / 2)
	case "ctrl+u":
		scroll(-page / 2)
	case "pgdown", " ", "ctrl+f":
		scroll(page)
	case "pgup", "ctrl+b":
		scroll(-page)
	case "g", "home":
		v.top, v.follow = 0, false
	case "G", "end":
		v.follow = true
	case "f":
		v.follow = !v.follow
	case "left", "h":
		v.xoff = max(0, v.xoff-8)
	case "right", "l":
		if !v.wrap {
			v.xoff += 8
		}
	case "0":
		v.xoff = 0
	case "w":
		v.wrap = !v.wrap
		v.xoff = 0
		src := v.srcLine(t, v.top)
		v.wrapped, v.wrapSrc, v.wrapN = nil, nil, 0
		v.ensureWrap(t, a.w)
		if src >= 0 {
			v.top = v.displayRow(t, src)
		}
	case "#":
		v.numbers = !v.numbers
	case "T":
		v.stamps = !v.stamps
	case "/":
		v.searching = true
		v.search.SetValue("")
		return v.search.Focus()
	case "n":
		v.jumpHit(t, 1)
	case "N":
		v.jumpHit(t, -1)
	case "]", "[":
		if t == nil || len(t.sections) == 0 {
			return nil
		}
		cur := v.srcLine(t, v.top)
		target := -1
		if msg.String() == "]" {
			for _, s := range t.sections {
				if s > cur {
					target = s
					break
				}
			}
		} else {
			for i := len(t.sections) - 1; i >= 0; i-- {
				if t.sections[i] < cur {
					target = t.sections[i]
					break
				}
			}
		}
		if target >= 0 {
			v.follow = false
			v.top = v.displayRow(t, target)
		}
	case "o":
		if j, _ := v.jobInfo(a); j != nil {
			return openBrowser(j.WebURL)
		}
	case "y":
		if j, _ := v.jobInfo(a); j != nil {
			return copyText(j.WebURL)
		}
	case "r":
		p, id := v.project, v.job
		var nj *gitlab.Job
		return a.action("retry "+v.title(), func() (err error) {
			nj, err = a.client.RetryJob(bg(), p, id)
			return err
		}, v.invalidations(a), func(a *App) tea.Cmd {
			if nj == nil || nj.ID == 0 {
				return nil
			}
			return a.replace(newTraceView(p, nj.ID, nj.Name))
		})
	case "p":
		p, id := v.project, v.job
		return a.action("play "+v.title(), func() error { _, err := a.client.PlayJob(bg(), p, id); return err }, v.invalidations(a), nil)
	case "x":
		p, id := v.project, v.job
		return a.action("cancel "+v.title(), func() error { return a.client.CancelJob(bg(), p, id) }, v.invalidations(a), nil)
	}
	v.top = max(0, min(v.top, total-page))
	return nil
}

func (v *traceView) invalidations(a *App) []string {
	ks := []string{jobKey(v.project, v.job)}
	for k := range a.store.m {
		if strings.HasPrefix(k, "jobs:") || strings.HasPrefix(k, "pipe:") {
			ks = append(ks, k)
		}
	}
	return ks
}

func (v *traceView) render(a *App, w, h int) string {
	t := v.trace(a)
	j, je := v.jobInfo(a)

	// header line
	var hdr string
	if j != nil {
		hdr = sTitle.Render(j.Name) + "  " + statusLabel(j.Status, j.AllowFailure)
		if d := elapsed(j.Status, j.StartedAt, j.FinishedAt, j.Duration); d != "" {
			hdr += sDim.Render("  ⏱ " + d)
		}
		if j.Runner != nil && j.Runner.Description != "" {
			hdr += sDim.Render("  " + j.Runner.Description)
		}
		if j.FailureReason != "" && j.Status == "failed" {
			hdr += sErr.Render("  " + strings.ReplaceAll(j.FailureReason, "_", " "))
		}
	} else {
		hdr = sTitle.Render(v.title()) + entryStatus(je)
	}
	var flags []string
	if v.follow {
		flags = append(flags, sOK.Render("follow"))
	}
	if v.wrap {
		flags = append(flags, "wrap")
	}
	if v.query != "" {
		flags = append(flags, fmt.Sprintf("/%s %d hits", v.query, len(v.hits)))
	}
	if t != nil {
		flags = append(flags, fmt.Sprintf("%d lines", t.lineCount()))
	}
	right := sDim.Render(strings.Join(flags, " · "))
	hdr = fit(hdr, w-ansi.StringWidth(right)-1)
	hdr += strings.Repeat(" ", max(1, w-ansi.StringWidth(hdr)-ansi.StringWidth(right))) + right
	h--
	if v.searching {
		h--
	}
	v.height = h

	var b strings.Builder
	b.WriteString(hdr + "\n")
	if v.searching {
		b.WriteString(v.search.View() + "\n")
	}

	switch {
	case t == nil || (t.lineCount() == 0 && (t.loading || t.at.IsZero())):
		if j != nil && j.StartedAt == nil {
			b.WriteString(sDim.Render("\n  job is " + j.Status + " — no log yet"))
			if j.Status == "manual" {
				b.WriteString(sDim.Render(" (p to play)"))
			}
		} else {
			b.WriteString(sDim.Render("\n  " + spinnerFrame() + " loading log…"))
		}
		return b.String()
	case t.err != nil && t.lineCount() == 0:
		b.WriteString(sErr.Render("\n  " + t.err.Error()))
		return b.String()
	}

	v.ensureWrap(t, w)
	if v.query != "" && v.hitVer != t.version {
		v.runSearch(t)
	}
	total := v.rowCount(t)
	if v.follow {
		v.top = max(0, total-h)
	}
	v.top = max(0, min(v.top, total-h))

	gw := v.gutterW(t)
	cw := w - gw
	hitSet := map[int]bool{}
	for _, hl := range v.hits {
		hitSet[hl] = true
	}
	prevSrc := -1
	if v.top > 0 {
		prevSrc = v.srcLine(t, v.top-1)
	}
	for r := v.top; r < min(total, v.top+h); r++ {
		src := v.srcLine(t, r)
		var line string
		if v.wrap {
			line = v.wrapped[r]
		} else {
			line = t.line(r)
			if v.xoff > 0 {
				line = ansi.Cut(line, v.xoff, v.xoff+cw)
			} else {
				line = ansi.Truncate(line, cw, "")
			}
		}
		if gw > 0 {
			num, stamp := "", ""
			if src != prevSrc {
				num = fmt.Sprint(src + 1)
				stamp = t.stamp(src)
			}
			g := ""
			if v.stamps && t.hasTS {
				g = fmt.Sprintf("%-8s ", stamp)
			}
			if v.numbers {
				g += fmt.Sprintf("%*s ", gw-len(g)-1, num)
			}
			if hitSet[src] {
				g = sAccent.Reverse(true).Render(g)
			} else {
				g = sFaint.Render(g)
			}
			line = g + line
		} else if hitSet[src] {
			line = sAccent.Render("▌") + ansi.Truncate(line, w-1, "")
		}
		prevSrc = src
		b.WriteString(line + "\n")
	}
	return b.String()
}

func (v *traceView) progress(a *App) string {
	t := v.trace(a)
	if t == nil || t.lineCount() == 0 {
		return ""
	}
	return textProgress(v.top, v.height, v.rowCount(t))
}
