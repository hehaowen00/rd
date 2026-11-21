package main

import "github.com/gdamore/tcell/v2"

// motion is a vim-style movement, shared by every screen.
type motion int

const (
	motionNone motion = iota
	motionLineDown
	motionLineUp
	motionHalfDown
	motionHalfUp
	motionPageDown
	motionPageUp
	motionTop
	motionBottom
)

// vimKeys turns keystrokes into motions, collecting a count prefix and the
// first g of gg along the way.
type vimKeys struct {
	count   int
	pendG   bool
	pending bool
}

// feed reads one key. It returns the motion, how many times to apply it, and
// whether a count was typed (for gg and G, which go to row or chapter n).
// consumed is true when the key was part of a motion or a pending prefix, so
// the screen should not act on it itself.
func (v *vimKeys) feed(ev *tcell.EventKey) (m motion, n int, counted, consumed bool) {
	if ev.Key() == tcell.KeyRune {
		r := ev.Rune()

		if r >= '1' && r <= '9' || r == '0' && v.count > 0 {
			v.count = min(v.count*10+int(r-'0'), 99999)
			v.pendG = false
			v.pending = true
			return motionNone, 0, false, true
		}

		if r == 'g' {
			if v.pendG {
				m = motionTop
			} else {
				v.pendG = true
				v.pending = true
				return motionNone, 0, false, true
			}
		}
	}

	if m == motionNone {
		m = keyMotion(ev)
	}

	pending := v.pending
	n, counted = max(v.count, 1), v.count > 0
	v.reset()

	if m != motionNone {
		return m, n, counted, true
	}

	// any other key cancels a half-typed command; Esc does only that
	return motionNone, 0, false, pending && ev.Key() == tcell.KeyEscape
}

func (v *vimKeys) reset() { *v = vimKeys{} }

func keyMotion(ev *tcell.EventKey) motion {
	switch ev.Key() {
	case tcell.KeyDown, tcell.KeyCtrlE, tcell.KeyCtrlN:
		return motionLineDown
	case tcell.KeyUp, tcell.KeyCtrlY, tcell.KeyCtrlP:
		return motionLineUp
	case tcell.KeyCtrlD:
		return motionHalfDown
	case tcell.KeyCtrlU:
		return motionHalfUp
	case tcell.KeyPgDn, tcell.KeyCtrlF:
		return motionPageDown
	case tcell.KeyPgUp, tcell.KeyCtrlB:
		return motionPageUp
	case tcell.KeyHome:
		return motionTop
	case tcell.KeyEnd:
		return motionBottom
	case tcell.KeyRune:
		switch ev.Rune() {
		case 'j':
			return motionLineDown
		case 'k':
			return motionLineUp
		case 'G':
			return motionBottom
		}
	}

	return motionNone
}

// apply moves the selection by a motion to a list of n rows showing rows at a time.
func (v *listView) apply(m motion, count int, counted bool, rows, n int) {
	half := max(rows/2, 1)

	switch m {
	case motionLineDown:
		v.move(count, n)
	case motionLineUp:
		v.move(-count, n)
	case motionHalfDown:
		v.move(half*count, n)
	case motionHalfUp:
		v.move(-half*count, n)
	case motionPageDown:
		v.move(rows*count, n)
	case motionPageUp:
		v.move(-rows*count, n)
	case motionTop:
		v.sel = 0
		if counted {
			v.sel = max(min(count-1, n-1), 0)
		}
	case motionBottom:
		v.sel = max(n-1, 0)
		if counted {
			v.sel = max(min(count-1, n-1), 0)
		}
	}
}
