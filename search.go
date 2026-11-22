package main

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/gdamore/tcell/v2"
	"github.com/mattn/go-runewidth"
)

// Searching a book. The query is matched against the laid-out text with
// runs of whitespace, line breaks included, read as one space, so a phrase
// that wraps onto the next line is still found. Case is ignored unless the
// query has a capital in it.

// cellPos is where a rune of the searchable text sits on the page: its line,
// and its column in cells, or -1 for the space standing in for a line break.
type cellPos struct{ line, col int }

// textIndex is a chapter's text as one string, with where each rune is drawn.
type textIndex struct {
	text []rune
	pos  []cellPos
}

type match struct{ start, end, line int }

func indexLines(lines []Line) textIndex {
	var ix textIndex

	space := func(p cellPos) {
		if n := len(ix.text); n > 0 && ix.text[n-1] != ' ' {
			ix.text = append(ix.text, ' ')
			ix.pos = append(ix.pos, p)
		}
	}

	for li, ln := range lines {
		col := 0

		for _, seg := range ln.Segs {
			for _, c := range seg.Text {
				w := runewidth.RuneWidth(c)
				if w == 0 {
					continue
				}

				if unicode.IsSpace(c) {
					space(cellPos{li, col})
				} else {
					ix.text = append(ix.text, c)
					ix.pos = append(ix.pos, cellPos{li, col})
				}

				col += w
			}
		}

		space(cellPos{li, -1})
	}

	return ix
}

// normalize trims a query and reads each run of whitespace in it as a space.
func normalize(s string) []rune {
	return []rune(strings.Join(strings.Fields(s), " "))
}

func foldRunes(rs []rune) []rune {
	out := make([]rune, len(rs))
	for i, c := range rs {
		out[i] = unicode.ToLower(c)
	}

	return out
}

func hasUpper(rs []rune) bool {
	for _, c := range rs {
		if unicode.IsUpper(c) {
			return true
		}
	}

	return false
}

// findAll is where q occurs in text, without overlaps.
func findAll(text, q []rune) []int {
	var out []int

	if len(q) == 0 {
		return nil
	}

	for i := 0; i+len(q) <= len(text); i++ {
		if text[i] == q[0] && runesEqual(text[i:i+len(q)], q) {
			out = append(out, i)
			i += len(q) - 1
		}
	}

	return out
}

func runesEqual(a, b []rune) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}

// search is the reader's query and what it found in the current chapter.
type search struct {
	query   []rune
	fold    bool
	dir     int // 1 forward, -1 back, for n
	ix      textIndex
	matches []match
	lit     bool // whether matches are highlighted
	// plain caches each chapter's text, for skipping chapters quickly.
	plain map[int][]rune

	prompt bool
	in     lineInput
}

func (sr *search) prep(rs []rune) []rune {
	if sr.fold {
		return foldRunes(rs)
	}

	return rs
}

// matchesIn indexes lines and finds the query in them.
func (sr *search) matchesIn(lines []Line) (textIndex, []match) {
	ix := indexLines(lines)
	if len(sr.query) == 0 {
		return ix, nil
	}

	var ms []match

	for _, at := range findAll(sr.prep(ix.text), sr.prep(sr.query)) {
		ms = append(ms, match{start: at, end: at + len(sr.query), line: ix.pos[at].line})
	}

	return ix, ms
}

// plainText is a chapter's text, whitespace normalised, for a quick check of
// whether it can hold a match before laying it out.
func (r *Reader) plainText(ch int) []rune {
	if t, ok := r.find.plain[ch]; ok {
		return t
	}

	var b strings.Builder

	for _, bl := range r.parse(ch) {
		b.WriteString(bl.Prefix)

		for _, sp := range bl.Spans {
			b.WriteString(sp.Text)
		}

		b.WriteByte(' ')
	}

	if r.find.plain == nil {
		r.find.plain = map[int][]rune{}
	}

	t := normalize(b.String())
	r.find.plain[ch] = t

	return t
}

// startSearch opens the prompt for a search forward through the book. N
// searches back.
func (r *Reader) startSearch() {
	r.find.prompt = true
	r.find.dir = 1
	r.find.in = lineInput{}
}

// promptKey handles a key while the search query is being typed.
func (r *Reader) promptKey(ev *tcell.EventKey) {
	switch ev.Key() {
	case tcell.KeyEscape, tcell.KeyCtrlC:
		r.find.prompt = false
	case tcell.KeyEnter:
		r.find.prompt = false

		if q := normalize(string(r.find.in.text)); len(q) > 0 {
			r.find.query = q
			r.find.fold = !hasUpper(q)
			r.find.ix, r.find.matches = r.find.matchesIn(r.lines)
		}

		r.searchNext(r.find.dir, true)
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if len(r.find.in.text) == 0 {
			r.find.prompt = false
			return
		}

		r.find.in.key(ev)
	default:
		r.find.in.key(ev)
	}
}

// searchNext goes to the next page with a match in direction dir, moving on
// through the book and wrapping at its ends. fresh counts matches on the
// pages showing now, for a new search.
func (r *Reader) searchNext(dir int, fresh bool) {
	sr := &r.find
	if len(sr.query) == 0 {
		r.msg = strNoSearch
		return
	}

	if r.lines == nil {
		return
	}

	sr.lit = true
	lo := r.pages[r.pg]
	hi := len(r.lines)

	if p := r.pg + r.columns; p < len(r.pages) {
		hi = r.pages[p]
	}

	if dir > 0 && fresh {
		hi = lo
	}

	if i := pickMatch(sr.matches, dir, lo, hi); i >= 0 {
		r.goMatch(i)
		return
	}

	q := sr.prep(sr.query)
	n := len(r.book.Spine)

	for k := 1; k <= n; k++ {
		c := ((r.chapter+dir*k)%n + n) % n
		wrapped := dir > 0 && c <= r.chapter || dir < 0 && c >= r.chapter

		if c == r.chapter {
			if len(sr.matches) == 0 {
				break
			}
		} else {
			if len(findAll(sr.prep(r.plainText(c)), q)) == 0 {
				continue
			}

			blocks := r.parse(c)
			lines := Layout(blocks, r.laid.w, r.laid.rows, r.pics.sizer(r.laid.cellW, r.laid.cellH))

			ix, ms := sr.matchesIn(lines)
			if len(ms) == 0 {
				continue
			}

			r.load(c)
			r.blocks = blocks
			r.lines = lines
			r.pages = Paginate(lines, r.laid.rows)
			sr.ix, sr.matches = ix, ms
		}

		i := 0
		if dir < 0 {
			i = len(sr.matches) - 1
		}

		r.goMatch(i)

		if wrapped {
			r.msg = strSearchWrapped
		}

		return
	}

	r.msg = fmt.Sprintf(strNotFound, string(sr.query))
}

// pickMatch is, going forward, the first match at or after line hi; going
// back, the last match before line lo; or -1.
func pickMatch(ms []match, dir, lo, hi int) int {
	if dir > 0 {
		for i, m := range ms {
			if m.line >= hi {
				return i
			}
		}

		return -1
	}

	for i := len(ms) - 1; i >= 0; i-- {
		if ms[i].line < lo {
			return i
		}
	}

	return -1
}

func (r *Reader) goMatch(i int) {
	r.setPage(r.pageAt(r.find.matches[i].line))
	r.msg = fmt.Sprintf(strMatchOf, string(r.find.query), i+1, len(r.find.matches))
}

// highlight picks out the matches among lines [first, end) drawn from x, y.
func (r *Reader) highlight(s tcell.Screen, x, y, first, end int) {
	if !r.find.lit {
		return
	}

	for _, m := range r.find.matches {
		if m.line >= end || r.find.ix.pos[m.end-1].line < first {
			continue
		}

		for k := m.start; k < m.end; k++ {
			p := r.find.ix.pos[k]
			if p.col < 0 || p.line < first || p.line >= end {
				continue
			}

			cx, cy := x+p.col, y+p.line-first
			c, comb, st, _ := s.GetContent(cx, cy)
			s.SetContent(cx, cy, c, comb, hit(st))
		}
	}
}
