package ui

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"gitlab-tui/internal/gitlab"
)

// traceState holds an incrementally-fetched, pre-processed job log.
type traceState struct {
	raw      []byte
	done     int      // bytes of raw already split into records
	lines    []string // committed display-ready lines (ANSI SGR only)
	ts       []string // per-line HH:MM:SS when the runner emits timestamps
	tail     []string // processed lines not yet committed (pending + partial)
	tailTS   []string
	pending  string // current logical line; may still get continuations
	pendTS   string
	hasPend  bool
	hasTS    bool
	sections []int    // line indexes where a log section starts
	section  []string // section names, parallel to sections
	loading  bool
	at       time.Time
	err      error
	version  int // bumped whenever content changes
	noRange  bool
}

func (t *traceState) lineCount() int { return len(t.lines) + len(t.tail) }

func (t *traceState) line(i int) string {
	if i < len(t.lines) {
		return t.lines[i]
	}
	return t.tail[i-len(t.lines)]
}

func (t *traceState) stamp(i int) string {
	if i < len(t.ts) {
		return t.ts[i]
	}
	if j := i - len(t.lines); j >= 0 && j < len(t.tailTS) {
		return t.tailTS[j]
	}
	return ""
}

type traceMsg struct {
	store  *Store
	key    string
	offset int
	chunk  *gitlab.TraceChunk
	err    error
}

func traceKey(project string, job int) string { return fmt.Sprintf("trace:%s:%d", project, job) }

func (s *Store) trace(key string) *traceState {
	t := s.traces[key]
	if t == nil {
		t = &traceState{}
		s.traces[key] = t
	}
	return t
}

func fetchTrace(s *Store, c *gitlab.Client, project string, job int, maxAge time.Duration) tea.Cmd {
	key := traceKey(project, job)
	t := s.trace(key)
	if t.loading || (!t.at.IsZero() && time.Since(t.at) < maxAge) || s.backedOff() {
		return nil
	}
	t.loading = true
	off := len(t.raw)
	if t.noRange {
		off = 0
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		ch, err := c.Trace(ctx, project, job, off)
		return traceMsg{store: s, key: key, offset: off, chunk: ch, err: err}
	}
}

func (s *Store) applyTrace(msg traceMsg) {
	t := s.trace(msg.key)
	t.loading = false
	t.at = time.Now()
	if msg.err != nil {
		t.err = msg.err
		if ra, ok := gitlab.IsRateLimited(msg.err); ok {
			s.backoffUntil = time.Now().Add(max(ra, 10*time.Second))
		}
		return
	}
	t.err = nil
	data := msg.chunk.Data
	switch {
	case msg.chunk.Partial:
		if msg.offset != len(t.raw) {
			return // stale response
		}
		if len(data) == 0 {
			return
		}
		t.raw = append(t.raw, data...)
	case msg.offset > 0:
		// Asked for a range, got the whole thing: remember not to bother.
		t.noRange = true
		fallthrough
	default:
		if len(data) >= len(t.raw) && bytes.Equal(data[:len(t.raw)], t.raw) {
			if len(data) == len(t.raw) {
				return
			}
			t.raw = append(t.raw, data[len(t.raw):]...)
		} else {
			*t = traceState{raw: data, noRange: t.noRange, at: t.at}
		}
	}
	t.process()
	t.version++
}

// parseRecord splits a runner log record of the form
// "2026-07-29T19:37:05.907760Z 00O+content" into its timestamp,
// continuation flag and content. Older runners don't emit the prefix.
func parseRecord(l string) (ts string, cont bool, content string, ok bool) {
	sp := strings.IndexByte(l, ' ')
	if sp < 20 || sp > 40 || l[10] != 'T' || l[sp-1] != 'Z' || len(l) < sp+5 {
		return "", false, l, false
	}
	if t := l[sp+3]; t != 'O' && t != 'E' {
		return "", false, l, false
	}
	return l[11:19], l[sp+4] == '+', l[sp+5:], true
}

func (t *traceState) process() {
	rest := t.raw[t.done:]
	if nl := bytes.LastIndexByte(rest, '\n'); nl >= 0 {
		for _, l := range strings.Split(string(rest[:nl]), "\n") {
			t.addRecord(l)
		}
		t.done += nl + 1
		rest = t.raw[t.done:]
	}
	// uncommitted content is shown as provisional tail lines
	t.tail, t.tailTS = t.tail[:0], t.tailTS[:0]
	partial := string(rest)
	pts, pcont, pcontent, pok := parseRecord(partial)
	if !pok {
		pcontent = partial
	}
	pend := t.pending
	if pok && pcont {
		pend += pcontent
		partial = ""
	}
	if t.hasPend {
		if out, _, only := cleanLine(pend); !only {
			t.tail, t.tailTS = append(t.tail, out), append(t.tailTS, t.pendTS)
		}
	}
	if partial != "" {
		if out, _, only := cleanLine(pcontent); !only {
			t.tail, t.tailTS = append(t.tail, out), append(t.tailTS, pts)
		}
	}
}

func (t *traceState) addRecord(l string) {
	ts, cont, content, ok := parseRecord(l)
	if ok {
		t.hasTS = true
	}
	if ok && cont && t.hasPend {
		t.pending += content
		return
	}
	t.commit()
	t.pending, t.pendTS, t.hasPend = content, ts, true
}

func (t *traceState) commit() {
	if !t.hasPend {
		return
	}
	t.hasPend = false
	clean, section, onlyMarkers := cleanLine(t.pending)
	if section != "" {
		t.sections = append(t.sections, len(t.lines))
		t.section = append(t.section, section)
	}
	if onlyMarkers {
		return
	}
	t.lines = append(t.lines, clean)
	t.ts = append(t.ts, t.pendTS)
}

var sectionRe = regexp.MustCompile(`section_(start|end):\d+:([^\r\n\[]*)(\[[^\]]*\])?\r`)

// cleanLine turns a raw GitLab log line into something safe to draw:
// section markers removed, carriage-return overwrites resolved, tabs
// expanded and every escape sequence except SGR colours dropped.
func cleanLine(l string) (out, section string, onlyMarkers bool) {
	l = strings.TrimSuffix(l, "\r")
	hadMarker := false
	if strings.Contains(l, "section_") {
		for _, m := range sectionRe.FindAllStringSubmatch(l, -1) {
			hadMarker = true
			if m[1] == "start" {
				section = m[2]
			}
		}
		l = sectionRe.ReplaceAllString(l, "")
	}
	if i := strings.LastIndexByte(l, '\r'); i >= 0 {
		// progress-bar style overwrite: keep the final state
		l = l[i+1:]
	}
	out = sanitize(l)
	if hadMarker && strings.TrimSpace(out) == "" {
		return "", section, true
	}
	return out, section, false
}

func sanitize(s string) string {
	if !strings.ContainsAny(s, "\x1b\t\b\x07") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 8)
	col, sgr := 0, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == 0x1b && i+1 < len(s) && s[i+1] == '[':
			j := i + 2
			for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
				j++
			}
			if j < len(s) && s[j] == 'm' {
				b.WriteString(s[i : j+1])
				sgr = true
			}
			i = j
		case c == 0x1b && i+1 < len(s) && s[i+1] == ']':
			// OSC: skip to BEL or ST
			j := i + 2
			for j < len(s) && s[j] != 0x07 && !(s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\') {
				j++
			}
			if j < len(s) && s[j] == 0x1b {
				j++
			}
			i = j
		case c == 0x1b:
			i++ // two-byte escape, drop
		case c == '\t':
			n := 8 - col%8
			b.WriteString(strings.Repeat(" ", n))
			col += n
		case c < 0x20 || c == 0x7f:
			// drop other control characters
		default:
			b.WriteByte(c)
			if c < 0x80 || c >= 0xc0 {
				col++
			}
		}
	}
	if sgr {
		b.WriteString("\x1b[0m")
	}
	return b.String()
}
