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

// testsPopup floats a pipeline's test report (its JUnit artifacts) over the
// page: suites with their tests on one side, the highlighted test's output
// on the other. It opens on the failures when there are any.

type testFilter int

const (
	testsFailed testFilter = iota // failed and errored
	testsAll
	testsSkipped
	nTestFilters
)

func (f testFilter) String() string {
	return [...]string{"failed", "all", "skipped"}[f]
}

func (f testFilter) keep(status string) bool {
	switch f {
	case testsFailed:
		return status == "failed" || status == "error"
	case testsSkipped:
		return status == "skipped"
	}
	return true
}

type testsPopup struct {
	project  string
	pipeline int
	webURL   string // the pipeline's page; its Tests tab is webURL+"/test_report"

	filter    testFilter
	chosen    bool // filter picked (by default or by hand) once the report loaded
	collapsed map[string]bool
	l         listState

	detail          bool // focus is on the output pane
	dscroll, dlines int
	dheight         int
}

func testsKey(p string, id int) string   { return fmt.Sprintf("tests:%s:%d", p, id) }
func testSumKey(p string, id int) string { return fmt.Sprintf("testsum:%s:%d", p, id) }

// fetchTestSummary loads the pipeline's test counts; finished pipelines'
// results don't change, so they're kept for a while.
func fetchTestSummary(a *App, p string, pl *gitlab.Pipeline, force bool) tea.Cmd {
	age := 10 * time.Minute
	if isActive(pl.Status) {
		age = 30 * time.Second
	}
	if force {
		age = 0
	}
	id := pl.ID
	return fetch(a.store, testSumKey(p, id), age, func(ctx ctxT) (*gitlab.TestSummary, error) {
		return a.client.TestReportSummary(ctx, p, id)
	})
}

// testsFact is the "Tests" line for a pipeline: counts, coloured.
func testsFact(a *App, p string, id int) string {
	s, e := get[*gitlab.TestSummary](a.store, testSumKey(p, id))
	if s == nil {
		if e != nil && e.err != nil {
			return sDim.Render("unavailable")
		}
		return sDim.Render("…")
	}
	t := s.Total
	if t.Count == 0 {
		return sDim.Render("no test reports")
	}
	return testCounts(t.Count, t.Success, t.Failed, t.Error, t.Skipped) + sDim.Render(" · T to view")
}

func testCounts(total, ok, failed, errored, skipped int) string {
	parts := []string{fmt.Sprintf("%d %s", total, plural(total, "test"))}
	if failed > 0 {
		parts = append(parts, sErr.Render(fmt.Sprintf("%s %d failed", ic.status["failed"], failed)))
	}
	if errored > 0 {
		parts = append(parts, sErr.Render(fmt.Sprintf("%s %d errors", ic.status["failed"], errored)))
	}
	if skipped > 0 {
		parts = append(parts, sDim.Render(fmt.Sprintf("%s %d skipped", ic.status["skipped"], skipped)))
	}
	if ok > 0 {
		parts = append(parts, sOK.Render(fmt.Sprintf("%s %d passed", ic.status["success"], ok)))
	}
	return strings.Join(parts, sDim.Render(" · "))
}

func (a *App) openTests(p string, pl *gitlab.Pipeline) tea.Cmd {
	if pl == nil {
		a.setFlash("no pipeline to show tests for", true)
		return nil
	}
	tp := &testsPopup{project: p, pipeline: pl.ID, webURL: pl.WebURL, collapsed: map[string]bool{}, l: newListState()}
	a.overlay = tp
	return tp.refresh(a, false)
}

func (tp *testsPopup) refresh(a *App, force bool) tea.Cmd {
	age := 5 * time.Minute
	if force {
		age = 0
	}
	p, id := tp.project, tp.pipeline
	return tea.Batch(
		fetch(a.store, testsKey(p, id), age, func(ctx ctxT) (*gitlab.TestReport, error) {
			return a.client.TestReport(ctx, p, id)
		}),
		fetch(a.store, testSumKey(p, id), age, func(ctx ctxT) (*gitlab.TestSummary, error) {
			return a.client.TestReportSummary(ctx, p, id)
		}),
	)
}

// jobOf finds the job that produced a suite; only the summary has it.
func (tp *testsPopup) jobOf(a *App, s *gitlab.TestSuite) int {
	if len(s.BuildIDs) > 0 {
		return s.BuildIDs[0]
	}
	sum, _ := get[*gitlab.TestSummary](a.store, testSumKey(tp.project, tp.pipeline))
	if sum != nil {
		for _, ss := range sum.TestSuites {
			if ss.Name == s.Name && len(ss.BuildIDs) > 0 {
				return ss.BuildIDs[0]
			}
		}
	}
	return 0
}

func (tp *testsPopup) debounced(*App, int) tea.Cmd { return nil }

func (tp *testsPopup) passthrough(msg tea.Msg) tea.Cmd {
	if !tp.l.filtering {
		return nil
	}
	var cmd tea.Cmd
	tp.l.filter, cmd = tp.l.filter.Update(msg)
	return cmd
}

func (tp *testsPopup) help() []kb {
	if tp.l.filtering {
		return []kb{{"enter", "keep filter"}, {"esc", "clear"}}
	}
	return []kb{{"f", "failed/all/skipped"}, {"/", "search"}, {"enter", "expand/output"}, {"tab", "output"},
		{"]/[", "next/prev failure"}, {"J", "job log"}, {"y", "copy output"}, {"o", "browser"}, {"esc", "close"}}
}

// testRow is a suite heading or one of its tests.
type testRow struct {
	suite *gitlab.TestSuite
	tc    *gitlab.TestCase
	shown int // a heading's tests that pass the filter
}

func testRank(status string) int {
	switch status {
	case "failed", "error":
		return 0
	case "skipped":
		return 1
	}
	return 2
}

// rows lists the suites with tests passing the filter and search, failures
// first within each suite.
func (tp *testsPopup) rows(r *gitlab.TestReport) []testRow {
	if r == nil {
		return nil
	}
	var out []testRow
	for si := range r.TestSuites {
		s := &r.TestSuites[si]
		var cases []*gitlab.TestCase
		for ci := range s.TestCases {
			c := &s.TestCases[ci]
			if tp.filter.keep(c.Status) && matches(tp.l.query, c.Name, c.Classname, c.File, s.Name) {
				cases = append(cases, c)
			}
		}
		if len(cases) == 0 && !(s.SuiteError != "" && tp.filter != testsSkipped) {
			continue
		}
		sort.SliceStable(cases, func(i, j int) bool { return testRank(cases[i].Status) < testRank(cases[j].Status) })
		out = append(out, testRow{suite: s, shown: len(cases)})
		if !tp.folded(s) {
			for _, c := range cases {
				out = append(out, testRow{suite: s, tc: c})
			}
		}
	}
	return out
}

// folded reports whether a suite's tests are hidden; a search opens them all.
func (tp *testsPopup) folded(s *gitlab.TestSuite) bool {
	return tp.collapsed[s.Name] && tp.l.query == ""
}

func (tp *testsPopup) report(a *App) (*gitlab.TestReport, *entry) {
	return get[*gitlab.TestReport](a.store, testsKey(tp.project, tp.pipeline))
}

// pickFilter starts on the failures, or everything when nothing failed.
func (tp *testsPopup) pickFilter(r *gitlab.TestReport) {
	if tp.chosen || r == nil {
		return
	}
	tp.chosen = true
	if r.FailedCount+r.ErrorCount == 0 {
		tp.filter = testsAll
	}
	// a big report opens with its suites folded
	if len(r.TestSuites) > 1 {
		shown := 0
		for _, s := range r.TestSuites {
			for _, c := range s.TestCases {
				if tp.filter.keep(c.Status) {
					shown++
				}
			}
		}
		if shown > 200 {
			for _, s := range r.TestSuites {
				tp.collapsed[s.Name] = true
			}
		}
	}
}

func (tp *testsPopup) key(a *App, msg tea.KeyMsg) tea.Cmd {
	r, _ := tp.report(a)
	tp.pickFilter(r)
	if tp.l.filtering {
		cmd, changed := tp.l.filterKey(msg)
		if changed {
			tp.moved()
		}
		return cmd
	}
	// a burst of typed runes (key repeat) can arrive as one message
	if msg.Type == tea.KeyRunes && len(msg.Runes) > 1 && !msg.Paste {
		var cmds []tea.Cmd
		for _, r := range msg.Runes {
			cmds = append(cmds, tp.key(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}))
		}
		return tea.Batch(cmds...)
	}
	rows := tp.rows(r)
	var cur *testRow
	if tp.l.cursor >= 0 && tp.l.cursor < len(rows) {
		cur = &rows[tp.l.cursor]
	}
	key := msg.String()
	switch key {
	case "esc", "q", "T":
		if tp.detail && key == "esc" {
			tp.detail = false
			return nil
		}
		a.overlay = nil
		return nil
	case "tab", "shift+tab":
		tp.detail = !tp.detail
		return nil
	case "f":
		tp.filter = (tp.filter + 1) % nTestFilters
		tp.l.cursor, tp.l.offset = 0, 0
		tp.moved()
		return nil
	case "/":
		tp.detail = false
		return tp.l.startFilter()
	case "o":
		if tp.webURL != "" {
			return openBrowser(tp.webURL + "/test_report")
		}
		return nil
	case "y":
		if cur != nil && cur.tc != nil {
			out := testOutput(cur.tc)
			if out == "" {
				out = cur.tc.Name
			}
			return copyTextAs(out, "copied the output of "+cur.tc.Name)
		}
		return nil
	case "J":
		job := 0
		if cur != nil {
			job = tp.jobOf(a, cur.suite)
		}
		if job == 0 {
			a.setFlash("no job found for this suite", true)
			return nil
		}
		a.overlay = nil
		return a.push(newTraceView(tp.project, job, cur.suite.Name))
	}

	if tp.detail {
		page := max(1, tp.dheight)
		switch key {
		case "j", "down":
			tp.dscroll++
		case "k", "up":
			tp.dscroll--
		case "ctrl+d":
			tp.dscroll += page / 2
		case "ctrl+u":
			tp.dscroll -= page / 2
		case "pgdown", "ctrl+f", " ":
			tp.dscroll += page
		case "pgup", "ctrl+b":
			tp.dscroll -= page
		case "g", "home":
			tp.dscroll = 0
		case "G", "end":
			tp.dscroll = tp.dlines
		case "h", "left", "enter":
			tp.detail = false
		}
		tp.dscroll = max(0, min(tp.dscroll, tp.dlines-tp.dheight))
		return nil
	}

	if handled, moved := tp.l.nav(key, len(rows)); handled {
		if moved {
			tp.moved()
		}
		return nil
	}
	switch key {
	case "]", "[":
		dir := 1
		if key == "[" {
			dir = -1
		}
		for i := tp.l.cursor + dir; i >= 0 && i < len(rows); i += dir {
			if tc := rows[i].tc; tc != nil && testRank(tc.Status) == 0 {
				tp.l.cursor = i
				tp.moved()
				return nil
			}
		}
		a.setFlash("no more failures", false)
	case "enter", " ", "l", "right":
		if cur == nil {
			return nil
		}
		if cur.tc == nil {
			if key == "l" || key == "right" {
				if !tp.folded(cur.suite) {
					tp.l.cursor++
					tp.moved()
				}
				delete(tp.collapsed, cur.suite.Name)
				return nil
			}
			tp.collapsed[cur.suite.Name] = !tp.collapsed[cur.suite.Name]
			return nil
		}
		tp.detail = true
	case "h", "left":
		if cur == nil {
			return nil
		}
		if cur.tc == nil {
			tp.collapsed[cur.suite.Name] = true
			return nil
		}
		for i := tp.l.cursor; i >= 0; i-- {
			if rows[i].tc == nil {
				tp.l.cursor = i
				tp.moved()
				break
			}
		}
	case "z":
		// fold or unfold every suite
		all := true
		for _, row := range rows {
			if row.tc == nil && !tp.collapsed[row.suite.Name] {
				all = false
			}
		}
		for _, s := range r.TestSuites {
			tp.collapsed[s.Name] = !all
		}
		tp.l.cursor = 0
		tp.moved()
	}
	return nil
}

func (tp *testsPopup) moved() { tp.dscroll = 0 }

// testOutput is what a test printed: its output and stack trace.
func testOutput(tc *gitlab.TestCase) string {
	var parts []string
	if s := strings.TrimSpace(tc.SystemOutput); s != "" {
		parts = append(parts, s)
	}
	if s := strings.TrimSpace(tc.StackTrace); s != "" {
		parts = append(parts, s)
	}
	return strings.Join(parts, "\n\n")
}

func testIcon(status string) string {
	switch status {
	case "error":
		return sErr.Render(ic.status["failed"])
	case "success", "failed", "skipped":
		return statusIcon(status, false)
	}
	return sDim.Render("?")
}

func secs(f float64) string {
	if f <= 0 {
		return ""
	}
	if f < 1 {
		return fmt.Sprintf("%dms", int(f*1000))
	}
	if f < 60 {
		return fmt.Sprintf("%.1fs", f)
	}
	return fmtDur(time.Duration(f * float64(time.Second)))
}

func (tp *testsPopup) render(a *App, body string, w, h int) string {
	r, e := tp.report(a)
	tp.pickFilter(r)
	bw := max(min(w, 50), w*9/10)
	bh := max(min(h, 12), h*9/10)
	x, y := (w-bw)/2, (h-bh)/2

	// summary across the top
	head := sDim.Render(spinnerFrame() + " loading test report…")
	if r != nil {
		head = testCounts(r.TotalCount, r.SuccessCount, r.FailedCount, r.ErrorCount, r.SkippedCount)
		if t := secs(r.TotalTime); t != "" {
			head += sDim.Render("  " + ic.clock + " " + t)
		}
	} else if e != nil && e.err != nil {
		head = sErr.Render(e.err.Error())
	}
	var tabs []string
	for f := testFilter(0); f < nTestFilters; f++ {
		if f == tp.filter {
			tabs = append(tabs, sActiveT.Render(f.String()))
		} else {
			tabs = append(tabs, sDim.Render(f.String()))
		}
	}
	right := strings.Join(tabs, sDim.Render(" │ ")) + sDim.Render("  f")
	if line := tp.l.filterLine(); line != "" {
		right = line + "  " + right
	}
	inner := bw - 4
	headLine := pad(fit(head, inner-ansi.StringWidth(right)-2), inner-ansi.StringWidth(right)) + right
	top := pane(fmt.Sprintf("%s Tests · pipeline #%d", ic.check, tp.pipeline), []string{headLine}, bw, 3, true)

	rows := tp.rows(r)
	side := bw >= 110
	listW, listH := bw, (bh-3)/2
	detW, detH := bw, bh-3-listH
	if side {
		listW, listH = bw*45/100, bh-3
		detW, detH = bw-listW, bh-3
	}

	// suites and tests
	lrows := listH - 2
	start, end := tp.l.window(len(rows), lrows)
	lw := listW - 4
	var lines []string
	for i := start; i < end; i++ {
		row := rows[i]
		sel := i == tp.l.cursor
		mark := "  "
		if sel {
			mark = sActive.Render(ic.caret) + " "
		}
		var left, rt string
		if row.tc == nil {
			s := row.suite
			arrow := "▾"
			if tp.folded(s) {
				arrow = "▸"
			}
			left = sDim.Render(arrow) + " " + sBold.Render(sanitize(s.Name))
			bad := s.FailedCount + s.ErrorCount
			switch {
			case s.SuiteError != "":
				rt = sErr.Render("suite error")
			case bad > 0:
				rt = sErr.Render(fmt.Sprintf("%s %d", ic.status["failed"], bad)) + sDim.Render(fmt.Sprintf("/%d", s.TotalCount))
			case s.TotalCount > 0:
				rt = sOK.Render(fmt.Sprintf("%s %d", ic.status["success"], s.SuccessCount)) + sDim.Render(fmt.Sprintf("/%d", s.TotalCount))
			}
			if row.shown != s.TotalCount && tp.folded(s) {
				rt = sDim.Render(fmt.Sprintf("%d shown  ", row.shown)) + rt
			}
		} else {
			left = "  " + testIcon(row.tc.Status) + " " + sanitize(row.tc.Name)
			if row.tc.Classname != "" {
				left += " " + sDim.Render(sanitize(row.tc.Classname))
			}
			rt = sDim.Render(secs(row.tc.ExecutionTime))
		}
		line := mark + left
		if rt != "" {
			line = pad(line, lw-ansi.StringWidth(rt)-len(colGap)) + colGap + rt
		}
		if sel {
			line = selectLine(line, lw)
		}
		i := i
		lines = append(lines, tp.l.clickRow(a, line, i,
			func() tea.Cmd { tp.detail = false; tp.moved(); return nil },
			func() tea.Cmd { tp.l.cursor = i; return tp.key(a, keyEnter) }))
	}
	if len(rows) == 0 && r != nil {
		msg := "No " + tp.filter.String() + " tests"
		if r.TotalCount == 0 {
			msg = "This pipeline has no test reports. Jobs publish them with artifacts:reports:junit."
		} else if tp.l.query != "" {
			msg = "No tests match /" + tp.l.query
		} else if tp.filter != testsAll {
			msg += " · f shows all"
		}
		lines = append(lines, sDim.Render(fit(msg, lw)))
	}
	listTitle := "Suites"
	if r != nil {
		listTitle = fmt.Sprintf("Suites (%d)", len(r.TestSuites))
	}
	list := a.zone(pane(listTitle, lines, listW, listH, !tp.detail), zone{
		scroll: func(dir int) tea.Cmd {
			tp.l.cursor = max(0, min(len(rows)-1, tp.l.cursor+3*dir))
			tp.moved()
			return nil
		},
	})

	// the highlighted test's output
	var cur *testRow
	if tp.l.cursor >= 0 && tp.l.cursor < len(rows) {
		cur = &rows[tp.l.cursor]
	}
	job := 0
	if cur != nil {
		job = tp.jobOf(a, cur.suite)
	}
	dl := testDetail(cur, job, detW-4)
	tp.dheight = detH - 2
	tp.dlines = len(dl)
	tp.dscroll = max(0, min(tp.dscroll, tp.dlines-tp.dheight))
	dl = dl[tp.dscroll:min(len(dl), tp.dscroll+tp.dheight)]
	detTitle := "Output"
	if tp.dlines > tp.dheight {
		detTitle += sDim.Render(" " + textProgress(tp.dscroll, tp.dheight, tp.dlines))
	}
	det := a.zone(pane(detTitle, dl, detW, detH, tp.detail), zone{
		click: func(bool) tea.Cmd { tp.detail = true; return nil },
		scroll: func(dir int) tea.Cmd {
			tp.dscroll = max(0, min(tp.dscroll+3*dir, tp.dlines-tp.dheight))
			return nil
		},
	})

	var box string
	if side {
		box = top + "\n" + joinHoriz(list, det)
	} else {
		box = top + "\n" + list + "\n" + det
	}
	return overlayAt(body, a.zone(box, zone{}), x, y)
}

// testDetail describes a suite or a test: what it is, then its output.
func testDetail(row *testRow, job int, w int) []string {
	if row == nil {
		return nil
	}
	var L []string
	kv := func(k, v string) {
		if v != "" {
			L = append(L, fit(sHeader.Render(pad(k, 7))+"  "+v, w))
		}
	}
	if row.tc == nil {
		s := row.suite
		L = append(L, wrapText(sBold.Render(s.Name), w)...)
		L = append(L, "")
		kv("Tests", testCounts(s.TotalCount, s.SuccessCount, s.FailedCount, s.ErrorCount, s.SkippedCount))
		kv("Time", secs(s.TotalTime))
		if job != 0 {
			kv("Job", fmt.Sprintf("#%d", job)+sDim.Render(" · J opens its log"))
		}
		if s.SuiteError != "" {
			L = append(L, "", rule("Suite error", w))
			L = append(L, wrapText(sErr.Render(s.SuiteError), w)...)
		}
		return L
	}
	tc := row.tc
	L = append(L, wrapText(sBold.Render(tc.Name), w)...)
	L = append(L, "")
	label := statusLabel(tc.Status, false)
	if tc.Status == "error" {
		label = sErr.Render(ic.status["failed"] + " error")
	}
	kv("Status", label)
	kv("Class", sanitize(tc.Classname))
	kv("File", sanitize(tc.File))
	kv("Time", secs(tc.ExecutionTime))
	kv("Suite", sanitize(row.suite.Name))
	if job != 0 {
		kv("Job", fmt.Sprintf("#%d", job)+sDim.Render(" · J opens its log"))
	}
	if s := strings.TrimSpace(tc.SystemOutput); s != "" {
		L = append(L, "", rule("Output", w))
		L = append(L, wrapText(s, w)...)
	}
	if s := strings.TrimSpace(tc.StackTrace); s != "" {
		L = append(L, "", rule("Stack trace", w))
		L = append(L, wrapText(s, w)...)
	}
	if testOutput(tc) == "" {
		L = append(L, "", sDim.Render("No output recorded."))
	}
	return L
}
