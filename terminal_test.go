package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
)

// The answers a terminal gives are read back under the keys the theme looks
// them up by, alongside the kitty and DA1 replies.
func TestParseColours(t *testing.T) {
	var b strings.Builder
	b.WriteString("\x1b_Gi=31;OK\x1b\\")
	b.WriteString("\x1b]10;rgb:abab/b2b2/bfbf\x1b\\")
	b.WriteString("\x1b]11;rgb:2828/2c2c/3434\x07")
	fmt.Fprintf(&b, "\x1b]4;%d;rgb:%s\x1b\\", 3, "e5/c0/7b")
	fmt.Fprintf(&b, "\x1b]4;%d;rgb:%s\x1b\\", 10, "b5/e8/90")
	b.WriteString("\x1b[?62;c")

	got := parseColours([]byte(b.String()))

	want := map[int]tcell.Color{
		slotFg: tcell.NewRGBColor(0xab, 0xb2, 0xbf),
		slotBg: tcell.NewRGBColor(0x28, 0x2c, 0x34),
		3:      tcell.NewRGBColor(0xe5, 0xc0, 0x7b),
		10:     tcell.NewRGBColor(0xb5, 0xe8, 0x90), // not the foreground
	}
	for k, c := range want {
		if got[k] != c {
			t.Errorf("slot %d: %v, want %v", k, got[k], c)
		}
	}

	if !kittyReply.Match([]byte(b.String())) || !da1Reply.Match([]byte(b.String())) {
		t.Error("kitty and DA1 replies are read from the same answer")
	}
}

func TestHexChannelWidths(t *testing.T) {
	cs := map[string]int32{"f": 255, "ff": 255, "fff": 255, "ffff": 255, "0": 0, "8080": 128}
	for in, want := range cs {
		if got := hexChannel(in); got != want {
			t.Errorf("%q: %d, want %d", in, got, want)
		}
	}
}

// On a light terminal the default theme's colours are pulled dark enough to
// read; the terminal's yellow would not be.
func TestTerminalColoursLight(t *testing.T) {
	saved := themes[0]
	defer func() { themes[0] = saved }()

	bg := tcell.NewRGBColor(0xff, 0xff, 0xff)
	useTerminalColours(termColours{
		slotBg: bg,
		slotFg: tcell.NewRGBColor(0x20, 0x20, 0x20),
		3:      tcell.NewRGBColor(0xff, 0xff, 0x80),
	})

	th := themes[0]
	if th.name != "default" || th.bg != bg {
		t.Fatalf("theme %q bg %v", th.name, th.bg)
	}

	if contrast(th.code, bg) < 3 || contrast(th.dim, bg) < 3 {
		t.Errorf("code %.2f dim %.2f against the page", contrast(th.code, bg), contrast(th.dim, bg))
	}

	if contrast(th.selFg, th.selBg) < 4.5 || contrast(th.hitFg, th.hitBg) < 4.5 {
		t.Error("selection or match text unreadable on its ground")
	}

	if contrast(th.selBg, bg) < 1.6 || contrast(th.hitBg, bg) < 1.6 {
		t.Error("selection or match ground indistinct from the page")
	}
}

// Without a background there is nothing to build on, so the theme is left
// as it was.
func TestTerminalColoursNoAnswer(t *testing.T) {
	saved := themes[0]
	defer func() { themes[0] = saved }()

	useTerminalColours(parseColours(nil))

	if themes[0] != saved {
		t.Error("theme changed without a background")
	}
}
