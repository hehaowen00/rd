package main

import (
	"encoding/base64"
	"strings"
	"unicode/utf8"

	"github.com/gdamore/tcell/v2"
	"github.com/mattn/go-runewidth"
)

// Text is selected by dragging over a page. The app takes the mouse, so the
// terminal cannot select for it; the reader draws the selection itself and,
// when the button is let go, copies the text with OSC 52. A selection stays
// within the page it was started on.

// selPoint is a cell in the chapter's lines: a line, and a cell along it.
type selPoint struct{ line, cell int }

func (p selPoint) before(q selPoint) bool {
	return p.line < q.line || p.line == q.line && p.cell < q.cell
}

type selection struct {
	on       bool // there is a selection to draw
	dragging bool
	page     int // which of the pages on screen it is in
	anchor   selPoint
	head     selPoint
}

// shownPage is where a page was drawn, for finding what the mouse is over.
type shownPage struct {
	x, y, w     int
	first, last int // the lines shown, last exclusive
}

func (r *Reader) clearSelection() { r.sel = selection{} }

// pointAt is the cell under (x, y) in shown page i, kept within the page.
func (r *Reader) pointAt(i, x, y int) selPoint {
	pg := r.shown[i]

	switch {
	case pg.last <= pg.first:
		return selPoint{pg.first, 0}
	case y < pg.y:
		return selPoint{pg.first, 0}
	case y >= pg.y+pg.last-pg.first:
		return selPoint{pg.last - 1, pg.w}
	}

	return selPoint{pg.first + y - pg.y, max(min(x-pg.x, pg.w), 0)}
}

// selectStart begins a selection at (x, y), if that is on a page.
func (r *Reader) selectStart(x, y int) {
	r.clearSelection()

	for i, pg := range r.shown {
		if x >= pg.x && x < pg.x+pg.w && y >= pg.y && y < pg.y+pg.last-pg.first {
			p := r.pointAt(i, x, y)
			r.sel = selection{dragging: true, page: i, anchor: p, head: p}

			return
		}
	}
}

func (r *Reader) selectDrag(x, y int) {
	if !r.sel.dragging || r.sel.page >= len(r.shown) {
		return
	}

	r.sel.head = r.pointAt(r.sel.page, x, y)
	r.sel.on = r.sel.head != r.sel.anchor
}

// selectEnd finishes a drag and returns the text selected, if any.
func (r *Reader) selectEnd(x, y int) string {
	if !r.sel.dragging {
		return ""
	}

	r.selectDrag(x, y)
	r.sel.dragging = false

	if !r.sel.on {
		r.clearSelection()
		return ""
	}

	text := r.selectedText()
	if text == "" {
		r.clearSelection()
	}

	return text
}

// span is the selection in order, the end cell included.
func (r *Reader) span() (from, to selPoint) {
	from, to = r.sel.anchor, r.sel.head
	if to.before(from) {
		from, to = to, from
	}

	return from, to
}

// cells is the part of line i the selection covers, as cells [a, b).
func (r *Reader) cells(i int, from, to selPoint) (a, b int) {
	a, b = 0, lineWidth(r.lines[i])
	if i == from.line {
		a = from.cell
	}

	if i == to.line {
		b = min(b, to.cell+1)
	}

	return a, max(a, b)
}

func lineText(ln Line) string {
	var b strings.Builder
	for _, seg := range ln.Segs {
		b.WriteString(seg.Text)
	}

	return b.String()
}

func lineWidth(ln Line) int { return runewidth.StringWidth(lineText(ln)) }

// cellSlice is the runes of s that start in cells [a, b).
func cellSlice(s string, a, b int) string {
	var out strings.Builder

	at := 0
	for _, c := range s {
		if at >= a && at < b {
			out.WriteRune(c)
		}

		at += runewidth.RuneWidth(c)
	}

	return out.String()
}

// selectedText is the text selected, with a paragraph's lines joined back
// together and a line break where each paragraph ends.
func (r *Reader) selectedText() string {
	from, to := r.span()

	var b strings.Builder

	sep := ""

	for i := from.line; i <= to.line && i < len(r.lines); i++ {
		ln := r.lines[i]
		if ln.Image != nil || ln.Rule {
			continue
		}

		a, e := r.cells(i, from, to)
		text := cellSlice(lineText(ln), a, e)

		if !ln.Pre {
			text = strings.TrimSpace(text)
		}

		if text == "" && len(ln.Segs) == 0 {
			// the gap between paragraphs
			if b.Len() > 0 {
				sep = "\n\n"
			}

			continue
		}

		if text != "" {
			if b.Len() > 0 {
				b.WriteString(sep)
			}

			b.WriteString(text)
		}

		sep = " "
		if ln.End || ln.Pre {
			sep = "\n"
		}
	}

	return b.String()
}

// drawSelection shows the selected cells in reverse.
func (r *Reader) drawSelection(s tcell.Screen) {
	if !r.sel.on || r.sel.page >= len(r.shown) {
		return
	}

	pg := r.shown[r.sel.page]
	from, to := r.span()

	for i := max(from.line, pg.first); i <= to.line && i < pg.last; i++ {
		a, b := r.cells(i, from, to)
		y := pg.y + i - pg.first

		for x := pg.x + a; x < pg.x+min(b, pg.w); x++ {
			c, comb, st, _ := s.GetContent(x, y)
			s.SetContent(x, y, c, comb, st.Reverse(true))
		}
	}
}

// osc52 asks the terminal to put text on the clipboard.
func osc52(text string) string {
	return "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(text)) + "\x1b\\"
}

func runeCount(s string) int { return utf8.RuneCountInString(s) }
