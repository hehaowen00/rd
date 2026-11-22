package main

import (
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/mattn/go-runewidth"
)

const (
	defaultTextWidth = 80
	minTextWidth     = 30
	widthStep        = 4
	// wheelGap is how long the wheel must rest between page turns, so one
	// trackpad flick turns one page rather than a dozen.
	wheelGap = 250 * time.Millisecond
	// spreadMinWidth is the narrowest a page may be in a two-page spread.
	spreadMinWidth = 50
	spreadGutter   = 6
)

// Reader shows one book, a chapter at a time.
type Reader struct {
	path string
	book *Book
	pics *pictures
	msg  string

	chapter int
	blocks  []Block
	lines   []Line
	// pages holds the first line of each page; pg is the page shown first.
	pages []int
	pg    int
	// pending is where to open once the chapter is laid out, as a fraction
	// of its lines (1 for the last page), or negative for nothing pending.
	pending float64

	width int
	laid  struct{ w, rows, cellW, cellH int }
	rows  int
	// columns is how many pages are on screen: 1, or 2 for a spread.
	columns int
	single  bool

	lastWheel time.Time

	find search

	// panel is the side panel, when one is open: the table of contents or
	// the bookmarks, as panelKind says.
	panel     *picker
	panelKind panelKind
	panelW    int
	keys      vimKeys

	// help is the key reference, when it is open.
	help *helpView

	// sel is the text selected with the mouse, and shown where each page
	// on screen was drawn, to find what the mouse is over.
	sel   selection
	shown []shownPage

	// bookmarks is the saved store, and marks this book's bookmarks in it.
	bookmarks *Bookmarks
	marks     []Bookmark
}

type panelKind int

const (
	panelContents panelKind = iota
	panelBookmarks
)

func OpenReader(path string, graphics bool, pos Position, resume bool, bookmarks *Bookmarks) (*Reader, error) {
	book, err := OpenBook(path)
	if err != nil {
		return nil, err
	}

	r := &Reader{
		path:    path,
		book:    book,
		pics:    newPictures(book, graphics),
		width:   defaultTextWidth,
		pending: -1,
		columns: 1,
	}

	if bookmarks != nil {
		r.bookmarks = bookmarks
		r.marks = bookmarks.Get(path)
	}

	// the keys, until the first one is pressed
	r.msg = strReaderHints
	if !graphics {
		r.msg = strNoGraphics
	}

	ch := 0
	if resume && pos.Chapter < len(book.Spine) {
		ch = pos.Chapter
	}

	r.load(ch)

	if resume {
		r.pending = pos.Fraction
	}

	return r, nil
}

func (r *Reader) Close() string {
	out := r.pics.release()
	r.book.Close()

	return out
}

func (r *Reader) title() string {
	if r.book.Title != "" {
		return r.book.Title
	}

	if r.path != "" {
		return filepath.Base(r.path)
	}

	return strUntitled
}

func (r *Reader) load(ch int) {
	r.chapter = ch
	r.pg = 0
	r.lines = nil
	r.pages = nil
	r.pending = -1
	r.find.matches = nil
	r.find.ix = textIndex{}
	r.clearSelection()
	r.blocks = r.parse(ch)
}

// parse reads chapter ch, or a block saying why it could not be read.
func (r *Reader) parse(ch int) []Block {
	if r.book.Pages != nil {
		return r.book.comicBlocks(ch)
	}

	name := r.book.Spine[ch]

	data, err := r.book.ReadFile(name)
	if err == nil {
		var blocks []Block
		if blocks, err = ParseChapter(name, data); err == nil {
			return blocks
		}
	}

	return []Block{
		{
			Spans: []Span{
				{
					Text:  err.Error(),
					Style: Dim,
				},
			},
		},
	}
}

func (r *Reader) fraction() float64 {
	if len(r.lines) == 0 {
		return 0
	}

	return float64(r.pages[r.pg]) / float64(len(r.lines))
}

// lastPage is the furthest pg can go: the last page, or in a spread the
// left page of the last pair.
func (r *Reader) lastPage() int {
	n := len(r.pages) - 1
	return n - n%r.columns
}

// setPage moves to page p, kept in range and on a spread's left page.
func (r *Reader) setPage(p int) {
	p = max(min(p, len(r.pages)-1), 0)
	p -= p % r.columns

	if p != r.pg {
		r.clearSelection()
	}

	r.pg = p
}

// pageAt is the page holding line i.
func (r *Reader) pageAt(i int) int {
	return sort.Search(len(r.pages), func(p int) bool { return r.pages[p] > i }) - 1
}

func (r *Reader) layout(w, rows, cellW, cellH int) {
	if r.lines != nil && r.laid.w == w && r.laid.rows == rows && r.laid.cellW == cellW && r.laid.cellH == cellH {
		return
	}

	frac := r.pending
	if frac < 0 && r.lines != nil {
		frac = r.fraction()
	}

	r.lines = Layout(r.blocks, w, rows, r.pics.sizer(cellW, cellH))
	r.pages = Paginate(r.lines, rows)
	r.find.ix, r.find.matches = r.find.matchesIn(r.lines)
	r.laid.w, r.laid.rows, r.laid.cellW, r.laid.cellH = w, rows, cellW, cellH
	r.rows = rows
	r.pending = -1
	r.clearSelection()

	switch {
	case frac >= 1:
		r.pg = len(r.pages) - 1
	case frac >= 0:
		r.pg = r.pageAt(int(math.Round(frac * float64(len(r.lines)))))
	}

	r.setPage(r.pg)
}

// Position is where the reader is, for saving.
func (r *Reader) Position() Position {
	n := len(r.book.Spine)
	through := 1.0

	if last := r.lastPage(); last > 0 {
		through = float64(r.pg) / float64(last)
	}

	return Position{
		Chapter:  r.chapter,
		Fraction: r.fraction(),
		Progress: (float64(r.chapter) + through) / float64(n),
	}
}

// chapterTitle is the contents entry for the current chapter, if any.
func (r *Reader) chapterTitle() string { return r.chapterTitleAt(r.chapter) }

// Draw paints the reader and returns the images to place this frame.
func (r *Reader) Draw(s tcell.Screen, cellW, cellH int) []placement {
	if r.help != nil {
		r.help.Draw(s)
		return nil
	}

	w, h := s.Size()
	margin := 2
	inner := max(w-2*margin, 1)

	put(s, margin, 0, inner, truncate(r.title(), inner), styleDim)

	textTop := 2
	rows := max(h-textTop-2, 1)
	// a comic's pages take all the room there is; text keeps to its width
	width := r.width
	if r.book.Pages != nil {
		width = inner
	}

	textW := max(min(width, inner), 1)
	r.columns = 1

	if half := (inner - spreadGutter) / 2; !r.single && half >= spreadMinWidth {
		r.columns = 2
		textW = min(width, half)
	}

	span := r.columns*textW + (r.columns-1)*spreadGutter
	left := (w - span) / 2

	r.layout(textW, rows, cellW, cellH)
	r.setPage(r.pg) // the column count may have changed

	var ps []placement

	r.shown = r.shown[:0]

	for c := range r.columns {
		p := r.pg + c
		if p >= len(r.pages) {
			break
		}

		end := len(r.lines)
		if p+1 < len(r.pages) {
			end = r.pages[p+1]
		}

		first := r.pages[p]
		x := left + c*(textW+spreadGutter)
		last := min(end, first+rows)
		ps = append(ps, r.drawPage(s, x, textTop, textW, r.lines[first:last])...)
		r.highlight(s, x, textTop, first, last)
		r.shown = append(r.shown, shownPage{x: x, y: textTop, w: textW, first: first, last: last})
	}

	r.drawSelection(s)

	pos := r.Position()
	shown := fmt.Sprint(r.pg + 1)
	if last := min(r.pg+r.columns, len(r.pages)); last > r.pg+1 {
		shown = fmt.Sprintf("%d-%d", r.pg+1, last)
	}

	mark := ""
	if r.bookmarkHere() >= 0 {
		mark = strBookmarked + "   "
	}

	right := fmt.Sprintf("%sp %s/%d   ch %d/%d   %3.0f%%", mark, shown, len(r.pages), r.chapter+1, len(r.book.Spine), pos.Progress*100)
	leftText := r.chapterTitle()

	if r.msg != "" {
		leftText = r.msg
	}

	rw := runewidth.StringWidth(right)
	put(s, margin+inner-rw, h-1, rw, right, styleDim)

	if r.find.prompt {
		r.find.in.draw(s, margin, h-1, inner-rw-2, "/")
	} else {
		put(s, margin, h-1, inner-rw-2, truncate(leftText, inner-rw-2), styleDim)
	}

	if r.panel != nil {
		ps = r.drawContents(s, w, h, ps)
	}
	return ps
}

// drawPage paints one page's lines into a column.
func (r *Reader) drawPage(s tcell.Screen, left, textTop, textW int, lines []Line) []placement {
	var ps []placement

	rows := len(lines)

	for i, ln := range lines {
		y := textTop + i

		switch {
		case ln.Image != nil:
			im := ln.Image
			if im.Row == 0 || i == 0 {
				ps = append(ps, placement{
					path:     im.Path,
					row:      y,
					col:      left + (textW-im.Cols)/2,
					cols:     im.Cols,
					rows:     min(im.Rows-im.Row, rows-i),
					firstRow: im.Row,
					ofRows:   im.Rows,
				})
			}
		case ln.Rule:
			rw := textW / 3
			for x := range rw {
				s.SetContent(left+(textW-rw)/2+x, y, '─', nil, styleDim)
			}
		default:
			x := left
			for _, seg := range ln.Segs {
				x += put(s, x, y, left+textW-x, seg.Text, spanStyle(seg.Style))
			}
		}
	}

	return ps
}

func (r *Reader) nextPage() {
	if r.pg < r.lastPage() {
		r.setPage(r.pg + r.columns)
		return
	}

	if r.chapter+1 < len(r.book.Spine) {
		r.load(r.chapter + 1)
	}
}

func (r *Reader) prevPage() {
	if r.pg > 0 {
		r.setPage(r.pg - r.columns)
		return
	}

	if r.chapter > 0 {
		r.load(r.chapter - 1)
		r.pending = 1
	}
}

func (r *Reader) chapterBy(d int) {
	ch := r.chapter + d
	if ch >= 0 && ch < len(r.book.Spine) {
		r.load(ch)
	}
}

// Key handles a key and reports whether the reader should close.
func (r *Reader) Key(ev *tcell.EventKey) (closeBook bool) {
	r.msg = ""

	if r.help != nil {
		if r.help.Key(ev) {
			r.help = nil
		}

		return false
	}

	if r.panel != nil {
		r.panelKey(ev)
		return false
	}

	if r.find.prompt {
		r.promptKey(ev)
		return false
	}

	if m, count, counted, ok := r.keys.feed(ev); ok {
		r.apply(m, count, counted)
		return false
	}

	switch ev.Key() {
	case tcell.KeyRight:
		r.nextPage()
	case tcell.KeyLeft, tcell.KeyBackspace, tcell.KeyBackspace2:
		r.prevPage()
	case tcell.KeyTab:
		r.openContents()
	case tcell.KeyEscape:
		if r.sel.on {
			r.clearSelection()
			return false
		}

		if r.find.lit {
			r.find.lit = false
			return false
		}

		return true
	case tcell.KeyRune:
		switch ev.Rune() {
		case ' ', 'f', 'l':
			r.nextPage()
		case 'b', 'h':
			r.prevPage()
		case '/':
			r.startSearch()
		case '?':
			r.help = newHelp(strHelp, readerHelp)
		case 'n':
			r.searchNext(r.find.dir, false)
		case 'N':
			r.searchNext(-r.find.dir, false)
		case ']':
			r.chapterBy(1)
		case '[':
			r.chapterBy(-1)
		case 't':
			r.openContents()
		case 'm':
			r.toggleBookmark()
		case '\'':
			r.openBookmarks()
		case '+', '=':
			r.width += widthStep
		case '-':
			r.width = max(r.width-widthStep, minTextWidth)
		case 'v':
			r.single = !r.single
		case 'c':
			r.msg = nextTheme()
		case 'q':
			return true
		}
	}

	return false
}

// apply runs a motion in the text. The text is paged, so every downward or
// upward motion turns a page. gg and G with a count go to that chapter.
func (r *Reader) apply(m motion, count int, counted bool) {
	switch m {
	case motionLineDown, motionHalfDown, motionPageDown:
		for range count {
			r.nextPage()
		}
	case motionLineUp, motionHalfUp, motionPageUp:
		for range count {
			r.prevPage()
		}
	case motionTop, motionBottom:
		if counted {
			if ch := count - 1; ch < len(r.book.Spine) {
				r.load(ch)
			}

			return
		}

		if m == motionTop {
			r.setPage(0)
		} else {
			r.setPage(len(r.pages) - 1)
		}
	}
}

func (r *Reader) Wheel(d int) {
	if r.help != nil {
		r.help.Wheel(d)
		return
	}

	if r.panel != nil {
		r.panel.Wheel(d)
		return
	}

	now := time.Now()
	turn := now.Sub(r.lastWheel) >= wheelGap
	r.lastWheel = now

	if !turn {
		return
	}

	if d > 0 {
		r.nextPage()
	} else {
		r.prevPage()
	}
}
