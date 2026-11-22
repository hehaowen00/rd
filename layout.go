package main

import (
	"path"
	"strings"
	"unicode"

	"github.com/mattn/go-runewidth"
)

// Line is one screen row of laid-out chapter content.
type Line struct {
	Segs  []Span
	Image *ImageRow // non-nil when this row is part of an image
	Rule  bool
	Break bool // a page must start at this line
	// End marks the last line of a paragraph, and Pre a line of
	// preformatted text, so copied text breaks where the book does.
	End bool
	Pre bool
}

// ImageRow is one terminal row of an image.
type ImageRow struct {
	Path string
	Row  int // 0-based row within the image
	Rows int // total rows the image occupies
	Cols int
}

// ImageSizer reports how many cells an image occupies when fitted to the
// given box, or ok=false if it cannot be displayed.
type ImageSizer func(path string, maxCols, maxRows int) (cols, rows int, ok bool)

// Layout wraps blocks to width cells. Images are fitted to width × maxRows.
func Layout(blocks []Block, width, maxRows int, size ImageSizer) []Line {
	var lines []Line
	for _, b := range blocks {
		start := len(lines)
		lines = layoutBlock(lines, b, width, maxRows, size)
		if b.PageBreak && start < len(lines) {
			lines[start].Break = true
		}
	}
	return lines
}

// layoutBlock appends the lines of one block.
func layoutBlock(lines []Line, b Block, width, maxRows int, size ImageSizer) []Line {
	if b.SpaceBefore && len(lines) > 0 {
		lines = append(lines, Line{})
	}
	switch b.Kind {
	case BlockRule:
		lines = append(lines, Line{Rule: true})
	case BlockImage:
		cols, rows, ok := size(b.Image, width, maxRows)
		if !ok {
			alt := b.Alt
			if alt == "" {
				alt = path.Base(b.Image)
			}
			b = Block{Spans: []Span{{Text: "[" + strImageMissing + ": " + alt + "]", Style: Dim | Italic}}}
			return append(lines, textLines(b, width)...)
		}
		for r := range rows {
			lines = append(lines, Line{Image: &ImageRow{Path: b.Image, Row: r, Rows: rows, Cols: cols}})
		}
	default:
		lines = append(lines, textLines(b, width)...)
	}
	return lines
}

// textLines wraps a text block and marks its lines for copying.
func textLines(b Block, width int) []Line {
	ls := wrap(b, width)
	for i := range ls {
		ls[i].Pre = b.Pre
	}

	if len(ls) > 0 {
		ls[len(ls)-1].End = true
	}

	return ls
}

// word is a run of non-space text, possibly spanning several styles.
type word struct {
	segs  []Span
	width int
}

func splitWords(spans []Span, pre bool) []word {
	var words []word
	var cur word
	var buf strings.Builder
	var bufStyle Style
	pushBuf := func() {
		if buf.Len() > 0 {
			cur.segs = append(cur.segs, Span{Text: buf.String(), Style: bufStyle})
			buf.Reset()
		}
	}
	endWord := func() {
		pushBuf()
		if len(cur.segs) > 0 {
			words = append(words, cur)
		}
		cur = word{}
	}
	for _, s := range spans {
		if buf.Len() > 0 && s.Style != bufStyle {
			pushBuf()
		}
		bufStyle = s.Style
		for _, r := range s.Text {
			if !pre && unicode.IsSpace(r) {
				endWord()
				bufStyle = s.Style
				continue
			}
			buf.WriteRune(r)
			cur.width += runewidth.RuneWidth(r)
		}
	}
	endWord()
	return words
}

func wrap(b Block, width int) []Line {
	avail := max(width-b.Indent, 10)
	firstPad := strings.Repeat(" ", b.Indent)
	if b.Prefix != "" {
		pw := runewidth.StringWidth(b.Prefix)
		firstPad = strings.Repeat(" ", max(b.Indent-pw, 0)) + b.Prefix
	}
	restPad := strings.Repeat(" ", b.Indent)

	var lines []Line
	var cur []Span
	curW := 0
	newLine := func() {
		pad := restPad
		if len(lines) == 0 {
			pad = firstPad
		}
		segs := append([]Span{{Text: pad}}, cur...)
		lines = append(lines, Line{Segs: segs})
		cur, curW = nil, 0
	}

	if b.Pre {
		// One word per line: the whole line, hard-broken at the width.
		for _, w := range splitWords(b.Spans, true) {
			for _, piece := range hardBreak(w, avail) {
				cur, curW = piece.segs, piece.width
				newLine()
			}
		}
		if len(lines) == 0 {
			lines = append(lines, Line{})
		}
		return lines
	}

	for _, w := range splitWords(b.Spans, false) {
		need := w.width
		if curW > 0 {
			need++
		}
		if curW+need > avail && curW > 0 {
			newLine()
			need = w.width
		}
		if w.width > avail {
			pieces := hardBreak(w, avail)
			for _, p := range pieces[:len(pieces)-1] {
				cur, curW = p.segs, p.width
				newLine()
			}
			w = pieces[len(pieces)-1]
			need = w.width
		}
		if curW > 0 {
			cur = append(cur, Span{Text: " ", Style: w.segs[0].Style &^ Underline})
		}
		cur = append(cur, w.segs...)
		curW += need
	}
	if len(cur) > 0 {
		newLine()
	}
	return lines
}

// hardBreak splits a word into pieces at most width cells wide.
func hardBreak(w word, width int) []word {
	var out []word
	var cur word
	for _, s := range w.segs {
		var buf strings.Builder
		for _, r := range s.Text {
			rw := runewidth.RuneWidth(r)
			if cur.width+rw > width && cur.width > 0 {
				if buf.Len() > 0 {
					cur.segs = append(cur.segs, Span{Text: buf.String(), Style: s.Style})
					buf.Reset()
				}
				out = append(out, cur)
				cur = word{}
			}
			buf.WriteRune(r)
			cur.width += rw
		}
		if buf.Len() > 0 {
			cur.segs = append(cur.segs, Span{Text: buf.String(), Style: s.Style})
		}
	}
	if len(cur.segs) > 0 || len(out) == 0 {
		out = append(out, cur)
	}
	return out
}

// Paginate splits lines into pages of at most rows lines and returns the
// index of each page's first line. A page never begins with a blank line,
// and an image that would straddle a break moves whole to the next page
// (unless it is taller than a page, which Layout prevents).
func Paginate(lines []Line, rows int) []int {
	rows = max(rows, 1)

	var pages []int

	for i := 0; i < len(lines); {
		for i < len(lines) && lines[i].blank() {
			i++
		}

		if i >= len(lines) {
			break
		}

		end := min(i+rows, len(lines))
		for j := i + 1; j < end; j++ {
			if lines[j].Break {
				end = j
				break
			}
		}
		if end < len(lines) {
			if im := lines[end].Image; im != nil && im.Row > 0 && end-im.Row > i {
				end -= im.Row
			}
		}

		pages = append(pages, i)
		i = end
	}

	if len(pages) == 0 {
		pages = []int{0}
	}

	return pages
}

func (l Line) blank() bool {
	if l.Image != nil || l.Rule {
		return false
	}

	for _, s := range l.Segs {
		if strings.TrimSpace(s.Text) != "" {
			return false
		}
	}

	return true
}
