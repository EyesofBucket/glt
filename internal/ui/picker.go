package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"gitlab-tui/internal/gitlab"
)

// projectPicker is a telescope.nvim-style fuzzy finder over projects:
// prompt at the bottom, best match just above it, preview on the right.
type projectPicker struct {
	input   textinput.Model
	cursor  int // index into results; 0 is the best match
	offset  int
	results []pickResult
	total   int
	lastQ   string
	dirty   bool

	searchSeq  int
	searchTerm string // server-side search in effect
	pvSeq      int
	pvPath     string // project whose preview data is loaded/loading

	// tree mode (ctrl+t): see picker_tree.go
	tree             bool
	expanded         map[string]bool // open groups, by path
	tcursor, toffset int

	height int // result rows shown at last render, for paging
}

type pickResult struct {
	p      gitlab.Project
	score  int
	pos    []int
	recent time.Time
}

const projectsKey = "projects"

func searchKey(term string) string { return "projsearch:" + strings.ToLower(term) }
func openMRsKey(p string) string   { return "openmrs:" + p }

func (a *App) openPicker() tea.Cmd {
	ti := textinput.New()
	ti.Prompt = sActive.Render(ic.caret) + " "
	ti.Placeholder = "search projects"
	focus := ti.Focus()
	pk := &projectPicker{input: ti, dirty: true, tree: a.pickerTree}
	a.overlay = pk
	return tea.Batch(focus, pk.refresh(a, false))
}

func (pk *projectPicker) refresh(a *App, force bool) tea.Cmd {
	age := 10 * time.Minute
	if force {
		age = 0
	}
	cmds := []tea.Cmd{fetch(a.store, projectsKey, age, a.client.ListProjects)}
	if pk.searchTerm != "" {
		term := pk.searchTerm
		cmds = append(cmds, fetch(a.store, searchKey(term), 5*time.Minute, func(ctx ctxT) ([]gitlab.Project, error) {
			return a.client.SearchProjects(ctx, term)
		}))
	}
	if pk.pvPath != "" {
		if r := pk.selected(); r != nil && r.p.PathWithNamespace == pk.pvPath {
			cmds = append(cmds, previewFetch(a, r.p))
		}
	}
	pk.dirty = true
	return tea.Batch(cmds...)
}

func previewFetch(a *App, p gitlab.Project) tea.Cmd {
	path := p.PathWithNamespace
	cmds := []tea.Cmd{fetch(a.store, openMRsKey(path), 2*time.Minute, func(ctx ctxT) (int, error) {
		return a.client.OpenMRCount(ctx, path)
	})}
	if p.DefaultBranch != "" {
		cmds = append(cmds, fetchLatest(a, path, p.DefaultBranch, 2*time.Minute))
	}
	return tea.Batch(cmds...)
}

// candidates merges membership projects, server search hits and recent
// projects, de-duplicated by path.
func (pk *projectPicker) candidates(a *App) []pickResult {
	seen := map[string]int{}
	var out []pickResult
	add := func(p gitlab.Project) {
		if p.PathWithNamespace == "" {
			return
		}
		if i, ok := seen[p.PathWithNamespace]; ok {
			if out[i].p.ID == 0 {
				out[i].p = p
			}
			return
		}
		seen[p.PathWithNamespace] = len(out)
		r := pickResult{p: p}
		r.recent, _ = a.recentAt(p.PathWithNamespace)
		out = append(out, r)
		a.learnProject(p.PathWithNamespace, p.ID)
	}
	mine, _ := get[[]gitlab.Project](a.store, projectsKey)
	for _, p := range mine {
		add(p)
	}
	if pk.searchTerm != "" {
		found, _ := get[[]gitlab.Project](a.store, searchKey(pk.searchTerm))
		for _, p := range found {
			add(p)
		}
	}
	for _, r := range a.recent {
		parts := strings.Split(r.Path, "/")
		add(gitlab.Project{ID: r.ID, PathWithNamespace: r.Path, Name: parts[len(parts)-1]})
	}
	return out
}

func (pk *projectPicker) recompute(a *App) {
	q := strings.TrimSpace(pk.input.Value())
	if !pk.dirty && q == pk.lastQ {
		return
	}
	pk.dirty = false
	all := pk.candidates(a)
	pk.total = len(all)
	res := all[:0:0]
	for _, c := range all {
		if q == "" {
			res = append(res, c)
			continue
		}
		if sc, pos, ok := fuzzyMatch(q, c.p.PathWithNamespace); ok {
			c.score, c.pos = sc, pos
			res = append(res, c)
		}
	}
	sort.SliceStable(res, func(i, j int) bool {
		ri, rj := res[i], res[j]
		if q != "" && ri.score != rj.score {
			return ri.score > rj.score
		}
		if !ri.recent.Equal(rj.recent) {
			return ri.recent.After(rj.recent)
		}
		if q != "" && len(ri.p.PathWithNamespace) != len(rj.p.PathWithNamespace) {
			return len(ri.p.PathWithNamespace) < len(rj.p.PathWithNamespace)
		}
		return ri.p.LastActivityAt.After(rj.p.LastActivityAt)
	})
	// keep the selection on the same project if it's still there
	var cur string
	if s := pk.selected(); s != nil && q == pk.lastQ {
		cur = s.p.PathWithNamespace
	}
	pk.results = res
	pk.cursor = 0
	if cur != "" {
		for i, r := range res {
			if r.p.PathWithNamespace == cur {
				pk.cursor = i
				break
			}
		}
	}
	if q != pk.lastQ {
		pk.offset = 0
	}
	pk.lastQ = q
}

// page is the number of result rows on screen (at least 2).
func (pk *projectPicker) page() int { return max(2, pk.height) }

func (pk *projectPicker) selected() *pickResult {
	if pk.tree {
		if n := pk.treeSelected(); n != nil && !n.group {
			return n.res
		}
		return nil
	}
	if pk.cursor >= 0 && pk.cursor < len(pk.results) {
		return &pk.results[pk.cursor]
	}
	return nil
}

func (pk *projectPicker) help() []kb {
	if pk.tree {
		return []kb{{"enter", "open/expand"}, {"←/→", "collapse/expand"}, {"^t", "list view"}, {"^o", "browser"}, {"^y", "copy URL"}, {"esc", "close"}}
	}
	return []kb{{"enter", "open"}, {"^t", "tree view"}, {"^o", "browser"}, {"^y", "copy URL"}, {"^n/^p", "move"}, {"esc", "close"}}
}

func (pk *projectPicker) moved(a *App) tea.Cmd {
	pk.pvSeq++
	return debounce(pk, pk.pvSeq, 150*time.Millisecond)
}

func (pk *projectPicker) debounced(a *App, seq int) tea.Cmd {
	// two debounce streams share the message: search uses negative seqs
	if seq < 0 {
		if -seq != pk.searchSeq {
			return nil
		}
		term := strings.TrimSpace(pk.input.Value())
		if len(term) < 3 {
			return nil
		}
		pk.searchTerm = term
		pk.dirty = true
		return pk.refresh(a, false)
	}
	if seq != pk.pvSeq {
		return nil
	}
	if r := pk.selected(); r != nil {
		pk.pvPath = r.p.PathWithNamespace
		return previewFetch(a, r.p)
	}
	return nil
}

func (pk *projectPicker) key(a *App, msg tea.KeyMsg) tea.Cmd {
	pk.recompute(a)
	n := len(pk.results)
	sel := pk.selected()
	move := func(d int) tea.Cmd {
		pk.cursor = max(0, min(n-1, pk.cursor+d))
		return pk.moved(a)
	}
	switch msg.String() {
	case "esc":
		a.overlay = nil
		return nil
	case "ctrl+t":
		pk.tree = !pk.tree
		a.pickerTree = pk.tree
		pk.tcursor, pk.toffset = 0, 0
		return pk.moved(a)
	}
	if pk.tree {
		if cmd, ok := pk.treeKey(a, msg); ok {
			return cmd
		}
	}
	switch msg.String() {
	// descending layout: "up" walks away from the prompt, to worse matches
	case "up", "ctrl+p", "ctrl+k":
		return move(1)
	case "down", "ctrl+n", "ctrl+j":
		return move(-1)
	case "pgup", "ctrl+b":
		return move(pk.page())
	case "pgdown", "ctrl+f":
		return move(-pk.page())
	case "ctrl+u":
		return move(pk.page() / 2)
	case "ctrl+d":
		return move(-pk.page() / 2)
	case "enter", "ctrl+o", "ctrl+y":
		if sel == nil {
			return nil
		}
		p := sel.p
		switch msg.String() {
		case "ctrl+o":
			return openBrowser(p.WebURL)
		case "ctrl+y":
			return copyText(p.WebURL)
		}
		a.overlay = nil
		a.learnProject(p.PathWithNamespace, p.ID)
		return a.openProject(newProjectView(p.PathWithNamespace))
	}
	var cmd tea.Cmd
	before := pk.input.Value()
	pk.input, cmd = pk.input.Update(msg)
	if pk.input.Value() == before {
		return cmd
	}
	pk.recompute(a)
	if pk.tree {
		// land on the best match (not the first project in tree order)
		pk.tcursor, pk.toffset = 0, 0
		if len(pk.results) > 0 {
			best := pk.results[0].p.PathWithNamespace
			for i, n := range pk.treeRows() {
				if !n.group && n.path == best {
					pk.tcursor = i
					break
				}
			}
		}
	}
	cmds := []tea.Cmd{cmd, pk.moved(a)}
	// ask the server too, for projects the user isn't a member of
	term := strings.TrimSpace(pk.input.Value())
	if len(term) >= 3 {
		pk.searchSeq++
		cmds = append(cmds, debounce(pk, -pk.searchSeq, 300*time.Millisecond))
	}
	return tea.Batch(cmds...)
}

// passthrough forwards non-key messages (cursor blink) to the input.
func (pk *projectPicker) passthrough(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	pk.input, cmd = pk.input.Update(msg)
	return cmd
}

// overlay renders the picker floating over the body, telescope's default
// horizontal layout: 80% x 90%, preview when the terminal is >= 120 wide.
func (pk *projectPicker) render(a *App, body string, w, h int) string {
	pk.recompute(a)
	bw := max(min(w, 50), w*8/10)
	bh := max(min(h, 10), h*9/10)
	x, y := (w-bw)/2, (h-bh)/2

	showPreview := w >= 120
	listW := bw
	if showPreview {
		listW = bw / 2
	}
	promptH := 3
	resH := bh - promptH

	// results, best match at the bottom
	rows := resH - 2
	pk.height = rows
	if pk.cursor < pk.offset {
		pk.offset = pk.cursor
	}
	if pk.cursor >= pk.offset+rows {
		pk.offset = pk.cursor - rows + 1
	}
	inner := listW - 4
	lines := make([]string, rows)
	for i := 0; i < rows && !pk.tree; i++ {
		idx := pk.offset + i
		if idx >= len(pk.results) {
			break
		}
		r := pk.results[idx]
		lines[rows-1-i] = a.clickRow(pk.renderRow(r, inner, idx == pk.cursor),
			func() tea.Cmd { pk.cursor = idx; return pk.moved(a) },
			func() tea.Cmd { return pk.key(a, keyEnter) })
	}
	_, pe := get[[]gitlab.Project](a.store, projectsKey)
	if len(pk.results) == 0 {
		msg := "no matching projects"
		if pe == nil || pe.val == nil {
			msg = spinnerFrame() + " loading projects…"
		} else if pk.searchTerm != "" && a.store.loading(searchKey(pk.searchTerm)) {
			msg = spinnerFrame() + " searching…"
		}
		lines[rows-1] = sDim.Render(msg)
	}
	title := "Results"
	if pk.tree {
		title = "Groups"
		if len(pk.results) > 0 {
			lines = pk.treeLines(a, inner, rows)
		} else {
			lines[0], lines[rows-1] = lines[rows-1], ""
		}
	}
	results := pane(title, lines, listW, resH, false)

	// prompt
	count := sDim.Render(fmt.Sprintf("%d / %d", len(pk.results), pk.total))
	if pk.searchTerm != "" && a.store.loading(searchKey(pk.searchTerm)) {
		count = sDim.Render(spinnerFrame()+" ") + count
	}
	pk.input.Width = max(5, inner-ansi.StringWidth(count)-6)
	in := pk.input.View()
	promptLine := in + strings.Repeat(" ", max(1, inner-ansi.StringWidth(in)-ansi.StringWidth(count))) + count
	prompt := pane(ic.project+" Projects", []string{promptLine}, listW, promptH, true)

	left := results + "\n" + prompt
	box := left
	if showPreview {
		box = joinHoriz(left, pk.renderPreview(a, bw-listW, bh))
	}
	// clicks inside the box shouldn't fall through and dismiss it
	return overlayAt(body, a.zone(box, zone{}), x, y)
}

func (pk *projectPicker) renderRow(r pickResult, w int, selected bool) string {
	path := r.p.PathWithNamespace
	ns, name := "", path
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		ns, name = path[:i+1], path[i+1:]
	}
	// highlight against the whole path, split back into namespace/name styles
	hl := highlight(path, r.pos, func(s string) string { return s })
	if len(r.pos) == 0 {
		hl = sDim.Render(ns) + name
	}
	mark := "  "
	if selected {
		mark = sActive.Render(ic.caret) + " "
	}
	right := ""
	if !r.recent.IsZero() {
		right = sDim.Render(ic.clock + " " + since(r.recent))
	} else if !r.p.LastActivityAt.IsZero() {
		right = sDim.Render(since(r.p.LastActivityAt))
	}
	left := mark + sDim.Render(ic.project) + " " + hl
	line := pad(left, w-ansi.StringWidth(right)-len(colGap)) + colGap + right
	if selected {
		return selectLine(line, w)
	}
	return line
}

func (pk *projectPicker) renderPreview(a *App, w, h int) string {
	if pk.tree {
		if n := pk.treeSelected(); n != nil && n.group {
			return pk.groupPreview(n, w, h)
		}
	}
	r := pk.selected()
	if r == nil {
		return pane("Preview", nil, w, h, false)
	}
	p := r.p
	inner := w - 4
	var L []string
	add := func(s ...string) { L = append(L, s...) }
	add(sTitle.Render(p.Name), sDim.Render(p.PathWithNamespace), "")
	if d := strings.TrimSpace(p.Description); d != "" {
		add(wrapText(d, inner)...)
		add("")
	}
	if p.DefaultBranch != "" {
		add(sDim.Render(ic.branch+" ") + p.DefaultBranch)
	}
	if n, e := get[int](a.store, openMRsKey(p.PathWithNamespace)); e != nil && e.val != nil {
		add(sDim.Render(ic.mr+" ") + fmt.Sprintf("%d open merge requests", n))
	}
	if p.DefaultBranch != "" {
		if ps, e := get[[]gitlab.Pipeline](a.store, latestKey(p.PathWithNamespace, p.DefaultBranch)); len(ps) > 0 {
			pl := ps[0]
			add(sDim.Render(ic.pipeline+" ") + statusLabel(pl.Status, false) + sDim.Render(fmt.Sprintf("  #%d · %s ago", pl.ID, since(pl.CreatedAt))))
		} else if e != nil && e.val != nil {
			add(sDim.Render(ic.pipeline + " no pipelines on " + p.DefaultBranch))
		}
	}
	if !p.LastActivityAt.IsZero() {
		add(sDim.Render(ic.clock+" ") + "active " + since(p.LastActivityAt) + " ago")
	}
	if p.StarCount > 0 || p.ForksCount > 0 {
		add(sDim.Render(fmt.Sprintf("★ %d  ⑂ %d", p.StarCount, p.ForksCount)))
	}
	if len(p.Topics) > 0 {
		add(sAccent.Render(strings.Join(p.Topics, " · ")))
	}
	if p.WebURL != "" {
		add("", sLink.Render(fit(p.WebURL, inner)))
	}
	if !r.recent.IsZero() {
		add(sDim.Render("visited " + since(r.recent) + " ago"))
	}
	return pane("Preview", L, w, h, false)
}

// joinHoriz places two equal-height blocks side by side.
func joinHoriz(a, b string) string {
	al, bl := strings.Split(a, "\n"), strings.Split(b, "\n")
	n := max(len(al), len(bl))
	aw := 0
	for _, l := range al {
		aw = max(aw, ansi.StringWidth(l))
	}
	out := make([]string, n)
	for i := 0; i < n; i++ {
		var l, r string
		if i < len(al) {
			l = al[i]
		}
		if i < len(bl) {
			r = bl[i]
		}
		out[i] = pad(l, aw) + r
	}
	return strings.Join(out, "\n")
}
