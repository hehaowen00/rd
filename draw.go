package main

import (
	"github.com/gdamore/tcell/v2"
	"github.com/mattn/go-runewidth"
)

// The styles everything is drawn with, set from the theme by applyTheme.
var (
	styleNormal = tcell.StyleDefault
	styleDim    = tcell.StyleDefault.Dim(true)
	styleTitle  = tcell.StyleDefault.Bold(true)
	styleCode   = tcell.StyleDefault.Foreground(tcell.PaletteColor(3))
)

// put writes str at (x, y), clipped to maxW cells, and returns the cells used.
func put(s tcell.Screen, x, y, maxW int, str string, st tcell.Style) int {
	used := 0

	for _, r := range str {
		w := runewidth.RuneWidth(r)
		if w == 0 {
			continue
		}

		if used+w > maxW {
			break
		}

		s.SetContent(x+used, y, r, nil, st)
		used += w
	}

	return used
}

func fillRow(s tcell.Screen, x, y, w int, st tcell.Style) {
	for i := range w {
		s.SetContent(x+i, y, ' ', nil, st)
	}
}

// truncate shortens str to w cells, marking the cut with an ellipsis.
func truncate(str string, w int) string {
	if runewidth.StringWidth(str) <= w {
		return str
	}

	return runewidth.Truncate(str, w, "…")
}

func spanStyle(st Style) tcell.Style {
	out := styleNormal

	if st&Bold != 0 {
		out = out.Bold(true)
	}

	if st&Italic != 0 {
		out = out.Italic(true)
	}

	if st&Underline != 0 {
		out = out.Underline(true)
	}

	if st&Dim != 0 {
		out = dim(out)
	}

	if st&Code != 0 {
		out = out.Foreground(cur.code)
	}

	return out
}

// listView is a scrolling list with one selected row.
type listView struct {
	sel, top int
}

func (v *listView) move(delta, n int) {
	v.sel = max(min(v.sel+delta, n-1), 0)
}

// scroll keeps the selection within the visible rows.
func (v *listView) scroll(rows, n int) {
	if v.sel < v.top {
		v.top = v.sel
	}

	if v.sel >= v.top+rows {
		v.top = v.sel - rows + 1
	}

	v.top = max(min(v.top, n-rows), 0)
}
