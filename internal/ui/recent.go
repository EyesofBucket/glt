package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

var recentMu sync.Mutex

// recentProject is a project the user opened in glt, persisted in the
// state dir so the picker and dashboard can surface it first.
type recentProject struct {
	Path  string    `json:"path"`
	ID    int       `json:"id,omitempty"`
	At    time.Time `json:"at"`
	Count int       `json:"count"`
}

const maxRecent = 40

func stateDir(host string) string {
	d := os.Getenv("XDG_STATE_HOME")
	if d == "" {
		home, _ := os.UserHomeDir()
		d = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(d, "glt", host)
}

func loadRecent(host string) []recentProject {
	b, err := os.ReadFile(filepath.Join(stateDir(host), "recent.json"))
	if err != nil {
		return nil
	}
	var r []recentProject
	_ = json.Unmarshal(b, &r)
	return r
}

func saveRecent(host string, r []recentProject) {
	recentMu.Lock()
	defer recentMu.Unlock()
	dir := stateDir(host)
	if os.MkdirAll(dir, 0o700) != nil {
		return
	}
	b, err := json.Marshal(r)
	if err != nil {
		return
	}
	p := filepath.Join(dir, "recent.json")
	if os.WriteFile(p+".tmp", b, 0o600) == nil {
		os.Rename(p+".tmp", p)
	}
}

// touchProject records a visit to path.
func (a *App) touchProject(path string) {
	if path == "" {
		return
	}
	now := time.Now()
	found := false
	for i := range a.recent {
		if a.recent[i].Path == path {
			a.recent[i].At = now
			a.recent[i].Count++
			if a.recent[i].ID == 0 {
				a.recent[i].ID = a.projIDs[path]
			}
			found = true
			break
		}
	}
	if !found {
		a.recent = append(a.recent, recentProject{Path: path, ID: a.projIDs[path], At: now, Count: 1})
	}
	sort.SliceStable(a.recent, func(i, j int) bool { return a.recent[i].At.After(a.recent[j].At) })
	if len(a.recent) > maxRecent {
		a.recent = a.recent[:maxRecent]
	}
	snapshot := append([]recentProject(nil), a.recent...)
	go saveRecent(a.ctx.Host, snapshot)
}

func (a *App) recentAt(path string) (time.Time, bool) {
	for _, r := range a.recent {
		if r.Path == path {
			return r.At, true
		}
	}
	return time.Time{}, false
}
