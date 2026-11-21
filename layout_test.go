package main

import (
	"slices"
	"testing"
)

func textLine(s string) Line { return Line{Segs: []Span{{Text: s}}} }

func imageLines(rows int) []Line {
	var out []Line
	for r := range rows {
		out = append(out, Line{Image: &ImageRow{Path: "i.png", Row: r, Rows: rows, Cols: 10}})
	}
	return out
}

func TestPaginate(t *testing.T) {
	a, blank := textLine("a"), Line{}

	tests := []struct {
		name  string
		lines []Line
		rows  int
		want  []int
	}{
		{"empty", nil, 5, []int{0}},
		{"fits", []Line{a, a, a}, 5, []int{0}},
		{"exact breaks", []Line{a, a, a, a, a, a}, 3, []int{0, 3}},
		{"skips blank at page top", []Line{a, a, blank, a, a}, 2, []int{0, 3}},
		{"skips leading blanks", []Line{blank, blank, a}, 2, []int{2}},
		{
			"image moves whole to next page",
			append([]Line{a, a}, imageLines(3)...),
			4,
			[]int{0, 2},
		},
		{
			"image taller than page is split",
			imageLines(5),
			3,
			[]int{0, 3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Paginate(tt.lines, tt.rows); !slices.Equal(got, tt.want) {
				t.Errorf("Paginate = %v, want %v", got, tt.want)
			}
		})
	}
}
