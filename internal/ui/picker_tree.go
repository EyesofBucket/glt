package ui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// The project picker's tree mode (ctrl+t) shows projects grouped by their
// GitLab groups, collapsed to start with. It lists top-down, unlike the
// flat mode's best-match-at-the-bottom. While a query is typed, matching
// projects are shown with all their groups opened.

type treeNode struct {
	name, path string
	depth      int
	group      bool
	res        *pickResult // projects only
	kids       []*treeNode
	count      int // projects underneath
	parent     *treeNode
}

// buildTree arranges the current results by group.
func (pk *projectPicker) buildTree() []*treeNode {
	root := &treeNode{group: true, depth: -1}
	groups := map[string]*treeNode{"": root}
	var groupFor func(path string) *treeNode
	groupFor = func(path string) *treeNode {
		if g, ok := groups[path]; ok {
			return g
		}
		parentPath, name := "", path
		if i := strings.LastIndexByte(path, '/'); i >= 0 {
			parentPath, name = path[:i], path[i+1:]
		}
		parent := groupFor(parentPath)
		g := &treeNode{name: name, path: path, depth: parent.depth + 1, group: true, parent: parent}
		parent.kids = append(parent.kids, g)
		groups[path] = g
		return g
	}
	for i := range pk.results {
		r := &pk.results[i]
		path := r.p.PathWithNamespace
		parentPath, name := "", path
		if j := strings.LastIndexByte(path, '/'); j >= 0 {
			parentPath, name = path[:j], path[j+1:]
		}
		parent := groupFor(parentPath)
		parent.kids = append(parent.kids, &treeNode{name: name, path: path, depth: parent.depth + 1, res: r, parent: parent})
	}
	var finish func(n *treeNode) int
	finish = func(n *treeNode) int {
		if !n.group {
			return 1
		}
		sort.SliceStable(n.kids, func(i, j int) bool {
			a, b := n.kids[i], n.kids[j]
			if a.group != b.group {
				return a.group
			}
			return strings.ToLower(a.name) < strings.ToLower(b.name)
		})
		n.count = 0
		for _, k := range n.kids {
			n.count += finish(k)
		}
		return n.count
	}
	finish(root)
	for _, k := range root.kids {
		k.parent = nil
	}
	return root.kids
}

// treeRows flattens the tree into the visible rows.
func (pk *projectPicker) treeRows() []*treeNode {
	roots := pk.buildTree()
	filtering := strings.TrimSpace(pk.input.Value()) != ""
	if pk.expanded == nil {
		pk.expanded = map[string]bool{}
		// open the chain of lone groups at the top (e.g. a single company
		// group) so the first screen isn't one collapsed line
		for kids := roots; len(kids) == 1 && kids[0].group; kids = kids[0].kids {
			pk.expanded[kids[0].path] = true
		}
	}
	var rows []*treeNode
	var visit func(n *treeNode)
	visit = func(n *treeNode) {
		rows = append(rows, n)
		if n.group && (filtering || pk.expanded[n.path]) {
			for _, k := range n.kids {
				visit(k)
			}
		}
	}
	for _, r := range roots {
		visit(r)
	}
	return rows
}

func (pk *projectPicker) treeSelected() *treeNode {
	rows := pk.treeRows()
	if pk.tcursor >= 0 && pk.tcursor < len(rows) {
		return rows[pk.tcursor]
	}
	return nil
}

// treeKey handles keys in tree mode; ok is false for keys it leaves to the
// prompt.
func (pk *projectPicker) treeKey(a *App, msg tea.KeyMsg) (tea.Cmd, bool) {
	rows := pk.treeRows()
	n := len(rows)
	move := func(d int) tea.Cmd {
		pk.tcursor = max(0, min(n-1, pk.tcursor+d))
		return pk.moved(a)
	}
	var cur *treeNode
	if pk.tcursor >= 0 && pk.tcursor < n {
		cur = rows[pk.tcursor]
	}
	switch msg.String() {
	case "up", "ctrl+p", "ctrl+k":
		return move(-1), true
	case "down", "ctrl+n", "ctrl+j":
		return move(1), true
	case "pgup", "ctrl+b":
		return move(-pk.page()), true
	case "pgdown", "ctrl+f":
		return move(pk.page()), true
	case "ctrl+u":
		return move(-pk.page() / 2), true
	case "ctrl+d":
		return move(pk.page() / 2), true
	case "right":
		if cur != nil && cur.group {
			if !pk.expanded[cur.path] {
				pk.expanded[cur.path] = true
				return nil, true
			}
			return move(1), true
		}
		return nil, true
	case "left":
		if cur == nil {
			return nil, true
		}
		if cur.group && pk.expanded[cur.path] {
			delete(pk.expanded, cur.path)
			return nil, true
		}
		if cur.parent != nil {
			for i, r := range rows {
				if r.path == cur.parent.path {
					pk.tcursor = i
				}
			}
			return pk.moved(a), true
		}
		return nil, true
	case "enter":
		if cur != nil && cur.group {
			pk.expanded[cur.path] = !pk.expanded[cur.path]
			return nil, true
		}
		return nil, false // a project: open it as in flat mode
	}
	return nil, false
}

func (pk *projectPicker) treeLines(a *App, w, rows int) []string {
	all := pk.treeRows()
	pk.tcursor = max(0, min(pk.tcursor, len(all)-1))
	if pk.tcursor < pk.toffset {
		pk.toffset = pk.tcursor
	}
	if pk.tcursor >= pk.toffset+rows {
		pk.toffset = pk.tcursor - rows + 1
	}
	filtering := strings.TrimSpace(pk.input.Value()) != ""
	lines := make([]string, rows)
	for i := 0; i < rows; i++ {
		idx := pk.toffset + i
		if idx >= len(all) {
			break
		}
		n := all[idx]
		selected := idx == pk.tcursor
		mark := "  "
		if selected {
			mark = sActive.Render(ic.caret) + " "
		}
		indent := strings.Repeat("  ", n.depth)
		var left, right string
		if n.group {
			open := filtering || pk.expanded[n.path]
			arrow, icon := "▸", ic.folder
			if open {
				arrow, icon = "▾", ic.folderOpen
			}
			left = indent + sDim.Render(arrow) + " " + sKey.Render(icon) + " " + sBold.Render(n.name)
			right = sDim.Render(fmt.Sprint(n.count))
		} else {
			name := n.name
			if pos := n.res.pos; len(pos) > 0 {
				off := len(n.path) - len(n.name)
				var shifted []int
				for _, p := range pos {
					if p >= off {
						shifted = append(shifted, p-off)
					}
				}
				name = highlight(name, shifted, func(s string) string { return s })
			}
			left = indent + "  " + sDim.Render(ic.project) + " " + name
			if !n.res.recent.IsZero() {
				right = sDim.Render(ic.clock + " " + since(n.res.recent))
			} else if !n.res.p.LastActivityAt.IsZero() {
				right = sDim.Render(since(n.res.p.LastActivityAt))
			}
		}
		line := pad(mark+left, w-ansi.StringWidth(right)-len(colGap)) + colGap + right
		if selected {
			line = selectLine(line, w)
		}
		lines[i] = a.clickRow(line,
			func() tea.Cmd { pk.tcursor = idx; return pk.moved(a) },
			func() tea.Cmd { return pk.key(a, keyEnter) })
	}
	return lines
}

// groupPreview describes the highlighted group.
func (pk *projectPicker) groupPreview(n *treeNode, w, h int) string {
	var L []string
	L = append(L, sTitle.Render(n.name), sDim.Render(n.path), "",
		fmt.Sprintf("%d %s", n.count, plural(n.count, "project")), "")
	for _, k := range n.kids {
		icon := ic.project
		if k.group {
			icon = ic.folder
		}
		L = append(L, sDim.Render(icon+" ")+fit(k.name, w-6))
	}
	return pane("Preview", L, w, h, false)
}
