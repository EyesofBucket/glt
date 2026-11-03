package ui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"gitlab-tui/internal/gitlab"
)

type pipelineView struct {
	project string // path, or numeric ID for other projects
	id      int
	branch  string // when set, track the latest pipeline on this branch
	l       listState
	seq     int
	moving  bool // cursor moved, preview fetch pending debounce
	flipped bool // preview toggled away from the configured default
	pvJob   int
	// jobs whose logs are shown side by side, by name so a watch follows
	// retries and new pipelines on the branch
	watch map[string]bool
}

func newPipelineView(project string, id int, branch string) *pipelineView {
	return &pipelineView{project: project, id: id, branch: branch, l: newListState(), watch: map[string]bool{}}
}

// NewPipeline opens pipeline id, or (id == 0) the latest pipeline for
// branch, following new pipelines as they're created.
func NewPipeline(project string, id int, branch string) view {
	return newPipelineView(project, id, branch)
}

func (v *pipelineView) title() string {
	if v.id == 0 {
		return "latest pipeline (" + v.branch + ")"
	}
	return fmt.Sprintf("#%d", v.id)
}

func (v *pipelineView) capturing() bool { return v.l.filtering }

func latestKey(p, branch string) string { return fmt.Sprintf("latest:%s:%s", p, branch) }

func (v *pipelineView) proj() string { return v.project }

func (v *pipelineView) preview(a *App) bool { return a.opts.Preview != v.flipped }

// fetchLatest loads the newest pipeline on branch (as a 0/1-element list).
func fetchLatest(a *App, project, branch string, age time.Duration) tea.Cmd {
	return fetch(a.store, latestKey(project, branch), age, func(ctx ctxT) ([]gitlab.Pipeline, error) {
		ps, err := a.client.ListPipelines(ctx, project, branch)
		if len(ps) > 1 {
			ps = ps[:1]
		}
		return ps, err
	})
}

func prefetchPipeline(a *App, project string, id int) tea.Cmd {
	return tea.Batch(
		fetch(a.store, pipeKey(project, id), time.Minute, func(ctx ctxT) (*gitlab.Pipeline, error) {
			return a.client.GetPipeline(ctx, project, id)
		}),
		fetch(a.store, jobsKey(project, id), time.Minute, func(ctx ctxT) ([]gitlab.Job, error) {
			return a.client.ListPipelineJobs(ctx, project, id)
		}),
	)
}

func (v *pipelineView) pipeline(a *App) (*gitlab.Pipeline, *entry) {
	return get[*gitlab.Pipeline](a.store, pipeKey(v.project, v.id))
}

// traceMaxAge decides how often a job's log needs refetching; ok is false
// when there's nothing (more) to fetch.
func traceMaxAge(j *gitlab.Job, t *traceState) (age time.Duration, ok bool) {
	if j == nil || j.StartedAt == nil {
		return 0, false
	}
	if isActive(j.Status) {
		return 1500 * time.Millisecond, true
	}
	if t == nil || t.at.IsZero() || (j.FinishedAt != nil && t.at.Before(j.FinishedAt.Add(3*time.Second))) {
		return 2 * time.Second, true
	}
	return 0, false
}

func (v *pipelineView) refresh(a *App, force bool) tea.Cmd {
	var cmds []tea.Cmd
	if v.branch != "" {
		k := latestKey(v.project, v.branch)
		age := 10 * time.Second
		if force || v.id == 0 {
			age = 0
		}
		if ps, e := get[[]gitlab.Pipeline](a.store, k); len(ps) > 0 && ps[0].ID != v.id {
			if v.id != 0 && !e.disk {
				a.setFlash(fmt.Sprintf("new pipeline #%d on %s", ps[0].ID, v.branch), false)
			}
			v.id = ps[0].ID
			v.l.cursor, v.l.offset, v.pvJob = 0, 0, 0
		} else if e != nil && e.val != nil && len(ps) == 0 {
			age = 10 * time.Second
		}
		cmds = append(cmds, fetchLatest(a, v.project, v.branch, age))
		if v.id == 0 {
			return tea.Batch(cmds...)
		}
	}

	pl, _ := v.pipeline(a)
	if pl != nil {
		if _, err := strconv.Atoi(v.project); err != nil {
			a.learnProject(v.project, pl.ProjectID)
		}
	}
	age := 30 * time.Second
	if pl == nil || isActive(pl.Status) {
		age = 3 * time.Second
	}
	if force {
		age = 0
	}
	p, id := v.project, v.id
	cmds = append(cmds,
		fetch(a.store, pipeKey(p, id), age, func(ctx ctxT) (*gitlab.Pipeline, error) { return a.client.GetPipeline(ctx, p, id) }),
		fetch(a.store, jobsKey(p, id), age, func(ctx ctxT) ([]gitlab.Job, error) { return a.client.ListPipelineJobs(ctx, p, id) }),
	)

	if sel := v.selected(a); sel != nil && !v.moving {
		v.pvJob = sel.ID
	}
	for _, j := range v.logJobs(a) {
		if j.IsBridge {
			continue
		}
		t := a.store.traces[traceKey(p, j.ID)]
		if tAge, ok := traceMaxAge(&j, t); ok {
			if force {
				tAge = 0
			}
			cmds = append(cmds, fetchTrace(a.store, a.client, p, j.ID, tAge))
		}
	}
	return tea.Batch(cmds...)
}

// watched lists the watched jobs in pipeline order.
func (v *pipelineView) watched(a *App) []gitlab.Job {
	if len(v.watch) == 0 {
		return nil
	}
	jobs, _ := get[[]gitlab.Job](a.store, jobsKey(v.project, v.id))
	var out []gitlab.Job
	for _, g := range groupStages(jobs) {
		for _, j := range g.jobs {
			if v.watch[j.Name] {
				out = append(out, j)
			}
		}
	}
	return out
}

// logJobs are the jobs whose logs are on screen: the watched ones, or
// else the previewed one.
func (v *pipelineView) logJobs(a *App) []gitlab.Job {
	if ws := v.watched(a); len(ws) > 0 {
		return ws
	}
	if v.preview(a) && v.pvJob != 0 {
		if j := v.job(a, v.pvJob); j != nil {
			return []gitlab.Job{*j}
		}
	}
	return nil
}

func (v *pipelineView) rows(a *App) []gitlab.Job {
	jobs, _ := get[[]gitlab.Job](a.store, jobsKey(v.project, v.id))
	var out []gitlab.Job
	for _, g := range groupStages(jobs) {
		for _, j := range g.jobs {
			if matches(v.l.query, j.Name, j.Stage, j.Status) {
				out = append(out, j)
			}
		}
	}
	return out
}

func (v *pipelineView) job(a *App, id int) *gitlab.Job {
	jobs, _ := get[[]gitlab.Job](a.store, jobsKey(v.project, v.id))
	for i := range jobs {
		if jobs[i].ID == id {
			return &jobs[i]
		}
	}
	return nil
}

func (v *pipelineView) selected(a *App) *gitlab.Job {
	rows := v.rows(a)
	if v.l.cursor < len(rows) {
		return &rows[v.l.cursor]
	}
	return nil
}

func (v *pipelineView) debounced(a *App, seq int) tea.Cmd {
	if seq != v.seq {
		return nil
	}
	v.moving = false
	return v.refresh(a, false)
}

// moved schedules the preview update for the newly selected job.
func (v *pipelineView) moved() tea.Cmd {
	v.seq++
	v.moving = true
	return debounce(v, v.seq, 120*time.Millisecond)
}

func (v *pipelineView) help() []kb {
	return []kb{{"enter", "log"}, {"r", "retry"}, {"p", "play/auto-play"}, {"x", "cancel"}, {"R/X", "retry/cancel pipeline"}, {"w", "watch"}, {"v", "preview"}, {"T", "tests"}, {"D", "dependencies"}, {"]/[", "next/prev failed"}}
}

func (v *pipelineView) key(a *App, msg tea.KeyMsg) tea.Cmd {
	if v.l.filtering {
		cmd, _ := v.l.filterKey(msg)
		return cmd
	}
	rows := v.rows(a)
	if ok, moved := v.l.nav(msg.String(), len(rows)); ok {
		if moved {
			return v.moved()
		}
		return nil
	}
	p, pid := v.project, v.id
	pl, _ := v.pipeline(a)
	inval := []string{pipeKey(p, pid), jobsKey(p, pid)}
	for k := range a.store.m {
		if strings.HasPrefix(k, "pipes:") || strings.HasPrefix(k, "mr:") {
			inval = append(inval, k)
		}
	}
	switch msg.String() {
	case "/":
		return v.l.startFilter()
	case "v":
		v.flipped = !v.flipped
		return v.refresh(a, false)
	case "]", "[":
		dir := 1
		if msg.String() == "[" {
			dir = -1
		}
		for i := v.l.cursor + dir; i >= 0 && i < len(rows); i += dir {
			if rows[i].Status == "failed" {
				v.l.cursor = i
				return v.moved()
			}
		}
		a.setFlash("no more failed jobs", false)
		return nil
	case "O":
		if pl != nil {
			return openBrowser(pl.WebURL)
		}
		return nil
	case "T":
		return a.openTests(p, pl)
	case "D":
		sel := ""
		if j := v.selected(a); j != nil {
			sel = j.Name
		}
		return a.openDeps(p, pl, sel)
	case "R":
		return a.action(fmt.Sprintf("retry pipeline #%d", pid), func() error { return a.client.RetryPipeline(bg(), p, pid) }, inval, nil)
	case "X":
		a.confirm(fmt.Sprintf("Cancel pipeline #%d?", pid), func() tea.Cmd {
			return a.action(fmt.Sprintf("cancel pipeline #%d", pid), func() error { return a.client.CancelPipeline(bg(), p, pid) }, inval, nil)
		})
		return nil
	}

	j := v.selected(a)
	if j == nil {
		return nil
	}
	jid, name := j.ID, j.Name
	switch msg.String() {
	case "enter", "l", "right":
		if j.IsBridge {
			if ds := j.DownstreamPipeline; ds != nil {
				return a.push(newPipelineView(a.projRef(ds.ProjectID), ds.ID, ""))
			}
			a.setFlash("trigger job has no downstream pipeline yet", true)
			return nil
		}
		return a.push(newTraceView(p, jid, name))
	case "o":
		if j.IsBridge && j.DownstreamPipeline != nil {
			return openBrowser(j.DownstreamPipeline.WebURL)
		}
		return openBrowser(j.WebURL)
	case "y":
		return copyText(j.WebURL)
	case "w":
		if v.watch[name] {
			delete(v.watch, name)
			a.setFlash("stopped watching "+name, false)
		} else {
			v.watch[name] = true
			a.setFlash(fmt.Sprintf("watching %s (%d)", name, len(v.watch)), false)
		}
		return v.refresh(a, false)
	case "r":
		if j.IsBridge {
			a.setFlash("trigger jobs can't be retried individually; use R", true)
			return nil
		}
		return a.action("retry "+name, func() error { _, err := a.client.RetryJob(bg(), p, jid); return err }, inval, nil)
	case "p":
		return a.playJob(p, v.id, j, inval)
	case "x":
		return a.action("cancel "+name, func() error { return a.client.CancelJob(bg(), p, jid) }, inval, nil)
	}
	return nil
}

func (v *pipelineView) render(a *App, w, h int) string {
	var b strings.Builder
	if v.id == 0 {
		_, e := get[[]gitlab.Pipeline](a.store, latestKey(v.project, v.branch))
		if e != nil && e.val != nil {
			return "\n" + sDim.Render("  No pipelines for "+v.branch+" yet — watching for one…")
		}
		return "\n" + emptyMsg(e, "")
	}
	pl, pe := v.pipeline(a)
	lines := 0
	if pl != nil {
		hdr := sTitle.Render(fmt.Sprintf("Pipeline #%d", pl.ID)) + "  " + statusLabel(pl.Status, false) + "  " +
			sKey.Render(pl.Ref) + sDim.Render(" · "+pl.Source+" · "+shortSHA(pl.SHA))
		if pl.User != nil {
			hdr += sDim.Render(" · @" + pl.User.Username)
		}
		hdr += sDim.Render(" · " + since(pl.CreatedAt) + " ago")
		if d := elapsed(pl.Status, pl.StartedAt, pl.FinishedAt, pl.Duration); d != "" {
			hdr += sDim.Render(" · ⏱ " + d)
		}
		b.WriteString(fit(hdr+entryStatus(pe), w) + "\n")
	} else {
		b.WriteString(fmt.Sprintf("Pipeline #%d", v.id) + entryStatus(pe) + "\n")
	}
	lines++
	if fl := v.l.filterLine(); fl != "" {
		b.WriteString(fl + "\n")
		lines++
	}

	rows := v.rows(a)
	_, je := get[[]gitlab.Job](a.store, jobsKey(v.project, v.id))
	if len(rows) == 0 {
		b.WriteString("\n" + emptyMsg(je, "No jobs."))
		return b.String()
	}

	avail := h - lines
	listH := avail
	watched := v.watched(a)
	showPreview := (v.preview(a) || len(watched) > 0) && avail >= 16
	if showPreview {
		listH = min(len(rows)+1, max(6, avail*45/100))
	}

	cells := make([][]string, len(rows))
	for i, j := range rows {
		name := j.Name
		if v.watch[j.Name] {
			name = sWatched.Render(name + " " + ic.watch)
		}
		if j.IsBridge {
			name = "⇢ " + name
			if ds := j.DownstreamPipeline; ds != nil {
				name += sDim.Render(fmt.Sprintf("  → #%d ", ds.ID)) + statusIcon(ds.Status, false)
			}
		}
		if j.FailureReason != "" && j.Status == "failed" {
			name += sDim.Render("  " + strings.ReplaceAll(j.FailureReason, "_", " "))
		}
		runner := ""
		if j.Runner != nil {
			runner = j.Runner.Description
		}
		cells[i] = []string{sStage.Render(j.Stage), statusLabel(a.shownStatus(&j), j.AllowFailure), name,
			sDim.Render(elapsed(j.Status, j.StartedAt, j.FinishedAt, j.Duration)), sDim.Render(runner), sDim.Render(strconv.Itoa(j.ID))}
	}
	t := newTable([]col{
		{title: "STAGE", max: 20}, {title: "STATUS"}, {title: "JOB", flex: true},
		{title: "DURATION", right: true}, {title: "RUNNER", max: 28, optional: true}, {title: "ID", optional: true},
	}, cells, w)
	b.WriteString(t.header() + "\n")

	start, end := v.l.window(len(rows), listH-1)
	for i := start; i < end; i++ {
		line := t.row(cells[i]...)
		if i == v.l.cursor {
			line = selectLine(line, w)
		}
		b.WriteString(v.l.clickRow(a, line, i, v.moved, func() tea.Cmd { return v.key(a, keyEnter) }) + "\n")
	}
	for i := end - start; i < listH-1; i++ {
		b.WriteString("\n")
	}

	if showPreview {
		b.WriteString(v.renderLogs(a, watched, w, avail-listH))
	}
	return b.String()
}

// renderLogs splits w columns evenly between the watched jobs' logs,
// side by side, or shows the selected job's when nothing is watched.
func (v *pipelineView) renderLogs(a *App, watched []gitlab.Job, w, h int) string {
	if len(watched) == 0 {
		if j := v.selected(a); j != nil {
			return strings.Join(v.renderLog(a, j, false, "", w, h), "\n")
		}
		return ""
	}
	const minW = 24
	n := min(len(watched), max(1, (w+1)/(minW+1)))
	cols := make([][]string, n)
	for i, j := range watched[:n] {
		pw := (w - (n - 1)) / n
		if i < (w-(n-1))%n {
			pw++
		}
		note := ""
		if i == n-1 && n < len(watched) {
			note = fmt.Sprintf("+%d more ", len(watched)-n)
		}
		cols[i] = v.renderLog(a, &j, true, note, pw, h)
	}
	rows := make([]string, h)
	for r := range rows {
		sep := sBorder.Render("│")
		if r == 0 {
			sep = sBorder.Render("┬")
		}
		parts := make([]string, n)
		for i := range cols {
			parts[i] = cols[i][r]
		}
		rows[r] = strings.Join(parts, sep)
	}
	return strings.Join(rows, "\n")
}

// renderLog draws one job's log tail as h lines of width w, under a rule
// with its name (and a note, if any). Clicking the name opens the full
// log.
func (v *pipelineView) renderLog(a *App, j *gitlab.Job, watched bool, note string, w, h int) []string {
	name := j.Name
	if room := w - 6 - ansi.StringWidth(note); ansi.StringWidth(name) > room {
		name = ansi.Truncate(name, max(1, room), "…")
	}
	label := sBold.Render(" " + name + " ")
	if watched {
		label = " " + statusIcon(a.shownStatus(j), j.AllowFailure) + sWatched.Render(" "+name+" ")
	}
	p, id, name := v.project, j.ID, j.Name
	label = a.zone(label, zone{click: func(bool) tea.Cmd { return a.push(newTraceView(p, id, name)) }})
	if note != "" {
		label += sDim.Render(note)
	}
	rule := sBorder.Render("──") + label
	rule += sBorder.Render(strings.Repeat("─", max(0, w-ansi.StringWidth(rule))))
	if ansi.StringWidth(rule) > w {
		rule = fit(rule, w)
	}
	if h <= 0 {
		return nil
	}
	h--
	var body []string
	switch {
	case j.IsBridge:
		if ds := j.DownstreamPipeline; ds != nil {
			body = []string{sDim.Render(fmt.Sprintf("  downstream pipeline #%d ", ds.ID)) + statusLabel(ds.Status, false) +
				sDim.Render("  (enter to open)")}
		} else {
			body = []string{sDim.Render("  trigger job, no downstream pipeline yet")}
		}
	case j.StartedAt == nil:
		body = []string{sDim.Render("  job is " + a.shownStatus(j) + " — no log yet")}
	default:
		t := a.store.traces[traceKey(v.project, j.ID)]
		switch {
		case t == nil || (t.lineCount() == 0 && t.loading):
			body = []string{sDim.Render("  " + spinnerFrame() + " loading log…")}
		case t.err != nil && t.lineCount() == 0:
			body = []string{sErr.Render("  " + t.err.Error())}
		case t.lineCount() == 0:
			body = []string{sDim.Render("  (empty log)")}
		default:
			body = logTail(t, w, h, a.opts.Wrap)
		}
	}
	for len(body) < h {
		body = append(body, "")
	}
	lines := []string{rule}
	for _, l := range body[:h] {
		lines = append(lines, pad(fit(l, w), w))
	}
	return lines
}

// logTail is the last h rows of t at width w, wrapping long lines when
// wrap is set.
func logTail(t *traceState, w, h int, wrap bool) []string {
	var rows []string
	for i := t.lineCount() - 1; i >= 0 && len(rows) < h; i-- {
		if !wrap {
			rows = append(rows, ansi.Truncate(t.line(i), w, ""))
			continue
		}
		parts := strings.Split(ansi.Hardwrap(t.line(i), max(1, w), true), "\n")
		for k := len(parts) - 1; k >= 0 && len(rows) < h; k-- {
			rows = append(rows, parts[k])
		}
	}
	slices.Reverse(rows)
	return rows
}

func (v *pipelineView) progress(a *App) string { return listProgress(v.l.cursor, len(v.rows(a))) }
