package main

import (
	"slices"
	"testing"

	"github.com/gdamore/tcell/v2"
)

func TestBookmarksRoundTrip(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	b := LoadBookmarks()
	if got := b.Get("/book.epub"); len(got) != 0 {
		t.Fatalf("fresh Get = %v", got)
	}

	marks := []Bookmark{
		{Chapter: 2, Fraction: 0.5, Snippet: "late"},
		{Chapter: 0, Fraction: 0.9, Snippet: "early"},
		{Chapter: 2, Fraction: 0.1, Snippet: "middle"},
	}
	if err := b.Set("/book.epub", marks); err != nil {
		t.Fatal(err)
	}

	got := LoadBookmarks().Get("/book.epub")
	want := []string{"early", "middle", "late"}

	if len(got) != len(want) {
		t.Fatalf("got %d bookmarks, want %d", len(got), len(want))
	}

	for i, w := range want {
		if got[i].Snippet != w {
			t.Errorf("bookmark %d = %q, want %q", i, got[i].Snippet, w)
		}
	}

	if err := b.Set("/book.epub", nil); err != nil {
		t.Fatal(err)
	}

	if got := LoadBookmarks().Get("/book.epub"); len(got) != 0 {
		t.Errorf("after clearing, Get = %v", got)
	}
}

func TestReaderBookmarks(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	path := writeTestEPUB(t)
	store := LoadBookmarks()

	r, err := OpenReader(path, false, Position{}, false, store)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	s := tcell.NewSimulationScreen("")
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	defer s.Fini()
	s.SetSize(80, 12)

	key := func(ch rune) {
		r.Key(tcell.NewEventKey(tcell.KeyRune, ch, tcell.ModNone))
		r.Draw(s, 0, 0)
	}

	r.Draw(s, 0, 0)
	r.nextPage()
	r.Draw(s, 0, 0)
	ch, pg := r.chapter, r.pg

	key('m')
	if len(store.Get(path)) != 1 || r.bookmarkHere() != 0 {
		t.Fatalf("after m: %d saved, here = %d", len(store.Get(path)), r.bookmarkHere())
	}

	key(']')
	key('\'')
	if r.panel == nil || r.panelKind != panelBookmarks {
		t.Fatal("' did not open the bookmarks panel")
	}

	r.Key(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	r.Draw(s, 0, 0)

	if r.panel != nil || r.chapter != ch || r.pg != pg {
		t.Fatalf("after Enter: chapter %d page %d, want %d %d", r.chapter, r.pg, ch, pg)
	}

	key('m')
	if len(store.Get(path)) != 0 {
		t.Errorf("m on a marked page left %v", store.Get(path))
	}
}

func TestRemoveBookmarkFromPanel(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	path := writeTestEPUB(t)
	store := LoadBookmarks()
	marks := []Bookmark{
		{Chapter: 0, Fraction: 0, Snippet: "first"},
		{Chapter: 1, Fraction: 0, Snippet: "second"},
		{Chapter: 1, Fraction: 0.5, Snippet: "third"},
	}
	if err := store.Set(path, marks); err != nil {
		t.Fatal(err)
	}

	r, err := OpenReader(path, false, Position{}, false, store)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	s := tcell.NewSimulationScreen("")
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	defer s.Fini()
	s.SetSize(80, 12)
	r.Draw(s, 0, 0)

	send := func(ev *tcell.EventKey) {
		r.Key(ev)
		r.Draw(s, 0, 0)
	}
	rn := func(ch rune) *tcell.EventKey { return tcell.NewEventKey(tcell.KeyRune, ch, tcell.ModNone) }

	snippets := func(ms []Bookmark) []string {
		var out []string
		for _, m := range ms {
			out = append(out, m.Snippet)
		}
		return out
	}

	send(rn('\''))
	send(rn('j')) // select "second"
	send(rn('x'))

	if got := snippets(LoadBookmarks().Get(path)); !slices.Equal(got, []string{"first", "third"}) {
		t.Fatalf("after x, saved = %v", got)
	}

	if r.panel == nil || len(r.panel.items) != 2 {
		t.Fatal("the panel did not stay open with the bookmark gone")
	}

	if it, _ := r.panel.selected(); it.n != 1 {
		t.Errorf("selection after x is entry %d, want the next one (1)", it.n)
	}

	// Ctrl-D removes while filtering
	send(rn('/'))
	for _, c := range "thi" {
		send(rn(c))
	}
	send(tcell.NewEventKey(tcell.KeyCtrlD, 0, tcell.ModNone))

	if got := snippets(LoadBookmarks().Get(path)); !slices.Equal(got, []string{"first"}) {
		t.Fatalf("after Ctrl-D, saved = %v", got)
	}

	if len(r.marks) != 1 || r.bookmarkHere() != 0 {
		t.Errorf("the reader still has %d marks", len(r.marks))
	}
}
