package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"gitlab-tui/internal/gitlab"
)

func labelsKey(p string) string { return "labels:" + p }

func fetchLabels(a *App, project string, age time.Duration) tea.Cmd {
	return fetch(a.store, labelsKey(project), age, func(ctx ctxT) ([]gitlab.Label, error) {
		return a.client.ListLabels(ctx, project)
	})
}

// chooseLabels opens a multi-select over the project's labels.
func (a *App) chooseLabels(project, title string, current []string, done func(a *App, chosen []string) tea.Cmd) tea.Cmd {
	return a.openChooser(&chooser{
		title:  title,
		prompt: ic.label + " Labels",
		multi:  true,
		load: func(a *App, force bool) tea.Cmd {
			age := 10 * time.Minute
			if force {
				age = 0
			}
			return fetchLabels(a, project, age)
		},
		items: func(a *App) ([]choice, bool) {
			ls, e := get[[]gitlab.Label](a.store, labelsKey(project))
			var out []choice
			known := map[string]bool{}
			for _, l := range ls {
				known[l.Name] = true
				out = append(out, choice{id: l.Name, text: l.Name, prefix: labelDot(l.Color), desc: l.Description})
			}
			// labels on the MR that the project list doesn't know still show
			for _, n := range current {
				if !known[n] {
					out = append(out, choice{id: n, text: n, prefix: " "})
				}
			}
			sortChoices(out)
			return out, e == nil || e.val == nil
		},
		exclusive: scopedSiblings,
		done:      done,
	}, current)
}

// sortChoices orders choices case-insensitively by text.
func sortChoices(cs []choice) {
	sort.SliceStable(cs, func(i, j int) bool { return strings.ToLower(cs[i].text) < strings.ToLower(cs[j].text) })
	for i := range cs {
		cs[i].order = i
	}
}

// scopedSiblings makes scoped labels (scope::value) exclusive, as in
// GitLab: turning one on turns off the others in its scope.
func scopedSiblings(on map[string]bool, name string) []string {
	i := strings.LastIndex(name, "::")
	if i <= 0 {
		return nil
	}
	scope := name[:i+2]
	var out []string
	for other := range on {
		if other != name && strings.HasPrefix(other, scope) && !strings.Contains(other[len(scope):], "::") {
			out = append(out, other)
		}
	}
	return out
}

// editMRLabels edits an existing MR's labels, sending only the changes so
// labels someone else added meanwhile survive.
func (a *App) editMRLabels(p string, iid int, current []string) tea.Cmd {
	orig := map[string]bool{}
	for _, l := range current {
		orig[l] = true
	}
	return a.chooseLabels(p, fmt.Sprintf("Labels for !%d", iid), current, func(a *App, chosen []string) tea.Cmd {
		var add, remove []string
		on := map[string]bool{}
		for _, l := range chosen {
			on[l] = true
			if !orig[l] {
				add = append(add, l)
			}
		}
		for _, l := range current {
			if !on[l] {
				remove = append(remove, l)
			}
		}
		if len(add) == 0 && len(remove) == 0 {
			a.setFlash("labels unchanged", false)
			return nil
		}
		var parts []string
		if len(add) > 0 {
			parts = append(parts, "+"+strings.Join(add, " +"))
		}
		if len(remove) > 0 {
			parts = append(parts, "-"+strings.Join(remove, " -"))
		}
		inval := []string{mrKey(p, iid)}
		inval = append(inval, a.keysWithPrefix(mrListPrefix(p), dashKey)...)
		return a.action(fmt.Sprintf("labels %s on !%d", strings.Join(parts, " "), iid),
			func() error { return a.client.UpdateLabels(bg(), p, iid, add, remove) }, inval, nil)
	})
}

func labelDot(color string) string {
	if color == "" {
		return sAccent.Render("●")
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render("●")
}

// labelChips renders label names with their colour as a dot.
func labelChips(names []string, labels []gitlab.Label) string {
	return strings.Join(labelChipList(names, labels), "  ")
}

// labelChipList renders each label as a coloured dot and its name.
func labelChipList(names []string, labels []gitlab.Label) []string {
	color := map[string]string{}
	for _, l := range labels {
		if _, ok := color[l.Name]; !ok {
			color[l.Name] = l.Color
		}
	}
	var parts []string
	for _, n := range names {
		parts = append(parts, labelDot(color[n])+" "+n)
	}
	return parts
}
