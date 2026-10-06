package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"gitlab-tui/internal/gitlab"
)

// The status line sits above the key hints, loosely after lualine:
//
//	 MODE  project                         activity  3/11   user@host
//	 (a)     (b)                                       (y)      (z)
//
// a and z take the mode's colour, b and y sit on a darker band, and the
// sections simply butt up against each other.

// progresser is implemented by views that can say where you are in them.
type progresser interface {
	progress(a *App) string
}

func listProgress(cursor, total int) string {
	if total == 0 {
		return ""
	}
	return fmt.Sprintf("%d/%d", cursor+1, total)
}

// textProgress is lualine's "progress": Top, Bot, All or a percentage.
func textProgress(top, height, total int) string {
	switch {
	case total <= height:
		return "All"
	case top <= 0:
		return "Top"
	case top+height >= total:
		return "Bot"
	}
	return fmt.Sprintf("%d%%", 100*top/max(1, total-height))
}

// mode names what has the keyboard, and its colour, like vim's modes:
// blue for normal browsing, green while typing, magenta while selecting.
func (a *App) mode() (string, lipgloss.Color) {
	switch {
	case a.ms.sel != nil && a.ms.sel.dragging:
		return "SELECT", cMagenta
	case a.modal != nil && a.modal.input != nil:
		return "COMMENT", cGreen
	case a.modal != nil:
		return "CONFIRM", cYellow
	case a.showHelp:
		return "HELP", cBlue
	}
	switch o := a.overlay.(type) {
	case *projectPicker:
		return "PROJECTS", cGreen
	case *testsPopup:
		return "TESTS", cBlue
	case *chooser:
		return strings.ToUpper(strings.TrimSpace(strings.TrimPrefix(o.prompt, firstWord(o.prompt)))), cGreen
	}
	v := a.top()
	name := strings.ToUpper(v.title())
	switch v.(type) {
	case *dashboardView:
		name = "HOME"
	case *projectView:
		name = "PROJECT"
	case *tagListView:
		name = "TAGS"
	case *mrListView:
		name = "MRS"
	case *mrDetailView:
		name = "MR"
	case *mrNewView:
		name = "NEW MR"
		if mv := v.(*mrNewView); mv.editing() {
			name = "EDIT MR"
		}
	case *pipelineListView:
		name = "PIPELINES"
	case *pipelineView:
		name = "PIPELINE"
	case *traceView:
		name = "LOG"
	}
	if v.capturing() {
		return name, cGreen
	}
	return name, cBlue
}

// firstWord returns the leading icon of a chooser prompt ("<icon> Labels").
func firstWord(s string) string {
	if i := strings.IndexByte(s, ' '); i >= 0 {
		return s[:i]
	}
	return ""
}

func (a *App) renderStatus() string {
	name, mc := a.mode()
	sA := lipgloss.NewStyle().Foreground(cOnAccent).Background(mc).Bold(true)
	sB := lipgloss.NewStyle().Foreground(mc).Background(cSurface)

	left := sA.Render(" " + name + " ")
	if p := a.top().proj(); p != "" {
		left += sB.Render(" " + ic.gitlab + " " + p + " ")
	}

	right := ""
	if a.store.backedOff() {
		right += sWarn.Render(fmt.Sprintf("%s rate limited %ds", ic.conflict, int(time.Until(a.store.backoffUntil).Seconds())+1)) + " "
	} else if a.anyLoading() {
		right += sDim.Render(spinnerFrame()) + " "
	}
	if pv, ok := a.top().(progresser); ok && !a.showHelp {
		if prog := pv.progress(a); prog != "" {
			right += sB.Render(" " + prog + " ")
		}
	}
	who := a.ctx.Host
	if me, _ := get[*gitlab.User](a.store, meKey); me != nil && me.Username != "" {
		who = me.Username + "@" + a.ctx.Host
	}
	right += sA.Render(" " + ic.user + " " + who + " ")

	left = fit(left, max(0, a.w-ansi.StringWidth(right)-1))
	gap := a.w - ansi.StringWidth(left) - ansi.StringWidth(right)
	return left + strings.Repeat(" ", max(gap, 0)) + right
}
