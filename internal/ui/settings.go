package ui

import (
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"gitlab-tui/internal/config"
	"gitlab-tui/internal/gitlab"
)

// settingsPopup edits the config file from inside glt. Every change is
// saved straight away (comments in the file are kept) and applied live
// where possible.
type settingsPopup struct {
	cursor int
}

const (
	setHost = iota
	setTheme
	setBackground
	setPicker
	setIcons
	setMouse
	setPreview
	setNotify
	nSettings
)

func (a *App) openSettings() tea.Cmd {
	if a.opts.Config == nil {
		a.setFlash("no config loaded", true)
		return nil
	}
	a.overlay = &settingsPopup{}
	return nil
}

func (sp *settingsPopup) refresh(*App, bool) tea.Cmd  { return nil }
func (sp *settingsPopup) debounced(*App, int) tea.Cmd { return nil }
func (sp *settingsPopup) passthrough(tea.Msg) tea.Cmd { return nil }
func (sp *settingsPopup) help() []kb {
	return []kb{{"enter", "change"}, {"j/k", "move"}, {"esc", "close"}}
}

func (sp *settingsPopup) key(a *App, msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "q", ",":
		a.overlay = nil
	case "j", "down", "tab", "ctrl+n":
		sp.cursor = (sp.cursor + 1) % nSettings
	case "k", "up", "shift+tab", "ctrl+p":
		sp.cursor = (sp.cursor + nSettings - 1) % nSettings
	case "enter", " ", "l", "right":
		return sp.activate(a, sp.cursor)
	}
	return nil
}

// save writes one setting, flashing the outcome.
func (a *App) saveSetting(what string, value any, path ...string) {
	if err := config.Set(value, path...); err != nil {
		a.setFlash("saving "+what+": "+err.Error(), true)
		return
	}
	a.setFlash("saved "+what+" to "+config.Path(), false)
}

func (sp *settingsPopup) activate(a *App, row int) tea.Cmd {
	cfg := a.opts.Config
	switch row {
	case setHost:
		return a.chooseHost(cfg, func(h string) tea.Cmd {
			cfg.DefaultHost = h
			a.saveSetting("default instance", h, "default_host")
			if h == a.ctx.Host {
				return nil
			}
			a.overlay = nil
			a.confirm("Switch to "+h+" now?", func() tea.Cmd { return a.switchHost(h) })
			return nil
		})
	case setTheme:
		return a.chooseTheme(func(t Theme) {
			cfg.UI.Theme = t.Name
			a.saveSetting("default theme", t.Name, "ui", "theme")
		})
	case setBackground:
		on := !cfg.BackgroundEnabled()
		cfg.UI.Background = &on
		SetBackground(on)
		a.saveSetting("theme background", on, "ui", "background")
	case setPicker:
		if cfg.UI.Picker == "tree" {
			cfg.UI.Picker = "list"
		} else {
			cfg.UI.Picker = "tree"
		}
		a.pickerTree = cfg.UI.Picker == "tree"
		a.saveSetting("project picker view", cfg.UI.Picker, "ui", "picker")
	case setIcons:
		if cfg.UI.Icons == "unicode" {
			cfg.UI.Icons = "nerd"
		} else {
			cfg.UI.Icons = "unicode"
		}
		SetIcons(cfg.UI.Icons)
		a.saveSetting("icons", cfg.UI.Icons, "ui", "icons")
	case setMouse:
		a.opts.Mouse = !a.opts.Mouse
		a.saveSetting("mouse", a.opts.Mouse, "ui", "mouse")
		if a.opts.Mouse {
			return tea.EnableMouseCellMotion
		}
		return tea.DisableMouse
	case setPreview:
		a.opts.Preview = !a.opts.Preview
		a.saveSetting("log preview", a.opts.Preview, "ui", "preview")
	case setNotify:
		a.opts.Notify = !a.opts.Notify
		a.saveSetting("notifications", a.opts.Notify, "ui", "notify")
	}
	return nil
}

func (sp *settingsPopup) render(a *App, body string, w, h int) string {
	cfg := a.opts.Config
	onOff := func(b bool) string {
		if b {
			return sOK.Render(ic.checked + " on")
		}
		return sDim.Render(ic.unchecked + " off")
	}
	t, _ := findTheme(cfg.UI.Theme)
	themeVal := t.Label
	if theme.Name != t.Name {
		themeVal += sDim.Render("  (now: " + theme.Label + ")")
	}
	hostVal := cfg.DefaultHost
	if a.ctx.Host != cfg.DefaultHost {
		hostVal += sDim.Render("  (now: " + a.ctx.Host + ")")
	}
	rows := []struct{ label, value, hint string }{
		{"Default instance", hostVal, "used when the current directory isn't a GitLab repo"},
		{"Theme", themeVal, "t tries themes without saving"},
		{"Theme background", onOff(cfg.BackgroundEnabled()), "off keeps your terminal's background (and transparency); the default theme never paints one"},
		{"Project picker", cfg.UI.Picker, "how P opens: a fuzzy list, or a tree of groups (ctrl+t switches in the picker)"},
		{"Icons", cfg.UI.Icons, "nerd needs a Nerd Font; unicode works everywhere"},
		{"Mouse", onOff(a.opts.Mouse), "clicks, wheel and drag-to-copy"},
		{"Log preview", onOff(a.opts.Preview), "job log under the pipeline's jobs"},
		{"Notifications", onOff(a.opts.Notify), "when a watched pipeline finishes"},
	}
	labelW := 0
	for _, r := range rows {
		labelW = max(labelW, len(r.label))
	}
	bw := min(w-4, 76)
	inner := bw - 4
	var lines []string
	for i, r := range rows {
		mark := "  "
		label := sDim.Render(pad(r.label, labelW))
		if i == sp.cursor {
			mark = sActive.Render(ic.caret) + " "
			label = sActiveT.Render(pad(r.label, labelW))
		}
		line := pad(mark+label+colGap+r.value, inner)
		if i == sp.cursor {
			line = selectLine(line, inner)
		}
		i := i
		lines = append(lines, a.clickRow(line, func() tea.Cmd { sp.cursor = i; return nil },
			func() tea.Cmd { return sp.activate(a, i) }))
	}
	lines = append(lines, "", sDim.Render(fit(rows[sp.cursor].hint, inner)), "",
		sDim.Render(fit("Saved to "+config.Path(), inner)))
	box := pane(ic.home+" Settings", lines, bw, len(lines)+2, true)
	x, y := (w-bw)/2, max(0, (h-lipgloss.Height(box))/2)
	return overlayAt(body, a.zone(box, zone{}), x, y)
}

// chooseHost picks a configured GitLab instance.
func (a *App) chooseHost(cfg *config.File, done func(string) tea.Cmd) tea.Cmd {
	return a.openChooser(&chooser{
		title:   "GitLab instances",
		prompt:  ic.gitlab + " Instances",
		initial: cfg.DefaultHost,
		items: func(a *App) ([]choice, bool) {
			names := make([]string, 0, len(cfg.Hosts)+1)
			seen := map[string]bool{}
			for n := range cfg.Hosts {
				names = append(names, n)
				seen[n] = true
			}
			if !seen["gitlab.com"] {
				names = append(names, "gitlab.com")
			}
			sort.Strings(names)
			var out []choice
			for i, n := range names {
				desc := ""
				if cfg.Hosts[n].Token == "" {
					desc = "no token (glt auth login --host " + n + ")"
				}
				if n == a.ctx.Host {
					desc = strings.TrimSpace("current " + desc)
				}
				out = append(out, choice{id: n, text: n, desc: desc, order: i})
			}
			return out, false
		},
		done: func(a *App, chosen []string) tea.Cmd { return done(chosen[0]) },
	}, nil)
}

// chooseTheme picks a theme, previewing each one as it's highlighted. Esc
// puts the previous theme back; done (if set) runs after a theme is chosen.
func (a *App) chooseTheme(done func(Theme)) tea.Cmd {
	prev := theme
	return a.openChooser(&chooser{
		title:   "Themes",
		prompt:  ic.home + " Themes",
		initial: prev.Name,
		items: func(a *App) ([]choice, bool) {
			out := make([]choice, len(themes))
			for i, t := range sortedThemes() {
				kind := "dark"
				if t.Light {
					kind = "light"
				}
				if !t.hex() {
					kind = "terminal colours"
				}
				out[i] = choice{id: t.Name, text: t.Label, prefix: swatch(t), desc: kind, order: i}
			}
			return out, false
		},
		hover:  func(a *App, id string) { SetTheme(id) },
		cancel: func(a *App) { applyTheme(prev) },
		done: func(a *App, chosen []string) tea.Cmd {
			t, _ := findTheme(chosen[0])
			applyTheme(t)
			if done != nil {
				done(t)
			} else {
				a.setFlash("theme: "+t.Label+" (set the default with ,)", false)
			}
			return nil
		},
	}, nil)
}

// sortedThemes lists the default theme first, then dark themes, then
// light ones, each alphabetically.
func sortedThemes() []Theme {
	out := append([]Theme(nil), themes...)
	rank := func(t Theme) int {
		switch {
		case !t.hex():
			return 0
		case t.Light:
			return 2
		}
		return 1
	}
	sort.SliceStable(out, func(i, j int) bool {
		if ri, rj := rank(out[i]), rank(out[j]); ri != rj {
			return ri < rj
		}
		return strings.ToLower(out[i].Label) < strings.ToLower(out[j].Label)
	})
	return out
}

// swatch shows a theme's accent colours.
func swatch(t Theme) string {
	var b strings.Builder
	for _, c := range []string{t.Red, t.Yellow, t.Green, t.Cyan, t.Blue, t.Magenta} {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(c)).Render("●"))
	}
	return b.String()
}

// switchHost points the session at another GitLab instance, starting again
// from its dashboard.
func (a *App) switchHost(h string) tea.Cmd {
	ctx, err := config.Resolve(a.opts.Config, "", "", h)
	if err != nil {
		a.setFlash(err.Error(), true)
		return nil
	}
	a.ctx = ctx
	a.client = gitlab.New(ctx.APIBase, ctx.Token, gitlab.TLSOptions{CACert: ctx.TLS.CACert, SkipVerify: ctx.TLS.SkipVerify})
	a.store = newStore(h)
	a.recent = loadRecent(h)
	a.projIDs, a.projPaths, a.seenStatus = map[string]int{}, map[int]string{}, map[int]string{}
	a.stack = []view{newDashboard()}
	a.overlay, a.modal = nil, nil
	a.setFlash("switched to "+h, false)
	return tea.Batch(a.top().refresh(a, true), a.warmProjects(), fetch(a.store, meKey, time.Hour, a.client.CurrentUser))
}
