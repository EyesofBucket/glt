package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestPaneWidths(t *testing.T) {
	for _, active := range []bool{false, true} {
		out := pane("1  Review requested (1)", []string{"hello", selectLine("x", 36)}, 40, 5, active)
		for i, l := range strings.Split(out, "\n") {
			if w := ansi.StringWidth(l); w != 40 {
				t.Errorf("active=%v line %d width %d: %q", active, i, w, l)
			}
		}
	}
}
