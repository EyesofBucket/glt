package ui

import (
	"sort"
	"testing"
)

func TestFuzzyRanking(t *testing.T) {
	paths := []string{
		"tektonux/ibcs/cwmi-hmi-v2",
		"tektonux/ibcs/cwmi-hmi",
		"tektonux/common/ci-tools/ci-images",
		"tektonux/common/ci-tools/ci-jobs",
		"tektonux/ibcs/faad-hmi",
	}
	rank := func(q string) []string {
		type r struct {
			p string
			s int
		}
		var rs []r
		for _, p := range paths {
			if s, _, ok := fuzzyMatch(q, p); ok {
				rs = append(rs, r{p, s})
			}
		}
		sort.SliceStable(rs, func(i, j int) bool { return rs[i].s > rs[j].s })
		var out []string
		for _, x := range rs {
			out = append(out, x.p)
		}
		return out
	}
	if got := rank("cijobs"); len(got) == 0 || got[0] != "tektonux/common/ci-tools/ci-jobs" {
		t.Errorf("cijobs: %v", got)
	}
	if got := rank("faad"); len(got) != 1 || got[0] != "tektonux/ibcs/faad-hmi" {
		t.Errorf("faad: %v", got)
	}
	if got := rank("ibcs hmi"); len(got) != 3 {
		t.Errorf("multi-term: %v", got)
	}
	if got := rank("zzz"); len(got) != 0 {
		t.Errorf("no match expected: %v", got)
	}
	if _, _, ok := fuzzyMatch("CWMI", "tektonux/ibcs/cwmi-hmi"); ok {
		t.Error("upper-case query should match case-sensitively")
	}
}

func TestFuzzyPositions(t *testing.T) {
	_, pos, ok := fuzzyMatch("hmi", "a/cwmi-hmi")
	if !ok || len(pos) != 3 || pos[0] != 7 {
		t.Errorf("expected tight match on the word 'hmi', got %v", pos)
	}
}
