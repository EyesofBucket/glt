package ui

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"gitlab-tui/internal/gitlab"
)

type mrDetailView struct {
	project string
	iid     int
	branch  string // resolve the MR for this branch when iid == 0
	summary *gitlab.MRSummary
	scroll  int
	height  int
	total   int
}

func newMRDetail(project string, iid int, branch string, summary *gitlab.MRSummary) *mrDetailView {
	return &mrDetailView{project: project, iid: iid, branch: branch, summary: summary}
}

// NewMRForBranch opens the open MR whose source branch is branch.
func NewMRForBranch(project, branch string) view { return newMRDetail(project, 0, branch, nil) }

// NewMR opens MR !iid.
func NewMR(project string, iid int) view { return newMRDetail(project, iid, "", nil) }

func (v *mrDetailView) proj() string { return v.project }

func (v *mrDetailView) title() string {
	if v.iid == 0 {
		return "MR for " + v.branch
	}
	return fmt.Sprintf("!%d", v.iid)
}

func (v *mrDetailView) capturing() bool { return false }

func mrKey(p string, iid int) string   { return fmt.Sprintf("mr:%s:%d", p, iid) }
func apprKey(p string, iid int) string { return fmt.Sprintf("appr:%s:%d", p, iid) }
func discKey(p string, iid int) string { return fmt.Sprintf("disc:%s:%d", p, iid) }
func pipeKey(p string, id int) string  { return fmt.Sprintf("pipe:%s:%d", p, id) }
func jobsKey(p string, id int) string  { return fmt.Sprintf("jobs:%s:%d", p, id) }
func mrForKey(p, branch string) string { return fmt.Sprintf("mrfor:%s:%s", p, branch) }
func mrListPrefix(p string) string     { return "mrs:" + p + ":" }

func prefetchMR(a *App, p string, iid int) tea.Cmd {
	return tea.Batch(
		fetch(a.store, mrKey(p, iid), time.Minute, func(ctx ctxT) (*gitlab.MR, error) { return a.client.GetMR(ctx, p, iid) }),
		fetch(a.store, apprKey(p, iid), time.Minute, func(ctx ctxT) (*gitlab.Approvals, error) { return a.client.GetApprovals(ctx, p, iid) }),
	)
}

func (v *mrDetailView) mr(a *App) (*gitlab.MR, *entry) {
	return get[*gitlab.MR](a.store, mrKey(v.project, v.iid))
}

func (v *mrDetailView) refresh(a *App, force bool) tea.Cmd {
	p := v.project
	if v.iid == 0 {
		k := mrForKey(p, v.branch)
		mr, e := get[*gitlab.MR](a.store, k)
		if mr != nil && e != nil && !e.disk {
			v.iid = mr.IID
		} else {
			age := time.Hour
			if force || (e != nil && e.disk) {
				age = 0
			}
			return fetch(a.store, k, age, func(ctx ctxT) (*gitlab.MR, error) {
				return a.client.FindMRForBranch(ctx, p, v.branch)
			})
		}
	}
	iid := v.iid
	mrAge, slowAge := 20*time.Second, 60*time.Second
	mr, _ := v.mr(a)
	var pl *gitlab.Pipeline
	if mr != nil {
		pl = mr.HeadPipeline
	}
	if pl != nil && isActive(pl.Status) {
		mrAge = 5 * time.Second
	}
	if force {
		mrAge, slowAge = 0, 0
	}
	cmds := []tea.Cmd{
		fetch(a.store, mrKey(p, iid), mrAge, func(ctx ctxT) (*gitlab.MR, error) { return a.client.GetMR(ctx, p, iid) }),
		fetch(a.store, apprKey(p, iid), slowAge/2, func(ctx ctxT) (*gitlab.Approvals, error) { return a.client.GetApprovals(ctx, p, iid) }),
		fetch(a.store, discKey(p, iid), slowAge, func(ctx ctxT) ([]gitlab.Discussion, error) { return a.client.ListDiscussions(ctx, p, iid) }),
	}
	plID := 0
	if pl != nil {
		plID = pl.ID
	} else if v.summary != nil {
		plID = v.summary.PipelineID
	}
	if mr != nil && len(mr.Labels) > 0 {
		cmds = append(cmds, fetchLabels(a, p, 10*time.Minute))
	}
	if plID != 0 {
		jobsAge := mrAge
		cmds = append(cmds, fetch(a.store, jobsKey(p, plID), jobsAge, func(ctx ctxT) ([]gitlab.Job, error) {
			return a.client.ListPipelineJobs(ctx, p, plID)
		}))
	}
	if pl != nil {
		cmds = append(cmds, fetchTestSummary(a, p, pl, force))
	}
	return tea.Batch(cmds...)
}

func (v *mrDetailView) help() []kb {
	return []kb{{"p", "pipeline"}, {"T", "tests"}, {"D", "dependencies"}, {"n", "run pipeline"}, {"e", "edit"}, {"l", "labels"}, {"a", "approve"}, {"m/M", "merge/auto"}, {"R", "rebase"}, {"d", "draft"}, {"c", "comment"}, {"o", "browser"}}
}

func (v *mrDetailView) key(a *App, msg tea.KeyMsg) tea.Cmd {
	page := max(1, v.height)
	switch msg.String() {
	case "j", "down":
		v.scroll++
	case "k", "up":
		v.scroll--
	case "ctrl+d":
		v.scroll += page / 2
	case "ctrl+u":
		v.scroll -= page / 2
	case "pgdown", " ", "ctrl+f":
		v.scroll += page
	case "pgup", "ctrl+b":
		v.scroll -= page
	case "g", "home":
		v.scroll = 0
	case "G", "end":
		v.scroll = v.total
	}
	v.scroll = max(0, min(v.scroll, v.total-v.height))

	mr, _ := v.mr(a)
	if mr == nil {
		if v.iid == 0 {
			switch msg.String() {
			case "c":
				return a.replace(newMRForm(v.project, v.branch))
			case "B":
				return a.push(newPipelineList(v.project, v.branch))
			}
		}
		return nil
	}
	p, iid := v.project, mr.IID
	inval := []string{mrKey(p, iid), apprKey(p, iid)}
	for k := range a.store.m {
		if strings.HasPrefix(k, mrListPrefix(p)) {
			inval = append(inval, k)
		}
	}
	switch msg.String() {
	case "p", "enter":
		if mr.HeadPipeline == nil {
			a.setFlash("MR has no pipeline", true)
			return nil
		}
		return a.push(newPipelineView(p, mr.HeadPipeline.ID, ""))
	case "B":
		return a.push(newPipelineList(p, mr.SourceBranch))
	case "T":
		return a.openTests(p, mr.HeadPipeline)
	case "D":
		return a.openDeps(p, mr.HeadPipeline, "")
	case "l":
		return a.editMRLabels(p, iid, mr.Labels)
	case "e":
		return a.push(newMREditForm(p, mr))
	case "n":
		return v.runPipeline(a, mr)
	case "o":
		return openBrowser(mr.WebURL)
	case "y":
		return copyText(mr.WebURL)
	case "a":
		ap, _ := get[*gitlab.Approvals](a.store, apprKey(p, iid))
		if ap != nil && ap.UserHasApproved {
			return a.action(fmt.Sprintf("unapprove !%d", iid), func() error { return a.client.Unapprove(bg(), p, iid) }, inval, nil)
		}
		return a.action(fmt.Sprintf("approve !%d", iid), func() error { return a.client.Approve(bg(), p, iid) }, inval, nil)
	case "m":
		a.confirm(fmt.Sprintf("Merge !%d into %s now?", iid, mr.TargetBranch), func() tea.Cmd {
			return a.action(fmt.Sprintf("merge !%d", iid), func() error { return a.client.Merge(bg(), p, iid, false) }, inval, nil)
		})
	case "M":
		a.confirm(fmt.Sprintf("Auto-merge !%d when the pipeline succeeds?", iid), func() tea.Cmd {
			return a.action(fmt.Sprintf("set auto-merge on !%d", iid), func() error { return a.client.Merge(bg(), p, iid, true) }, inval, nil)
		})
	case "R":
		a.confirm(fmt.Sprintf("Rebase %s onto %s?", mr.SourceBranch, mr.TargetBranch), func() tea.Cmd {
			return a.action(fmt.Sprintf("rebase !%d", iid), func() error { return a.client.Rebase(bg(), p, iid) }, inval, nil)
		})
	case "d":
		title, desc := toggleDraft(mr.Title, mr.Draft)
		return a.action(fmt.Sprintf("%s !%d", desc, iid), func() error { return a.client.SetTitle(bg(), p, iid, title) }, inval, nil)
	case "c":
		return a.prompt(fmt.Sprintf("Comment on !%d", iid), func(body string) tea.Cmd {
			return a.action("comment", func() error { return a.client.AddNote(bg(), p, iid, body) },
				[]string{discKey(p, iid), mrKey(p, iid)}, nil)
		})
	}
	return nil
}

// runPipeline starts a new pipeline for the MR. It matches the kind of the
// MR's current pipeline: a merge request pipeline if that's what the
// project runs, otherwise a branch pipeline on the source branch.
func (v *mrDetailView) runPipeline(a *App, mr *gitlab.MR) tea.Cmd {
	p, iid, branch := v.project, mr.IID, mr.SourceBranch
	mrPipeline := mr.HeadPipeline == nil || mr.HeadPipeline.Source == "merge_request_event"
	prompt := fmt.Sprintf("Run a new pipeline on %s?", branch)
	if mrPipeline {
		prompt = fmt.Sprintf("Run a new merge request pipeline for !%d?", iid)
	}
	inval := []string{mrKey(p, iid)}
	inval = append(inval, a.keysWithPrefix("pipes:"+p+":", "latest:"+p+":", mrListPrefix(p))...)
	a.confirm(prompt, func() tea.Cmd {
		var created *gitlab.Pipeline
		return a.action("start pipeline", func() (err error) {
			if mrPipeline {
				created, err = a.client.CreateMRPipeline(bg(), p, iid)
				if err == nil || mr.HeadPipeline != nil {
					return err
				}
				// no pipeline yet and no MR pipeline rules: try the branch
			}
			created, err = a.client.CreatePipeline(bg(), p, branch)
			return err
		}, inval, func(a *App) tea.Cmd {
			return a.push(newPipelineView(p, created.ID, ""))
		})
	})
	return nil
}

var draftRe = regexp.MustCompile(`(?i)^\s*(\[draft\]|\(draft\)|draft:|draft\s+-|draft\s)\s*`)

func toggleDraft(title string, draft bool) (string, string) {
	if draft {
		return draftRe.ReplaceAllString(title, ""), "mark ready"
	}
	return "Draft: " + title, "mark draft"
}

func (v *mrDetailView) render(a *App, w, h int) string {
	lines := v.content(a, w)
	v.total, v.height = len(lines), h
	v.scroll = max(0, min(v.scroll, v.total-h))
	end := min(len(lines), v.scroll+h)
	return strings.Join(lines[v.scroll:end], "\n")
}

func (v *mrDetailView) content(a *App, w int) []string {
	p := v.project
	if v.iid == 0 {
		mr, e := get[*gitlab.MR](a.store, mrForKey(p, v.branch))
		if e != nil && !e.loading && e.err == nil && mr == nil {
			return []string{"", sDim.Render(fmt.Sprintf("  No open merge request for branch %q.", v.branch)),
				sDim.Render("  Press c to create one, B for its pipelines, or esc to go back.")}
		}
		return []string{"", emptyMsg(e, "")}
	}
	mr, e := v.mr(a)
	var L []string
	add := func(s ...string) { L = append(L, s...) }

	if mr == nil {
		if s := v.summary; s != nil {
			add(sTitle.Render(fmt.Sprintf("!%d %s", s.IID, s.Title)),
				sDim.Render("@"+s.Author+" · "+s.SourceBranch+" → "+s.TargetBranch), "")
		}
		add(emptyMsg(e, ""))
		return L
	}

	// title, then badges summing up where the MR stands
	title := mr.Title
	if mr.Draft {
		title = strings.TrimSpace(draftRe.ReplaceAllString(title, ""))
	}
	add(fit(sDim.Render(fmt.Sprintf("!%d", mr.IID))+"  "+sTitle.Render(title)+entryStatus(e), w))
	add(fit(strings.Join(v.badges(a, mr), "   "), w), "")

	// details: one-line facts on the left, lists (one item per line) on the
	// right; on narrow screens the two run one after the other
	type field struct {
		k string
		v []string
	}
	me, _ := get[*gitlab.User](a.store, meKey)
	ap, _ := get[*gitlab.Approvals](a.store, apprKey(p, mr.IID))
	approvals := []string{sDim.Render("none yet")}
	if ap != nil {
		approvals = approvals[:0]
		switch {
		case ap.ApprovalsRequired > 0:
			st := sWarn
			if ap.ApprovalsLeft == 0 {
				st = sOK
			}
			approvals = append(approvals, st.Render(fmt.Sprintf("%d of %d", ap.ApprovalsRequired-ap.ApprovalsLeft, ap.ApprovalsRequired)))
		case len(ap.ApprovedBy) == 0:
			approvals = append(approvals, sDim.Render("none yet"))
		}
		for _, u := range ap.ApprovedBy {
			line := sOK.Render(ic.approved) + " @" + u.User.Username
			if me != nil && u.User.Username == me.Username {
				line += sDim.Render(" (a revokes)")
			}
			approvals = append(approvals, line)
		}
	}
	changes := sDim.Render("—")
	if mr.ChangesCount != "" {
		changes = mr.ChangesCount + " files"
	}
	if mr.DivergedCommits > 0 {
		changes += sWarn.Render(fmt.Sprintf(" · %d behind", mr.DivergedCommits))
	}
	threads := sDim.Render("none")
	if ds, _ := get[[]gitlab.Discussion](a.store, discKey(p, mr.IID)); ds != nil {
		total, open := 0, 0
		for _, d := range userThreads(ds) {
			total++
			if n := d.Notes[0]; n.Resolvable && !n.Resolved {
				open++
			}
		}
		if total > 0 {
			threads = fmt.Sprintf("%d", total)
			if open > 0 {
				threads += sWarn.Render(fmt.Sprintf(" · %d unresolved", open))
			} else {
				threads += sOK.Render(" · all resolved")
			}
		}
	}
	listW := w - 2
	if w >= 100 {
		listW = (w-4)/2 - 12
	}
	current := mr.Labels
	labels := []string{sDim.Render("none · l to add")}
	if len(mr.Labels) > 0 {
		known, _ := get[[]gitlab.Label](a.store, labelsKey(p))
		labels = wrapChips(labelChipList(mr.Labels, known), listW)
	}
	for i, l := range labels {
		labels[i] = a.zone(l, zone{click: func(bool) tea.Cmd { return a.editMRLabels(p, mr.IID, current) }})
	}
	facts := []field{
		{"Branches", []string{sKey.Render(mr.SourceBranch) + sDim.Render(" → ") + sKey.Render(mr.TargetBranch)}},
		{"Changes", []string{changes}},
		{"Threads", []string{threads}},
		{"Opened", []string{since(mr.CreatedAt) + " ago"}},
		{"Updated", []string{since(mr.UpdatedAt) + " ago"}},
	}
	lists := []field{
		{"Author", []string{"@" + mr.Author.Username}},
		{"Assignees", peopleLines(mr.Assignees)},
		{"Reviewers", peopleLines(mr.Reviewers)},
		{"Approvals", approvals},
		{"Labels", labels},
	}
	flatten := func(fs []field) [][2]string {
		var out [][2]string
		for _, f := range fs {
			for i, v := range f.v {
				k := ""
				if i == 0 {
					k = sHeader.Render(f.k)
				}
				out = append(out, [2]string{k, v})
			}
		}
		return out
	}
	var rows [][]string
	var cols []col
	if w >= 100 {
		l, r := flatten(facts), flatten(lists)
		for i := 0; i < max(len(l), len(r)); i++ {
			row := make([]string, 4)
			if i < len(l) {
				row[0], row[1] = l[i][0], l[i][1]
			}
			if i < len(r) {
				row[2], row[3] = r[i][0], r[i][1]
			}
			rows = append(rows, row)
		}
		cols = []col{{}, {flex: true, max: (w-4)/2 - 12}, {}, {flex: true}}
	} else {
		for _, kv := range flatten(append(facts, lists...)) {
			rows = append(rows, []string{kv[0], kv[1]})
		}
		cols = []col{{}, {flex: true}}
	}
	t := newTable(cols, rows, w)
	for _, r := range rows {
		add(t.row(r...))
	}

	// pipeline
	add("", rule("Pipeline", w))
	if pl := mr.HeadPipeline; pl != nil {
		id := pl.ID
		line := statusLabel(pl.Status, false) + "  " + sBold.Render(fmt.Sprintf("#%d", pl.ID))
		if d := elapsed(pl.Status, pl.StartedAt, pl.FinishedAt, pl.Duration); d != "" {
			line += sDim.Render("  " + ic.clock + " " + d)
		}
		if pl.Source != "" {
			line += sDim.Render("  " + strings.ReplaceAll(pl.Source, "_", " "))
		}
		line += sDim.Render("  · p or click to open, n to run a new one")
		add(a.zone(fit(line, w), zone{click: func(bool) tea.Cmd { return a.push(newPipelineView(p, id, "")) }}))
		if jobs, _ := get[[]gitlab.Job](a.store, jobsKey(p, pl.ID)); jobs != nil {
			add(stageChips(jobs, w)...)
		}
		if s, _ := get[*gitlab.TestSummary](a.store, testSumKey(p, pl.ID)); s != nil && s.Total.Count > 0 {
			pl := pl
			add(a.zone(fit(sHeader.Render("Tests")+"  "+testsFact(a, p, pl.ID), w),
				zone{click: func(bool) tea.Cmd { return a.openTests(p, pl) }}))
		}
	} else {
		add(sDim.Render("No pipeline yet · n to run one"))
	}

	// description
	add("", rule("Description", w))
	desc := strings.TrimSpace(mr.Description)
	if desc == "" {
		add(sDim.Render("No description."))
	} else {
		add(wrapText(desc, w)...)
	}

	// threads
	if ds, de := get[[]gitlab.Discussion](a.store, discKey(p, mr.IID)); ds != nil {
		threads := userThreads(ds)
		add("", rule(fmt.Sprintf("Threads (%d)", len(threads)), w))
		for _, d := range threads {
			for i, n := range d.Notes {
				indent := ""
				if i > 0 {
					indent = "  ↳ "
				}
				hdr := indent + sKey.Render("@"+n.Author.Username) + sDim.Render(" · "+since(n.CreatedAt)+" ago")
				if i == 0 && n.Resolvable {
					if n.Resolved {
						hdr += sOK.Render("  resolved")
					} else {
						hdr += sWarn.Render("  unresolved")
					}
				}
				if i == 0 && n.Position != nil {
					path, line := n.Position.NewPath, n.Position.NewLine
					if line == 0 {
						path, line = n.Position.OldPath, n.Position.OldLine
					}
					hdr += sDim.Render(fmt.Sprintf("  %s:%d", path, line))
				}
				add(fit(hdr, w))
				pre := "  "
				if i > 0 {
					pre = "    "
				}
				for _, l := range wrapText(n.Body, w-len(pre)) {
					add(pre + l)
				}
			}
			add("")
		}
		if de != nil && de.err != nil {
			add(sErr.Render(de.err.Error()))
		}
	}
	return L
}

func userThreads(ds []gitlab.Discussion) []gitlab.Discussion {
	out := make([]gitlab.Discussion, 0, len(ds))
	for _, d := range ds {
		var notes []gitlab.Note
		for _, n := range d.Notes {
			if !n.System {
				notes = append(notes, n)
			}
		}
		if len(notes) > 0 {
			d.Notes = notes
			out = append(out, d)
		}
	}
	return out
}

var mdLinkRe = regexp.MustCompile(`\[([^\]\n]+)\]\((https?://[^)\s]+)\)`)

func wrapText(s string, w int) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = sanitize(strings.ReplaceAll(s, "\t", "    "))
	// long URLs swamp the text; show the link text (o opens the MR in the browser)
	s = mdLinkRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := mdLinkRe.FindStringSubmatch(m)
		return sKey.Underline(true).Render(sub[1]) + sDim.Render("↗")
	})
	if w < 10 {
		w = 10
	}
	return strings.Split(ansi.Wrap(s, w, ""), "\n")
}

// ---- job ordering shared with the pipeline view ----

type stageGroup struct {
	name string
	jobs []gitlab.Job
}

// groupStages orders stages by the lowest job ID in each (GitLab creates
// jobs in stage order) and jobs by name within a stage.
func groupStages(jobs []gitlab.Job) []stageGroup {
	idx := map[string]int{}
	var groups []stageGroup
	minID := map[string]int{}
	for _, j := range jobs {
		i, ok := idx[j.Stage]
		if !ok {
			i = len(groups)
			idx[j.Stage] = i
			groups = append(groups, stageGroup{name: j.Stage})
			minID[j.Stage] = j.ID
		}
		groups[i].jobs = append(groups[i].jobs, j)
		if j.ID < minID[j.Stage] {
			minID[j.Stage] = j.ID
		}
	}
	sort.SliceStable(groups, func(a, b int) bool { return minID[groups[a].name] < minID[groups[b].name] })
	for _, g := range groups {
		sort.SliceStable(g.jobs, func(a, b int) bool { return naturalLess(g.jobs[a].Name, g.jobs[b].Name) })
	}
	return groups
}

// naturalLess compares strings treating digit runs as numbers, so
// "test 2/10" sorts before "test 10/10".
func naturalLess(a, b string) bool {
	for a != "" && b != "" {
		ad, bd := a[0] >= '0' && a[0] <= '9', b[0] >= '0' && b[0] <= '9'
		if ad && bd {
			i, j := 0, 0
			for i < len(a) && a[i] >= '0' && a[i] <= '9' {
				i++
			}
			for j < len(b) && b[j] >= '0' && b[j] <= '9' {
				j++
			}
			na, nb := strings.TrimLeft(a[:i], "0"), strings.TrimLeft(b[:j], "0")
			if len(na) != len(nb) {
				return len(na) < len(nb)
			}
			if na != nb {
				return na < nb
			}
			a, b = a[i:], b[j:]
			continue
		}
		if a[0] != b[0] {
			return a[0] < b[0]
		}
		a, b = a[1:], b[1:]
	}
	return len(a) < len(b)
}

// stageChips renders a compact "stage ✔✔✘ › stage ●○" summary, wrapped to w.
func stageChips(jobs []gitlab.Job, w int) []string {
	var lines []string
	cur := ""
	for i, g := range groupStages(jobs) {
		var icons strings.Builder
		for _, j := range g.jobs {
			icons.WriteString(statusIcon(j.Status, j.AllowFailure))
		}
		chip := sDim.Render(g.name) + " " + icons.String()
		if i > 0 {
			chip = sDim.Render(" › ") + chip
		}
		if cur != "" && ansi.StringWidth(cur)+ansi.StringWidth(chip) > w {
			lines = append(lines, cur)
			cur = strings.TrimPrefix(chip, sDim.Render(" › "))
		} else {
			cur += chip
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

func (v *mrDetailView) progress(a *App) string { return textProgress(v.scroll, v.height, v.total) }

// badges sums up the MR's state: open/merged/closed, draft, conflicts,
// approval, auto-merge.
func (v *mrDetailView) badges(a *App, mr *gitlab.MR) []string {
	var out []string
	status := mr.DetailedMergeStatus
	if mr.HasConflicts {
		status = "conflict"
	}
	switch mr.State {
	case "opened":
		state := sOK.Render("● Open")
		if phrase := mergePhrase(status); phrase != "" {
			state += sDim.Render(" - ") + phrase
		}
		out = append(out, state)
	case "merged":
		out = append(out, sAccent.Render("● Merged"))
	case "closed", "locked":
		out = append(out, sErr.Render("● Closed"))
	default:
		out = append(out, sDim.Render("● "+mr.State))
	}
	// extras the merge status doesn't already say
	if mr.Draft && status != "draft_status" {
		out = append(out, sWarn.Render(ic.draft+" Draft"))
	}
	if ap, _ := get[*gitlab.Approvals](a.store, apprKey(v.project, mr.IID)); ap != nil && ap.Approved && ap.ApprovalsRequired > 0 {
		out = append(out, sOK.Render(ic.approved+" Approved"))
	}
	if mr.AutoMerge {
		out = append(out, sOK.Render(ic.check+" Auto-merge set"))
	}
	return out
}

// mergePhrase describes GitLab's detailed merge status in words.
func mergePhrase(s string) string {
	switch s {
	case "mergeable":
		return sOK.Render("ready to merge")
	case "not_approved":
		return sWarn.Render("needs approval")
	case "ci_still_running":
		return sDim.Render("waiting for the pipeline")
	case "ci_must_pass":
		return sErr.Render("pipeline must pass")
	case "discussions_not_resolved":
		return sWarn.Render("unresolved threads")
	case "draft_status":
		return sWarn.Render("draft")
	case "need_rebase":
		return sWarn.Render("needs a rebase")
	case "conflict", "broken_status":
		return sErr.Render("has conflicts")
	case "checking", "unchecked", "preparing", "approvals_syncing":
		return sDim.Render("checking mergeability…")
	case "blocked_status", "merge_request_blocked":
		return sWarn.Render("blocked by another MR")
	case "requested_changes":
		return sErr.Render("changes requested")
	case "jira_association_missing":
		return sWarn.Render("needs a Jira issue")
	case "external_status_checks":
		return sWarn.Render("waiting on status checks")
	case "security_policy_violations", "security_policy_pipeline_check":
		return sWarn.Render("blocked by security policy")
	case "locked_paths", "locked_lfs_files":
		return sWarn.Render("locked files")
	case "commits_status":
		return sWarn.Render("commit checks failing")
	case "", "not_open":
		return ""
	}
	return sDim.Render(strings.ReplaceAll(s, "_", " "))
}

// rule is a section heading drawn across the width.
func rule(title string, w int) string {
	t := "── " + title + " "
	return sHeader.Render(t + strings.Repeat("─", max(0, w-ansi.StringWidth(t))))
}

// peopleLines lists users one per line, folding long lists.
func peopleLines(us []gitlab.User) []string {
	if len(us) == 0 {
		return []string{sDim.Render("none")}
	}
	const most = 5
	var out []string
	for i, u := range us {
		if i == most-1 && len(us) > most {
			out = append(out, sDim.Render(fmt.Sprintf("+%d more", len(us)-i)))
			break
		}
		out = append(out, "@"+u.Username)
	}
	return out
}

// wrapChips packs chips onto lines no wider than w.
func wrapChips(chips []string, w int) []string {
	var lines []string
	cur := ""
	for _, c := range chips {
		if cur != "" && ansi.StringWidth(cur)+2+ansi.StringWidth(c) > w {
			lines = append(lines, cur)
			cur = ""
		}
		if cur != "" {
			cur += "  "
		}
		cur += c
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}
