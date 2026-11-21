package main

import (
	"testing"

	"github.com/gdamore/tcell/v2"
)

func keys(s string) []*tcell.EventKey {
	var out []*tcell.EventKey

	for _, r := range s {
		out = append(out, tcell.NewEventKey(tcell.KeyRune, r, tcell.ModNone))
	}

	return out
}

func TestListMotions(t *testing.T) {
	tests := []struct {
		input string
		start int
		want  int
	}{
		{"j", 0, 1},
		{"5j", 0, 5},
		{"12j", 0, 12},
		{"3k", 10, 7},
		{"G", 0, 99},
		{"gg", 50, 0},
		{"g", 50, 50},
		{"10G", 0, 9},
		{"20gg", 0, 19},
		{"999G", 0, 99},
		{"gj", 5, 6},
	}

	for _, tt := range tests {
		var v vimKeys

		lv := listView{sel: tt.start}

		for _, ev := range keys(tt.input) {
			if m, n, counted, ok := v.feed(ev); ok {
				lv.apply(m, n, counted, 20, 100)
			}
		}

		if lv.sel != tt.want {
			t.Errorf("%q from %d: sel = %d, want %d", tt.input, tt.start, lv.sel, tt.want)
		}
	}
}

func TestCtrlMotions(t *testing.T) {
	tests := []struct {
		key  tcell.Key
		want int
	}{
		{tcell.KeyCtrlD, 60},
		{tcell.KeyCtrlU, 40},
		{tcell.KeyCtrlF, 70},
		{tcell.KeyCtrlB, 30},
		{tcell.KeyCtrlE, 51},
		{tcell.KeyCtrlY, 49},
	}

	for _, tt := range tests {
		var v vimKeys

		lv := listView{sel: 50}

		if m, n, counted, ok := v.feed(tcell.NewEventKey(tt.key, 0, tcell.ModNone)); ok {
			lv.apply(m, n, counted, 20, 100)
		}

		if lv.sel != tt.want {
			t.Errorf("key %v: sel = %d, want %d", tt.key, lv.sel, tt.want)
		}
	}
}

func TestEscCancelsCount(t *testing.T) {
	var v vimKeys

	v.feed(keys("5")[0])

	if _, _, _, ok := v.feed(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone)); !ok {
		t.Error("Esc after a count should only cancel it")
	}

	if _, _, _, ok := v.feed(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone)); ok {
		t.Error("a bare Esc should reach the screen")
	}
}
