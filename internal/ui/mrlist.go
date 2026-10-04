package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"gitlab-tui/internal/gitlab"
)

var mrFilters = []gitlab.MRFilter{gitlab.MRAll, gitlab.MRMine, gitlab.MRReviewer, gitlab.MRMerged, gitlab.MRClosed}

var mrFilterLabels = []string{"All open", "Mine", "Review requested", "Merged", "Closed"}

// finished reports whether the tab lists merged/closed MRs.
func (v *mrListView) finished() bool {
	f := mrFilters[v.filter]
	return f == gitlab.MRMerged || f == gitlab.MRClosed
}

type mrListView struct {
	project string
	filter  int
	l       listState
	seq     int
}

func newMRList(project string) *mrListView {
	return &mrListView{project: project, l: newListState()}
}

// NewMRList lists a project's open merge requests.
func NewMRList(project string) view { return newMRList(project) }

func (v *mrListView) proj() string { return v.project }

func (v *mrListView) title() string { return "merge requests" }

func (v *mrListView) capturing() bool { return v.l.filtering }

func (v *mrListView) key_(a *App) string {
	return fmt.Sprintf("mrs:%s:%s", v.project, mrFilters[v.filter])
}

func (v *mrListView) refresh(a *App, force bool) tea.Cmd {
	age := 30 * time.Second
	if v.finished() {
		age = 2 * time.Minute
	}
	if force {
		age = 0
	}
	f, p := mrFilters[v.filter], v.project
	return fetch(a.store, v.key_(a), age, func(ctx ctxT) ([]gitlab.MRSummary, error) {
		return a.client.ListMRs(ctx, p, f)
	})
}

func (v *mrListView) rows(a *App) []gitlab.MRSummary {
	all, _ := get[[]gitlab.MRSummary](a.store, v.key_(a))
	if v.l.query == "" {
		return all
	}
	out := make([]gitlab.MRSummary, 0, len(all))
	for _, m := range all {
		if matches(v.l.query, fmt.Sprint(m.IID), m.Title, m.Author, m.SourceBranch) {
			out = append(out, m)
		}
	}
	return out
}

func (v *mrListView) selected(a *App) *gitlab.MRSummary {
	rows := v.rows(a)
	if v.l.cursor < len(rows) {
		return &rows[v.l.cursor]
	}
	return nil
}

func (v *mrListView) moved() tea.Cmd {
	v.seq++
	return debounce(v, v.seq, 200*time.Millisecond)
}

// debounced prefetches the hovered MR so opening it is instant.
func (v *mrListView) debounced(a *App, seq int) tea.Cmd {
	if seq != v.seq {
		return nil
	}
	m := v.selected(a)
	if m == nil {
		return nil
	}
	cmds := []tea.Cmd{prefetchMR(a, v.project, m.IID)}
	if m.PipelineID != 0 {
		cmds = append(cmds, prefetchPipeline(a, v.project, m.PipelineID))
	}
	return tea.Batch(cmds...)
}

func (v *mrListView) key(a *App, msg tea.KeyMsg) tea.Cmd {
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
	m := v.selected(a)
	switch msg.String() {
	case "/":
		return v.l.startFilter()
	case "tab", "]":
		return v.setFilter(a, (v.filter+1)%len(mrFilters))
	case "shift+tab", "[":
		return v.setFilter(a, (v.filter+len(mrFilters)-1)%len(mrFilters))
	case "1", "2", "3", "4", "5":
		return v.setFilter(a, int(msg.String()[0]-'1'))
	case "c":
		return a.push(newMRForm(v.project, a.branchFor(v.project)))
	case "B":
		return a.push(newPipelineList(v.project, a.branchFor(v.project)))
	}
	if m == nil {
		return nil
	}
	switch msg.String() {
	case "enter", "l", "right":
		return a.push(newMRDetail(v.project, m.IID, "", m))
	case "p":
		if m.PipelineID == 0 {
			a.setFlash("MR has no pipeline", true)
			return nil
		}
		return a.push(newPipelineView(v.project, m.PipelineID, ""))
	case "o":
		return openBrowser(m.WebURL)
	case "y":
		return copyText(m.WebURL)
	}
	return nil
}

func (v *mrListView) setFilter(a *App, f int) tea.Cmd {
	if f != v.filter {
		v.filter = f
		v.l.cursor, v.l.offset = 0, 0
	}
	return v.refresh(a, false)
}

func (v *mrListView) help() []kb {
	return []kb{{"enter", "open"}, {"p", "pipeline"}, {"tab", "filter"}, {"/", "search"}, {"c", "new MR"}, {"B", "branch pipelines"}, {"o", "browser"}}
}

func mergeStatusLabel(s string, conflicts bool) string {
	if conflicts {
		return sErr.Render("conflict")
	}
	switch s {
	case "mergeable":
		return sOK.Render("ready")
	case "not_approved":
		return sWarn.Render("needs approval")
	case "ci_still_running":
		return sDim.Render("ci running")
	case "ci_must_pass":
		return sErr.Render("ci must pass")
	case "discussions_not_resolved":
		return sWarn.Render("threads")
	case "draft_status":
		return sDim.Render("draft")
	case "need_rebase":
		return sWarn.Render("needs rebase")
	case "conflict", "broken_status":
		return sErr.Render("conflict")
	case "checking", "unchecked", "preparing", "approvals_syncing":
		return sDim.Render("checking…")
	case "blocked_status":
		return sWarn.Render("blocked")
	case "requested_changes":
		return sErr.Render("changes req.")
	case "jira_association_missing":
		return sWarn.Render("jira missing")
	case "not_open":
		return sDim.Render("closed")
	case "external_status_checks":
		return sWarn.Render("ext. checks")
	case "security_policy_violations", "security_policy_pipeline_check":
		return sWarn.Render("policy")
	case "locked_paths", "locked_lfs_files":
		return sWarn.Render("locked")
	case "merge_request_blocked", "commits_status":
		return sWarn.Render("blocked")
	}
	return sDim.Render(strings.ReplaceAll(s, "_", " "))
}

func (v *mrListView) render(a *App, w, h int) string {
	var b strings.Builder
	// filter tabs (keys 1-5, tab, or click)
	_, e := get[[]gitlab.MRSummary](a.store, v.key_(a))
	for i, l := range mrFilterLabels {
		t := sTabOff.Render(l)
		if i == v.filter {
			if rows, _ := get[[]gitlab.MRSummary](a.store, v.key_(a)); rows != nil {
				n := fmt.Sprint(len(rows))
				if len(rows) >= 100 {
					n = "100+" // the query fetches one page
				}
				l += " (" + n + ")"
			}
			t = sTabOn.Render(l)
		}
		b.WriteString(" " + a.zone(t, zone{click: func(bool) tea.Cmd { return v.setFilter(a, i) }}) + "  ")
	}
	b.WriteString(entryStatus(e))
	b.WriteString("\n")
	h--
	if fl := v.l.filterLine(); fl != "" {
		b.WriteString(fl + "\n")
		h--
	}

	rows := v.rows(a)
	if len(rows) == 0 {
		empty := "No open merge requests."
		switch mrFilters[v.filter] {
		case gitlab.MRMerged:
			empty = "No merged merge requests."
		case gitlab.MRClosed:
			empty = "No closed merge requests."
		}
		b.WriteString("\n" + emptyMsg(e, empty))
		return b.String()
	}

	when := "UPDATED"
	switch mrFilters[v.filter] {
	case gitlab.MRMerged:
		when = "MERGED"
	case gitlab.MRClosed:
		when = "CLOSED"
	}
	cells := make([][]string, len(rows))
	for i, m := range rows {
		title := m.Title
		if m.Draft {
			title = sDim.Render("Draft ") + strings.TrimSpace(draftRe.ReplaceAllString(title, ""))
		}
		status, at := mergeStatusLabel(m.MergeStatus, m.Conflicts), m.UpdatedAt
		if m.Approved {
			status = sOK.Render("✓") + " " + status
		}
		switch m.State {
		case "merged":
			status = sAccent.Render("merged")
		case "closed", "locked":
			status = sDim.Render("closed")
		}
		if v.finished() && !m.ClosedAt.IsZero() {
			at = m.ClosedAt
		}
		notes := ""
		if m.Notes > 0 {
			notes = fmt.Sprint(m.Notes)
		}
		cells[i] = []string{statusIcon(m.PipelineStatus, false), sDim.Render(fmt.Sprintf("!%d", m.IID)), title,
			sDim.Render("@" + m.Author), sDim.Render(m.SourceBranch), status, sDim.Render(notes), sDim.Render(since(at))}
	}
	t := newTable([]col{
		{}, {title: "MR"}, {title: "TITLE", flex: true}, {title: "AUTHOR", max: 18},
		{title: "BRANCH", max: 30, optional: true}, {title: "STATUS"}, {title: ic.comment, right: true}, {title: when, right: true},
	}, cells, w)
	b.WriteString(t.header() + "\n")
	h--

	start, end := v.l.window(len(rows), h)
	for i := start; i < end; i++ {
		line := t.row(cells[i]...)
		if i == v.l.cursor {
			line = selectLine(line, w)
		}
		b.WriteString(v.l.clickRow(a, line, i, v.moved, func() tea.Cmd { return v.key(a, keyEnter) }) + "\n")
	}
	return b.String()
}

// entryStatus is a short "updated 3s ago" / error indicator.
func entryStatus(e *entry) string {
	if e == nil {
		return ""
	}
	if e.err != nil {
		return sErr.Render(" ⚠ " + e.err.Error())
	}
	if e.disk {
		return sDim.Render(" (cached)")
	}
	return ""
}

func emptyMsg(e *entry, msg string) string {
	switch {
	case e == nil || (e.val == nil && e.loading):
		return sDim.Render("  " + spinnerFrame() + " loading…")
	case e.err != nil && e.val == nil:
		return sErr.Render("  " + e.err.Error())
	}
	return sDim.Render("  " + msg)
}

func (v *mrListView) progress(a *App) string { return listProgress(v.l.cursor, len(v.rows(a))) }
