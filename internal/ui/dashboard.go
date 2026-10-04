package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"gitlab-tui/internal/gitlab"
)

// dashboardView is the home screen: things that need the user's attention
// across every project, in four panes.
type dashboardView struct {
	focus int
	cur   [4]int
	off   [4]int
	rows  [4]int // visible rows per pane at last render
}

const (
	paneReview = iota
	paneTodo
	paneMine
	paneRecent
)

func newDashboard() *dashboardView { return &dashboardView{} }

const (
	dashKey  = "dashboard"
	todosKey = "todos"
)

func (v *dashboardView) title() string   { return "dashboard" }
func (v *dashboardView) proj() string    { return "" }
func (v *dashboardView) capturing() bool { return false }

func (v *dashboardView) refresh(a *App, force bool) tea.Cmd {
	age := time.Minute
	if force {
		age = 0
	}
	return tea.Batch(
		fetch(a.store, dashKey, age, a.client.DashboardMRs),
		fetch(a.store, todosKey, age, a.client.ListTodos),
	)
}

func (v *dashboardView) help() []kb {
	return []kb{{"tab/1-4", "pane"}, {"enter", "open"}, {"p", "pipeline"}, {"x", "done (to-do)"}, {"o", "browser"}}
}

func (v *dashboardView) data(a *App) (*gitlab.Dashboard, []gitlab.Todo) {
	d, _ := get[*gitlab.Dashboard](a.store, dashKey)
	t, _ := get[[]gitlab.Todo](a.store, todosKey)
	return d, t
}

func (v *dashboardView) count(a *App, p int) int {
	d, todos := v.data(a)
	switch p {
	case paneReview:
		if d != nil {
			return len(d.Review)
		}
	case paneMine:
		if d != nil {
			return len(d.Authored)
		}
	case paneTodo:
		return len(todos)
	case paneRecent:
		return len(a.recent)
	}
	return 0
}

func (v *dashboardView) selectedMR(a *App) *gitlab.MRSummary {
	d, _ := v.data(a)
	if d == nil {
		return nil
	}
	var list []gitlab.MRSummary
	switch v.focus {
	case paneReview:
		list = d.Review
	case paneMine:
		list = d.Authored
	default:
		return nil
	}
	if i := v.cur[v.focus]; i < len(list) {
		return &list[i]
	}
	return nil
}

func (v *dashboardView) selectedTodo(a *App) *gitlab.Todo {
	_, todos := v.data(a)
	if i := v.cur[paneTodo]; v.focus == paneTodo && i < len(todos) {
		return &todos[i]
	}
	return nil
}

func (v *dashboardView) key(a *App, msg tea.KeyMsg) tea.Cmd {
	n := v.count(a, v.focus)
	c := &v.cur[v.focus]
	page := max(1, v.rows[v.focus])
	switch msg.String() {
	case "tab":
		v.focus = (v.focus + 1) % 4
		return nil
	case "shift+tab":
		v.focus = (v.focus + 3) % 4
		return nil
	case "1", "2", "3", "4":
		v.focus = int(msg.String()[0] - '1')
		return nil
	case "h", "left", "l", "right":
		v.focus ^= 1 // panes are laid out [0 1] / [2 3]
		return nil
	case "j", "down":
		*c++
	case "k", "up":
		*c--
	case "g", "home":
		*c = 0
	case "G", "end":
		*c = n - 1
	case "ctrl+d":
		*c += page / 2
	case "ctrl+u":
		*c -= page / 2
	}
	*c = max(0, min(*c, n-1))

	if mr := v.selectedMR(a); mr != nil {
		switch msg.String() {
		case "enter":
			return a.push(newMRDetail(mr.Project, mr.IID, "", mr))
		case "p":
			if mr.PipelineID == 0 {
				a.setFlash("MR has no pipeline", true)
				return nil
			}
			return a.push(newPipelineView(mr.Project, mr.PipelineID, ""))
		case "o":
			return openBrowser(mr.WebURL)
		case "y":
			return copyText(mr.WebURL)
		}
	}
	if t := v.selectedTodo(a); t != nil {
		project := ""
		if t.Project != nil {
			project = t.Project.PathWithNamespace
		}
		switch msg.String() {
		case "enter":
			if t.TargetType == "MergeRequest" && project != "" {
				return a.push(newMRDetail(project, t.Target.IID, "", nil))
			}
			return openBrowser(t.TargetURL)
		case "o":
			return openBrowser(t.TargetURL)
		case "y":
			return copyText(t.TargetURL)
		case "x":
			id := t.ID
			return a.action("mark to-do done", func() error { return a.client.MarkTodoDone(bg(), id) }, []string{todosKey}, nil)
		}
	}
	if msg.String() == "X" && v.focus == paneTodo {
		a.confirm("Mark all to-dos as done?", func() tea.Cmd {
			return a.action("mark all to-dos done", func() error { return a.client.MarkAllTodosDone(bg()) }, []string{todosKey}, nil)
		})
		return nil
	}
	if v.focus == paneRecent && v.cur[paneRecent] < len(a.recent) {
		p := a.recent[v.cur[paneRecent]].Path
		switch msg.String() {
		case "enter":
			return a.push(newProjectView(p))
		case "o":
			return openBrowser(a.ctx.WebBase + "/" + p)
		}
	}
	return nil
}

func (v *dashboardView) render(a *App, w, h int) string {
	d, todos := v.data(a)
	_, de := get[*gitlab.Dashboard](a.store, dashKey)
	_, te := get[[]gitlab.Todo](a.store, todosKey)

	// who you are lives in the status line; only problems get a line here
	head := ""
	if (de != nil && de.err != nil) || (te != nil && te.err != nil) {
		head = strings.TrimSpace(entryStatus(de) + entryStatus(te))
		h--
	}

	type spec struct {
		title string
		lines func(iw, rows int) []string
	}
	specs := [4]spec{
		{fmt.Sprintf("%s Review requested (%d)", ic.review, v.count(a, paneReview)), func(iw, rows int) []string {
			if d == nil {
				return []string{emptyMsg(de, "")}
			}
			return v.mrLines(a, paneReview, d.Review, iw, rows, "Nothing to review "+ic.check)
		}},
		{fmt.Sprintf("%s To-Do (%d)", ic.todo, v.count(a, paneTodo)), func(iw, rows int) []string {
			if todos == nil {
				return []string{emptyMsg(te, "")}
			}
			return v.todoLines(a, todos, iw, rows)
		}},
		{fmt.Sprintf("%s My merge requests (%d)", ic.mr, v.count(a, paneMine)), func(iw, rows int) []string {
			if d == nil {
				return []string{emptyMsg(de, "")}
			}
			return v.mrLines(a, paneMine, d.Authored, iw, rows, "No open merge requests")
		}},
		{fmt.Sprintf("%s Recent projects", ic.project), func(iw, rows int) []string {
			return v.recentLines(a, iw, rows)
		}},
	}

	renderPane := func(i, pw, ph int) string {
		rows := ph - 2
		v.rows[i] = rows
		title := fmt.Sprintf("%d %s", i+1, specs[i].title)
		box := pane(title, specs[i].lines(pw-4, rows), pw, ph, v.focus == i)
		return a.zone(box, zone{
			click: func(bool) tea.Cmd { v.focus = i; return nil },
			scroll: func(dir int) tea.Cmd {
				v.focus = i
				v.cur[i] += 3 * dir
				v.cur[i] = max(0, min(v.cur[i], v.count(a, i)-1))
				return nil
			},
		})
	}

	var body string
	if w >= 100 {
		lw := w / 2
		rw := w - lw
		th := h / 2
		bh := h - th
		top := joinHoriz(renderPane(0, lw, th), renderPane(1, rw, th))
		bot := joinHoriz(renderPane(2, lw, bh), renderPane(3, rw, bh))
		body = top + "\n" + bot
	} else {
		ph := h / 4
		var parts []string
		for i := 0; i < 4; i++ {
			hh := ph
			if i == 3 {
				hh = h - 3*ph
			}
			parts = append(parts, renderPane(i, w, hh))
		}
		body = strings.Join(parts, "\n")
	}
	if head != "" {
		body = fit(head, w) + "\n" + body
	}
	return body
}

func (v *dashboardView) progress(a *App) string {
	return listProgress(v.cur[v.focus], v.count(a, v.focus))
}

// clickRow makes row i of pane p clickable: click selects, double opens.
func (v *dashboardView) clickRow(a *App, p, i int, line string) string {
	return a.clickRow(line,
		func() tea.Cmd { v.focus, v.cur[p] = p, i; return nil },
		func() tea.Cmd { return v.key(a, keyEnter) })
}

// window keeps the cursor for pane p visible in rows lines.
func (v *dashboardView) window(p, n, rows int) (int, int) {
	c := &v.cur[p]
	*c = max(0, min(*c, n-1))
	o := &v.off[p]
	if *c < *o {
		*o = *c
	}
	if *c >= *o+rows {
		*o = *c - rows + 1
	}
	*o = max(0, min(*o, n-rows))
	return *o, min(n, *o+rows)
}

func shortProject(p string) string {
	parts := strings.Split(p, "/")
	if len(parts) > 2 {
		return parts[len(parts)-2] + "/" + parts[len(parts)-1]
	}
	return p
}

func (v *dashboardView) mrLines(a *App, p int, list []gitlab.MRSummary, w, rows int, empty string) []string {
	if len(list) == 0 {
		return []string{sDim.Render(empty)}
	}
	cells := make([][]string, len(list))
	for i, m := range list {
		title := m.Title
		if m.Draft {
			title = sDim.Render(ic.draft+" ") + strings.TrimSpace(draftRe.ReplaceAllString(title, ""))
		}
		flag := ""
		if m.Approved {
			flag = sOK.Render(ic.approved) + " "
		}
		if m.Conflicts {
			flag = sErr.Render(ic.conflict) + " "
		}
		cells[i] = []string{statusIcon(m.PipelineStatus, false), sDim.Render(fmt.Sprintf("!%d", m.IID)), flag + title,
			sDim.Render(shortProject(m.Project)), sDim.Render(since(m.UpdatedAt))}
	}
	t := newTable([]col{{}, {}, {flex: true}, {max: 24}, {right: true}}, cells, w)
	start, end := v.window(p, len(list), rows)
	var out []string
	for i := start; i < end; i++ {
		line := t.row(cells[i]...)
		if i == v.cur[p] && v.focus == p {
			line = selectLine(line, w)
		}
		out = append(out, v.clickRow(a, p, i, line))
	}
	return out
}

var todoActions = map[string]string{
	"assigned": "assigned", "mentioned": "mentioned", "build_failed": "build failed",
	"marked": "to-do", "approval_required": "approval", "unmergeable": "unmergeable",
	"directly_addressed": "@you", "merge_train_removed": "train removed",
	"review_requested": "review", "member_access_requested": "access req.",
	"review_submitted": "reviewed", "added_approver": "approver",
}

func todoAction(name string) string {
	label, ok := todoActions[name]
	if !ok {
		label = strings.ReplaceAll(name, "_", " ")
	}
	switch name {
	case "build_failed", "unmergeable", "merge_train_removed":
		return sErr.Render(label)
	case "review_requested", "approval_required", "added_approver":
		return sWarn.Render(label)
	case "mentioned", "directly_addressed":
		return sLink.Render(label)
	}
	return sAccent.Render(label)
}

func (v *dashboardView) todoLines(a *App, todos []gitlab.Todo, w, rows int) []string {
	if len(todos) == 0 {
		return []string{sDim.Render("All done " + ic.check)}
	}
	cells := make([][]string, len(todos))
	for i, t := range todos {
		ref := ""
		switch t.TargetType {
		case "MergeRequest":
			ref = fmt.Sprintf("!%d", t.Target.IID)
		case "Issue", "WorkItem":
			ref = fmt.Sprintf("#%d", t.Target.IID)
		}
		project := ""
		if t.Project != nil {
			project = shortProject(t.Project.PathWithNamespace)
		}
		cells[i] = []string{todoAction(t.ActionName), sDim.Render(ref), t.Target.Title, sDim.Render(project), sDim.Render(since(t.CreatedAt))}
	}
	tbl := newTable([]col{{}, {}, {flex: true}, {max: 24}, {right: true}}, cells, w)
	start, end := v.window(paneTodo, len(todos), rows)
	var out []string
	for i := start; i < end; i++ {
		line := tbl.row(cells[i]...)
		if i == v.cur[paneTodo] && v.focus == paneTodo {
			line = selectLine(line, w)
		}
		out = append(out, v.clickRow(a, paneTodo, i, line))
	}
	return out
}

func (v *dashboardView) recentLines(a *App, w, rows int) []string {
	if len(a.recent) == 0 {
		return []string{sDim.Render("Projects you open show up here."), sDim.Render("P to find one.")}
	}
	cells := make([][]string, len(a.recent))
	for i, r := range a.recent {
		ns, name := "", r.Path
		if j := strings.LastIndexByte(r.Path, '/'); j >= 0 {
			ns, name = r.Path[:j+1], r.Path[j+1:]
		}
		cells[i] = []string{sDim.Render(ic.project), sDim.Render(ns) + name, sDim.Render(since(r.At))}
	}
	t := newTable([]col{{}, {flex: true}, {right: true}}, cells, w)
	start, end := v.window(paneRecent, len(a.recent), rows)
	var out []string
	for i := start; i < end; i++ {
		line := t.row(cells[i]...)
		if i == v.cur[paneRecent] && v.focus == paneRecent {
			line = selectLine(line, w)
		}
		out = append(out, v.clickRow(a, paneRecent, i, line))
	}
	return out
}
