package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/gdamore/tcell/v2"
)

// theme is a colour scheme. A colour left as ColorDefault falls back to the
// terminal's own: the default theme sets none, and uses dim and reverse video.
type theme struct {
	name   string
	fg, bg tcell.Color
	dim    tcell.Color // secondary text
	title  tcell.Color
	code   tcell.Color
	dir    tcell.Color // folders in lists
	selFg  tcell.Color // the selected row
	selBg  tcell.Color
	hitFg  tcell.Color // search matches
	hitBg  tcell.Color
}

var themes = []theme{
	{
		name: "default",
		code: tcell.PaletteColor(3),
	},
	{
		name:  "gruvbox-light",
		fg:    tcell.NewHexColor(0x3c3836),
		bg:    tcell.NewHexColor(0xfbf1c7),
		dim:   tcell.NewHexColor(0x7c6f64),
		title: tcell.NewHexColor(0xaf3a03),
		code:  tcell.NewHexColor(0xb57614),
		dir:   tcell.NewHexColor(0x076678),
		selFg: tcell.NewHexColor(0x282828),
		selBg: tcell.NewHexColor(0xd5c4a1),
		hitFg: tcell.NewHexColor(0x282828),
		hitBg: tcell.NewHexColor(0xfabd2f),
	},
}

var cur = themes[0]

// applyTheme makes t the scheme every style below is drawn from.
func applyTheme(t theme) {
	cur = t
	styleNormal = tcell.StyleDefault.Foreground(t.fg).Background(t.bg)
	styleDim = dim(styleNormal)
	styleTitle = styleNormal.Bold(true).Foreground(t.title)
	styleCode = styleNormal.Foreground(t.code)
}

func set(c tcell.Color) bool { return c != tcell.ColorDefault }

// dim is st as secondary text.
func dim(st tcell.Style) tcell.Style {
	if set(cur.dim) {
		return st.Foreground(cur.dim)
	}

	return st.Dim(true)
}

// selected is st as the selected row of a list.
func selected(st tcell.Style) tcell.Style {
	if set(cur.selBg) {
		return st.Foreground(cur.selFg).Background(cur.selBg)
	}

	return st.Reverse(true)
}

// hit is st as a search match.
func hit(st tcell.Style) tcell.Style {
	if set(cur.hitBg) {
		return st.Foreground(cur.hitFg).Background(cur.hitBg)
	}

	return st.Reverse(true)
}

// folder is st for a folder's name in a list.
func folder(st tcell.Style) tcell.Style {
	st = st.Bold(true)
	if set(cur.dir) {
		st = st.Foreground(cur.dir)
	}

	return st
}

func themeFile() string {
	dir := stateDir()
	if dir == "" {
		return ""
	}

	return filepath.Join(dir, "theme")
}

// loadTheme applies the theme named by $READER_THEME, or else the one last
// chosen.
func loadTheme() {
	name := os.Getenv("READER_THEME")
	if name == "" {
		if data, err := os.ReadFile(themeFile()); err == nil {
			name = strings.TrimSpace(string(data))
		}
	}

	for _, t := range themes {
		if t.name == name {
			applyTheme(t)
			return
		}
	}

	applyTheme(themes[0])
}

// nextTheme switches to the next theme, remembers it, and returns its name.
func nextTheme() string {
	i := 0

	for j, t := range themes {
		if t.name == cur.name {
			i = j
		}
	}

	t := themes[(i+1)%len(themes)]
	applyTheme(t)

	if f := themeFile(); f != "" {
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err == nil {
			_ = os.WriteFile(f, []byte(t.name+"\n"), 0o644)
		}
	}

	return t.name
}
