package ui

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"gitlab-tui/internal/gitlab"
)

// Store is a stale-while-revalidate cache of API resources. Views never
// block on the network: they render whatever the store has (in memory, or
// from the on-disk cache of the previous session) and ask for a refresh.
// It's only touched from the Bubble Tea update goroutine, so no locking.
type Store struct {
	m        map[string]*entry
	traces   map[string]*traceState
	cacheDir string

	backoffUntil time.Time
	backoffStep  time.Duration
}

type ctxT = context.Context

// bg is the context for user-initiated writes; the HTTP client enforces
// its own timeout.
func bg() context.Context { return context.Background() }

type entry struct {
	val     any
	at      time.Time // last network response (success or error)
	disk    bool      // val came from the disk cache and hasn't been refreshed
	loading bool
	err     error
}

type resultMsg struct {
	store *Store // results for a store we've since replaced are dropped
	key   string
	val   any
	err   error
	disk  bool
}

func newStore(host string) *Store {
	dir := ""
	if d, err := os.UserCacheDir(); err == nil {
		dir = filepath.Join(d, "glt", host)
		_ = os.MkdirAll(dir, 0o700)
		go pruneCache(dir)
	}
	return &Store{m: map[string]*entry{}, traces: map[string]*traceState{}, cacheDir: dir}
}

func pruneCache(dir string) {
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > 14*24*time.Hour {
			os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

func (s *Store) entry(key string) *entry {
	e := s.m[key]
	if e == nil {
		e = &entry{}
		s.m[key] = e
	}
	return e
}

// get returns the cached value for key, if any.
func get[T any](s *Store, key string) (T, *entry) {
	var zero T
	e := s.m[key]
	if e == nil || e.val == nil {
		return zero, e
	}
	v, ok := e.val.(T)
	if !ok {
		return zero, e
	}
	return v, e
}

func (s *Store) fresh(key string, maxAge time.Duration) bool {
	e := s.m[key]
	return e != nil && (e.loading || (!e.at.IsZero() && time.Since(e.at) < maxAge))
}

func (s *Store) loading(key string) bool {
	e := s.m[key]
	return e != nil && e.loading
}

func (s *Store) backedOff() bool { return time.Now().Before(s.backoffUntil) }

// fetch loads key from the network unless it's younger than maxAge or
// already in flight. On first access it also races in the disk cache so
// something can be drawn immediately.
func fetch[T any](s *Store, key string, maxAge time.Duration, f func(context.Context) (T, error)) tea.Cmd {
	if s.fresh(key, maxAge) || s.backedOff() {
		return nil
	}
	e := s.entry(key)
	e.loading = true
	var cmds []tea.Cmd
	if e.val == nil && s.cacheDir != "" {
		cmds = append(cmds, diskLoad[T](s, key))
	}
	dir := s.cacheDir
	cmds = append(cmds, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		v, err := f(ctx)
		if err == nil && dir != "" {
			go diskSave(dir, key, v)
		}
		return resultMsg{store: s, key: key, val: v, err: err}
	})
	return tea.Batch(cmds...)
}

func (s *Store) apply(msg resultMsg) {
	e := s.entry(msg.key)
	if msg.disk {
		if e.val == nil && e.at.IsZero() {
			e.val, e.disk = msg.val, true
		}
		return
	}
	e.loading = false
	e.at = time.Now()
	if msg.err != nil {
		e.err = msg.err
		if ra, ok := gitlab.IsRateLimited(msg.err); ok {
			s.backoffStep = min(max(s.backoffStep*2, 5*time.Second), 2*time.Minute)
			s.backoffUntil = time.Now().Add(max(ra, s.backoffStep))
		}
		return
	}
	s.backoffStep = 0
	e.val, e.err, e.disk = msg.val, nil, false
}

// invalidate forces the next fetch of key to hit the network.
func (s *Store) invalidate(keys ...string) {
	for _, k := range keys {
		if e := s.m[k]; e != nil {
			e.at = time.Time{}
		}
	}
}

func cachePath(dir, key string) string {
	h := sha1.Sum([]byte(key))
	return filepath.Join(dir, hex.EncodeToString(h[:12])+".json")
}

func diskLoad[T any](s *Store, key string) tea.Cmd {
	dir := s.cacheDir
	return func() tea.Msg {
		b, err := os.ReadFile(cachePath(dir, key))
		if err != nil {
			return nil
		}
		var v T
		if json.Unmarshal(b, &v) != nil {
			return nil
		}
		return resultMsg{store: s, key: key, val: v, disk: true}
	}
}

func diskSave(dir, key string, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	p := cachePath(dir, key)
	tmp := p + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		os.Rename(tmp, p)
	}
}
