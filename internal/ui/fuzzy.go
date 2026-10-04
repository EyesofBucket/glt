package ui

import (
	"strings"
	"unicode"
)

// fuzzyMatch scores s against a space-separated query, fzf style: every
// term must appear as a subsequence; matches at word boundaries and runs of
// consecutive characters score higher, gaps cost. Smart case: a term with
// an upper-case letter matches case-sensitively. pos holds the matched
// byte offsets in s, for highlighting.
func fuzzyMatch(query, s string) (score int, pos []int, ok bool) {
	for _, term := range strings.Fields(query) {
		sc, p, ok := matchTerm(term, s)
		if !ok {
			return 0, nil, false
		}
		score += sc
		pos = append(pos, p...)
	}
	return score, pos, true
}

const (
	scoreMatch       = 16
	bonusBoundary    = 10
	bonusCamel       = 7
	bonusConsecutive = 6
	bonusFirstChar   = 8
	penaltyGapStart  = 3
	penaltyGapExt    = 1
)

func lowerASCII(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + 32
	}
	return b
}

func isSep(b byte) bool {
	return b == '/' || b == '-' || b == '_' || b == ' ' || b == '.' || b == ':'
}

func matchTerm(term, s string) (int, []int, bool) {
	sensitive := strings.IndexFunc(term, unicode.IsUpper) >= 0
	eq := func(a, b byte) bool {
		if sensitive {
			return a == b
		}
		return lowerASCII(a) == lowerASCII(b)
	}
	// forward pass: find where the shortest-ending match finishes
	ti, end := 0, -1
	for i := 0; i < len(s) && ti < len(term); i++ {
		if eq(s[i], term[ti]) {
			ti++
			if ti == len(term) {
				end = i
			}
		}
	}
	if end < 0 {
		return 0, nil, false
	}
	// backward pass from end: tightest start for that end
	ti = len(term) - 1
	start := end
	for i := end; i >= 0 && ti >= 0; i-- {
		if eq(s[i], term[ti]) {
			ti--
			start = i
		}
	}
	// forward again within [start,end], preferring boundary positions
	pos := make([]int, 0, len(term))
	ti = 0
	for i := start; i <= end && ti < len(term); i++ {
		if eq(s[i], term[ti]) {
			pos = append(pos, i)
			ti++
		}
	}
	score := 0
	for k, i := range pos {
		score += scoreMatch
		switch {
		case i == 0 || isSep(s[i-1]):
			score += bonusBoundary
		case s[i] >= 'A' && s[i] <= 'Z' && s[i-1] >= 'a' && s[i-1] <= 'z':
			score += bonusCamel
		}
		if k == 0 && (i == 0 || isSep(s[i-1])) {
			score += bonusFirstChar
		}
		if k > 0 {
			if gap := i - pos[k-1] - 1; gap == 0 {
				score += bonusConsecutive
			} else {
				score -= penaltyGapStart + penaltyGapExt*(gap-1)
			}
		}
	}
	// matches inside the last path segment (the project name) matter most
	if base := strings.LastIndexByte(s, '/'); base >= 0 && pos[0] > base {
		score += 2 * len(term)
	}
	return score, pos, true
}

// highlight renders s with the byte offsets in pos styled as matches and the
// rest in style rest.
func highlight(s string, pos []int, rest func(string) string) string {
	if len(pos) == 0 {
		return rest(s)
	}
	mark := make(map[int]bool, len(pos))
	for _, p := range pos {
		mark[p] = true
	}
	var b strings.Builder
	runStart, inMatch := 0, mark[0]
	flush := func(i int) {
		if i <= runStart {
			return
		}
		if inMatch {
			b.WriteString(sMatch.Render(s[runStart:i]))
		} else {
			b.WriteString(rest(s[runStart:i]))
		}
		runStart = i
	}
	for i := 0; i < len(s); i++ {
		if mark[i] != inMatch {
			flush(i)
			inMatch = mark[i]
		}
	}
	flush(len(s))
	return b.String()
}
