package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// Bookmark is a saved spot in a book: a chapter, how far through it, and a
// few words from the top of the page to recognise it by.
type Bookmark struct {
	Chapter  int       `json:"chapter"`
	Fraction float64   `json:"fraction"`
	Snippet  string    `json:"snippet,omitempty"`
	Added    time.Time `json:"added"`
}

// Bookmarks are kept in $XDG_STATE_HOME/reader/bookmarks.json, keyed by
// absolute book path, each book's list in reading order.
type Bookmarks struct {
	file string
	m    map[string][]Bookmark
}

func LoadBookmarks() *Bookmarks {
	b := &Bookmarks{m: map[string][]Bookmark{}}

	dir := stateDir()
	if dir == "" {
		return b
	}

	b.file = filepath.Join(dir, "bookmarks.json")

	if data, err := os.ReadFile(b.file); err == nil {
		_ = json.Unmarshal(data, &b.m)
	}

	return b
}

func (b *Bookmarks) Get(path string) []Bookmark { return b.m[path] }

// Set replaces a book's bookmarks, sorted into reading order, and writes the
// file.
func (b *Bookmarks) Set(path string, marks []Bookmark) error {
	marks = slices.Clone(marks)
	slices.SortStableFunc(marks, compareBookmarks)

	if len(marks) == 0 {
		delete(b.m, path)
	} else {
		b.m[path] = marks
	}

	if b.file == "" {
		return nil
	}

	data, err := json.MarshalIndent(b.m, "", "  ")
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(b.file), 0o755); err != nil {
		return err
	}

	tmp := b.file + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}

	return os.Rename(tmp, b.file)
}

func compareBookmarks(a, b Bookmark) int {
	if a.Chapter != b.Chapter {
		return a.Chapter - b.Chapter
	}

	switch {
	case a.Fraction < b.Fraction:
		return -1
	case a.Fraction > b.Fraction:
		return 1
	}

	return 0
}

// --- in the reader ---

// pageOf is the page a bookmark in the current chapter falls on.
func (r *Reader) pageOf(bm Bookmark) int {
	p := r.pageAt(int(math.Round(bm.Fraction * float64(len(r.lines)))))
	return p - p%r.columns
}

// bookmarkHere is the index of a bookmark on the page shown, or -1.
func (r *Reader) bookmarkHere() int {
	if r.lines == nil {
		return -1
	}

	for i, bm := range r.marks {
		if bm.Chapter == r.chapter && r.pageOf(bm) == r.pg {
			return i
		}
	}

	return -1
}

// pageSnippet is the first words of text on the page shown.
func (r *Reader) pageSnippet() string {
	if r.lines == nil {
		return ""
	}

	for _, ln := range r.lines[r.pages[r.pg]:] {
		text := ""
		for _, seg := range ln.Segs {
			text += seg.Text
		}

		if text != "" {
			return truncate(text, 60)
		}
	}

	return ""
}

// toggleBookmark adds a bookmark at the page shown, or removes the one there.
func (r *Reader) toggleBookmark() {
	if r.lines == nil {
		return
	}

	marks := slices.Clone(r.marks)
	msg := strBookmarkAdded

	if i := r.bookmarkHere(); i >= 0 {
		marks = slices.Delete(marks, i, i+1)
		msg = strBookmarkRemoved
	} else {
		marks = append(marks, Bookmark{
			Chapter:  r.chapter,
			Fraction: r.fraction(),
			Snippet:  r.pageSnippet(),
			Added:    time.Now(),
		})
	}

	r.saveBookmarks(marks)

	if r.msg == "" {
		r.msg = msg
	}
}

func (r *Reader) saveBookmarks(marks []Bookmark) {
	if r.bookmarks == nil {
		r.marks = marks
		return
	}

	if err := r.bookmarks.Set(r.path, marks); err != nil {
		r.msg = err.Error()
	}

	r.marks = r.bookmarks.Get(r.path)
}

// chapterTitleAt is the contents entry for chapter ch, if any.
func (r *Reader) chapterTitleAt(ch int) string {
	best := -1

	for i, e := range r.book.TOC {
		if e.Spine >= 0 && e.Spine <= ch && (best < 0 || e.Spine > r.book.TOC[best].Spine) {
			best = i
		}
	}

	if best < 0 {
		return ""
	}

	return r.book.TOC[best].Title
}

func (r *Reader) bookmarkItems() []pickItem {
	items := make([]pickItem, 0, len(r.marks))
	here := r.bookmarkHere()

	for i, bm := range r.marks {
		text := bm.Snippet
		if t := r.chapterTitleAt(bm.Chapter); t != "" {
			text = t + "   " + text
		}

		if text == "" {
			text = strUntitled
		}

		detail := fmtBookmarkDetail(bm, len(r.book.Spine))
		if i == here {
			detail = strHere + " " + detail
		}

		items = append(items, pickItem{text: text, detail: detail, n: i})
	}

	return items
}

// openBookmarks shows the book's bookmarks in the side panel, with the one
// at or just before the page shown selected.
func (r *Reader) openBookmarks() {
	p := newPicker(strBookmarks, strNoBookmarks, r.bookmarkItems())
	p.hints = strBookmarksHints
	p.typingHints = strContentsTypingHints
	p.vim = true

	p.remove = func(it pickItem) []pickItem {
		r.saveBookmarks(slices.Delete(slices.Clone(r.marks), it.n, it.n+1))
		return r.bookmarkItems()
	}

	cur := Bookmark{Chapter: r.chapter, Fraction: r.fraction()}
	sel := 0

	for i, bm := range r.marks {
		if compareBookmarks(bm, cur) <= 0 || i == r.bookmarkHere() {
			sel = i
		}
	}

	p.view.sel = sel
	p.view.top = max(sel-5, 0)

	r.panel = p
	r.panelKind = panelBookmarks
}

func (r *Reader) goToBookmark(i int) {
	if i < 0 || i >= len(r.marks) {
		return
	}

	bm := r.marks[i]
	if bm.Chapter >= len(r.book.Spine) {
		return
	}

	r.load(bm.Chapter)
	r.pending = bm.Fraction
}

// fmtBookmarkDetail says where a bookmark is: its chapter, and how far
// through the book.
func fmtBookmarkDetail(bm Bookmark, chapters int) string {
	through := (float64(bm.Chapter) + bm.Fraction) / float64(max(chapters, 1))
	return fmt.Sprintf("ch %d  %3.0f%%", bm.Chapter+1, through*100)
}
