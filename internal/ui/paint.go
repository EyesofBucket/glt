package ui

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// painter gives true-colour themes their own background and foreground.
// Rather than threading a background through every style, the finished
// frame is post-processed: each line starts in the theme's colours, every
// reset (and default-colour code) falls back to them instead of the
// terminal's, lines are padded to the full width, and the 16 ANSI colours
// (as used by job logs) are mapped onto the theme's palette.
type painter struct {
	base   string // SGR params: theme background and foreground
	fg, bg string
	ansi   [16]string // SGR colour params, "2;r;g;b"
}

var (
	themePaint *painter
	// paintBg is false when the user wants the terminal's own background
	// (e.g. to keep its transparency) under a true-colour theme.
	paintBg     = true
	baseProfile = lipgloss.ColorProfile()
	sgrRe       = regexp.MustCompile(`\x1b\[([0-9;]*)m`)
)

func newPainter(t Theme) *painter {
	if !t.hex() {
		lipgloss.SetColorProfile(baseProfile)
		return nil
	}
	// hex themes need true colour; terminals without it (and tmux) map
	// the colours down themselves
	lipgloss.SetColorProfile(termenv.TrueColor)
	p := &painter{fg: "38;" + rgbParams(t.Fg), bg: "48;" + rgbParams(t.Bg)}
	if !paintBg {
		p.bg = "49"
	}
	p.base = p.bg + ";" + p.fg
	for i, c := range t.ansi16() {
		p.ansi[i] = rgbParams(c)
	}
	return p
}

func rgbParams(hex string) string {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		return "5;0"
	}
	n, err := strconv.ParseUint(hex, 16, 32)
	if err != nil {
		return "5;0"
	}
	return "2;" + strconv.Itoa(int(n>>16&0xff)) + ";" + strconv.Itoa(int(n>>8&0xff)) + ";" + strconv.Itoa(int(n&0xff))
}

// line repaints one rendered line, w cells wide.
func (p *painter) line(l string, w int) string {
	out := "\x1b[0;" + p.base + "m" + sgrRe.ReplaceAllStringFunc(l, p.sgr)
	if pad := w - ansi.StringWidth(l); pad > 0 {
		out += strings.Repeat(" ", pad)
	}
	return out
}

func (p *painter) sgr(seq string) string {
	params := seq[2 : len(seq)-1]
	if params == "" {
		return "\x1b[0;" + p.base + "m"
	}
	codes := strings.Split(params, ";")
	var out []string
	for i := 0; i < len(codes); i++ {
		c, _ := strconv.Atoi(codes[i])
		switch {
		case codes[i] == "" || c == 0:
			out = append(out, "0", p.base)
		case c == 39:
			out = append(out, p.fg)
		case c == 49:
			out = append(out, p.bg)
		case c >= 30 && c <= 37:
			out = append(out, "38;"+p.ansi[c-30])
		case c >= 90 && c <= 97:
			out = append(out, "38;"+p.ansi[c-90+8])
		case c >= 40 && c <= 47:
			out = append(out, "48;"+p.ansi[c-40])
		case c >= 100 && c <= 107:
			out = append(out, "48;"+p.ansi[c-100+8])
		case (c == 38 || c == 48) && i+2 < len(codes) && codes[i+1] == "5":
			n, _ := strconv.Atoi(codes[i+2])
			if n < 16 {
				out = append(out, codes[i]+";"+p.ansi[n])
			} else {
				out = append(out, strings.Join(codes[i:i+3], ";"))
			}
			i += 2
		case (c == 38 || c == 48) && i+4 < len(codes) && codes[i+1] == "2":
			out = append(out, strings.Join(codes[i:i+5], ";"))
			i += 4
		default:
			out = append(out, codes[i])
		}
	}
	return "\x1b[" + strings.Join(out, ";") + "m"
}
