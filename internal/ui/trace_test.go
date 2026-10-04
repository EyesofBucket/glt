package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"gitlab-tui/internal/gitlab"
)

func TestCleanLine(t *testing.T) {
	cases := []struct {
		in, out, section string
		skip             bool
	}{
		{"plain", "plain", "", false},
		{"section_start:1700000000:step_script\r\x1b[0K\x1b[36;1mExecuting\x1b[0;m", "\x1b[36;1mExecuting\x1b[0;m\x1b[0m", "step_script", false},
		{"section_end:1700000000:step_script\r\x1b[0K", "", "", true},
		{"section_start:1:prep[collapsed=true]\r\x1b[0K\x1b[0KPreparing", "Preparing", "prep", false},
		{"10%\r50%\r100%", "100%", "", false},
		{"crlf\r", "crlf", "", false},
		{"a\tb", "a       b", "", false},
		{"\x1b[2Jcleared\x1b[1;1H", "cleared", "", false},
	}
	for _, c := range cases {
		out, sec, skip := cleanLine(c.in)
		if out != c.out || sec != c.section || skip != c.skip {
			t.Errorf("cleanLine(%q) = %q,%q,%v want %q,%q,%v", c.in, out, sec, skip, c.out, c.section, c.skip)
		}
	}
}

func TestTraceIncremental(t *testing.T) {
	s := newStore("test")
	s.cacheDir = ""
	key := "trace:p:1"
	s.trace(key).loading = true
	s.applyTrace(traceMsg{key: key, offset: 0, chunk: &gitlab.TraceChunk{Data: []byte("one\ntw")}})
	tr := s.trace(key)
	if tr.lineCount() != 2 || tr.line(0) != "one" || tr.line(1) != "tw" {
		t.Fatalf("after first chunk: %q %q", tr.lines, tr.tail)
	}
	if len(tr.lines) != 0 {
		t.Fatalf("last line must stay provisional: %q", tr.lines)
	}
	// server honoured Range
	s.applyTrace(traceMsg{key: key, offset: 6, chunk: &gitlab.TraceChunk{Data: []byte("o\nthree\n"), Partial: true}})
	if tr.lineCount() != 3 || tr.line(1) != "two" || tr.line(2) != "three" {
		t.Fatalf("after partial: %q %q", tr.lines, tr.tail)
	}
	// server ignored Range and sent the whole log
	s.applyTrace(traceMsg{key: key, offset: 14, chunk: &gitlab.TraceChunk{Data: []byte("one\ntwo\nthree\nfour\n")}})
	if !tr.noRange || tr.lineCount() != 4 || tr.line(3) != "four" {
		t.Fatalf("after full: %q noRange=%v", tr.lines, tr.noRange)
	}
	// stale partial response is ignored
	s.applyTrace(traceMsg{key: key, offset: 3, chunk: &gitlab.TraceChunk{Data: []byte("zzz"), Partial: true}})
	if tr.lineCount() != 4 {
		t.Fatalf("stale chunk applied: %q", tr.lines)
	}
}

func TestNaturalLess(t *testing.T) {
	if !naturalLess("test 2/10", "test 10/10") || naturalLess("test 10/10", "test 2/10") {
		t.Error("digit runs should compare numerically")
	}
	if !naturalLess("build", "deploy") {
		t.Error("lexical fallback")
	}
}

func TestGroupStages(t *testing.T) {
	jobs := []gitlab.Job{
		{ID: 30, Stage: "deploy", Name: "prod"},
		{ID: 21, Stage: "test", Name: "unit 10"},
		{ID: 20, Stage: "test", Name: "unit 2"},
		{ID: 10, Stage: "build", Name: "image"},
		{ID: 40, Stage: "build", Name: "retried"}, // retried later, still in build
	}
	g := groupStages(jobs)
	if len(g) != 3 || g[0].name != "build" || g[1].name != "test" || g[2].name != "deploy" {
		t.Fatalf("stage order: %+v", g)
	}
	if g[1].jobs[0].Name != "unit 2" {
		t.Errorf("job order: %+v", g[1].jobs)
	}
}

func TestToggleDraft(t *testing.T) {
	if got, _ := toggleDraft("Draft: fix thing", true); got != "fix thing" {
		t.Errorf("got %q", got)
	}
	if got, _ := toggleDraft("[Draft] fix thing", true); got != "fix thing" {
		t.Errorf("got %q", got)
	}
	if got, _ := toggleDraft("fix thing", false); got != "Draft: fix thing" {
		t.Errorf("got %q", got)
	}
}

func TestTimestampedRecords(t *testing.T) {
	log := "2026-07-29T19:37:05.907760Z 00O \x1b[0KRunning with gitlab-runner\x1b[0;m\n" +
		"2026-07-29T19:37:05.907794Z 00O section_start:1785353825:prepare_executor\r\n" +
		"2026-07-29T19:37:05.907794Z 00O+\x1b[0K\x1b[0K\x1b[36;1mPreparing\x1b[0;m\n" +
		"2026-07-29T19:37:23.003634Z 01O \r\n" +
		"2026-07-29T19:37:23.003637Z 01O+Uploading 0 B   \rUploading 161 B   \n" +
		"2026-07-29T19:37:24.492297Z 00O section_end:1785353844:prepare_executor\r\n" +
		"2026-07-29T19:37:24.492307Z 00O+\x1b[0K\n" +
		"2026-07-29T19:37:24.641890Z 00O \x1b[32;1mJob succeeded\x1b[0;m\n"
	s := newStore("test")
	s.cacheDir = ""
	s.applyTrace(traceMsg{key: "k", chunk: &gitlab.TraceChunk{Data: []byte(log)}})
	tr := s.trace("k")
	var got []string
	for i := 0; i < tr.lineCount(); i++ {
		got = append(got, ansi.Strip(tr.line(i)))
	}
	want := []string{"Running with gitlab-runner", "Preparing", "Uploading 161 B   ", "Job succeeded"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q\nwant %q", got, want)
	}
	if !tr.hasTS || tr.stamp(0) != "19:37:05" || tr.stamp(3) != "19:37:24" {
		t.Errorf("timestamps: %q", tr.ts)
	}
	if len(tr.sections) != 1 || tr.section[0] != "prepare_executor" || tr.sections[0] != 1 {
		t.Errorf("sections: %v %v", tr.sections, tr.section)
	}
}
