package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/mattn/go-runewidth"
)

// Library is the landing page: a directory listing of folders and EPUBs.
type Library struct {
	dir     string
	entries []libEntry
	hidden  bool
	err     string
	msg     string
	view    listView
	keys    vimKeys
	pos     *Positions
	pick    *picker
	// pickBooks is true when pick is the book finder, false for places.
	pickBooks bool
}

type libEntry struct {
	name  string
	isDir bool
	size  int64
}

func (e libEntry) label() string {
	if e.isDir {
		return e.name + "/"
	}

	return e.name
}

// Load lists dir, selecting the entry named sel if present.
func (l *Library) Load(dir, sel string) {
	l.dir = dir
	l.entries = nil
	l.err = ""
	l.view = listView{}

	names, err := os.ReadDir(dir)
	if err != nil {
		l.err = err.Error()
	}

	sort.SliceStable(names, func(i, j int) bool {
		if names[i].IsDir() != names[j].IsDir() {
			return names[i].IsDir()
		}

		return strings.ToLower(names[i].Name()) < strings.ToLower(names[j].Name())
	})

	if filepath.Dir(dir) != dir {
		l.entries = append(l.entries, libEntry{
			name:  "..",
			isDir: true,
		})
	}

	for _, d := range names {
		if !l.hidden && strings.HasPrefix(d.Name(), ".") {
			continue
		}

		isDir := d.IsDir()

		if d.Type()&os.ModeSymlink != 0 {
			if st, err := os.Stat(filepath.Join(dir, d.Name())); err == nil {
				isDir = st.IsDir()
			}
		}

		if !isDir && !isBookFile(d.Name()) {
			continue
		}

		en := libEntry{
			name:  d.Name(),
			isDir: isDir,
		}

		if info, err := d.Info(); err == nil && !isDir {
			en.size = info.Size()
		}

		l.entries = append(l.entries, en)
	}

	for i, en := range l.entries {
		if en.name == sel {
			l.view.sel = i
		}
	}

	if sel == "" && len(l.entries) > 1 && l.entries[0].name == ".." {
		l.view.sel = 1
	}
}

func (l *Library) selected() (libEntry, bool) {
	if l.view.sel < 0 || l.view.sel >= len(l.entries) {
		return libEntry{}, false
	}

	return l.entries[l.view.sel], true
}

func (l *Library) Draw(s tcell.Screen, pos *Positions) {
	if l.pick != nil {
		l.pick.Draw(s)
		return
	}

	w, h := s.Size()
	margin := 2
	inner := max(w-2*margin, 1)

	n := put(s, margin, 0, inner, strLibrary, styleTitle)
	put(s, margin+n+2, 0, inner-n-2, truncate(tildeHome(l.dir), inner-n-2), styleDim)

	listTop := 2
	rows := max(h-listTop-2, 1)
	l.view.scroll(rows, len(l.entries))

	if l.err != "" {
		put(s, margin, listTop, inner, truncate(l.err, inner), styleDim)
	} else if len(l.entries) == 0 || (len(l.entries) == 1 && l.entries[0].name == "..") {
		put(s, margin, h/2, inner, strEmptyDir, styleDim)
	}

	for i := range rows {
		idx := l.view.top + i
		if idx >= len(l.entries) {
			break
		}

		en := l.entries[idx]
		y := listTop + i
		st := styleNormal

		if en.isDir {
			st = folder(st)
		}

		if idx == l.view.sel {
			st = selected(st)
			fillRow(s, margin, y, inner, st)
		}

		right := ""

		if !en.isDir {
			right = humanSize(en.size)

			if p, ok := pos.Get(filepath.Join(l.dir, en.name)); ok {
				right = fmt.Sprintf("%3.0f%%   %s", p.Progress*100, right)
			}
		}

		rw := runewidth.StringWidth(right)
		put(s, margin+1, y, inner-rw-3, truncate(en.label(), inner-rw-3), st)
		rst := st.Bold(false)
		if idx != l.view.sel {
			rst = dim(rst)
		}

		put(s, margin+inner-rw-1, y, rw, right, rst)
	}

	if l.msg != "" {
		put(s, margin, h-1, inner, truncate(l.msg, inner), styleDim)
	} else {
		put(s, margin, h-1, inner, truncate(strLibraryHints, inner), styleDim)
	}
}

// Key handles a key; it returns the EPUB path to open, if one was chosen.
func (l *Library) Key(ev *tcell.EventKey, pageRows int) (open string, quit bool) {
	l.msg = ""

	if l.pick != nil {
		it, done := l.pick.Key(ev)
		if done {
			l.pick = nil
		}

		if it != nil {
			return l.choose(*it), false
		}

		return "", false
	}

	if m, count, counted, ok := l.keys.feed(ev); ok {
		l.view.apply(m, count, counted, pageRows, len(l.entries))
		return "", false
	}

	switch ev.Key() {
	case tcell.KeyEnter, tcell.KeyRight:
		return l.enter(), false
	case tcell.KeyBackspace, tcell.KeyBackspace2, tcell.KeyLeft:
		l.up()
	case tcell.KeyEscape, tcell.KeyCtrlC:
		return "", true
	case tcell.KeyRune:
		switch ev.Rune() {
		case 'l':
			return l.enter(), false
		case 'h', '-':
			l.up()
		case '~':
			if home, err := os.UserHomeDir(); err == nil {
				l.Load(home, "")
			}
		case '.':
			l.hidden = !l.hidden
			sel, _ := l.selected()
			l.Load(l.dir, sel.name)
		case 'c':
			l.msg = nextTheme()
		case '/':
			l.openFinder()
		case 'p':
			l.openPlaces()
		case 'a':
			l.savePlace()
		case 'q':
			return "", true
		}
	}

	return "", false
}

// openFinder fuzzy-finds the EPUBs anywhere under the current folder, and
// the folders they are in.
func (l *Library) openFinder() {
	var items []pickItem

	for _, f := range findShelves(l.dir, l.hidden) {
		full := filepath.Join(l.dir, f.rel)
		it := pickItem{text: f.rel, value: full}

		if f.dir {
			it.text += string(filepath.Separator)
			it.bold = true
			it.folder = true
			items = append(items, it)

			continue
		}

		if p, ok := l.pos.Get(full); ok {
			it.detail = fmt.Sprintf("%3.0f%%", p.Progress*100)
		}

		items = append(items, it)
	}

	l.pick = newPicker(strFindBooks+"  "+tildeHome(l.dir), strNoBooks, items)
	l.pickBooks = true
}

func placeItems(dirs []string) []pickItem {
	items := make([]pickItem, 0, len(dirs))

	for _, d := range dirs {
		it := pickItem{text: tildeHome(d), value: d}
		if !isDir(d) {
			it.detail = strMissing
			it.dim = true
		}

		items = append(items, it)
	}

	return items
}

// openPlaces picks a saved folder to jump to.
func (l *Library) openPlaces() {
	l.pick = newPicker(strPlaces, strNoPlaces, placeItems(LoadPlaces()))
	l.pickBooks = false
	l.pick.remove = func(it pickItem) []pickItem {
		dirs := slices.DeleteFunc(LoadPlaces(), func(d string) bool { return d == it.value })
		if err := SavePlaces(dirs); err != nil {
			l.msg = err.Error()
		}

		return placeItems(dirs)
	}
}

func (l *Library) savePlace() {
	dirs := LoadPlaces()
	name := tildeHome(l.dir)

	if slices.Contains(dirs, l.dir) {
		l.msg = fmt.Sprintf(strPlaceKnown, name)
		return
	}

	if err := SavePlaces(append(dirs, l.dir)); err != nil {
		l.msg = err.Error()
		return
	}

	l.msg = fmt.Sprintf(strPlaceSaved, name)
}

// choose acts on a picked item: a book is returned to open, a place is gone to.
func (l *Library) choose(it pickItem) string {
	if l.pickBooks && !it.folder {
		return it.value
	}

	if !isDir(it.value) {
		l.msg = fmt.Sprintf(strPlaceGone, it.text)
		return ""
	}

	l.Load(it.value, "")

	return ""
}

func (l *Library) enter() string {
	en, ok := l.selected()
	if !ok {
		return ""
	}

	if en.name == ".." {
		l.up()
		return ""
	}

	full := filepath.Join(l.dir, en.name)

	if en.isDir {
		l.Load(full, "")
		return ""
	}

	return full
}

func (l *Library) up() {
	parent := filepath.Dir(l.dir)
	if parent == l.dir {
		return
	}

	l.Load(parent, filepath.Base(l.dir))
}

// Click selects the row under the mouse; clicking the selected row opens it.
func (l *Library) Click(y int) string {
	if l.pick != nil {
		it := l.pick.Click(y)
		if it == nil {
			return ""
		}

		l.pick = nil

		return l.choose(*it)
	}

	idx := l.view.top + y - 2
	if idx < 0 || idx >= len(l.entries) {
		return ""
	}

	if idx == l.view.sel {
		return l.enter()
	}

	l.view.sel = idx

	return ""
}

func humanSize(n int64) string {
	units := []string{"B", "K", "M", "G"}
	size := float64(n)
	u := 0

	for size >= 1024 && u < len(units)-1 {
		size /= 1024
		u++
	}

	if u == 0 {
		return fmt.Sprintf("%d%s", n, units[u])
	}

	return fmt.Sprintf("%.1f%s", size, units[u])
}

func tildeHome(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}

	if p == home {
		return "~"
	}

	if strings.HasPrefix(p, home+string(filepath.Separator)) {
		return "~" + p[len(home):]
	}

	return p
}
