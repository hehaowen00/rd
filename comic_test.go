package main

import (
	"archive/zip"
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func writeTestCBZ(t *testing.T, files map[string][]byte) string {
	t.Helper()

	name := filepath.Join(t.TempDir(), "test.cbz")

	f, err := os.Create(name)
	if err != nil {
		t.Fatal(err)
	}

	zw := zip.NewWriter(f)

	// write in a scrambled order: the reader must sort
	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}

	slices.Sort(keys)
	slices.Reverse(keys)

	for _, k := range keys {
		w, err := zw.Create(k)
		if err != nil {
			t.Fatal(err)
		}

		w.Write(files[k])
	}

	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	f.Close()

	return name
}

func testPNG(t *testing.T, w, h int) []byte {
	t.Helper()

	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewGray(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}

	return buf.Bytes()
}

func TestOpenComic(t *testing.T) {
	page := testPNG(t, 200, 300)
	name := writeTestCBZ(t, map[string][]byte{
		"ch1/p1.png":          page,
		"ch1/p10.png":         page,
		"ch1/p2.png":          page,
		"ch2/001.png":         page,
		"ch2/notes.txt":       []byte("not a page"),
		"__MACOSX/ch1/p1.png": page,
		"ComicInfo.xml":       []byte(`<ComicInfo><Series>Test Comic</Series><Number>3</Number></ComicInfo>`),
	})

	b, err := OpenBook(name)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	if b.Title != "Test Comic #3" {
		t.Errorf("Title = %q", b.Title)
	}

	if !slices.Equal(b.Spine, []string{"ch1", "ch2"}) {
		t.Errorf("Spine = %v", b.Spine)
	}

	if want := []string{"ch1/p1.png", "ch1/p2.png", "ch1/p10.png"}; !slices.Equal(b.Pages[0], want) {
		t.Errorf("Pages[0] = %v, want %v", b.Pages[0], want)
	}

	if len(b.TOC) != 2 || b.TOC[1].Title != "ch2" || b.TOC[1].Spine != 1 {
		t.Errorf("TOC = %+v", b.TOC)
	}

	// every page image is a page of its own, even when two would fit
	r := &Reader{book: b, pics: newPictures(b, true), columns: 1, pending: -1}
	blocks := r.parse(0)
	lines := Layout(blocks, 80, 40, r.pics.sizer(10, 20))

	if got := Paginate(lines, 40); len(got) != 3 {
		t.Errorf("pages = %v, want 3", got)
	}
}

func TestOpenComicEmpty(t *testing.T) {
	name := writeTestCBZ(t, map[string][]byte{"readme.txt": []byte("hi")})
	if _, err := OpenBook(name); err == nil {
		t.Error("opened a CBZ with no pages")
	}
}

func TestNaturalCompare(t *testing.T) {
	names := []string{"p10.jpg", "P2.jpg", "p1.jpg", "p02b.jpg", "a/p1.jpg"}
	slices.SortFunc(names, naturalCompare)

	want := []string{"a/p1.jpg", "p1.jpg", "P2.jpg", "p02b.jpg", "p10.jpg"}
	if !slices.Equal(names, want) {
		t.Errorf("sorted = %v, want %v", names, want)
	}
}

func TestIsBookFile(t *testing.T) {
	for name, want := range map[string]bool{"a.epub": true, "b.CBZ": true, "e.zip": true, "c.pdf": false, "d.cbr": false} {
		if isBookFile(name) != want {
			t.Errorf("isBookFile(%q) = %v", name, !want)
		}
	}
}

func TestOpenZipOfImages(t *testing.T) {
	dir := writeTestCBZ(t, map[string][]byte{"1.png": testPNG(t, 10, 10), "2.png": testPNG(t, 10, 10)})
	name := filepath.Join(filepath.Dir(dir), "pages.zip")

	if err := os.Rename(dir, name); err != nil {
		t.Fatal(err)
	}

	b, err := OpenBook(name)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	if len(b.Pages) != 1 || len(b.Pages[0]) != 2 {
		t.Errorf("Pages = %v", b.Pages)
	}
}
