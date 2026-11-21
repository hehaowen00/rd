package main

import (
	"archive/zip"
	"encoding/xml"
	"path"
	"slices"
	"strings"
	"unicode"
)

// A CBZ (or a plain .zip) is a zip of page images, read in name order. Pages in subfolders are
// grouped into one chapter per folder; ComicInfo.xml, if present, gives the
// title.

var comicExts = []string{".jpg", ".jpeg", ".png", ".gif", ".webp"}

func isComicPage(name string) bool {
	base := path.Base(name)
	if strings.HasPrefix(base, ".") || strings.HasPrefix(name, "__MACOSX/") {
		return false
	}

	return slices.Contains(comicExts, strings.ToLower(path.Ext(name)))
}

func openComic(zr *zip.ReadCloser) (*Book, error) {
	b := &Book{zr: zr, files: make(map[string]*zip.File, len(zr.File))}

	var pages []string

	for _, f := range zr.File {
		b.files[f.Name] = f
		if !f.FileInfo().IsDir() && isComicPage(f.Name) {
			pages = append(pages, f.Name)
		}
	}

	if len(pages) == 0 {
		return nil, errNoPages
	}

	slices.SortFunc(pages, naturalCompare)

	// one chapter per folder, in the order the folders first appear
	chapter := map[string]int{}

	for _, p := range pages {
		dir := path.Dir(p)

		ch, ok := chapter[dir]
		if !ok {
			ch = len(b.Spine)
			chapter[dir] = ch
			b.Spine = append(b.Spine, dir)
			b.Pages = append(b.Pages, nil)
		}

		b.Pages[ch] = append(b.Pages[ch], p)
	}

	for i, dir := range b.Spine {
		title := path.Base(dir)
		if dir == "." {
			title = strPages
		}

		b.TOC = append(b.TOC, TOCEntry{Title: title, Spine: i})
	}

	var info struct {
		Title  string `xml:"Title"`
		Series string `xml:"Series"`
		Number string `xml:"Number"`
	}

	if data, err := b.ReadFile("ComicInfo.xml"); err == nil && xml.Unmarshal(data, &info) == nil {
		switch {
		case info.Series != "" && info.Number != "":
			b.Title = info.Series + " #" + info.Number
		case info.Title != "":
			b.Title = info.Title
		default:
			b.Title = info.Series
		}
	}

	return b, nil
}

// comicBlocks is chapter ch of a comic: each page an image on a page of its own.
func (b *Book) comicBlocks(ch int) []Block {
	var blocks []Block

	for _, p := range b.Pages[ch] {
		blocks = append(blocks, Block{
			Kind:      BlockImage,
			Image:     p,
			Alt:       path.Base(p),
			PageBreak: true,
		})
	}

	return blocks
}

// naturalCompare orders names as people number them: page2 before page10.
func naturalCompare(a, b string) int {
	ar, br := []rune(strings.ToLower(a)), []rune(strings.ToLower(b))
	i, j := 0, 0

	for i < len(ar) && j < len(br) {
		if unicode.IsDigit(ar[i]) && unicode.IsDigit(br[j]) {
			si, sj := i, j

			for i < len(ar) && unicode.IsDigit(ar[i]) {
				i++
			}

			for j < len(br) && unicode.IsDigit(br[j]) {
				j++
			}

			na := strings.TrimLeft(string(ar[si:i]), "0")
			nb := strings.TrimLeft(string(br[sj:j]), "0")

			if len(na) != len(nb) {
				return len(na) - len(nb)
			}

			if c := strings.Compare(na, nb); c != 0 {
				return c
			}

			continue
		}

		if ar[i] != br[j] {
			return int(ar[i]) - int(br[j])
		}

		i++
		j++
	}

	return (len(ar) - i) - (len(br) - j)
}
