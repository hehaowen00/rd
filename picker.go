package main

import (
	"fmt"
	"unicode"

	"github.com/gdamore/tcell/v2"
	"github.com/mattn/go-runewidth"

	"rd/fuzzy"
)

// lineInput is a one-line text field: typing, Backspace, Ctrl-W and Ctrl-U.
type lineInput struct {
	text []rune
}

// key edits the text and reports whether the key was one it handles.
func (in *lineInput) key(ev *tcell.EventKey) bool {
	switch ev.Key() {
	case tcell.KeyRune:
		in.text = append(in.text, ev.Rune())
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if len(in.text) > 0 {
			in.text = in.text[:len(in.text)-1]
		}
	case tcell.KeyCtrlU:
		in.text = nil
	case tcell.KeyCtrlW:
		t := in.text
		for len(t) > 0 && unicode.IsSpace(t[len(t)-1]) {
			t = t[:len(t)-1]
		}

		for len(t) > 0 && !unicode.IsSpace(t[len(t)-1]) {
			t = t[:len(t)-1]
		}

		in.text = t
	default:
		return false
	}

	return true
}

// draw writes prefix and the text at (x, y) and puts the cursor after it.
func (in *lineInput) draw(s tcell.Screen, x, y, w int, prefix string) {
	n := put(s, x, y, w, prefix, styleNormal)
	text := string(in.text)

	// keep the end of a long query in view
	for runewidth.StringWidth(text) > w-n-1 && text != "" {
		_, size := firstRune(text)
		text = text[size:]
	}

	n += put(s, x+n, y, w-n, text, styleNormal)
	s.ShowCursor(x+n, y)
}

func firstRune(s string) (rune, int) {
	for i, r := range s {
		if i > 0 {
			return r, i
		}
	}

	return 0, len(s)
}

// pickItem is one candidate in a picker.
type pickItem struct {
	text   string // what is matched and shown
	detail string // shown dimmed at the right
	value  string // what accepting it means, such as a path
	dim    bool
	bold   bool
	folder bool // accepting it goes into the folder
	n      int  // an index, for lists of things that are not paths
}

// picker is a fuzzy finder over a list: the query is typed on the bottom line
// and the matches are listed above it, best first.
type picker struct {
	title string
	empty string
	items []pickItem
	in    lineInput
	shown []pickItem
	view  listView
	rows  int
	hints string // overrides the default key hints
	// vim pickers start in a normal mode where keys move the selection as
	// in a vim list; / starts typing the filter, and Esc stops.
	vim         bool
	typing      bool
	keys        vimKeys
	typingHints string
	// remove, if set, is what Ctrl-D does to the selected item. It returns
	// the list without it.
	remove func(pickItem) []pickItem
}

func newPicker(title, empty string, items []pickItem) *picker {
	p := &picker{
		title: title,
		empty: empty,
		items: items,
		rows:  1,
	}
	p.filter()

	return p
}

func (p *picker) filter() {
	p.shown = fuzzy.Filter(string(p.in.text), p.items, func(it pickItem) string { return it.text })
	p.view = listView{}
}

func (p *picker) selected() (pickItem, bool) {
	if p.view.sel < 0 || p.view.sel >= len(p.shown) {
		return pickItem{}, false
	}

	return p.shown[p.view.sel], true
}

// Key handles a key. done is true when the picker should close, and chosen
// is the accepted item, if any.
func (p *picker) Key(ev *tcell.EventKey) (chosen *pickItem, done bool) {
	n := len(p.shown)

	if p.vim && !p.typing {
		return p.normalKey(ev)
	}

	if p.vim && ev.Key() == tcell.KeyEscape {
		p.typing = false
		return nil, false
	}

	switch ev.Key() {
	case tcell.KeyEscape, tcell.KeyCtrlC:
		return nil, true
	case tcell.KeyEnter:
		if it, ok := p.selected(); ok {
			return &it, true
		}
	case tcell.KeyUp, tcell.KeyCtrlP, tcell.KeyCtrlK:
		p.view.move(-1, n)
	case tcell.KeyDown, tcell.KeyCtrlN, tcell.KeyCtrlJ:
		p.view.move(1, n)
	case tcell.KeyPgUp:
		p.view.move(-p.rows, n)
	case tcell.KeyPgDn:
		p.view.move(p.rows, n)
	case tcell.KeyCtrlD:
		if it, ok := p.selected(); ok && p.remove != nil {
			sel := p.view.sel
			p.items = p.remove(it)
			p.filter()
			p.view.sel = max(min(sel, len(p.shown)-1), 0)
		}
	default:
		if p.in.key(ev) {
			p.filter()
		}
	}

	return nil, false
}

// normalKey handles a key in a vim picker's normal mode.
func (p *picker) normalKey(ev *tcell.EventKey) (chosen *pickItem, done bool) {
	if m, count, counted, ok := p.keys.feed(ev); ok {
		p.view.apply(m, count, counted, max(p.rows, 1), len(p.shown))
		return nil, false
	}

	switch ev.Key() {
	case tcell.KeyEscape, tcell.KeyCtrlC:
		return nil, true
	case tcell.KeyEnter:
		if it, ok := p.selected(); ok {
			return &it, true
		}
	case tcell.KeyRune:
		switch ev.Rune() {
		case 'l':
			if it, ok := p.selected(); ok {
				return &it, true
			}
		case 'q':
			return nil, true
		case '/':
			p.typing = true
		}
	}

	return nil, false
}

// Click selects the row under the mouse, or accepts it if already selected.
func (p *picker) Click(y int) *pickItem {
	idx := p.view.top + y - 2
	if idx < 0 || idx >= len(p.shown) {
		return nil
	}

	if idx == p.view.sel {
		it := p.shown[idx]
		return &it
	}

	p.view.sel = idx

	return nil
}

func (p *picker) Wheel(d int) { p.view.move(d, len(p.shown)) }

func (p *picker) Draw(s tcell.Screen) {
	w, h := s.Size()
	p.DrawIn(s, 0, w, h)
}

// DrawIn draws the picker in w columns from x, over the top h rows.
func (p *picker) DrawIn(s tcell.Screen, x, w, h int) {
	margin := x + 2
	inner := max(w-4, 1)

	n := put(s, margin, 0, inner, p.title, styleTitle)
	count := fmt.Sprintf("%d/%d", len(p.shown), len(p.items))
	put(s, margin+n+2, 0, inner-n-2, count, styleDim)

	listTop := 2
	p.rows = max(h-listTop-2, 1)
	p.view.scroll(p.rows, len(p.shown))

	if len(p.shown) == 0 {
		msg := p.empty
		if len(p.in.text) > 0 {
			msg = strNoMatches
		}

		put(s, margin+1, listTop, inner-1, msg, styleDim)
	}

	query := string(p.in.text)

	for i := range p.rows {
		idx := p.view.top + i
		if idx >= len(p.shown) {
			break
		}

		it := p.shown[idx]
		y := listTop + i
		st := styleNormal

		if it.dim {
			st = dim(st)
		}

		if it.bold {
			st = folder(st)
		}

		if idx == p.view.sel {
			st = selected(st)
			fillRow(s, margin, y, inner, st)
		}

		rw := runewidth.StringWidth(it.detail)
		drawMatched(s, margin+1, y, inner-rw-3, it.text, fuzzy.Positions(query, it.text), st)
		dst := st
		if idx != p.view.sel {
			dst = dim(dst)
		}

		put(s, margin+inner-rw-1, y, rw, it.detail, dst)
	}

	hints := strPickerHints
	if p.remove != nil {
		hints = strPickerRemoveHints
	}

	if p.hints != "" {
		hints = p.hints
	}

	if p.typing && p.typingHints != "" {
		hints = p.typingHints
	}

	hw := runewidth.StringWidth(hints)

	// a narrow picker puts its hints above the query to leave it room
	if inner-hw-2 < 20 {
		put(s, margin, h-2, inner, truncate(hints, inner), styleDim)
		p.drawQuery(s, margin, h-1, inner)

		return
	}

	put(s, margin+inner-hw, h-1, hw, hints, styleDim)
	p.drawQuery(s, margin, h-1, max(inner-hw-2, 1))
}

// drawQuery shows the query being typed, or in normal mode the filter in
// force, if any, without a cursor.
func (p *picker) drawQuery(s tcell.Screen, x, y, w int) {
	if !p.vim {
		p.in.draw(s, x, y, w, "> ")
		return
	}

	if p.typing {
		p.in.draw(s, x, y, w, "/")
		return
	}

	if len(p.in.text) > 0 {
		put(s, x, y, w, truncate("/"+string(p.in.text), w), styleDim)
	}
}

// drawMatched writes text with the runes at hits picked out, clipped to w
// cells with an ellipsis.
func drawMatched(s tcell.Screen, x, y, w int, text string, hits []int, st tcell.Style) {
	hit := make(map[int]bool, len(hits))
	for _, i := range hits {
		hit[i] = true
	}

	fits := runewidth.StringWidth(text) <= w
	used := 0

	for i, r := range []rune(text) {
		rw := runewidth.RuneWidth(r)
		if rw == 0 {
			continue
		}

		if !fits && used+rw > w-1 {
			s.SetContent(x+used, y, '…', nil, st)
			return
		}

		cst := st
		if hit[i] {
			cst = cst.Bold(true).Underline(true)
		}

		s.SetContent(x+used, y, r, nil, cst)
		used += rw
	}
}
