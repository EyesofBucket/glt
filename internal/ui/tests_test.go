package ui

import (
	"testing"

	"gitlab-tui/internal/gitlab"
)

func testReport() *gitlab.TestReport {
	return &gitlab.TestReport{
		TotalCount: 5, SuccessCount: 2, FailedCount: 1, ErrorCount: 1, SkippedCount: 1,
		TestSuites: []gitlab.TestSuite{
			{Name: "unit", TotalCount: 4, TestCases: []gitlab.TestCase{
				{Name: "passes", Status: "success"},
				{Name: "breaks", Status: "failed"},
				{Name: "later", Status: "skipped"},
				{Name: "crashes", Status: "error"},
			}},
			{Name: "lint", TotalCount: 1, TestCases: []gitlab.TestCase{{Name: "clean", Status: "success"}}},
		},
	}
}

func rowNames(rows []testRow) []string {
	var out []string
	for _, r := range rows {
		if r.tc == nil {
			out = append(out, "# "+r.suite.Name)
		} else {
			out = append(out, r.tc.Name)
		}
	}
	return out
}

func TestTestRows(t *testing.T) {
	r := testReport()
	tp := &testsPopup{collapsed: map[string]bool{}, l: newListState()}
	tp.pickFilter(r)
	check := func(want ...string) {
		t.Helper()
		got := rowNames(tp.rows(r))
		if len(got) != len(want) {
			t.Fatalf("rows = %q, want %q", got, want)
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("rows = %q, want %q", got, want)
			}
		}
	}
	// opens on the failures; suites with none are hidden
	check("# unit", "breaks", "crashes")

	// all: failures first, then skipped, then passed
	tp.filter = testsAll
	check("# unit", "breaks", "crashes", "later", "passes", "# lint", "clean")

	tp.collapsed["unit"] = true
	check("# unit", "# lint", "clean")

	// a search opens folded suites
	tp.l.query = "crash"
	check("# unit", "crashes")
}

func TestTestFilterWhenAllPass(t *testing.T) {
	r := &gitlab.TestReport{TotalCount: 1, SuccessCount: 1,
		TestSuites: []gitlab.TestSuite{{Name: "s", TestCases: []gitlab.TestCase{{Name: "ok", Status: "success"}}}}}
	tp := &testsPopup{collapsed: map[string]bool{}, l: newListState()}
	tp.pickFilter(r)
	if tp.filter != testsAll {
		t.Fatalf("filter = %v, want all when nothing failed", tp.filter)
	}
}
