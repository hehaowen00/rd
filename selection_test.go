package main

import (
	"testing"

	"github.com/gdamore/tcell/v2"
)

func selLine(s string, end bool) Line {
	return Line{Segs: []Span{{Text: s}}, End: end}
}

// selReader is a reader showing lines as one page at (10, 2), 30 wide.
func selReader(lines ...Line) *Reader {
	return &Reader{
		lines: lines,
		shown: []shownPage{{x: 10, y: 2, w: 30, first: 0, last: len(lines)}},
	}
}

func TestSelectedText(t *testing.T) {
	r := selReader(
		selLine("The quick brown fox", false),
		selLine("jumps over the lazy", false),
		selLine("dog.", true),
		Line{},
		selLine("  Next paragraph", true),
	)

	tests := []struct {
		name           string
		x0, y0, x1, y1 int
		want           string
	}{
		{"within a line", 14, 2, 18, 2, "quick"},
		{"backwards", 18, 2, 14, 2, "quick"},
		{"across wrapped lines", 20, 2, 19, 3, "brown fox jumps over"},
		{"across paragraphs", 10, 4, 15, 6, "dog.\n\nNext"},
		{"dragged off the bottom", 10, 6, 10, 20, "Next paragraph"},
		{"past the end of a line", 25, 4, 26, 4, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r.selectStart(tt.x0, tt.y0)
			r.selectDrag(tt.x1, tt.y1)

			if got := r.selectEnd(tt.x1, tt.y1); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSelectPreformatted(t *testing.T) {
	pre := func(s string) Line { return Line{Segs: []Span{{Text: s}}, Pre: true} }
	r := selReader(pre("if x {"), pre("    y()"), pre("}"))

	r.selectStart(10, 2)

	if got := r.selectEnd(10, 4); got != "if x {\n    y()\n}" {
		t.Errorf("got %q", got)
	}
}

func TestClickDoesNotSelect(t *testing.T) {
	r := selReader(selLine("word", true))

	r.selectStart(11, 2)
	if got := r.selectEnd(11, 2); got != "" || r.sel.on {
		t.Errorf("a click selected %q", got)
	}

	r.selectStart(0, 0) // off the page
	r.selectDrag(12, 2)
	if r.sel.on {
		t.Error("a drag started off the page selected text")
	}
}

func TestDrawSelection(t *testing.T) {
	s := tcell.NewSimulationScreen("")
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	defer s.Fini()
	s.SetSize(50, 6)

	r := selReader(selLine("hello world", true))
	put(s, 10, 2, 30, "hello world", styleNormal)

	r.selectStart(16, 2)
	r.selectDrag(20, 2)
	r.drawSelection(s)

	for x := 9; x < 23; x++ {
		_, _, st, _ := s.GetContent(x, 2)
		_, _, attrs := st.Decompose()
		want := x >= 16 && x <= 20
		if got := attrs&tcell.AttrReverse != 0; got != want {
			t.Errorf("cell %d reversed = %v, want %v", x, got, want)
		}
	}
}

func TestOSC52(t *testing.T) {
	if got := osc52("hi"); got != "\x1b]52;c;aGk=\x1b\\" {
		t.Errorf("osc52 = %q", got)
	}
}

func TestSelectLaidOutParagraphs(t *testing.T) {
	blocks := []Block{
		{Spans: []Span{{Text: "Hello "}, {Text: "wide", Style: Italic}, {Text: " world, this is a test paragraph."}}},
		{Spans: []Span{{Text: "A second one."}}},
		{Spans: []Span{{Text: "After a gap."}}, SpaceBefore: true},
	}

	lines := Layout(blocks, 14, 40, nil)
	if len(lines) < 5 {
		t.Fatalf("expected the first paragraph to wrap, got %d lines", len(lines))
	}

	r := &Reader{lines: lines, shown: []shownPage{{x: 0, y: 0, w: 14, first: 0, last: len(lines)}}}
	r.selectStart(0, 0)

	want := "Hello wide world, this is a test paragraph.\nA second one.\n\nAfter a gap."
	if got := r.selectEnd(13, len(lines)-1); got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}
