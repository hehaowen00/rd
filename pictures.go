package main

import (
	"bytes"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"strings"

	_ "golang.org/x/image/webp"
)

// pictures holds one book's images and what the terminal holds of them.
type pictures struct {
	book     *Book
	graphics bool
	next     int
	byPath   map[string]*picture
}

type picture struct {
	id    int
	w, h  int
	raw   []byte
	kind  string
	err   error
	sent  bool
	shown bool
	seen  bool
}

// placement is one image on screen for one frame.
type placement struct {
	path             string
	row, col         int
	cols, rows       int
	firstRow, ofRows int
}

func newPictures(book *Book, graphics bool) *pictures {
	return &pictures{
		book:     book,
		graphics: graphics,
		next:     1,
		byPath:   map[string]*picture{},
	}
}

func (p *pictures) get(path string) *picture {
	if pic, ok := p.byPath[path]; ok {
		return pic
	}

	pic := &picture{id: p.next}
	p.next++
	p.byPath[path] = pic

	raw, err := p.book.ReadFile(path)
	if err != nil {
		pic.err = err
		return pic
	}

	cfg, kind, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		pic.err = err
		return pic
	}

	pic.raw, pic.kind, pic.w, pic.h = raw, kind, cfg.Width, cfg.Height

	return pic
}

// sizer returns an ImageSizer for the current cell size.
func (p *pictures) sizer(cellW, cellH int) ImageSizer {
	return func(path string, maxCols, maxRows int) (int, int, bool) {
		if !p.graphics {
			return 0, 0, false
		}

		pic := p.get(path)
		if pic.err != nil {
			return 0, 0, false
		}

		cols, rows := fit(pic.w, pic.h, maxCols, maxRows, cellW, cellH)

		return cols, rows, true
	}
}

func (pic *picture) pngData() ([]byte, error) {
	if pic.kind == "png" {
		return pic.raw, nil
	}

	img, _, err := image.Decode(bytes.NewReader(pic.raw))
	if err != nil {
		return nil, err
	}

	var out bytes.Buffer

	if err := png.Encode(&out, img); err != nil {
		return nil, err
	}

	pic.raw, pic.kind = out.Bytes(), "png"

	return pic.raw, nil
}

// frame returns the escape sequences that put this frame's images on screen
// and take off the ones no longer shown.
func (p *pictures) frame(ps []placement) string {
	var out strings.Builder

	for _, pic := range p.byPath {
		pic.seen = false
	}

	for _, pl := range ps {
		pic := p.get(pl.path)
		if pic.err != nil || pl.ofRows <= 0 {
			continue
		}

		if !pic.sent {
			data, err := pic.pngData()
			if err != nil {
				pic.err = err
				continue
			}

			out.WriteString(kittyTransmit(pic.id, data))
			pic.sent = true
		}

		y := pic.h * pl.firstRow / pl.ofRows
		end := pic.h * (pl.firstRow + pl.rows) / pl.ofRows
		src := box{
			w: pic.w,
			y: y,
			h: max(end-y, 1),
		}

		out.WriteString(kittyPlace(pic.id, pl.row, pl.col, pl.cols, pl.rows, src))
		pic.seen = true
		pic.shown = true
	}

	for _, pic := range p.byPath {
		if pic.shown && !pic.seen {
			out.WriteString(kittyUnplace(pic.id))
			pic.shown = false
		}
	}

	return out.String()
}

// release frees every image the terminal holds for this book.
func (p *pictures) release() string {
	var out strings.Builder

	for _, pic := range p.byPath {
		if pic.sent {
			out.WriteString(kittyForget(pic.id))
		}
	}

	p.byPath = map[string]*picture{}

	return out.String()
}
