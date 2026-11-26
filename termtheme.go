package main

import (
	"math"

	"github.com/gdamore/tcell/v2"
)

// useTerminalColours rebuilds the default theme out of what the terminal
// reported, so it keeps the terminal's look while the secondary text, code,
// selection and search matches read on a light background as well as a dark
// one. A terminal that did not give its background leaves the theme as it is.
func useTerminalColours(got termColours) {
	bg, ok := got[slotBg]
	if !ok {
		return
	}

	at := func(n int, fallback tcell.Color) tcell.Color {
		if c, ok := got[n]; ok {
			return c
		}

		return fallback
	}

	fg := at(slotFg, away(bg))
	sel := ground(mix(bg, at(4, fg), 0.30), bg, 1.6)
	hl := ground(mix(bg, at(3, fg), 0.45), bg, 1.6)

	themes[0] = theme{
		name:  themes[0].name,
		fg:    fg,
		bg:    bg,
		dim:   readable(at(8, mix(bg, fg, 0.55)), bg, 3),
		code:  readable(at(3, fg), bg, 3),
		selFg: readable(fg, sel, 4.5),
		selBg: sel,
		hitFg: readable(fg, hl, 4.5),
		hitBg: hl,
	}
}

// WCAG 2.1 relative luminance.
func luminance(c tcell.Color) float64 {
	r, g, b := c.RGB()

	ch := func(v int32) float64 {
		s := float64(v) / 255
		if s <= 0.03928 {
			return s / 12.92
		}

		return math.Pow((s+0.055)/1.055, 2.4)
	}

	return 0.2126*ch(r) + 0.7152*ch(g) + 0.0722*ch(b)
}

func contrast(a, b tcell.Color) float64 {
	la := luminance(a) + 0.05
	lb := luminance(b) + 0.05

	return max(la, lb) / min(la, lb)
}

func mix(a, b tcell.Color, t float64) tcell.Color {
	ar, ag, ab := a.RGB()
	br, bg, bb := b.RGB()
	f := func(x, y int32) int32 {
		return int32(float64(x) + (float64(y)-float64(x))*t)
	}

	return tcell.NewRGBColor(f(ar, br), f(ag, bg), f(ab, bb))
}

// away is the end a colour moves towards to stand off bg: white on a dark
// scheme, black on a light one.
func away(bg tcell.Color) tcell.Color {
	if luminance(bg) > 0.4 {
		return tcell.NewRGBColor(0, 0, 0)
	}

	return tcell.NewRGBColor(255, 255, 255)
}

// walk moves c towards the far end from bg until the two clear the ratio.
func walk(c, bg tcell.Color, want float64) (tcell.Color, bool) {
	target := away(bg)

	for i := 0; i <= 50; i++ {
		m := mix(c, target, float64(i)/50)
		if contrast(m, bg) >= want {
			return m, true
		}
	}

	return target, false
}

// readable is a text colour moved off its ground until it can be read.
func readable(c, bg tcell.Color, want float64) tcell.Color {
	m, _ := walk(c, bg, want)

	return m
}

// ground is a background moved off the page's until the two can be told
// apart.
func ground(c, bg tcell.Color, want float64) tcell.Color {
	m, ok := walk(c, bg, want)
	if !ok {
		return c
	}

	return m
}
