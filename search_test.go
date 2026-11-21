package main

import (
	"slices"
	"testing"
)

func TestSearchAcrossLines(t *testing.T) {
	lines := []Line{
		{Segs: []Span{{Text: "  "}, {Text: "The quick brown"}}},
		{Segs: []Span{{Text: "  "}, {Text: "Fox", Style: Bold}, {Text: " jumps"}}},
		{},
		{Segs: []Span{{Text: "another fox"}}},
	}

	sr := search{query: normalize("brown  fox"), fold: true}

	ix, ms := sr.matchesIn(lines)
	if len(ms) != 1 || ms[0].line != 0 {
		t.Fatalf("matches = %+v, want one on line 0", ms)
	}

	// the match ends on "Fox" at line 1, column 2
	if p := ix.pos[ms[0].end-1]; p != (cellPos{1, 4}) {
		t.Errorf("last rune at %+v, want {1 4}", p)
	}

	sr.query, sr.fold = normalize("Fox"), false
	if _, ms := sr.matchesIn(lines); len(ms) != 1 {
		t.Errorf("case-sensitive Fox: %d matches, want 1", len(ms))
	}

	sr.fold = true
	if _, ms := sr.matchesIn(lines); len(ms) != 2 {
		t.Errorf("folded fox: %d matches, want 2", len(ms))
	}
}

func TestPickMatch(t *testing.T) {
	ms := []match{{line: 2}, {line: 5}, {line: 9}}

	tests := []struct {
		dir, lo, hi, want int
	}{
		{1, 0, 0, 0},
		{1, 0, 3, 1},
		{1, 0, 10, -1},
		{-1, 6, 0, 1},
		{-1, 2, 0, -1},
	}

	for _, tt := range tests {
		if got := pickMatch(ms, tt.dir, tt.lo, tt.hi); got != tt.want {
			t.Errorf("pickMatch(dir %d, lo %d, hi %d) = %d, want %d", tt.dir, tt.lo, tt.hi, got, tt.want)
		}
	}
}

func TestFindAll(t *testing.T) {
	if got := findAll([]rune("aaaa"), []rune("aa")); !slices.Equal(got, []int{0, 2}) {
		t.Errorf("findAll = %v", got)
	}
}
