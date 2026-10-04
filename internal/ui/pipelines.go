package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"gitlab-tui/internal/gitlab"
)

type pipelineListView struct {
	project string
	ref     string
	l       listState
	seq     int
}

func newPipelineList(project, ref string) *pipelineListView {
	return &pipelineListView{project: project, ref: ref, l: newListState()}
}

// NewPipelineList lists a project's pipelines, optionally only for ref.
func NewPipelineList(project, ref string) view { return newPipelineList(project, ref) }

func (v *pipelineListView) proj() string { return v.project }

// branchFor is the checked-out branch, if project is the cwd's repo.
func (a *App) branchFor(project string) string {
	if project == a.ctx.Project {
		return a.ctx.Branch
	}
	return ""
}

func (v *pipelineListView) title() string {
	if v.ref != "" {
		return "pipelines (" + v.ref + ")"
	}
	return "pipelines"
}

func (v *pipelineListView) capturing() bool { return v.l.filtering }

func pipesKey(p, ref string) string { return fmt.Sprintf("pipes:%s:%s", p, ref) }

func (v *pipelineListView) refresh(a *App, force bool) tea.Cmd {
	p, ref := v.project, v.ref
	k := pipesKey(p, ref)
	age := 30 * time.Second
	if ps, _ := get[[]gitlab.Pipeline](a.store, k); anyActive(ps) {
		age = 5 * time.Second
	}
	if force {
		age = 0
	}
	return fetch(a.store, k, age, func(ctx ctxT) ([]gitlab.Pipeline, error) {
		return a.client.ListPipelines(ctx, p, ref)
	})
}

func anyActive(ps []gitlab.Pipeline) bool {
	for _, p := range ps {
		if isActive(p.Status) {
			return true
		}
	}
	return false
}

func (v *pipelineListView) rows(a *App) []gitlab.Pipeline {
	all, _ := get[[]gitlab.Pipeline](a.store, pipesKey(v.project, v.ref))
	if v.l.query == "" {
		return all
	}
	var out []gitlab.Pipeline
	for _, p := range all {
		if matches(v.l.query, fmt.Sprint(p.ID), p.Ref, p.Status, p.Source, p.SHA) {
			out = append(out, p)
		}
	}
	return out
}

func (v *pipelineListView) selected(a *App) *gitlab.Pipeline {
	rows := v.rows(a)
	if v.l.cursor < len(rows) {
		return &rows[v.l.cursor]
	}
	return nil
}

func (v *pipelineListView) debounced(a *App, seq int) tea.Cmd {
	if seq != v.seq {
		return nil
	}
	if p := v.selected(a); p != nil {
		return prefetchPipeline(a, v.project, p.ID)
	}
	return nil
}

func (v *pipelineListView) moved() tea.Cmd {
	v.seq++
	return debounce(v, v.seq, 200*time.Millisecond)
}

// allBranches is the chooser ID for clearing the branch filter.
const allBranches = "\x00all"

// chooseRef picks the branch to list pipelines for. The checked-out branch
// comes first when this is the cwd's project.
func (v *pipelineListView) chooseRef(a *App, current string) tea.Cmd {
	var extra []choice
	if current != "" {
		extra = append(extra, choice{id: current, text: "Current (" + current + ")", desc: "checked out here"})
	}
	extra = append(extra, choice{id: allBranches, text: "All branches"})
	initial := v.ref
	if initial == "" {
		initial = allBranches
		if current != "" {
			initial = current
		}
	}
	return a.chooseBranch(v.project, "Pipelines for branch", extra, initial, func(b string) tea.Cmd {
		if b == allBranches {
			b = ""
		}
		v.ref = b
		v.l.cursor, v.l.offset = 0, 0
		return v.refresh(a, false)
	})
}

func (v *pipelineListView) help() []kb {
	return []kb{{"enter", "open"}, {"b", "branch"}, {"n", "run pipeline"}, {"R", "retry"}, {"X", "cancel"}, {"/", "search"}}
}

func (v *pipelineListView) key(a *App, msg tea.KeyMsg) tea.Cmd {
	if v.l.filtering {
		cmd, _ := v.l.filterKey(msg)
		return cmd
	}
	if ok, moved := v.l.nav(msg.String(), len(v.rows(a))); ok {
		if moved {
			return v.moved()
		}
		return nil
	}
	p := v.project
	branch := a.branchFor(p)
	switch msg.String() {
	case "/":
		return v.l.startFilter()
	case "b":
		return v.chooseRef(a, branch)
	case "n":
		ref := v.ref
		if ref == "" {
			ref = branch
		}
		if ref == "" {
			a.setFlash("no branch to run a pipeline on", true)
			return nil
		}
		a.confirm(fmt.Sprintf("Run a new pipeline on %s?", ref), func() tea.Cmd {
			var created *gitlab.Pipeline
			return a.action("create pipeline", func() (err error) {
				created, err = a.client.CreatePipeline(bg(), p, ref)
				return err
			}, []string{pipesKey(p, v.ref)}, func(a *App) tea.Cmd {
				return a.push(newPipelineView(p, created.ID, ""))
			})
		})
		return nil
	}
	sel := v.selected(a)
	if sel == nil {
		return nil
	}
	id := sel.ID
	switch msg.String() {
	case "enter", "l", "right":
		return a.push(newPipelineView(p, id, ""))
	case "o":
		return openBrowser(sel.WebURL)
	case "y":
		return copyText(sel.WebURL)
	case "R":
		return a.action(fmt.Sprintf("retry pipeline #%d", id), func() error { return a.client.RetryPipeline(bg(), p, id) },
			[]string{pipesKey(p, v.ref), pipeKey(p, id), jobsKey(p, id)}, nil)
	case "X":
		a.confirm(fmt.Sprintf("Cancel pipeline #%d?", id), func() tea.Cmd {
			return a.action(fmt.Sprintf("cancel pipeline #%d", id), func() error { return a.client.CancelPipeline(bg(), p, id) },
				[]string{pipesKey(p, v.ref), pipeKey(p, id), jobsKey(p, id)}, nil)
		})
	}
	return nil
}

func (v *pipelineListView) render(a *App, w, h int) string {
	var b strings.Builder
	_, e := get[[]gitlab.Pipeline](a.store, pipesKey(v.project, v.ref))
	scope := "all branches"
	if v.ref != "" {
		scope = "ref " + v.ref
	}
	b.WriteString(sDim.Render(scope) + entryStatus(e) + "\n")
	h--
	if fl := v.l.filterLine(); fl != "" {
		b.WriteString(fl + "\n")
		h--
	}
	rows := v.rows(a)
	if len(rows) == 0 {
		b.WriteString("\n" + emptyMsg(e, "No pipelines."))
		return b.String()
	}
	cells := make([][]string, len(rows))
	for i, p := range rows {
		cells[i] = []string{statusLabel(p.Status, false), fmt.Sprintf("#%d", p.ID), sKey.Render(p.Ref),
			sDim.Render(p.Source), sDim.Render(shortSHA(p.SHA)), sDim.Render(since(p.CreatedAt))}
	}
	t := newTable([]col{
		{title: "STATUS"}, {title: "PIPELINE"}, {title: "REF", flex: true},
		{title: "SOURCE", optional: true}, {title: "SHA", optional: true}, {title: "AGE", right: true},
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

func (v *pipelineListView) progress(a *App) string { return listProgress(v.l.cursor, len(v.rows(a))) }
