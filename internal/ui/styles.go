package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Colours come from the active theme (see themes.go). The default theme
// uses the terminal's 16-colour palette, so glt follows the terminal theme.
var (
	theme Theme

	cOnAccent lipgloss.Color // text on a coloured block
	cSurface  lipgloss.Color // selection band, status line band
	cRed      lipgloss.Color
	cGreen    lipgloss.Color
	cYellow   lipgloss.Color
	cBlue     lipgloss.Color
	cMagenta  lipgloss.Color
	cCyan     lipgloss.Color
	cGray     lipgloss.Color
	cBYellow  lipgloss.Color
	cBBlue    lipgloss.Color

	sBold    lipgloss.Style
	sDim     lipgloss.Style
	sFaint   lipgloss.Style
	sAccent  lipgloss.Style
	sKey     lipgloss.Style
	sLink    lipgloss.Style
	sErr     lipgloss.Style
	sOK      lipgloss.Style
	sWarn    lipgloss.Style
	sHeader  lipgloss.Style
	sStage   lipgloss.Style
	sTitle   lipgloss.Style
	sBorder  lipgloss.Style
	sActive  lipgloss.Style
	sActiveT lipgloss.Style
	sMatch   lipgloss.Style
	sSection lipgloss.Style
	sBrand   lipgloss.Style
	sTabOn   lipgloss.Style
	sTabOff  lipgloss.Style

	selBg string // SGR sequence for the selection band
)

func init() { applyTheme(themes[0]) }

// SetBackground chooses whether true-colour themes paint their background.
func SetBackground(on bool) {
	paintBg = on
	applyTheme(theme)
}

// SetTheme switches to the named theme, reporting whether it exists.
func SetTheme(name string) bool {
	t, ok := findTheme(name)
	applyTheme(t)
	return ok
}

func applyTheme(t Theme) {
	theme = t
	cOnAccent = lipgloss.Color("0")
	if t.Bg != "" {
		cOnAccent = lipgloss.Color(t.Bg)
	}
	cSurface = lipgloss.Color(t.Surface)
	cRed, cGreen, cYellow = lipgloss.Color(t.Red), lipgloss.Color(t.Green), lipgloss.Color(t.Yellow)
	cBlue, cMagenta, cCyan = lipgloss.Color(t.Blue), lipgloss.Color(t.Magenta), lipgloss.Color(t.Cyan)
	cGray, cBYellow, cBBlue = lipgloss.Color(t.Muted), lipgloss.Color(t.Orange), lipgloss.Color(t.Blue)
	if !t.hex() {
		cBBlue = lipgloss.Color("12")
	}
	if t.Running != "" {
		cBBlue = lipgloss.Color(t.Running)
	}

	sBold = lipgloss.NewStyle().Bold(true)
	sDim = lipgloss.NewStyle().Foreground(cGray)
	sFaint = lipgloss.NewStyle().Faint(true)
	sAccent = lipgloss.NewStyle().Foreground(cMagenta).Bold(true)
	sKey = lipgloss.NewStyle().Foreground(cBlue).Bold(true)
	sLink = lipgloss.NewStyle().Foreground(cCyan)
	sErr = lipgloss.NewStyle().Foreground(cRed)
	sOK = lipgloss.NewStyle().Foreground(cGreen)
	sWarn = lipgloss.NewStyle().Foreground(cYellow)
	sHeader = lipgloss.NewStyle().Foreground(cGray).Bold(true)
	sStage = lipgloss.NewStyle().Foreground(cBlue).Bold(true)
	sTitle = lipgloss.NewStyle().Bold(true)
	sBorder = lipgloss.NewStyle().Foreground(cGray)
	sActive = lipgloss.NewStyle().Foreground(cBlue)
	sActiveT = lipgloss.NewStyle().Foreground(cBlue).Bold(true)
	sMatch = lipgloss.NewStyle().Foreground(cBlue).Bold(true)
	sSection = lipgloss.NewStyle().Foreground(cCyan).Bold(true)
	sBrand = lipgloss.NewStyle().Foreground(cMagenta)
	sTabOn = lipgloss.NewStyle().Foreground(cBlue).Bold(true).Underline(true)
	sTabOff = lipgloss.NewStyle().Foreground(cGray)

	selBg = sgrOf(lipgloss.NewStyle().Background(cSurface))
	themePaint = newPainter(t)
}

// sgrOf returns the escape sequence a style starts with.
func sgrOf(st lipgloss.Style) string {
	out := st.Render("x")
	if i := strings.IndexByte(out, 'x'); i > 0 {
		return out[:i]
	}
	return ""
}

// iconSet maps glyphs for the configured font.
type iconSet struct {
	status                                       map[string]string
	mr, pipeline, project, todo, branch, gitlab  string
	user, review, comment, check, search, home   string
	job, approved, draft, caret, clock, conflict string
	label, checked, unchecked                    string
	folder, folderOpen                           string
}

var nerdIcons = iconSet{
	status: map[string]string{
		"success": "\uf058", "failed": "\uf057", "allowed": "\uf06a", "running": "\uf192",
		"pending": "\uf017", "scheduled": "\uf073", "preparing": "\uf110", "created": "\uf10c",
		"manual": "\uf144", "skipped": "\uf050", "canceled": "\uf05e",
	},
	mr: "\uf407", pipeline: "\uf135", project: "\uf401", todo: "\uf0ae", branch: "\ue725",
	gitlab: "\uf296", user: "\uf007", review: "\uf06e", comment: "\uf075", check: "\uf00c",
	search: "\uf002", home: "\uf015", job: "\uf120", approved: "\uf00c", draft: "\uf040",
	caret: "\uf054", clock: "\uf017", conflict: "\uf071",
	label: "\uf02b", checked: "\uf14a", unchecked: "\uf096",
	folder: "\uf07b", folderOpen: "\uf07c",
}

var unicodeIcons = iconSet{
	status: map[string]string{
		"success": "✔", "failed": "✘", "allowed": "!", "running": "●",
		"pending": "◔", "scheduled": "◷", "preparing": "◌", "created": "○",
		"manual": "▶", "skipped": "»", "canceled": "⊘",
	},
	mr: "⇄", pipeline: "▶", project: "▣", todo: "☐", branch: "⎇",
	gitlab: "◆", user: "@", review: "◉", comment: "💬", check: "✓",
	search: "/", home: "⌂", job: "$", approved: "✓", draft: "✎",
	caret: ">", clock: "◷", conflict: "⚠",
	label: "#", checked: "☑", unchecked: "☐",
	folder: "▤", folderOpen: "▤",
}

var ic = nerdIcons

// SetIcons selects "nerd" or "unicode" glyphs.
func SetIcons(name string) {
	if name == "unicode" || name == "ascii" {
		ic = unicodeIcons
	} else {
		ic = nerdIcons
	}
}

type statusStyle struct {
	icon  string
	style lipgloss.Style
}

func statusOf(status string, allowFailure bool) statusStyle {
	st := lipgloss.NewStyle()
	key := status
	switch status {
	case "success":
		st = st.Foreground(cGreen)
	case "failed":
		if allowFailure {
			key, st = "allowed", st.Foreground(cBYellow)
		} else {
			st = st.Foreground(cRed).Bold(true)
		}
	case "running":
		st = st.Foreground(cBBlue).Bold(true)
	case "pending", "waiting_for_resource", "waiting_for_callback":
		key, st = "pending", st.Foreground(cYellow)
	case "scheduled":
		st = st.Foreground(cYellow)
	case "preparing":
		st = st.Foreground(cBlue)
	case "created":
		st = st.Foreground(cCyan).Faint(true)
	case "manual":
		st = st.Foreground(cMagenta)
	case "skipped":
		st = st.Foreground(cGray)
	case "canceled", "canceling":
		key, st = "canceled", st.Foreground(cGray)
	case "":
		return statusStyle{" ", st}
	default:
		return statusStyle{"?", st}
	}
	return statusStyle{ic.status[key], st}
}

func statusIcon(status string, allowFailure bool) string {
	s := statusOf(status, allowFailure)
	return s.style.Render(s.icon)
}

func statusLabel(status string, allowFailure bool) string {
	s := statusOf(status, allowFailure)
	label := status
	if status == "failed" && allowFailure {
		label = "failed (allowed)"
	}
	return s.style.Render(s.icon + " " + label)
}

func isActive(status string) bool {
	switch status {
	case "running", "pending", "created", "preparing", "waiting_for_resource",
		"waiting_for_callback", "scheduled", "canceling":
		return true
	}
	return false
}

func since(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 60*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dmo", int(d.Hours()/24/30))
	}
	return fmt.Sprintf("%dy", int(d.Hours()/24/365))
}

func fmtDur(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	d = d.Round(time.Second)
	h, m, s := int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60
	switch {
	case h > 0:
		return fmt.Sprintf("%dh%02dm%02ds", h, m, s)
	case m > 0:
		return fmt.Sprintf("%dm%02ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

// elapsed gives a job/pipeline duration, ticking live while it runs.
// Things parked without finishing (e.g. waiting on a manual job) don't tick.
func elapsed(status string, started, finished *time.Time, dur float64) string {
	if started == nil {
		return ""
	}
	if finished == nil && !isActive(status) {
		if dur > 0 {
			return fmtDur(time.Duration(dur * float64(time.Second)))
		}
		return ""
	}
	if finished != nil {
		if dur > 0 {
			return fmtDur(time.Duration(dur * float64(time.Second)))
		}
		return fmtDur(finished.Sub(*started))
	}
	return fmtDur(time.Since(*started))
}

// pad truncates or pads s (which may contain ANSI) to exactly w cells.
func pad(s string, w int) string {
	if w <= 0 {
		return ""
	}
	sw := ansi.StringWidth(s)
	if sw > w {
		return ansi.Truncate(s, w, "…")
	}
	return s + strings.Repeat(" ", w-sw)
}

// fit truncates s to w cells.
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if ansi.StringWidth(s) > w {
		return ansi.Truncate(s, w, "…")
	}
	return s
}

func shortSHA(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

// selectLine highlights a row with the theme's selection band, keeping the
// row's own colours, and pads it to the full width.
func selectLine(line string, w int) string {
	bg := selBg
	line = pad(line, w)
	line = strings.ReplaceAll(line, "\x1b[0m", "\x1b[0m"+bg)
	line = strings.ReplaceAll(line, "\x1b[m", "\x1b[m"+bg)
	return bg + "\x1b[1m" + strings.ReplaceAll(line, bg, bg+"\x1b[1m") + "\x1b[0m"
}
