package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gdamore/tcell/v2"
)

// These tests press every key the help screen lists and check that each one
// does something, so a key that is documented but not handled is caught.

func runeKey(c rune) *tcell.EventKey { return tcell.NewEventKey(tcell.KeyRune, c, tcell.ModNone) }
func specialKey(k tcell.Key) *tcell.EventKey {
	return tcell.NewEventKey(k, 0, tcell.ModNone)
}

// readerState is what a key in the reader can visibly change.
type readerState struct {
	chapter, pg, width, marks int
	single, panel, help       bool
	prompt                    bool
	panelKind                 panelKind
	theme                     string
}

func stateOf(r *Reader) readerState {
	return readerState{
		chapter: r.chapter, pg: r.pg, width: r.width, marks: len(r.marks),
		single: r.single, panel: r.panel != nil, help: r.help != nil,
		prompt: r.find.prompt, panelKind: r.panelKind, theme: cur.name,
	}
}

func TestReaderKeysAllDoSomething(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	path := writeTestEPUB(t)

	s := tcell.NewSimulationScreen("")
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	defer s.Fini()
	// short, so the first chapter runs to several pages
	s.SetSize(80, 7)

	// open starts a fresh reader on page 2 of chapter 1, so every move has
	// somewhere to go.
	open := func(t *testing.T) *Reader {
		r, err := OpenReader(path, false, Position{}, false, LoadBookmarks())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { r.Close() })

		r.single = true
		r.Draw(s, 0, 0)

		if len(r.pages) < 3 {
			t.Fatalf("test chapter has only %d pages", len(r.pages))
		}

		r.setPage(1)
		r.Draw(s, 0, 0)

		return r
	}

	tests := []struct {
		name string
		keys []*tcell.EventKey
	}{
		{"Space", []*tcell.EventKey{runeKey(' ')}},
		{"f", []*tcell.EventKey{runeKey('f')}},
		{"l", []*tcell.EventKey{runeKey('l')}},
		{"Right", []*tcell.EventKey{specialKey(tcell.KeyRight)}},
		{"b", []*tcell.EventKey{runeKey('b')}},
		{"h", []*tcell.EventKey{runeKey('h')}},
		{"Left", []*tcell.EventKey{specialKey(tcell.KeyLeft)}},
		{"Backspace", []*tcell.EventKey{specialKey(tcell.KeyBackspace2)}},
		{"j", []*tcell.EventKey{runeKey('j')}},
		{"k", []*tcell.EventKey{runeKey('k')}},
		{"Ctrl-D", []*tcell.EventKey{specialKey(tcell.KeyCtrlD)}},
		{"Ctrl-U", []*tcell.EventKey{specialKey(tcell.KeyCtrlU)}},
		{"PgDn", []*tcell.EventKey{specialKey(tcell.KeyPgDn)}},
		{"PgUp", []*tcell.EventKey{specialKey(tcell.KeyPgUp)}},
		{"gg", []*tcell.EventKey{runeKey('g'), runeKey('g')}},
		{"G", []*tcell.EventKey{runeKey('G')}},
		{"]", []*tcell.EventKey{runeKey(']')}},
		{"[ from chapter 2", []*tcell.EventKey{runeKey(']'), runeKey('[')}},
		{"2gg", []*tcell.EventKey{runeKey('2'), runeKey('g'), runeKey('g')}},
		{"2G", []*tcell.EventKey{runeKey('2'), runeKey('G')}},
		{"/", []*tcell.EventKey{runeKey('/')}},
		{"t", []*tcell.EventKey{runeKey('t')}},
		{"Tab", []*tcell.EventKey{specialKey(tcell.KeyTab)}},
		{"m", []*tcell.EventKey{runeKey('m')}},
		{"'", []*tcell.EventKey{runeKey('\'')}},
		{"+", []*tcell.EventKey{runeKey('+')}},
		{"=", []*tcell.EventKey{runeKey('=')}},
		{"-", []*tcell.EventKey{runeKey('+'), runeKey('-')}},
		{"v", []*tcell.EventKey{runeKey('v')}},
		{"c", []*tcell.EventKey{runeKey('c')}},
		{"?", []*tcell.EventKey{runeKey('?')}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := open(t)
			before := stateOf(r)

			// a two-key test checks the last key against the state after the first
			for i, ev := range tt.keys {
				if i == len(tt.keys)-1 && len(tt.keys) > 1 && tt.name != "gg" && tt.name != "2gg" && tt.name != "2G" {
					before = stateOf(r)
				}

				if r.Key(ev) {
					t.Fatalf("%s closed the book", tt.name)
				}

				r.Draw(s, 0, 0)
			}

			if stateOf(r) == before {
				t.Errorf("%s did nothing", tt.name)
			}
		})
	}

	for _, k := range []*tcell.EventKey{runeKey('q'), specialKey(tcell.KeyEscape)} {
		if r := open(t); !r.Key(k) {
			t.Errorf("%v did not go back to the library", k.Name())
		}
	}

	t.Run("Esc clears matches before leaving", func(t *testing.T) {
		r := open(t)
		r.find.lit = true

		if r.Key(specialKey(tcell.KeyEscape)) || r.find.lit {
			t.Error("Esc did not just clear the highlighted matches")
		}
	})

	t.Run("Esc clears a selection before leaving", func(t *testing.T) {
		r := open(t)
		r.sel.on = true

		if r.Key(specialKey(tcell.KeyEscape)) || r.sel.on {
			t.Error("Esc did not just clear the selection")
		}
	})

	t.Run("n and N", func(t *testing.T) {
		r := open(t)
		r.setPage(0)
		r.Draw(s, 0, 0)

		r.Key(runeKey('/'))
		for _, c := range "Two" {
			r.Key(runeKey(c))
		}
		r.Key(specialKey(tcell.KeyEnter))
		r.Draw(s, 0, 0)

		if r.chapter != 1 {
			t.Fatalf("search went to chapter %d, want 1", r.chapter)
		}

		// "Two" is only in chapter 2, so N from there wraps round to it
		r.chapterBy(-1)
		r.Draw(s, 0, 0)
		r.Key(runeKey('n'))
		r.Draw(s, 0, 0)

		if r.chapter != 1 {
			t.Errorf("n went to chapter %d, want 1", r.chapter)
		}

		r.chapterBy(-1)
		r.Draw(s, 0, 0)
		r.Key(runeKey('N'))
		r.Draw(s, 0, 0)

		if r.chapter != 1 {
			t.Errorf("N went to chapter %d, want 1", r.chapter)
		}
	})

	t.Run("mouse wheel", func(t *testing.T) {
		r := open(t)
		before := r.pg
		r.Wheel(1)

		if r.pg == before {
			t.Error("the wheel did not turn the page")
		}
	})
}

// Every key the panels list, in both the contents and the bookmarks.
func TestPanelKeys(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	path := writeTestEPUB(t)
	store := LoadBookmarks()
	_ = store.Set(path, []Bookmark{{Chapter: 0}, {Chapter: 1}, {Chapter: 1, Fraction: 0.5}})

	s := tcell.NewSimulationScreen("")
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	defer s.Fini()
	s.SetSize(80, 12)

	for _, kind := range []struct {
		name string
		open rune
	}{{"contents", 't'}, {"bookmarks", '\''}} {
		open := func(t *testing.T) *Reader {
			r, err := OpenReader(path, false, Position{}, false, store)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { r.Close() })

			r.Draw(s, 0, 0)
			r.Key(runeKey(kind.open))
			r.Draw(s, 0, 0)

			if r.panel == nil {
				t.Fatalf("%c did not open the panel", kind.open)
			}

			r.panel.view.sel = 0

			return r
		}

		t.Run(kind.name, func(t *testing.T) {
			for _, k := range []*tcell.EventKey{runeKey('j'), specialKey(tcell.KeyCtrlD), runeKey('G')} {
				r := open(t)
				r.Key(k)

				if r.panel.view.sel == 0 {
					t.Errorf("%s did not move", k.Name())
				}
			}

			for _, ks := range [][]*tcell.EventKey{{runeKey('j'), runeKey('k')}, {runeKey('G'), specialKey(tcell.KeyCtrlU)}, {runeKey('G'), runeKey('g'), runeKey('g')}} {
				r := open(t)
				for _, k := range ks {
					r.Key(k)
				}

				if r.panel.view.sel != 0 {
					t.Errorf("%s did not move up", ks[len(ks)-1].Name())
				}
			}

			for _, k := range []*tcell.EventKey{specialKey(tcell.KeyEnter), runeKey('l')} {
				// the last entry is in chapter 2 in both panels
				r := open(t)
				r.Key(runeKey('G'))
				r.Key(k)
				r.Draw(s, 0, 0)

				if r.panel != nil || r.chapter != 1 {
					t.Errorf("%s did not go to the entry (chapter %d)", k.Name(), r.chapter)
				}
			}

			r := open(t)
			r.Key(runeKey('G'))
			r.Draw(s, 0, 0)
			r.Click(5, 2+r.panel.view.sel-r.panel.view.top)
			r.Draw(s, 0, 0)

			if r.panel != nil || r.chapter != 1 {
				t.Error("clicking the selected entry did not go to it")
			}

			r = open(t)
			r.Key(runeKey('/'))

			if !r.panel.typing {
				t.Fatal("/ did not start filtering")
			}

			r.Key(specialKey(tcell.KeyEscape))

			if r.panel == nil || r.panel.typing {
				t.Error("Esc while filtering did not just stop typing")
			}

			for _, k := range []*tcell.EventKey{specialKey(tcell.KeyEscape), runeKey('q'), specialKey(tcell.KeyTab), runeKey(kind.open)} {
				r := open(t)
				if r.Key(k); r.panel != nil {
					t.Errorf("%s did not close the panel", k.Name())
				}
			}
		})
	}
}

func TestHelpKeys(t *testing.T) {
	for _, k := range []*tcell.EventKey{runeKey('?'), runeKey('q'), specialKey(tcell.KeyEscape), specialKey(tcell.KeyEnter)} {
		if h := newHelp(strHelp, readerHelp); !h.Key(k) {
			t.Errorf("%s did not close help", k.Name())
		}
	}

	h := newHelp(strHelp, readerHelp)
	h.rows = 5

	for _, k := range []*tcell.EventKey{runeKey('j'), specialKey(tcell.KeyCtrlD), runeKey('G')} {
		h.top = 0
		h.Key(k)

		if h.top == 0 {
			t.Errorf("%s did not scroll help", k.Name())
		}
	}

	h.Wheel(-1)
	if h.top == len(h.lines())-h.rows {
		t.Error("the wheel did not scroll help")
	}
}

// libraryState is what a key in the library can visibly change.
type libraryState struct {
	dir, msg     string
	sel          int
	hidden, help bool
	pick         bool
	places       int
	theme        string
}

func libState(l *Library) libraryState {
	return libraryState{
		dir: l.dir, msg: l.msg, sel: l.view.sel, hidden: l.hidden, help: l.help != nil,
		pick: l.pick != nil, places: len(LoadPlaces()), theme: cur.name,
	}
}

func TestLibraryKeysAllDoSomething(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)

	for _, p := range []string{"a.epub", "b.epub", "c.epub", "shelf/d.epub", ".hidden.epub"} {
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(full, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// open starts on b.epub, between other entries, so up, down and out all
	// have somewhere to go.
	open := func() *Library {
		l := &Library{pos: LoadPositions()}
		l.Load(root, "b.epub")

		return l
	}

	const rows = 1

	tests := []struct {
		name string
		keys []*tcell.EventKey
	}{
		{"j", []*tcell.EventKey{runeKey('j')}},
		{"k", []*tcell.EventKey{runeKey('k')}},
		{"Down", []*tcell.EventKey{specialKey(tcell.KeyDown)}},
		{"Up", []*tcell.EventKey{specialKey(tcell.KeyUp)}},
		{"Ctrl-D", []*tcell.EventKey{specialKey(tcell.KeyCtrlD)}},
		{"Ctrl-U", []*tcell.EventKey{specialKey(tcell.KeyCtrlU)}},
		{"PgDn", []*tcell.EventKey{specialKey(tcell.KeyPgDn)}},
		{"PgUp", []*tcell.EventKey{specialKey(tcell.KeyPgUp)}},
		{"gg", []*tcell.EventKey{runeKey('g'), runeKey('g')}},
		{"G", []*tcell.EventKey{runeKey('G')}},
		{"1G", []*tcell.EventKey{runeKey('1'), runeKey('G')}},
		{"h", []*tcell.EventKey{runeKey('h')}},
		{"-", []*tcell.EventKey{runeKey('-')}},
		{"Left", []*tcell.EventKey{specialKey(tcell.KeyLeft)}},
		{"Backspace", []*tcell.EventKey{specialKey(tcell.KeyBackspace2)}},
		{"~", []*tcell.EventKey{runeKey('~')}},
		{"/", []*tcell.EventKey{runeKey('/')}},
		{"p", []*tcell.EventKey{runeKey('p')}},
		{"a", []*tcell.EventKey{runeKey('a')}},
		{".", []*tcell.EventKey{runeKey('.')}},
		{"c", []*tcell.EventKey{runeKey('c')}},
		{"?", []*tcell.EventKey{runeKey('?')}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := open()
			before := libState(l)

			for _, ev := range tt.keys {
				if _, quit := l.Key(ev, rows); quit {
					t.Fatalf("%s quit", tt.name)
				}
			}

			if libState(l) == before {
				t.Errorf("%s did nothing", tt.name)
			}
		})
	}

	for _, k := range []*tcell.EventKey{specialKey(tcell.KeyEnter), runeKey('l'), specialKey(tcell.KeyRight)} {
		l := open()
		if got, _ := l.Key(k, rows); got != filepath.Join(root, "b.epub") {
			t.Errorf("%s opened %q", k.Name(), got)
		}

		l.Load(root, "shelf")
		if l.Key(k, rows); l.dir != filepath.Join(root, "shelf") {
			t.Errorf("%s did not go into the folder", k.Name())
		}
	}

	for _, k := range []*tcell.EventKey{runeKey('q'), specialKey(tcell.KeyEscape)} {
		if _, quit := open().Key(k, rows); !quit {
			t.Errorf("%s did not quit", k.Name())
		}
	}

	t.Run("Ctrl-D removes a place", func(t *testing.T) {
		l := open()
		l.Key(runeKey('a'), rows)
		l.Key(runeKey('p'), rows)
		l.Key(specialKey(tcell.KeyCtrlD), rows)

		if n := len(LoadPlaces()); n != 0 {
			t.Errorf("%d places left", n)
		}
	})
}

// The app turns raw mouse events into press, drag and release; this checks
// a drag over the page reaches the reader as a copied selection.
func TestAppMouseSelects(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	s := tcell.NewSimulationScreen("")
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	defer s.Fini()
	s.SetSize(80, 12)

	a := &App{s: s, positions: LoadPositions(), bookmarks: LoadBookmarks()}
	a.lib.pos = a.positions
	a.open(writeTestEPUB(t))

	if a.reader == nil {
		t.Fatal("the book did not open")
	}
	defer a.reader.Close()

	a.draw()

	pg := a.reader.shown[0]
	mouse := func(x int, btn tcell.ButtonMask) {
		a.mouse(tcell.NewEventMouse(x, pg.y, btn, tcell.ModNone))
		a.draw()
	}

	mouse(pg.x, tcell.Button1)
	mouse(pg.x+6, tcell.Button1)

	if !a.reader.sel.on {
		t.Fatal("dragging did not select")
	}

	mouse(pg.x+6, tcell.ButtonNone)

	if a.reader.msg == "" || !a.reader.sel.on {
		t.Errorf("letting go did not copy: msg %q", a.reader.msg)
	}

	// a click clears it
	mouse(pg.x, tcell.Button1)
	mouse(pg.x, tcell.ButtonNone)

	if a.reader.sel.on {
		t.Error("a click did not clear the selection")
	}
}
