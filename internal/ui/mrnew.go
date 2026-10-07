package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"gitlab-tui/internal/gitlab"
)

// mrNewView is the form for opening a merge request, or for editing one
// (when orig is set). When opening one, the title and description are
// filled in from the commits (or the project's MR template) until the user
// edits them.
type mrNewView struct {
	project        string
	source, target string
	orig           *gitlab.MR // the MR being edited

	titleIn     textinput.Model
	desc        textarea.Model
	titleEdited bool
	descEdited  bool
	autoFor     string // source/target/template the auto-fill was done for
	template    string // MR template key; "" for none
	tmplChosen  bool   // default template decided

	labels    []string
	assignees []gitlab.User // editing; a new MR has assignMe instead
	reviewers []gitlab.User

	assignMe, draft, removeSource, squash bool
	defaultsSet                           bool

	focus      int
	typing     bool // the cursor is in the title or description
	lastSubmit time.Time
}

const (
	fSource = iota
	fTarget
	fTemplate
	fTitle
	fDesc
	fLabels
	fAssignees
	fReviewers
	fAssign
	fDraft
	fRemove
	fSquash
	fCreate
	nFields
)

func newMRForm(project, source string) *mrNewView {
	ti := textinput.New()
	ti.Prompt = ""
	ti.Placeholder = "Title"
	ta := textarea.New()
	ta.ShowLineNumbers = false
	ta.Prompt = ""
	ta.CharLimit = 0
	ta.Placeholder = "Description (markdown)"
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	v := &mrNewView{project: project, source: source, titleIn: ti, desc: ta, assignMe: true, removeSource: true}
	if source == "" {
		v.focus = fSource
	} else {
		v.focus = fTitle
	}
	return v
}

// newMREditForm edits an existing MR.
func newMREditForm(project string, mr *gitlab.MR) *mrNewView {
	v := newMRForm(project, mr.SourceBranch)
	v.orig = mr
	v.target = mr.TargetBranch
	v.titleIn.SetValue(strings.TrimSpace(draftRe.ReplaceAllString(mr.Title, "")))
	v.titleIn.CursorEnd()
	v.desc.SetValue(mr.Description)
	v.labels = append([]string(nil), mr.Labels...)
	v.assignees = append([]gitlab.User(nil), mr.Assignees...)
	v.reviewers = append([]gitlab.User(nil), mr.Reviewers...)
	v.draft, v.removeSource, v.squash = mr.Draft, mr.RemoveSourceBranch, mr.Squash
	v.defaultsSet, v.tmplChosen = true, true
	return v
}

func (v *mrNewView) editing() bool { return v.orig != nil }

// fields lists the form's fields in tab order.
func (v *mrNewView) fields() []int {
	if v.editing() {
		return []int{fTitle, fTarget, fLabels, fAssignees, fReviewers, fDraft, fRemove, fSquash, fDesc, fCreate}
	}
	return []int{fTitle, fSource, fTarget, fTemplate, fLabels, fReviewers, fAssign, fDraft, fRemove, fSquash, fDesc, fCreate}
}

// step moves the focus d fields along, wrapping around.
func (v *mrNewView) step(d int) tea.Cmd {
	fs := v.fields()
	i := 0
	for j, f := range fs {
		if f == v.focus {
			i = j
		}
	}
	return v.setFocus(fs[((i+d)%len(fs)+len(fs))%len(fs)])
}

// NewMRForm opens the create-merge-request form, with source preselected
// if it's not empty.
func NewMRForm(project, source string) View { return newMRForm(project, source) }

func (v *mrNewView) title() string {
	if v.editing() {
		return fmt.Sprintf("edit !%d", v.orig.IID)
	}
	return "new merge request"
}

func (v *mrNewView) proj() string { return v.project }

// capturing: while typing in a text field, q and friends are typed, not
// global shortcuts.
func (v *mrNewView) capturing() bool { return v.typing }

func projInfoKey(p string) string          { return "projinfo:" + p }
func branchesKey(p string) string          { return "branches:" + p }
func membersKey(p string) string           { return "members:" + p }
func compareKey(p, from, to string) string { return fmt.Sprintf("compare:%s:%s:%s", p, from, to) }
func mrTemplatesKey(p string) string       { return "mrtemplates:" + p }
func mrTemplateKey(p, key string) string   { return fmt.Sprintf("mrtemplate:%s:%s", p, key) }

const meKey = "me"

func (v *mrNewView) refresh(a *App, force bool) tea.Cmd {
	p := v.project
	if v.editing() {
		return fetchLabels(a, p, 10*time.Minute)
	}
	age := func(d time.Duration) time.Duration {
		if force {
			return 0
		}
		return d
	}
	cmds := []tea.Cmd{
		fetch(a.store, projInfoKey(p), age(10*time.Minute), func(ctx ctxT) (*gitlab.ProjectInfo, error) { return a.client.GetProject(ctx, p) }),
		fetch(a.store, branchesKey(p), age(time.Minute), func(ctx ctxT) ([]gitlab.Branch, error) { return a.client.ListBranches(ctx, p) }),
		fetch(a.store, meKey, age(time.Hour), a.client.CurrentUser),
		fetch(a.store, mrTemplatesKey(p), age(10*time.Minute), func(ctx ctxT) ([]gitlab.Template, error) { return a.client.ListMRTemplates(ctx, p) }),
	}
	// like GitLab, start from the template called "default", if any
	if ts, e := get[[]gitlab.Template](a.store, mrTemplatesKey(p)); !v.tmplChosen && e != nil && (e.val != nil || e.err != nil) {
		v.tmplChosen = true
		for _, t := range ts {
			if strings.EqualFold(t.Name, "default") {
				v.template = t.Key
			}
		}
	}
	if info, _ := get[*gitlab.ProjectInfo](a.store, projInfoKey(p)); info != nil && !v.defaultsSet {
		v.defaultsSet = true
		if v.target == "" {
			v.target = info.DefaultBranch
		}
		v.removeSource = info.RemoveSourceBranch
		v.squash = info.SquashOption == "always" || info.SquashOption == "default_on"
	}
	src, tgt := v.source, v.target
	if src != "" {
		cmds = append(cmds, fetch(a.store, mrForKey(p, src), age(30*time.Second), func(ctx ctxT) (*gitlab.MR, error) {
			return a.client.FindMRForBranch(ctx, p, src)
		}))
	}
	if key := v.template; key != "" {
		cmds = append(cmds, fetch(a.store, mrTemplateKey(p, key), age(10*time.Minute), func(ctx ctxT) (string, error) {
			return a.client.GetMRTemplate(ctx, p, key)
		}))
	}
	if src != "" && tgt != "" && src != tgt {
		cmds = append(cmds, fetch(a.store, compareKey(p, tgt, src), age(30*time.Second), func(ctx ctxT) ([]gitlab.Commit, error) {
			return a.client.Compare(ctx, p, tgt, src)
		}))
	}
	v.autofill(a)
	return tea.Batch(cmds...)
}

// autofill sets the title and description from the commits and template
// once they've loaded, unless the user has typed their own.
func (v *mrNewView) autofill(a *App) {
	p, src, tgt := v.project, v.source, v.target
	key := src + "\x00" + tgt + "\x00" + v.template
	if src == "" || tgt == "" || src == tgt || v.autoFor == key || !v.tmplChosen {
		return
	}
	commits, ce := get[[]gitlab.Commit](a.store, compareKey(p, tgt, src))
	if ce == nil || ce.val == nil {
		return
	}
	tmpl := ""
	if v.template != "" {
		t, te := get[string](a.store, mrTemplateKey(p, v.template))
		if te == nil || (te.val == nil && te.err == nil) {
			return
		}
		tmpl = t
	}
	info, _ := get[*gitlab.ProjectInfo](a.store, projInfoKey(p))
	v.autoFor = key

	title, body := humanizeBranch(src), ""
	if len(commits) == 1 {
		title = commits[0].Title
		_, body, _ = strings.Cut(strings.TrimSpace(commits[0].Message), "\n")
		body = strings.TrimSpace(body)
	}
	if strings.TrimSpace(tmpl) != "" {
		body = tmpl
	} else if v.template == "" && info != nil && strings.TrimSpace(info.MergeRequestTemplate) != "" {
		// the project's own default description setting
		body = info.MergeRequestTemplate
	}
	if !v.titleEdited {
		v.titleIn.SetValue(title)
		v.titleIn.CursorEnd()
	}
	if !v.descEdited {
		v.desc.SetValue(body)
	}
}

// humanizeBranch turns "123-fix-the-thing" into "123 fix the thing" with
// a capital first letter, like GitLab's default title.
func humanizeBranch(b string) string {
	if i := strings.LastIndexByte(b, '/'); i >= 0 {
		b = b[i+1:]
	}
	b = strings.Join(strings.FieldsFunc(b, func(r rune) bool { return r == '-' || r == '_' }), " ")
	r := []rune(b)
	if len(r) > 0 {
		r[0] = unicode.ToUpper(r[0])
	}
	return string(r)
}

func (v *mrNewView) help() []kb {
	save := "create"
	if v.editing() {
		save = "save"
	}
	if v.typing {
		h := []kb{{"esc", "done editing"}}
		if v.focus == fDesc {
			h = append(h, kb{"ctrl+e", "$EDITOR"})
		}
		return append(h, kb{"ctrl+s", save})
	}
	h := []kb{{"j/k", "move"}, {"enter", "edit/choose/toggle"}}
	if v.focus == fDesc {
		h = append(h, kb{"ctrl+e", "$EDITOR"})
	}
	return append(h, kb{"ctrl+s", save}, kb{"esc", "cancel"})
}

func isTextField(f int) bool { return f == fTitle || f == fDesc }

// setFocus moves to field f, leaving any text field being typed in.
func (v *mrNewView) setFocus(f int) tea.Cmd {
	v.focus = f
	v.typing = false
	v.titleIn.Blur()
	v.desc.Blur()
	return nil
}

// startTyping puts the cursor in the focused text field.
func (v *mrNewView) startTyping() tea.Cmd {
	v.typing = true
	switch v.focus {
	case fTitle:
		return v.titleIn.Focus()
	case fDesc:
		return v.desc.Focus()
	}
	v.typing = false
	return nil
}

func (v *mrNewView) dirty() bool {
	if v.editing() {
		return len(v.changes()) > 0
	}
	return v.titleEdited || v.descEdited
}

// guardLeave asks before going back with unsaved changes.
func (v *mrNewView) guardLeave(a *App) bool {
	if !v.dirty() {
		return false
	}
	q := "Discard this merge request?"
	if v.editing() {
		q = "Discard your changes?"
	}
	a.confirm(q, func() tea.Cmd { return a.pop() })
	return true
}

// key: the form starts out navigating, so j/k move between fields; enter
// on the title or description starts typing there and esc stops.
func (v *mrNewView) key(a *App, msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "ctrl+s":
		v.setFocus(v.focus)
		return v.submit(a)
	case "ctrl+e":
		if v.focus == fDesc {
			return a.editText(v.desc.Value(), func(text string) tea.Cmd {
				if text != v.desc.Value() {
					v.desc.SetValue(text)
					v.descEdited = true
				}
				return nil
			})
		}
	}
	if v.typing {
		switch msg.String() {
		case "esc":
			return v.setFocus(v.focus)
		case "tab":
			return v.step(1)
		case "shift+tab":
			return v.step(-1)
		}
		switch v.focus {
		case fTitle:
			if msg.String() == "enter" {
				return v.setFocus(v.focus)
			}
			before := v.titleIn.Value()
			var cmd tea.Cmd
			v.titleIn, cmd = v.titleIn.Update(msg)
			if v.titleIn.Value() != before {
				v.titleEdited = true
			}
			return cmd
		case fDesc:
			before := v.desc.Value()
			var cmd tea.Cmd
			v.desc, cmd = v.desc.Update(msg)
			if v.desc.Value() != before {
				v.descEdited = true
			}
			return cmd
		}
		return nil
	}
	fs := v.fields()
	switch msg.String() {
	case "j", "down", "tab", "ctrl+n":
		return v.step(1)
	case "k", "up", "shift+tab", "ctrl+p":
		return v.step(-1)
	case "g", "home":
		return v.setFocus(fs[0])
	case "G", "end":
		return v.setFocus(fs[len(fs)-1])
	case "enter", " ", "i", "l", "right":
		if isTextField(v.focus) {
			return v.startTyping()
		}
		return v.activate(a, v.focus)
	case "esc":
		if v.guardLeave(a) {
			return nil
		}
		return a.pop()
	}
	return nil
}

// activate opens the field's chooser or toggles its checkbox.
func (v *mrNewView) activate(a *App, f int) tea.Cmd {
	p := v.project
	switch f {
	case fSource:
		return a.chooseBranch(p, "Source branch", nil, v.source, func(b string) tea.Cmd {
			v.source = b
			return v.refresh(a, false)
		})
	case fTarget:
		return a.chooseBranch(p, "Target branch", nil, v.target, func(b string) tea.Cmd {
			v.target = b
			return v.refresh(a, false)
		})
	case fTemplate:
		return a.chooseTemplate(p, func(key string) tea.Cmd {
			apply := func() tea.Cmd {
				v.template, v.descEdited = key, false
				return v.refresh(a, false)
			}
			if v.descEdited {
				a.confirm("Replace your description with the template?", apply)
				return nil
			}
			return apply()
		})
	case fLabels:
		return a.chooseLabels(p, "Labels", v.labels, func(a *App, chosen []string) tea.Cmd {
			v.labels = chosen
			return nil
		})
	case fAssignees:
		return a.chooseMembers(p, "Assignees", v.assignees, func(us []gitlab.User) { v.assignees = us })
	case fReviewers:
		return a.chooseMembers(p, "Reviewers", v.reviewers, func(us []gitlab.User) { v.reviewers = us })
	case fAssign:
		v.assignMe = !v.assignMe
	case fDraft:
		v.draft = !v.draft
	case fRemove:
		v.removeSource = !v.removeSource
	case fSquash:
		v.squash = !v.squash
	case fCreate:
		return v.submit(a)
	}
	return nil
}

func (v *mrNewView) submit(a *App) tea.Cmd {
	if v.editing() {
		return v.save(a)
	}
	title := strings.TrimSpace(v.titleIn.Value())
	switch {
	case v.source == "":
		a.setFlash("choose a source branch", true)
		return v.setFocus(fSource)
	case v.target == "":
		a.setFlash("choose a target branch", true)
		return v.setFocus(fTarget)
	case v.source == v.target:
		a.setFlash("source and target are the same branch", true)
		return v.setFocus(fTarget)
	case title == "":
		a.setFlash("the title is empty", true)
		return v.setFocus(fTitle)
	case time.Since(v.lastSubmit) < 5*time.Second:
		return nil
	}
	v.lastSubmit = time.Now()
	if v.draft && !draftRe.MatchString(title) {
		title = "Draft: " + title
	}
	req := gitlab.NewMR{
		SourceBranch:       v.source,
		TargetBranch:       v.target,
		Title:              title,
		Description:        v.desc.Value(),
		Labels:             strings.Join(v.labels, ","),
		RemoveSourceBranch: v.removeSource,
		Squash:             v.squash,
	}
	if me, _ := get[*gitlab.User](a.store, meKey); me != nil && v.assignMe {
		req.AssigneeIDs = []int{me.ID}
	}
	for _, u := range v.reviewers {
		req.ReviewerIDs = append(req.ReviewerIDs, u.ID)
	}
	p := v.project
	inval := append([]string{mrForKey(p, v.source), dashKey}, a.keysWithPrefix(mrListPrefix(p))...)
	var created *gitlab.MR
	return a.action("create merge request", func() (err error) {
		created, err = a.client.CreateMR(bg(), p, req)
		return err
	}, inval, func(a *App) tea.Cmd {
		a.setFlash(fmt.Sprintf("created !%d", created.IID), false)
		mr := newMRDetail(p, created.IID, "", nil)
		if a.top() == view(v) {
			return a.replace(mr)
		}
		return a.push(mr)
	})
}

// changes lists the edited MR's fields that differ from what it had, as
// the API wants them.
func (v *mrNewView) changes() map[string]any {
	mr := v.orig
	out := map[string]any{}
	title := strings.TrimSpace(v.titleIn.Value())
	if v.draft {
		title = "Draft: " + title
	}
	// only send the title if it or the draft state changed, keeping the
	// MR's own draft spelling ("[Draft]", "Draft -") otherwise
	bare := strings.TrimSpace(draftRe.ReplaceAllString(mr.Title, ""))
	if v.draft != mr.Draft || strings.TrimSpace(v.titleIn.Value()) != bare {
		out["title"] = title
	}
	if v.desc.Value() != mr.Description {
		out["description"] = v.desc.Value()
	}
	if v.target != mr.TargetBranch {
		out["target_branch"] = v.target
	}
	if !sameStrings(v.labels, mr.Labels) {
		out["labels"] = strings.Join(v.labels, ",")
	}
	if ids, changed := userIDs(v.assignees, mr.Assignees); changed {
		out["assignee_ids"] = ids
	}
	if ids, changed := userIDs(v.reviewers, mr.Reviewers); changed {
		out["reviewer_ids"] = ids
	}
	if v.removeSource != mr.RemoveSourceBranch {
		out["remove_source_branch"] = v.removeSource
	}
	if v.squash != mr.Squash {
		out["squash"] = v.squash
	}
	return out
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[string]bool{}
	for _, x := range a {
		seen[x] = true
	}
	for _, x := range b {
		if !seen[x] {
			return false
		}
	}
	return true
}

// userIDs gives the IDs to send for now, and whether they differ from was.
// GitLab clears the list when sent [0].
func userIDs(now, was []gitlab.User) ([]int, bool) {
	ids := []int{}
	set := map[int]bool{}
	for _, u := range now {
		ids = append(ids, u.ID)
		set[u.ID] = true
	}
	changed := len(now) != len(was)
	for _, u := range was {
		if !set[u.ID] {
			changed = true
		}
	}
	if len(ids) == 0 {
		ids = []int{0}
	}
	return ids, changed
}

func (v *mrNewView) save(a *App) tea.Cmd {
	if strings.TrimSpace(v.titleIn.Value()) == "" {
		a.setFlash("the title is empty", true)
		return v.setFocus(fTitle)
	}
	if time.Since(v.lastSubmit) < 5*time.Second {
		return nil
	}
	fields := v.changes()
	if len(fields) == 0 {
		a.setFlash("nothing changed", false)
		return a.pop()
	}
	v.lastSubmit = time.Now()
	p, iid := v.project, v.orig.IID
	inval := append([]string{mrKey(p, iid), apprKey(p, iid), dashKey}, a.keysWithPrefix(mrListPrefix(p))...)
	return a.action(fmt.Sprintf("update !%d", iid), func() error {
		_, err := a.client.UpdateMR(bg(), p, iid, fields)
		return err
	}, inval, func(a *App) tea.Cmd {
		if a.top() == view(v) {
			return a.pop()
		}
		return nil
	})
}

func (v *mrNewView) render(a *App, w, h int) string {
	p := v.project
	// label column: caret, the longest label, then the column gap
	const labelW = len("Delete source branch")
	const lw = 2 + labelW + len(colGap)
	vw := max(20, w-lw)
	var L []string
	add := func(s ...string) { L = append(L, s...) }

	row := func(f int, label, value string) {
		head := "  " + sDim.Render(pad(label, labelW)) + colGap
		if f < 0 {
			add(pad(head+value, w))
			return
		}
		if v.focus == f {
			head = sActive.Render(ic.caret+" ") + sActiveT.Render(pad(label, labelW)) + colGap
		}
		line := head + value
		add(a.zone(pad(line, w), zone{click: func(bool) tea.Cmd {
			v.setFocus(f)
			if isTextField(f) {
				return v.startTyping()
			}
			return v.activate(a, f)
		}}))
	}
	branch := func(b string) string {
		if b == "" {
			return sDim.Render("choose… (enter)")
		}
		return sKey.Render(b) + sDim.Render("  ▾")
	}
	check := func(on bool) string {
		if on {
			return sOK.Render(ic.checked)
		}
		return sDim.Render(ic.unchecked)
	}

	titleRow := func() {
		v.titleIn.Width = vw - 1
		row(fTitle, "Title", v.titleIn.View())
		add("")
	}
	if v.editing() {
		add(sTitle.Render(fmt.Sprintf("Edit !%d", v.orig.IID))+sDim.Render(" in "+p), "")
		titleRow()
		row(-1, "Source branch", sKey.Render(v.source)+sDim.Render("  (can't be changed)"))
		row(fTarget, "Target branch", branch(v.target))
		add("")
	} else {
		add(sTitle.Render("New merge request")+sDim.Render(" in "+p), "")
		titleRow()
		row(fSource, "Source branch", branch(v.source))
		row(fTarget, "Target branch", branch(v.target))
		add(strings.Repeat(" ", lw) + v.status(a))
		tmpl := sDim.Render("none")
		if v.template != "" {
			tmpl = v.template + sDim.Render("  ▾")
		} else if ts, _ := get[[]gitlab.Template](a.store, mrTemplatesKey(p)); len(ts) > 0 {
			tmpl = sDim.Render(fmt.Sprintf("none (%d available)  ▾", len(ts)))
		}
		row(fTemplate, "Template", tmpl)
		add("")
	}

	labels := sDim.Render("none")
	if len(v.labels) > 0 {
		known, _ := get[[]gitlab.Label](a.store, labelsKey(p))
		labels = labelChips(v.labels, known)
	}
	row(fLabels, "Labels", fit(labels, vw))
	people := func(us []gitlab.User) string {
		if len(us) == 0 {
			return sDim.Render("none")
		}
		var names []string
		for _, u := range us {
			names = append(names, "@"+u.Username)
		}
		return fit(strings.Join(names, " "), vw)
	}
	if v.editing() {
		row(fAssignees, "Assignees", people(v.assignees))
		row(fReviewers, "Reviewers", people(v.reviewers))
	} else {
		row(fReviewers, "Reviewers", people(v.reviewers))
		row(fAssign, "Assign to me", check(v.assignMe))
	}
	row(fDraft, "Draft", check(v.draft))
	row(fRemove, "Delete source branch", check(v.removeSource)+sDim.Render("  when merged"))
	row(fSquash, "Squash commits", check(v.squash)+sDim.Render("  when merged"))
	add("")
	// the description is last, taking the height left above the button
	descH := max(3, h-len(L)-2)
	v.desc.SetWidth(vw)
	v.desc.SetHeight(descH)
	descLines := strings.Split(v.desc.View(), "\n")
	for i, dl := range descLines {
		if i == 0 {
			row(fDesc, "Description", dl)
		} else if i == 1 {
			hint := pad(sDim.Render("  ctrl+e: $EDITOR"), lw)
			add(a.zone(hint+dl, zone{click: func(bool) tea.Cmd { v.setFocus(fDesc); return v.startTyping() }}))
		} else {
			add(a.zone(strings.Repeat(" ", lw)+dl, zone{click: func(bool) tea.Cmd { v.setFocus(fDesc); return v.startTyping() }}))
		}
	}
	add("")
	btn := "Create merge request"
	if v.editing() {
		btn = "Save changes"
	}
	row(fCreate, "", button(btn, v.focus == fCreate)+sDim.Render("  ctrl+s"))
	return strings.Join(L, "\n")
}

// status summarises what the MR would contain and anything in the way.
func (v *mrNewView) status(a *App) string {
	p, src, tgt := v.project, v.source, v.target
	if src == "" || tgt == "" {
		return ""
	}
	if src == tgt {
		return sErr.Render(ic.conflict + " source and target are the same")
	}
	var parts []string
	if bs, _ := get[[]gitlab.Branch](a.store, branchesKey(p)); bs != nil {
		found := false
		for _, b := range bs {
			if b.Name == src {
				found = true
				break
			}
		}
		if !found {
			return sWarn.Render(ic.conflict + " " + src + " isn't on the server — push it first (git push -u origin " + src + ")")
		}
	}
	commits, ce := get[[]gitlab.Commit](a.store, compareKey(p, tgt, src))
	switch {
	case ce == nil || (ce.val == nil && ce.loading):
		parts = append(parts, sDim.Render(spinnerFrame()+" comparing…"))
	case ce.err != nil && ce.val == nil:
		parts = append(parts, sErr.Render(ce.err.Error()))
	case len(commits) == 0:
		parts = append(parts, sWarn.Render(ic.conflict+" no commits between "+tgt+" and "+src))
	case len(commits) == 1:
		parts = append(parts, sDim.Render("1 commit"))
	default:
		parts = append(parts, sDim.Render(strconv.Itoa(len(commits))+" commits"))
	}
	if mr, _ := get[*gitlab.MR](a.store, mrForKey(p, src)); mr != nil {
		parts = append(parts, sWarn.Render(fmt.Sprintf("%s !%d is already open for %s", ic.conflict, mr.IID, src)))
	}
	return strings.Join(parts, sDim.Render(" · "))
}

// chooseBranch opens a single-select over the project's branches, most
// recently updated first, after any extra choices (which come first and
// hide a branch with the same ID). initial is highlighted to start with.
func (a *App) chooseBranch(project, title string, extra []choice, initial string, done func(string) tea.Cmd) tea.Cmd {
	for i := range extra {
		extra[i].order = i - len(extra)
	}
	return a.openChooser(&chooser{
		title:   title,
		prompt:  ic.branch + " Branches",
		initial: initial,
		load: func(a *App, force bool) tea.Cmd {
			age := time.Minute
			if force {
				age = 0
			}
			return fetch(a.store, branchesKey(project), age, func(ctx ctxT) ([]gitlab.Branch, error) {
				return a.client.ListBranches(ctx, project)
			})
		},
		items: func(a *App) ([]choice, bool) {
			bs, e := get[[]gitlab.Branch](a.store, branchesKey(project))
			out := append(make([]choice, 0, len(extra)+len(bs)), extra...)
			for i, b := range bs {
				desc := b.Commit.Title + " · " + since(b.Commit.CommittedDate)
				if b.Default {
					desc = "default · " + desc
				} else if b.Protected {
					desc = "protected · " + desc
				}
				out = append(out, choice{id: b.Name, text: b.Name, desc: desc, order: i})
			}
			return out, e == nil || e.val == nil
		},
		done: func(a *App, chosen []string) tea.Cmd { return done(chosen[0]) },
	}, nil)
}

// chooseMembers opens a multi-select over the project's members.
func (a *App) chooseMembers(project, title string, current []gitlab.User, done func([]gitlab.User)) tea.Cmd {
	var ids []string
	for _, u := range current {
		ids = append(ids, strconv.Itoa(u.ID))
	}
	return a.openChooser(&chooser{
		title:  title,
		prompt: ic.user + " Members",
		multi:  true,
		load: func(a *App, force bool) tea.Cmd {
			age := 10 * time.Minute
			if force {
				age = 0
			}
			return fetch(a.store, membersKey(project), age, func(ctx ctxT) ([]gitlab.User, error) {
				return a.client.ListMembers(ctx, project)
			})
		},
		items: func(a *App) ([]choice, bool) {
			us, e := get[[]gitlab.User](a.store, membersKey(project))
			out := make([]choice, 0, len(us))
			for _, u := range us {
				out = append(out, choice{id: strconv.Itoa(u.ID), text: u.Username, desc: u.Name})
			}
			sortChoices(out)
			return out, e == nil || e.val == nil
		},
		done: func(a *App, chosen []string) tea.Cmd {
			us, _ := get[[]gitlab.User](a.store, membersKey(project))
			byID := map[string]gitlab.User{}
			for _, u := range append(us, current...) {
				byID[strconv.Itoa(u.ID)] = u
			}
			var out []gitlab.User
			for _, id := range chosen {
				if u, ok := byID[id]; ok {
					out = append(out, u)
				}
			}
			done(out)
			return nil
		},
	}, ids)
}

// chooseTemplate picks an MR description template ("" for none).
func (a *App) chooseTemplate(project string, done func(string) tea.Cmd) tea.Cmd {
	return a.openChooser(&chooser{
		title:  "Description templates",
		prompt: ic.mr + " Templates",
		load: func(a *App, force bool) tea.Cmd {
			return fetch(a.store, mrTemplatesKey(project), 10*time.Minute, func(ctx ctxT) ([]gitlab.Template, error) {
				return a.client.ListMRTemplates(ctx, project)
			})
		},
		items: func(a *App) ([]choice, bool) {
			ts, e := get[[]gitlab.Template](a.store, mrTemplatesKey(project))
			var out []choice
			for _, t := range ts {
				out = append(out, choice{id: t.Key, text: t.Name})
			}
			sortChoices(out)
			out = append([]choice{{id: "", text: "(no template)", order: -1}}, out...)
			return out, e == nil || e.val == nil
		},
		done: func(a *App, chosen []string) tea.Cmd { return done(chosen[0]) },
	}, nil)
}
