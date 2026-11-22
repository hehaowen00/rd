package main

import (
	"strings"

	"github.com/gdamore/tcell/v2"
)

// The table of contents is a panel over the left of the page, so the page
// stays in view behind it. It moves like a vim list (j k, Ctrl-D Ctrl-U, gg
// G); / filters it, as in the finder.

func (r *Reader) openContents() {
	var items []pickItem

	for i, e := range r.book.TOC {
		items = append(items, pickItem{
			text: strings.Repeat("  ", e.Depth) + e.Title,
			n:    i,
			dim:  e.Spine < 0,
		})
	}

	p := newPicker(strContents, strNoContents, items)
	p.hints = strContentsHints
	p.typingHints = strContentsTypingHints
	p.vim = true

	if i := r.currentEntry(); i >= 0 {
		p.items[i].detail = strHere
		p.filter()
		p.view.sel = i
		p.view.top = max(i-5, 0)
	}

	r.panel = p
	r.panelKind = panelContents
}

// currentEntry is the contents entry for the current chapter, or -1.
func (r *Reader) currentEntry() int {
	best := -1

	for i, e := range r.book.TOC {
		if e.Spine >= 0 && e.Spine <= r.chapter && (best < 0 || e.Spine > r.book.TOC[best].Spine) {
			best = i
		}
	}

	return best
}

// panelKey works the open panel. The key that opened it closes it.
func (r *Reader) panelKey(ev *tcell.EventKey) {
	toggle := 't'
	if r.panelKind == panelBookmarks {
		toggle = '\''
	}

	if ev.Key() == tcell.KeyTab || !r.panel.typing && ev.Key() == tcell.KeyRune && ev.Rune() == toggle {
		r.panel = nil
		return
	}

	it, done := r.panel.Key(ev)
	if done {
		r.panel = nil
	}

	if it != nil {
		r.choose(it.n)
	}
}

// choose goes to entry n of the open panel.
func (r *Reader) choose(n int) {
	if r.panelKind == panelBookmarks {
		r.goToBookmark(n)
	} else {
		r.goToEntry(n)
	}
}

func (r *Reader) goToEntry(i int) {
	if e := r.book.TOC[i]; e.Spine >= 0 {
		r.load(e.Spine)
	}
}

// drawContents paints the side panel and drops the images it would cover.
func (r *Reader) drawContents(s tcell.Screen, w, h int, ps []placement) []placement {
	pw := w
	if w >= 70 {
		pw = min(max(w*2/5, 36), 64)
	}

	r.panelW = pw

	for y := range h {
		fillRow(s, 0, y, pw, styleNormal)

		if pw < w {
			s.SetContent(pw-1, y, '│', nil, styleDim)
		}
	}

	r.panel.DrawIn(s, 0, pw-1, h)

	kept := ps[:0]

	for _, p := range ps {
		if p.col >= pw {
			kept = append(kept, p)
		}
	}

	return kept
}

// Click works the side panel, and a click beside it closes it. With no
// panel open, it starts selecting text.
func (r *Reader) Click(x, y int) {
	if r.help != nil {
		return
	}

	if r.panel == nil {
		r.selectStart(x, y)
		return
	}

	if x >= r.panelW {
		r.panel = nil
		return
	}

	if it := r.panel.Click(y); it != nil {
		r.panel = nil
		r.choose(it.n)
	}
}
