package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestThemesComplete(t *testing.T) {
	seen := map[string]bool{}
	for _, th := range themes {
		if seen[th.Name] {
			t.Errorf("duplicate theme %s", th.Name)
		}
		seen[th.Name] = true
		for _, c := range []string{th.Surface, th.Muted, th.Red, th.Green, th.Yellow, th.Blue, th.Magenta, th.Cyan, th.Orange} {
			if c == "" || (th.hex() && len(c) != 7) {
				t.Errorf("%s: bad colour %q", th.Name, c)
			}
		}
		if th.hex() && (len(th.Bg) != 7 || len(th.Fg) != 7) {
			t.Errorf("%s: hex theme needs bg and fg", th.Name)
		}
	}
	for _, want := range []string{"catppuccin-mocha", "github-dark", "gruvbox-dark", "miasma", "kanagawa", "noctis", "rose-pine", "shades-of-purple", "tokyonight"} {
		if !seen[want] {
			t.Errorf("missing theme %s", want)
		}
	}
}

func TestPainter(t *testing.T) {
	defer applyTheme(themes[0])
	if !SetTheme("tokyonight") {
		t.Fatal("tokyonight missing")
	}
	p := themePaint
	out := p.line("a\x1b[31mred\x1b[0m b\x1b[39m", 10)
	if ansi.StringWidth(out) != 10 {
		t.Errorf("width %d", ansi.StringWidth(out))
	}
	if strings.Contains(out, "[31m") || !strings.Contains(out, "38;2;247;118;142") {
		t.Errorf("ANSI red not mapped to the theme: %q", out)
	}
	if !strings.Contains(out, "0;48;2;26;27;38;38;2;192;202;245") {
		t.Errorf("reset doesn't restore theme colours: %q", out)
	}
	SetTheme("default")
	if themePaint != nil {
		t.Error("default theme shouldn't paint")
	}
}

func TestSortedThemes(t *testing.T) {
	ts := sortedThemes()
	if ts[0].Name != "default" {
		t.Fatalf("first = %s", ts[0].Name)
	}
	var names []string
	for i := 1; i < len(ts); i++ {
		prev, cur := ts[i-1], ts[i]
		names = append(names, cur.Label)
		if prev.hex() && prev.Light && !cur.Light {
			t.Errorf("dark %s after light %s", cur.Name, prev.Name)
		}
		if prev.hex() && prev.Light == cur.Light && strings.ToLower(prev.Label) > strings.ToLower(cur.Label) {
			t.Errorf("%s before %s", prev.Label, cur.Label)
		}
	}
	t.Log(strings.Join(names, ", "))
}

func TestPainterWithoutBackground(t *testing.T) {
	defer func() { paintBg = true; applyTheme(themes[0]) }()
	SetTheme("tokyonight")
	SetBackground(false)
	out := themePaint.line("x\x1b[0m", 3)
	if strings.Contains(out, "48;2;26;27;38") {
		t.Errorf("background painted: %q", out)
	}
	if !strings.Contains(out, "38;2;192;202;245") {
		t.Errorf("foreground missing: %q", out)
	}
}
