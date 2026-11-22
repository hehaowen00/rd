package main

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
)

// screenText is what a simulation screen shows, a line to a row.
func screenText(s tcell.SimulationScreen) string {
	cells, w, h := s.GetContents()

	var b strings.Builder

	for y := range h {
		for x := range w {
			if r := cells[y*w+x].Runes; len(r) > 0 {
				b.WriteRune(r[0])
			} else {
				b.WriteRune(' ')
			}
		}

		b.WriteString("\n")
	}

	return b.String()
}

func TestReaderHelp(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	r, err := OpenReader(writeTestEPUB(t), false, Position{}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	s := tcell.NewSimulationScreen("")
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	defer s.Fini()
	s.SetSize(90, 14)

	key := func(ch rune) {
		r.Key(tcell.NewEventKey(tcell.KeyRune, ch, tcell.ModNone))
	}

	key('?')
	if r.help == nil || r.find.prompt {
		t.Fatal("? did not open help")
	}

	if ps := r.Draw(s, 0, 0); ps != nil {
		t.Errorf("help drew %d images", len(ps))
	}
	s.Show()

	if txt := screenText(s); !strings.Contains(txt, strHelpTurning) || !strings.Contains(txt, "Next page") {
		t.Errorf("help screen is missing its first section:\n%s", txt)
	}

	// G scrolls to the end, where the last section is
	key('G')
	r.Draw(s, 0, 0)
	s.Show()

	if txt := screenText(s); !strings.Contains(txt, "Back to the library") {
		t.Errorf("after G the end is not in view:\n%s", txt)
	}

	key('?')
	if r.help != nil {
		t.Error("? did not close help")
	}
}

func TestLibraryHelp(t *testing.T) {
	var l Library

	l.Key(tcell.NewEventKey(tcell.KeyRune, '?', tcell.ModNone), 10)
	if l.help == nil {
		t.Fatal("? did not open help")
	}

	l.Key(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone), 10)
	if l.help != nil {
		t.Error("Esc did not close help")
	}
}
