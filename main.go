// Command reader is a terminal EPUB reader that draws images with the kitty
// graphics protocol.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gdamore/tcell/v2"
)

// App switches between the library and an open book.
type App struct {
	s         tcell.Screen
	graphics  bool
	positions *Positions
	bookmarks *Bookmarks
	lib       Library
	reader    *Reader
	buttons   tcell.ButtonMask
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "rd:", err)
		os.Exit(1)
	}
}

func run() error {
	start, err := os.Getwd()
	if err != nil {
		return err
	}

	openPath := ""

	if len(os.Args) > 1 {
		arg, err := filepath.Abs(os.Args[1])
		if err != nil {
			return err
		}

		st, err := os.Stat(arg)
		if err != nil {
			return err
		}

		if st.IsDir() {
			start = arg
		} else {
			start = filepath.Dir(arg)
			openPath = arg
		}
	}

	loadTheme()

	graphics := DetectGraphics(500 * time.Millisecond)

	s, err := tcell.NewScreen()
	if err != nil {
		return err
	}

	if err := s.Init(); err != nil {
		return err
	}

	s.EnableMouse()

	a := &App{
		s:         s,
		graphics:  graphics,
		positions: LoadPositions(),
		bookmarks: LoadBookmarks(),
	}

	a.lib.pos = a.positions
	a.lib.Load(start, filepath.Base(openPath))

	if openPath != "" {
		a.open(openPath)
	}

	a.loop()
	a.closeBook()
	s.Fini()

	return nil
}

func (a *App) loop() {
	for {
		a.draw()

		switch ev := a.s.PollEvent().(type) {
		case nil:
			return
		case *tcell.EventResize:
			a.s.Sync()
		case *tcell.EventKey:
			if a.key(ev) {
				return
			}
		case *tcell.EventMouse:
			a.mouse(ev)
		}
	}
}

func (a *App) key(ev *tcell.EventKey) (quit bool) {
	if a.reader != nil {
		if ev.Key() == tcell.KeyCtrlC {
			return true
		}

		if a.reader.Key(ev) {
			a.closeBook()
		}

		return false
	}

	_, h := a.s.Size()
	path, quit := a.lib.Key(ev, max(h-4, 1))

	if path != "" {
		a.open(path)
	}

	return quit
}

func (a *App) mouse(ev *tcell.EventMouse) {
	btn := ev.Buttons()
	held := a.buttons&tcell.Button1 != 0
	pressed := btn&tcell.Button1 != 0 && !held
	dragged := btn&tcell.Button1 != 0 && held
	released := btn&tcell.Button1 == 0 && held
	a.buttons = btn
	x, y := ev.Position()

	switch {
	case btn&tcell.WheelUp != 0:
		a.wheel(-1)
	case btn&tcell.WheelDown != 0:
		a.wheel(1)
	case pressed && a.reader != nil:
		a.reader.Click(x, y)
	case dragged && a.reader != nil:
		a.reader.selectDrag(x, y)
	case released && a.reader != nil:
		if text := a.reader.selectEnd(x, y); text != "" {
			a.rawWrite(osc52(text))
			a.reader.msg = fmt.Sprintf(strCopied, runeCount(text))
		}
	case pressed:
		if path := a.lib.Click(y); path != "" {
			a.open(path)
		}
	}
}

func (a *App) wheel(d int) {
	if a.reader != nil {
		a.reader.Wheel(d)
		return
	}

	if a.lib.help != nil {
		a.lib.help.Wheel(d)
		return
	}

	if a.lib.pick != nil {
		a.lib.pick.Wheel(d)
		return
	}

	a.lib.view.move(d, len(a.lib.entries))
}

func (a *App) open(path string) {
	pos, ok := a.positions.Get(path)

	r, err := OpenReader(path, a.graphics, pos, ok, a.bookmarks)
	if err != nil {
		a.lib.err = fmt.Sprintf("%s: %v", filepath.Base(path), err)
		return
	}

	a.reader = r
}

func (a *App) closeBook() {
	if a.reader == nil {
		return
	}

	if a.reader.lines != nil {
		_ = a.positions.Set(a.reader.path, a.reader.Position())
	}

	a.rawWrite(a.reader.Close())
	a.reader = nil

	sel, _ := a.lib.selected()
	a.lib.Load(a.lib.dir, sel.name)
}

func (a *App) draw() {
	a.s.SetStyle(styleNormal)
	a.s.Fill(' ', styleNormal) // Clear would paint the terminal's own background
	a.s.HideCursor()

	var ps []placement

	if a.reader != nil {
		cw, ch := a.cellSize()
		ps = a.reader.Draw(a.s, cw, ch)
	} else {
		a.lib.Draw(a.s, a.positions)
	}

	a.s.Show()

	if a.reader != nil && a.graphics {
		a.rawWrite(a.reader.pics.frame(ps))
	}
}

func (a *App) cellSize() (w, h int) {
	tty, ok := a.s.Tty()
	if !ok {
		return 0, 0
	}

	ws, err := tty.WindowSize()
	if err != nil {
		return 0, 0
	}

	return ws.CellDimensions()
}

func (a *App) rawWrite(s string) {
	if s == "" {
		return
	}

	if tty, ok := a.s.Tty(); ok {
		_, _ = tty.Write([]byte(s))
	}
}
