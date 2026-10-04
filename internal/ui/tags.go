package ui

import (
	"net/url"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"gitlab-tui/internal/gitlab"
)

// tagListView lists a project's tags, newest first.
type tagListView struct {
	project string
	l       listState
}

func newTagList(project string) *tagListView {
	return &tagListView{project: project, l: newListState()}
}

// NewTagList lists a project's tags.
func NewTagList(project string) View { return newTagList(project) }

func (v *tagListView) title() string   { return "tags" }
func (v *tagListView) proj() string    { return v.project }
func (v *tagListView) capturing() bool { return v.l.filtering }

func (v *tagListView) refresh(a *App, force bool) tea.Cmd {
	age := 2 * time.Minute
	if force {
		age = 0
	}
	p := v.project
	return fetch(a.store, tagsKey(p), age, func(ctx ctxT) ([]gitlab.Tag, error) { return a.client.ListTags(ctx, p) })
}

func (v *tagListView) rows(a *App) []gitlab.Tag {
	all, _ := get[[]gitlab.Tag](a.store, tagsKey(v.project))
	if v.l.query == "" {
		return all
	}
	var out []gitlab.Tag
	for _, t := range all {
		if matches(v.l.query, t.Name, t.Message, t.Commit.Title, t.Commit.ShortID) {
			out = append(out, t)
		}
	}
	return out
}

func (v *tagListView) help() []kb {
	return []kb{{"enter", "pipelines"}, {"/", "search"}, {"o", "browser"}, {"y", "copy name"}}
}

func (v *tagListView) progress(a *App) string { return listProgress(v.l.cursor, len(v.rows(a))) }

func (v *tagListView) tagURL(a *App, t gitlab.Tag) string {
	return a.ctx.WebBase + "/" + v.project + "/-/tags/" + url.PathEscape(t.Name)
}

func (v *tagListView) key(a *App, msg tea.KeyMsg) tea.Cmd {
	if v.l.filtering {
		cmd, _ := v.l.filterKey(msg)
		return cmd
	}
	rows := v.rows(a)
	if ok, _ := v.l.nav(msg.String(), len(rows)); ok {
		return nil
	}
	if msg.String() == "/" {
		return v.l.startFilter()
	}
	if v.l.cursor >= len(rows) {
		return nil
	}
	t := rows[v.l.cursor]
	switch msg.String() {
	case "enter", "l", "right":
		return a.push(newPipelineList(v.project, t.Name))
	case "o":
		return openBrowser(v.tagURL(a, t))
	case "y":
		return copyText(t.Name)
	}
	return nil
}

func (v *tagListView) render(a *App, w, h int) string {
	var b strings.Builder
	_, e := get[[]gitlab.Tag](a.store, tagsKey(v.project))
	if fl := v.l.filterLine(); fl != "" {
		b.WriteString(fl + "\n")
		h--
	}
	rows := v.rows(a)
	if len(rows) == 0 {
		b.WriteString("\n" + emptyMsg(e, "No tags."))
		return b.String()
	}
	cells := make([][]string, len(rows))
	for i, t := range rows {
		c := tagCells(t)
		cells[i] = []string{c[0], c[1], c[2], sDim.Render(t.Commit.ShortID), c[3]}
	}
	t := newTable([]col{{title: "TAG", max: 40}, {title: "RELEASE"}, {title: "MESSAGE", flex: true}, {title: "COMMIT", optional: true}, {title: "AGE", right: true}}, cells, w)
	b.WriteString(t.header() + "\n")
	h--
	start, end := v.l.window(len(rows), h)
	for i := start; i < end; i++ {
		line := t.row(cells[i]...)
		if i == v.l.cursor {
			line = selectLine(line, w)
		}
		b.WriteString(v.l.clickRow(a, line, i, func() tea.Cmd { return nil }, func() tea.Cmd { return v.key(a, keyEnter) }) + "\n")
	}
	return b.String()
}
