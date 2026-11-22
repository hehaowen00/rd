package main

import (
	"github.com/gdamore/tcell/v2"
	"github.com/mattn/go-runewidth"
)

// The help screen lists the keys, a section to a heading. It takes the whole
// screen, so no images need hiding behind it, and scrolls like a vim list;
// ?, Esc or q closes it.

type helpKey struct{ keys, what string }

type helpSection struct {
	title string
	keys  []helpKey
}

var readerHelp = []helpSection{
	{strHelpTurning, []helpKey{
		{"Space  f  l  →", "Next page"},
		{"b  h  ←  Backspace", "Previous page"},
		{"j k  Ctrl-D Ctrl-U  PgDn PgUp", "Next or previous page; a count turns that many"},
		{"gg  G", "First or last page of the chapter"},
		{"]  [", "Next or previous chapter"},
		{"{n}gg  {n}G", "Go to chapter n"},
		{"Mouse wheel", "Turn the page"},
	}},
	{strHelpMouse, []helpKey{
		{"Drag", "Select text and copy it"},
		{"Esc", "Clear the selection"},
	}},
	{strHelpFinding, []helpKey{
		{"/", "Search the book"},
		{"n  N", "Next or previous match"},
		{"Esc", "Clear the highlighted matches"},
		{"t  Tab", "Table of contents"},
	}},
	{strBookmarks, []helpKey{
		{"m", "Bookmark this page, or remove its bookmark"},
		{"'", "Show the bookmarks"},
	}},
	{strHelpView, []helpKey{
		{"+  -", "Wider or narrower text"},
		{"v", "Two pages side by side, or one"},
		{"c", "Next theme"},
	}},
	{strHelpPanels, []helpKey{
		{"j k  gg G  Ctrl-D Ctrl-U", "Move"},
		{"/", "Filter the list; Esc stops typing"},
		{"Enter  l  click", "Go there"},
		{"x", "Remove a bookmark"},
		{"Esc  q", "Close"},
	}},
	{strHelpLeaving, []helpKey{
		{"?", "This help"},
		{"q  Esc", "Back to the library"},
		{"Ctrl-C", "Quit"},
	}},
}

var libraryHelp = []helpSection{
	{strHelpMoving, []helpKey{
		{"j k  ↓ ↑", "Move"},
		{"Ctrl-D Ctrl-U  PgDn PgUp", "Move by half or a whole screen"},
		{"gg  G  {n}G", "First, last or nth entry"},
		{"Enter  l  →", "Open the book or folder"},
		{"h  -  ←  Backspace", "Up a folder"},
		{"~", "Home folder"},
	}},
	{strHelpFinding, []helpKey{
		{"/", "Find books anywhere under this folder"},
		{"p", "Saved places"},
		{"a", "Save this folder as a place"},
		{"Ctrl-D", "Remove a place, in the places list"},
	}},
	{strHelpView, []helpKey{
		{".", "Show or hide hidden files"},
		{"c", "Next theme"},
	}},
	{strHelpLeaving, []helpKey{
		{"?", "This help"},
		{"q  Esc", "Quit"},
	}},
}

type helpView struct {
	title    string
	sections []helpSection
	top      int
	rows     int
	keys     vimKeys
}

func newHelp(title string, sections []helpSection) *helpView {
	return &helpView{title: title, sections: sections, rows: 1}
}

type helpLine struct {
	heading string
	key     helpKey
}

func (h *helpView) lines() []helpLine {
	var out []helpLine

	for i, sec := range h.sections {
		if i > 0 {
			out = append(out, helpLine{})
		}

		out = append(out, helpLine{heading: sec.title})

		for _, k := range sec.keys {
			out = append(out, helpLine{key: k})
		}
	}

	return out
}

func (h *helpView) keyWidth() int {
	w := 0

	for _, sec := range h.sections {
		for _, k := range sec.keys {
			w = max(w, runewidth.StringWidth(k.keys))
		}
	}

	return w
}

func (h *helpView) Draw(s tcell.Screen) {
	w, sh := s.Size()
	margin := 2
	inner := max(w-2*margin, 1)

	put(s, margin, 0, inner, h.title, styleTitle)

	lines := h.lines()
	listTop := 2
	h.rows = max(sh-listTop-2, 1)
	h.top = max(min(h.top, len(lines)-h.rows), 0)

	kw := h.keyWidth()
	// keep the two columns together, in the middle of a wide screen
	span := min(inner, kw+4+60)
	left := margin + (inner-span)/2

	for i := range h.rows {
		idx := h.top + i
		if idx >= len(lines) {
			break
		}

		ln, y := lines[idx], listTop+i

		if ln.heading != "" {
			put(s, left, y, span, ln.heading, styleTitle)
			continue
		}

		if ln.key.keys == "" {
			continue
		}

		if span-kw-4 < 20 {
			// too narrow for two columns: just the keys, then the meaning
			n := put(s, left+2, y, span-2, ln.key.keys, styleNormal)
			put(s, left+n+4, y, span-n-4, truncate(ln.key.what, span-n-4), styleDim)

			continue
		}

		put(s, left+2, y, kw, ln.key.keys, styleNormal)
		put(s, left+kw+4, y, span-kw-4, truncate(ln.key.what, span-kw-4), styleDim)
	}

	hints := strHelpHints
	if len(lines) > h.rows {
		hints = strHelpScrollHints
	}

	put(s, margin, sh-1, inner, truncate(hints, inner), styleDim)
}

// Key scrolls the help, and reports whether it should close.
func (h *helpView) Key(ev *tcell.EventKey) (done bool) {
	n := len(h.lines())

	if m, count, counted, ok := h.keys.feed(ev); ok {
		v := listView{sel: h.top, top: h.top}
		// scroll the view itself: the "selection" is its top row
		v.apply(m, count, counted, h.rows, max(n-h.rows+1, 1))
		h.top = v.sel

		return false
	}

	switch ev.Key() {
	case tcell.KeyEscape, tcell.KeyEnter:
		return true
	case tcell.KeyRune:
		switch ev.Rune() {
		case '?', 'q':
			return true
		}
	}

	return false
}

func (h *helpView) Wheel(d int) {
	h.top = max(min(h.top+d, len(h.lines())-h.rows), 0)
}
