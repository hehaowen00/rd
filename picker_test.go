package main

import (
	"fmt"
	"testing"

	"github.com/gdamore/tcell/v2"
)

func TestVimPicker(t *testing.T) {
	var items []pickItem
	for i := range 100 {
		items = append(items, pickItem{text: fmt.Sprintf("Chapter %d", i+1), n: i})
	}

	p := newPicker("Contents", "", items)
	p.vim = true
	p.rows = 20

	r := func(c rune) *tcell.EventKey { return tcell.NewEventKey(tcell.KeyRune, c, 0) }
	k := func(key tcell.Key) *tcell.EventKey { return tcell.NewEventKey(key, 0, 0) }

	steps := []struct {
		name string
		keys []*tcell.EventKey
		want int
	}{
		{"G", []*tcell.EventKey{r('G')}, 99},
		{"gg", []*tcell.EventKey{r('g'), r('g')}, 0},
		{"ctrl-d", []*tcell.EventKey{k(tcell.KeyCtrlD)}, 10},
		{"ctrl-d again", []*tcell.EventKey{k(tcell.KeyCtrlD)}, 20},
		{"ctrl-u", []*tcell.EventKey{k(tcell.KeyCtrlU)}, 10},
		{"j", []*tcell.EventKey{r('j')}, 11},
		{"5k", []*tcell.EventKey{r('5'), r('k')}, 6},
		{"42gg", []*tcell.EventKey{r('4'), r('2'), r('g'), r('g')}, 41},
	}

	for _, st := range steps {
		for _, ev := range st.keys {
			if _, done := p.Key(ev); done {
				t.Fatalf("%s: picker closed", st.name)
			}
		}

		if p.view.sel != st.want {
			t.Errorf("%s: sel = %d, want %d", st.name, p.view.sel, st.want)
		}
	}

	// / filters; Esc stops typing but keeps the filter; G is a motion again
	for _, ev := range []*tcell.EventKey{r('/'), r('9'), r('9'), k(tcell.KeyEscape)} {
		p.Key(ev)
	}

	if p.typing || len(p.shown) != 1 || p.shown[0].n != 98 {
		t.Fatalf("after /99 Esc: typing %v, shown %v", p.typing, p.shown)
	}

	if it, done := p.Key(k(tcell.KeyEnter)); !done || it == nil || it.n != 98 {
		t.Errorf("Enter chose %v", it)
	}
}
