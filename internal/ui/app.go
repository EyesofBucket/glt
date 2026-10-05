// Package ui implements the glt terminal interface.
package ui

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/aymanbagabas/go-osc52/v2"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"gitlab-tui/internal/config"
	"gitlab-tui/internal/gitlab"
)

// view is one screen on the navigation stack.
type view interface {
	title() string
	// proj is the project the view belongs to ("" for global views).
	proj() string
	// refresh returns commands to fetch whatever is stale. force ignores
	// the usual polling interval.
	refresh(a *App, force bool) tea.Cmd
	key(a *App, msg tea.KeyMsg) tea.Cmd
	render(a *App, w, h int) string
	help() []kb
	// capturing reports whether the view wants raw keys (text input).
	capturing() bool
}

// View is a screen that can be pushed onto the stack.
type View = view

// kb is a key/label pair for the footer and help screen.
type kb struct{ k, d string }

type Options struct {
	Notify  bool
	Preview bool         // log preview pane in the pipeline view
	Mouse   bool         // mouse captured
	Config  *config.File // for the settings popup; may be nil
}

type App struct {
	ctx    *config.Context
	client *gitlab.Client
	store  *Store
	opts   Options

	stack []view
	w, h  int

	flash    string
	flashErr bool
	flashAt  time.Time

	modal   *modal
	overlay overlay // floating picker (projects, labels), if open
	ms      mouseState

	pickerTree bool // project picker opens in tree mode
	showHelp   bool
	spinning   bool

	recent    []recentProject
	projIDs   map[string]int // project path -> numeric ID, as learned
	projPaths map[int]string
	// pipeline statuses we've seen, for completion notifications
	seenStatus map[int]string
}

// overlay is a floating finder drawn over the current view; it takes all
// keys while open.
type overlay interface {
	key(a *App, msg tea.KeyMsg) tea.Cmd
	render(a *App, body string, w, h int) string
	help() []kb
	refresh(a *App, force bool) tea.Cmd
	debounced(a *App, seq int) tea.Cmd
	passthrough(msg tea.Msg) tea.Cmd
}

type modal struct {
	prompt  string
	onYes   func() tea.Cmd
	input   *textarea.Model
	onInput func(string) tea.Cmd
}

type tickMsg time.Time
type flashMsg struct {
	text string
	err  bool
}

// actionMsg is the result of a write operation.
type actionMsg struct {
	ok         string
	err        error
	invalidate []string
	then       func(a *App) tea.Cmd
}

type debounceMsg struct {
	v   any
	seq int
}

func New(ctx *config.Context, client *gitlab.Client, stack []View, opts Options) *App {
	a := &App{
		ctx:        ctx,
		client:     client,
		store:      newStore(ctx.Host),
		opts:       opts,
		stack:      stack,
		recent:     loadRecent(ctx.Host),
		projIDs:    map[string]int{},
		projPaths:  map[int]string{},
		seenStatus: map[int]string{},
	}
	if opts.Config != nil {
		a.pickerTree = opts.Config.UI.Picker == "tree"
	}
	for _, r := range a.recent {
		if r.ID != 0 {
			a.learnProject(r.Path, r.ID)
		}
	}
	a.touchProject(a.top().proj())
	return a
}

// NewDashboard is the global (not project-specific) home view.
func NewDashboard() View { return newDashboard() }

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (a *App) top() view { return a.stack[len(a.stack)-1] }

func (a *App) Init() tea.Cmd {
	return tea.Batch(tick(), a.top().refresh(a, true), a.warmProjects(),
		fetch(a.store, meKey, time.Hour, a.client.CurrentUser))
}

// warmProjects loads the project list in the background shortly after
// start, so the first P is instant.
func (a *App) warmProjects() tea.Cmd {
	return tea.Tick(1500*time.Millisecond, func(time.Time) tea.Msg { return warmMsg{} })
}

type warmMsg struct{}

func (a *App) learnProject(path string, id int) {
	if path == "" || id == 0 {
		return
	}
	a.projIDs[path] = id
	a.projPaths[id] = path
}

// projRef maps a numeric project ID to the string used in keys/URLs,
// preferring the path form so caches are shared between views.
func (a *App) projRef(id int) string {
	if p, ok := a.projPaths[id]; ok {
		return p
	}
	return strconv.Itoa(id)
}

func (a *App) push(v view) tea.Cmd {
	a.stack = append(a.stack, v)
	a.touchProject(v.proj())
	return v.refresh(a, false)
}

// back goes back one view; at the root there's nowhere to go.
func (a *App) back() tea.Cmd {
	if len(a.stack) <= 1 {
		a.setFlash("q or ctrl+c to quit", false)
		return nil
	}
	return a.pop()
}

// atHome reports whether glt is on its starting screen, where q quits: the
// dashboard, or the cwd project's home page sitting right on top of it.
func (a *App) atHome() bool {
	if len(a.stack) == 1 {
		return true
	}
	pv, ok := a.top().(*projectView)
	return ok && len(a.stack) == 2 && a.ctx.Project != "" && pv.project == a.ctx.Project
}

// popTo goes back to stack index i.
func (a *App) popTo(i int) tea.Cmd {
	if i < 0 || i >= len(a.stack)-1 {
		return nil
	}
	a.stack = a.stack[:i+1]
	return a.top().refresh(a, false)
}

func (a *App) pop() tea.Cmd {
	if len(a.stack) <= 1 {
		return nil
	}
	a.stack = a.stack[:len(a.stack)-1]
	return a.top().refresh(a, false)
}

// replace swaps the top view (used when resolving e.g. "MR for branch").
func (a *App) replace(v view) tea.Cmd {
	a.stack[len(a.stack)-1] = v
	return v.refresh(a, true)
}

// home returns to the dashboard, keeping it as the stack root.
func (a *App) home() tea.Cmd {
	if _, ok := a.stack[0].(*dashboardView); ok {
		a.stack = a.stack[:1]
	} else {
		a.stack = []view{newDashboard()}
	}
	return a.top().refresh(a, false)
}

// openProject jumps to a project page: the dashboard and the project's
// home page stay underneath, so esc leads back through them.
func (a *App) openProject(v view) tea.Cmd {
	a.home()
	if _, ok := v.(*projectView); ok {
		return a.push(v)
	}
	return tea.Batch(a.push(newProjectView(v.proj())), a.push(v))
}

func (a *App) setFlash(s string, isErr bool) {
	a.flash, a.flashErr, a.flashAt = s, isErr, time.Now()
}

func (a *App) confirm(prompt string, onYes func() tea.Cmd) {
	a.modal = &modal{prompt: prompt, onYes: onYes}
}

func (a *App) prompt(title string, onInput func(string) tea.Cmd) tea.Cmd {
	ta := textarea.New()
	ta.Placeholder = "Write a comment… (ctrl+s to send, esc to cancel)"
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.SetWidth(max(20, a.w-4))
	ta.SetHeight(max(3, min(12, a.h/3)))
	a.modal = &modal{prompt: title, input: &ta, onInput: onInput}
	return ta.Focus()
}

// action runs a write in the background and reports the outcome.
func (a *App) action(desc string, f func() error, invalidate []string, then func(a *App) tea.Cmd) tea.Cmd {
	a.setFlash(desc+"…", false)
	return func() tea.Msg {
		err := f()
		return actionMsg{ok: desc, err: err, invalidate: invalidate, then: then}
	}
}

// keysWithPrefix lists cached keys starting with any of prefixes.
func (a *App) keysWithPrefix(prefixes ...string) []string {
	var ks []string
	for k := range a.store.m {
		for _, p := range prefixes {
			if strings.HasPrefix(k, p) {
				ks = append(ks, k)
				break
			}
		}
	}
	return ks
}

type spinMsg struct{}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmd := a.update(msg)
	// animate the spinner only while something is in flight
	if !a.spinning && a.anyLoading() {
		a.spinning = true
		cmd = tea.Batch(cmd, tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return spinMsg{} }))
	}
	return a, cmd
}

// refreshVisible refreshes the top view and the picker, if open.
func (a *App) refreshVisible(force bool) tea.Cmd {
	cmd := a.top().refresh(a, force)
	if a.overlay != nil {
		cmd = tea.Batch(cmd, a.overlay.refresh(a, force))
	}
	return cmd
}

func (a *App) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case spinMsg:
		a.spinning = false
		return nil

	case warmMsg:
		return fetch(a.store, projectsKey, 10*time.Minute, a.client.ListProjects)

	case tea.WindowSizeMsg:
		a.w, a.h = msg.Width, msg.Height
		return nil

	case tickMsg:
		if a.flash != "" && time.Since(a.flashAt) > 6*time.Second {
			a.flash = ""
		}
		return tea.Batch(tick(), a.refreshVisible(false))

	case resultMsg:
		if msg.store != a.store {
			return nil // from before a host switch
		}
		a.store.apply(msg)
		if a.opts.Notify && !msg.disk && msg.err == nil {
			a.checkNotify(msg)
		}
		// newly arrived data may unlock dependent fetches
		return a.refreshVisible(false)

	case traceMsg:
		if msg.store != a.store {
			return nil
		}
		a.store.applyTrace(msg)
		return nil

	case actionMsg:
		if msg.err != nil {
			a.setFlash(msg.ok+" failed: "+msg.err.Error(), true)
			return nil
		}
		a.setFlash(msg.ok+" ✓", false)
		a.store.invalidate(msg.invalidate...)
		cmds := []tea.Cmd{a.top().refresh(a, false)}
		if msg.then != nil {
			cmds = append(cmds, msg.then(a))
		}
		return tea.Batch(cmds...)

	case flashMsg:
		a.setFlash(msg.text, msg.err)
		return nil

	case debounceMsg:
		if a.overlay != nil && msg.v == any(a.overlay) {
			return a.overlay.debounced(a, msg.seq)
		}
		if d, ok := msg.v.(interface{ debounced(*App, int) tea.Cmd }); ok && msg.v == any(a.top()) {
			return d.debounced(a, msg.seq)
		}
		return nil

	case tea.KeyMsg:
		return a.handleKey(msg)

	case tea.MouseMsg:
		return a.mouse(msg)
	}

	if a.modal != nil && a.modal.input != nil {
		var cmd tea.Cmd
		*a.modal.input, cmd = a.modal.input.Update(msg)
		return cmd
	}
	if a.overlay != nil {
		return a.overlay.passthrough(msg)
	}
	return nil
}

func (a *App) handleKey(msg tea.KeyMsg) tea.Cmd {
	// ctrl+c is the only way out; q and esc go back one step
	if msg.String() == "ctrl+c" {
		return tea.Quit
	}
	a.ms.sel = nil
	if a.overlay != nil {
		return a.overlay.key(a, msg)
	}
	if m := a.modal; m != nil {
		if m.input != nil {
			switch msg.String() {
			case "esc":
				a.modal = nil
				return nil
			case "ctrl+s":
				text := strings.TrimSpace(m.input.Value())
				a.modal = nil
				if text == "" {
					return nil
				}
				return m.onInput(text)
			}
			var cmd tea.Cmd
			*m.input, cmd = m.input.Update(msg)
			return cmd
		}
		a.modal = nil
		if s := msg.String(); s == "y" || s == "Y" || s == "enter" {
			return m.onYes()
		}
		a.setFlash("cancelled", false)
		return nil
	}
	if a.showHelp {
		a.showHelp = false
		return nil
	}
	v := a.top()
	// a burst of typed runes (e.g. key repeat) can arrive as one message
	if !v.capturing() && msg.Type == tea.KeyRunes && len(msg.Runes) > 1 && !msg.Paste {
		var cmds []tea.Cmd
		for _, r := range msg.Runes {
			cmds = append(cmds, a.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}, Alt: msg.Alt}))
		}
		return tea.Batch(cmds...)
	}
	if !v.capturing() {
		switch msg.String() {
		case "P":
			return a.openPicker()
		case "q":
			if a.atHome() {
				return tea.Quit
			}
			return a.back()
		case "esc", "backspace":
			return a.back()
		case "H":
			return a.home()
		case "t":
			return a.chooseTheme(nil)
		case ",":
			return a.openSettings()
		case "?":
			a.showHelp = true
			return nil
		case "ctrl+r":
			a.setFlash("refreshing…", false)
			a.store.backoffUntil = time.Time{}
			return v.refresh(a, true)
		}
	}
	return v.key(a, msg)
}

// checkNotify fires a desktop notification when a pipeline we've been
// watching finishes.
func (a *App) checkNotify(msg resultMsg) {
	p, ok := msg.val.(*gitlab.Pipeline)
	if !ok || p == nil {
		return
	}
	prev, seen := a.seenStatus[p.ID]
	a.seenStatus[p.ID] = p.Status
	if seen && isActive(prev) && !isActive(p.Status) {
		title := fmt.Sprintf("Pipeline #%d %s", p.ID, p.Status)
		body := p.Ref
		go notifyDesktop(title, body)
		fmt.Fprint(os.Stderr, "\a")
	}
}

// ---- rendering ----

func (a *App) View() string {
	if a.w == 0 {
		return ""
	}
	a.ms.zones, a.ms.layer = a.ms.zones[:0], 0
	status := a.renderStatus()
	footer := a.renderFooter()
	bodyH := a.h - 1 - lipgloss.Height(footer)
	var body string
	switch {
	case a.showHelp:
		body = a.renderHelp(bodyH)
	default:
		body = a.top().render(a, a.w, bodyH)
	}
	body = fixHeight(body, bodyH)
	if a.overlay != nil {
		a.ms.layer = 1
		body = a.overlay.render(a, body, a.w, bodyH)
		a.ms.layer = 0
	}
	return a.finishFrame(body + "\n" + status + "\n" + footer)
}

func fixHeight(s string, h int) string {
	if h <= 0 {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) > h {
		lines = lines[:h]
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

func (a *App) anyLoading() bool {
	for _, e := range a.store.m {
		if e.loading {
			return true
		}
	}
	for _, t := range a.store.traces {
		if t.loading {
			return true
		}
	}
	return false
}

var spinFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func spinnerFrame() string {
	return spinFrames[(time.Now().UnixMilli()/100)%int64(len(spinFrames))]
}

func (a *App) renderFooter() string {
	if m := a.modal; m != nil {
		if m.input != nil {
			return sBold.Render(m.prompt) + "\n" + m.input.View()
		}
		return sWarn.Bold(true).Render(m.prompt) + sDim.Render("  [y/N]")
	}
	if a.flash != "" {
		st := sOK
		if a.flashErr {
			st = sErr
		}
		return fit(st.Render(a.flash), a.w)
	}
	var parts []string
	help := a.top().help()
	if a.overlay != nil {
		help = a.overlay.help()
	}
	for _, k := range help {
		parts = append(parts, sKey.Render(k.k)+" "+sDim.Render(k.d))
	}
	if a.overlay == nil {
		parts = append(parts, sKey.Render("P")+" "+sDim.Render("projects"), sKey.Render(",")+" "+sDim.Render("settings"), sKey.Render("?")+" "+sDim.Render("help"))
	}
	return fit(strings.Join(parts, "  "), a.w)
}

func (a *App) renderHelp(h int) string {
	var b strings.Builder
	global := []kb{
		{"P", "find a project"}, {"H", "home (dashboard)"},
		{"j/k ↑/↓", "move"}, {"g/G", "top/bottom"}, {"ctrl+d/u", "half page down/up"},
		{"q/esc", "back (q quits from home)"}, {"ctrl+c", "quit"}, {"ctrl+r", "force refresh"},
		{"t", "try a theme"}, {",", "settings"},
		{"o", "open in browser"}, {"y", "copy URL"},
		{"click", "select (double: open)"}, {"drag", "select text, copies on release"},
	}
	// one key column for both sections so they line up
	kw := 0
	for _, k := range append(a.top().help(), global...) {
		kw = max(kw, ansi.StringWidth(k.k))
	}
	section := func(title string, keys []kb) {
		b.WriteString(sTitle.Render(title) + "\n\n")
		for _, k := range keys {
			b.WriteString("  " + sKey.Render(pad(k.k, kw)) + colGap + k.d + "\n")
		}
	}
	section("Keys for "+a.top().title(), a.top().help())
	b.WriteString("\n")
	section("Global", global)
	b.WriteString("\n" + sDim.Render("  press any key to close"))
	return b.String()
}

// ---- helpers shared by views ----

func openBrowser(url string) tea.Cmd {
	return func() tea.Msg {
		if url == "" {
			return flashMsg{"no URL", true}
		}
		cmd := urlOpener(url)
		if err := cmd.Start(); err != nil {
			return flashMsg{"open: " + err.Error(), true}
		}
		go cmd.Wait()
		return flashMsg{"opened " + url, false}
	}
}

func copyText(s string) tea.Cmd { return copyTextAs(s, "copied "+s) }

// copyTextAs copies s to the clipboard, flashing label.
func copyTextAs(s, label string) tea.Cmd {
	return func() tea.Msg {
		if s == "" {
			return flashMsg{"nothing to copy", true}
		}
		// Prefer a native clipboard tool, fall back to OSC 52.
		for _, c := range [][]string{{"wl-copy"}, {"xclip", "-selection", "clipboard"}, {"pbcopy"}} {
			if _, err := exec.LookPath(c[0]); err != nil {
				continue
			}
			cmd := exec.Command(c[0], c[1:]...)
			cmd.Stdin = strings.NewReader(s)
			if cmd.Run() == nil {
				return flashMsg{label, false}
			}
		}
		seq := osc52.New(s)
		if os.Getenv("TMUX") != "" {
			seq = seq.Tmux()
		}
		seq.WriteTo(os.Stderr)
		return flashMsg{label, false}
	}
}

func debounce(v any, seq int, d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return debounceMsg{v, seq} })
}

// overlayAt draws box (multi-line) over bg at column x, row y.
func overlayAt(bg, box string, x, y int) string {
	bgLines := strings.Split(bg, "\n")
	for i, bl := range strings.Split(box, "\n") {
		row := y + i
		if row < 0 || row >= len(bgLines) {
			continue
		}
		under := bgLines[row]
		bw := ansi.StringWidth(bl)
		left := ansi.Truncate(under, x, "")
		if lw := ansi.StringWidth(left); lw < x {
			left += strings.Repeat(" ", x-lw)
		}
		right := ""
		if ansi.StringWidth(under) > x+bw {
			right = ansi.TruncateLeft(under, x+bw, "")
		}
		bgLines[row] = left + "\x1b[0m" + bl + "\x1b[0m" + right
	}
	return strings.Join(bgLines, "\n")
}

// pane draws a rounded box with a title in the top border, highlighted
// when active.
func pane(title string, body []string, w, h int, active bool) string {
	bs := sBorder
	ts := sDim.Bold(true)
	if active {
		bs, ts = sActive, sActiveT
	}
	if w < 4 || h < 2 {
		return ""
	}
	inner := w - 4
	var b strings.Builder
	t := ""
	if title != "" {
		t = " " + fit(title, w-6) + " "
	}
	b.WriteString(bs.Render("╭─") + ts.Render(t) + bs.Render(strings.Repeat("─", max(0, w-3-ansi.StringWidth(t)))+"╮") + "\n")
	for i := 0; i < h-2; i++ {
		line := ""
		if i < len(body) {
			line = body[i]
		}
		b.WriteString(bs.Render("│") + " " + pad(line, inner) + " " + bs.Render("│") + "\n")
	}
	b.WriteString(bs.Render("╰" + strings.Repeat("─", w-2) + "╯"))
	return b.String()
}
