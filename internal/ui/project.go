package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"gitlab-tui/internal/gitlab"
)

// projectView is a project's home page: details, merge requests, pipelines
// and tags as four panes. The panes (not their contents) are selectable;
// enter on one opens its full page.
type projectView struct {
	project string
	focus   int
}

const (
	ppDetails = iota
	ppMRs
	ppPipelines
	ppTags
)

func newProjectView(project string) *projectView { return &projectView{project: project, focus: ppMRs} }

// NewProject opens a project's home page.
func NewProject(project string) View { return newProjectView(project) }

func (v *projectView) title() string {
	if i := strings.LastIndexByte(v.project, '/'); i >= 0 {
		return v.project[i+1:]
	}
	return v.project
}

func (v *projectView) proj() string    { return v.project }
func (v *projectView) capturing() bool { return false }

func tagsKey(p string) string { return "tags:" + p }

func (v *projectView) mrsKey() string { return fmt.Sprintf("mrs:%s:%s", v.project, gitlab.MRAll) }

func (v *projectView) refresh(a *App, force bool) tea.Cmd {
	p := v.project
	age := func(d time.Duration) time.Duration {
		if force {
			return 0
		}
		return d
	}
	pipeAge := 30 * time.Second
	if ps, _ := get[[]gitlab.Pipeline](a.store, pipesKey(p, "")); anyActive(ps) {
		pipeAge = 5 * time.Second
	}
	cmds := []tea.Cmd{
		fetch(a.store, projInfoKey(p), age(2*time.Minute), func(ctx ctxT) (*gitlab.ProjectInfo, error) { return a.client.GetProject(ctx, p) }),
		fetch(a.store, v.mrsKey(), age(30*time.Second), func(ctx ctxT) ([]gitlab.MRSummary, error) {
			return a.client.ListMRs(ctx, p, gitlab.MRAll)
		}),
		fetch(a.store, pipesKey(p, ""), age(pipeAge), func(ctx ctxT) ([]gitlab.Pipeline, error) {
			return a.client.ListPipelines(ctx, p, "")
		}),
		fetch(a.store, tagsKey(p), age(2*time.Minute), func(ctx ctxT) ([]gitlab.Tag, error) { return a.client.ListTags(ctx, p) }),
	}
	if info, _ := get[*gitlab.ProjectInfo](a.store, projInfoKey(p)); info != nil {
		a.learnProject(p, info.ID)
		if info.DefaultBranch != "" {
			cmds = append(cmds, fetchLatest(a, p, info.DefaultBranch, age(30*time.Second)))
		}
	}
	return tea.Batch(cmds...)
}

func (v *projectView) help() []kb {
	return []kb{{"tab/1-4", "pane"}, {"enter", "open"}, {"c", "new MR"}, {"o", "browser"}, {"y", "copy URL"}}
}

func (v *projectView) open(a *App, pane int) tea.Cmd {
	switch pane {
	case ppMRs:
		return a.push(newMRList(v.project))
	case ppPipelines:
		return a.push(newPipelineList(v.project, ""))
	case ppTags:
		return a.push(newTagList(v.project))
	}
	return nil
}

func (v *projectView) webURL(a *App) string {
	if info, _ := get[*gitlab.ProjectInfo](a.store, projInfoKey(v.project)); info != nil && info.WebURL != "" {
		return info.WebURL
	}
	return a.ctx.WebBase + "/" + v.project
}

func (v *projectView) key(a *App, msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "tab":
		v.focus = (v.focus + 1) % 4
	case "shift+tab":
		v.focus = (v.focus + 3) % 4
	case "1", "2", "3", "4":
		v.focus = int(msg.String()[0] - '1')
	case "h", "left", "l", "right":
		v.focus ^= 1 // panes are laid out [0 1] / [2 3]
	case "j", "down", "k", "up":
		v.focus ^= 2
	case "enter":
		return v.open(a, v.focus)
	case "c":
		return a.push(newMRForm(v.project, a.branchFor(v.project)))
	case "o":
		return openBrowser(v.webURL(a))
	case "y":
		return copyText(v.webURL(a))
	}
	return nil
}

func (v *projectView) progress(*App) string { return "" }

func (v *projectView) render(a *App, w, h int) string {
	info, ie := get[*gitlab.ProjectInfo](a.store, projInfoKey(v.project))
	mrs, me := get[[]gitlab.MRSummary](a.store, v.mrsKey())
	pipes, pe := get[[]gitlab.Pipeline](a.store, pipesKey(v.project, ""))
	tags, te := get[[]gitlab.Tag](a.store, tagsKey(v.project))

	mrTitle := ic.mr + " Merge requests"
	if mrs != nil {
		mrTitle += fmt.Sprintf(" (%d)", len(mrs))
	}
	tagTitle := ic.label + " Tags"
	if tags != nil {
		tagTitle += fmt.Sprintf(" (%d)", len(tags))
	}
	specs := [4]struct {
		title string
		lines func(iw, rows int) []string
	}{
		{ic.project + " Details", func(iw, rows int) []string { return v.detailLines(a, info, ie, iw) }},
		{mrTitle, func(iw, rows int) []string { return v.mrLines(a, mrs, me, iw, rows) }},
		{ic.pipeline + " Pipelines", func(iw, rows int) []string { return v.pipeLines(pipes, pe, iw, rows) }},
		{tagTitle, func(iw, rows int) []string { return v.tagLines(tags, te, iw, rows) }},
	}
	renderPane := func(i, pw, ph int) string {
		title := fmt.Sprintf("%d %s", i+1, specs[i].title)
		if i != ppDetails {
			title += sDim.Render("  ↵")
		}
		box := pane(title, specs[i].lines(pw-4, ph-2), pw, ph, v.focus == i)
		return a.zone(box, zone{click: func(double bool) tea.Cmd {
			v.focus = i
			if double {
				return v.open(a, i)
			}
			return nil
		}})
	}
	if w >= 100 {
		lw, th := w/2, h/2
		top := joinHoriz(renderPane(0, lw, th), renderPane(1, w-lw, th))
		bot := joinHoriz(renderPane(2, lw, h-th), renderPane(3, w-lw, h-th))
		return top + "\n" + bot
	}
	ph := h / 4
	var parts []string
	for i := 0; i < 4; i++ {
		hh := ph
		if i == 3 {
			hh = h - 3*ph
		}
		parts = append(parts, renderPane(i, w, hh))
	}
	return strings.Join(parts, "\n")
}

func (v *projectView) detailLines(a *App, info *gitlab.ProjectInfo, e *entry, w int) []string {
	if info == nil {
		return []string{emptyMsg(e, "")}
	}
	var L []string
	head := sTitle.Render(info.Name)
	if info.Archived {
		head += "  " + sWarn.Render("archived")
	}
	L = append(L, fit(head, w), sDim.Render(fit(info.PathWithNamespace, w)))
	if d := strings.TrimSpace(info.Description); d != "" {
		wrapped := wrapText(d, w)
		if len(wrapped) > 2 {
			wrapped = append(wrapped[:2:2], sDim.Render("…"))
		}
		L = append(L, wrapped...)
	}
	L = append(L, "")

	var rows [][]string
	kv := func(k, val string) { rows = append(rows, []string{sHeader.Render(k), val}) }
	if info.DefaultBranch != "" {
		branch := sKey.Render(info.DefaultBranch)
		if ps, _ := get[[]gitlab.Pipeline](a.store, latestKey(v.project, info.DefaultBranch)); len(ps) > 0 {
			branch += "  " + statusLabel(ps[0].Status, false) + sDim.Render("  "+since(ps[0].CreatedAt)+" ago")
		}
		kv("Default branch", branch)
	} else if info.EmptyRepo {
		kv("Repository", sDim.Render("empty"))
	}
	kv("Visibility", info.Visibility)
	kv("Last activity", since(info.LastActivityAt)+" ago")
	kv("Created", info.CreatedAt.Format("2 Jan 2006"))
	if info.OpenIssuesCount > 0 {
		kv("Open issues", fmt.Sprint(info.OpenIssuesCount))
	}
	if st := info.Statistics; st != nil {
		kv("Repository", fmt.Sprintf("%d commits · %s", st.CommitCount, humanBytes(st.RepositorySize)))
	}
	if info.StarCount > 0 || info.ForksCount > 0 {
		kv("Stars / forks", fmt.Sprintf("%d / %d", info.StarCount, info.ForksCount))
	}
	if len(info.Topics) > 0 {
		kv("Topics", sAccent.Render(strings.Join(info.Topics, " · ")))
	}
	if info.SSHURL != "" {
		kv("Clone", sDim.Render(info.SSHURL))
	}
	t := newTable([]col{{}, {flex: true}}, rows, w)
	for _, r := range rows {
		L = append(L, t.row(r...))
	}
	return L
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

func (v *projectView) mrLines(a *App, mrs []gitlab.MRSummary, e *entry, w, rows int) []string {
	if mrs == nil {
		return []string{emptyMsg(e, "")}
	}
	if len(mrs) == 0 {
		return []string{sDim.Render("No open merge requests.")}
	}
	drafts, ready, conflicts, yours := 0, 0, 0, 0
	me, _ := get[*gitlab.User](a.store, meKey)
	for _, m := range mrs {
		switch {
		case m.Draft:
			drafts++
		case m.MergeStatus == "mergeable":
			ready++
		}
		if m.Conflicts {
			conflicts++
		}
		if me != nil && m.Author == me.Username {
			yours++
		}
	}
	parts := []string{fmt.Sprintf("%d open", len(mrs))}
	if ready > 0 {
		parts = append(parts, sOK.Render(fmt.Sprintf("%d ready", ready)))
	}
	if drafts > 0 {
		parts = append(parts, sDim.Render(fmt.Sprintf("%d draft", drafts)))
	}
	if conflicts > 0 {
		parts = append(parts, sErr.Render(fmt.Sprintf("%d conflicting", conflicts)))
	}
	if yours > 0 {
		parts = append(parts, sKey.Render(fmt.Sprintf("%d yours", yours)))
	}
	L := []string{fit(strings.Join(parts, sDim.Render(" · ")), w), ""}
	cells := make([][]string, 0, len(mrs))
	for _, m := range mrs {
		title := m.Title
		if m.Draft {
			title = sDim.Render(ic.draft+" ") + strings.TrimSpace(draftRe.ReplaceAllString(title, ""))
		}
		cells = append(cells, []string{statusIcon(m.PipelineStatus, false), sDim.Render(fmt.Sprintf("!%d", m.IID)), title,
			sDim.Render("@" + m.Author), sDim.Render(since(m.UpdatedAt))})
	}
	cells = cells[:min(len(cells), max(0, rows-len(L)))]
	t := newTable([]col{{}, {}, {flex: true}, {max: 16, optional: true}, {right: true}}, cells, w)
	for _, c := range cells {
		L = append(L, t.row(c...))
	}
	return L
}

func (v *projectView) pipeLines(pipes []gitlab.Pipeline, e *entry, w, rows int) []string {
	if pipes == nil {
		return []string{emptyMsg(e, "")}
	}
	if len(pipes) == 0 {
		return []string{sDim.Render("No pipelines.")}
	}
	running, failed := 0, 0
	day := time.Now().Add(-24 * time.Hour)
	for _, p := range pipes {
		if isActive(p.Status) {
			running++
		}
		if p.Status == "failed" && p.CreatedAt.After(day) {
			failed++
		}
	}
	var parts []string
	if running > 0 {
		parts = append(parts, statusOf("running", false).style.Render(fmt.Sprintf("%d running", running)))
	}
	if failed > 0 {
		parts = append(parts, sErr.Render(fmt.Sprintf("%d failed today", failed)))
	}
	if len(parts) == 0 {
		parts = append(parts, sDim.Render("nothing running"))
	}
	L := []string{fit(strings.Join(parts, sDim.Render(" · ")), w), ""}
	cells := make([][]string, 0, len(pipes))
	for _, p := range pipes {
		cells = append(cells, []string{statusLabel(p.Status, false), sDim.Render(fmt.Sprintf("#%d", p.ID)),
			sKey.Render(p.Ref), sDim.Render(since(p.CreatedAt))})
	}
	cells = cells[:min(len(cells), max(0, rows-len(L)))]
	t := newTable([]col{{}, {optional: true}, {flex: true}, {right: true}}, cells, w)
	for _, c := range cells {
		L = append(L, t.row(c...))
	}
	return L
}

func (v *projectView) tagLines(tags []gitlab.Tag, e *entry, w, rows int) []string {
	if tags == nil {
		return []string{emptyMsg(e, "")}
	}
	if len(tags) == 0 {
		return []string{sDim.Render("No tags.")}
	}
	releases := 0
	for _, t := range tags {
		if t.Release != nil {
			releases++
		}
	}
	latest := tags[0]
	summary := "latest " + sBold.Render(latest.Name) + sDim.Render(" · "+since(latest.When())+" ago")
	if releases > 0 {
		summary += sDim.Render(fmt.Sprintf(" · %d %s", releases, plural(releases, "release")))
	}
	L := []string{fit(summary, w), ""}
	cells := make([][]string, 0, len(tags))
	for _, t := range tags {
		cells = append(cells, tagCells(t))
	}
	cells = cells[:min(len(cells), max(0, rows-len(L)))]
	tb := newTable([]col{{max: 30}, {}, {flex: true, optional: true}, {right: true}}, cells, w)
	for _, c := range cells {
		L = append(L, tb.row(c...))
	}
	return L
}

// tagCells is a tag's row: name, release marker, message, age.
func tagCells(t gitlab.Tag) []string {
	rel := ""
	if t.Release != nil {
		rel = sOK.Render("release")
	}
	msg := strings.TrimSpace(t.Message)
	if msg == "" {
		msg = t.Commit.Title
	}
	if i := strings.IndexByte(msg, '\n'); i >= 0 {
		msg = msg[:i]
	}
	return []string{sKey.Render(t.Name), rel, sDim.Render(msg), sDim.Render(since(t.When()))}
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}
