package ui

import (
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"gitlab-tui/internal/gitlab"
)

func TestPickerTree(t *testing.T) {
	pk := &projectPicker{input: textinput.New(), tree: true}
	for _, p := range []string{"acme/web/site", "acme/web/api", "acme/tools", "acme/infra/k8s/charts"} {
		pk.results = append(pk.results, pickResult{p: gitlab.Project{PathWithNamespace: p}})
	}
	names := func() []string {
		var out []string
		for _, n := range pk.treeRows() {
			out = append(out, n.path)
		}
		return out
	}
	// the lone top group opens; its subgroups come first, then projects
	got := names()
	want := []string{"acme", "acme/infra", "acme/web", "acme/tools"}
	if len(got) != len(want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("rows = %v, want %v", got, want)
		}
	}
	if n := pk.treeRows()[0]; n.count != 4 {
		t.Errorf("acme count = %d", n.count)
	}
	// enter on a group expands it
	pk.tcursor = 2
	if _, ok := pk.treeKey(&App{}, keyEnter); !ok {
		t.Fatal("enter on a group should be handled")
	}
	if got := names(); len(got) != 6 || got[3] != "acme/web/api" {
		t.Errorf("after expanding web: %v", got)
	}
	// left on a project goes to its group, then collapses it
	pk.tcursor = 3
	pk.treeKey(&App{}, tea.KeyMsg{Type: tea.KeyLeft})
	if pk.tcursor != 2 {
		t.Errorf("left went to row %d", pk.tcursor)
	}
	pk.treeKey(&App{}, tea.KeyMsg{Type: tea.KeyLeft})
	if len(names()) != 4 {
		t.Errorf("web didn't collapse: %v", names())
	}
	// enter on a project is left to the caller (it opens it)
	pk.tcursor = 3
	if _, ok := pk.treeKey(&App{}, keyEnter); ok {
		t.Error("enter on a project shouldn't be handled by the tree")
	}
	if s := pk.selected(); s == nil || s.p.PathWithNamespace != "acme/tools" {
		t.Errorf("selected = %v", s)
	}
}
