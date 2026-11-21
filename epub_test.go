package main

import (
	"archive/zip"
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTestEPUB(t *testing.T) string {
	t.Helper()

	var pic bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 40, 80))
	img.Set(1, 1, color.White)

	if err := png.Encode(&pic, img); err != nil {
		t.Fatal(err)
	}

	files := []struct {
		name string
		body []byte
	}{
		{
			"mimetype",
			[]byte("application/epub+zip"),
		},
		{
			"META-INF/container.xml",
			[]byte(`<?xml version="1.0"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles><rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/></rootfiles>
</container>`),
		},
		{
			"OEBPS/content.opf",
			[]byte(`<?xml version="1.0"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>Test Book</dc:title></metadata>
  <manifest>
    <item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>
    <item id="c1" href="text/ch%201.xhtml" media-type="application/xhtml+xml"/>
    <item id="c2" href="text/ch2.xhtml" media-type="application/xhtml+xml"/>
    <item id="img" href="images/pic.png" media-type="image/png"/>
  </manifest>
  <spine><itemref idref="c1"/><itemref idref="c2"/></spine>
</package>`),
		},
		{
			"OEBPS/nav.xhtml",
			[]byte(`<html xmlns:epub="http://www.idpf.org/2007/ops"><body>
<nav epub:type="toc"><ol>
  <li><a href="text/ch%201.xhtml">One</a>
    <ol><li><a href="text/ch%201.xhtml#s">One A</a></li></ol></li>
  <li><a href="text/ch2.xhtml">Two</a></li>
</ol></nav></body></html>`),
		},
		{
			"OEBPS/text/ch 1.xhtml",
			[]byte(`<html><head><title>x</title><style>p{}</style></head><body>
<h1>Chapter One</h1>
<p>Hello <em>wide</em> world, this is a <b>test</b> paragraph.</p>
<img src="../images/pic.png" alt="a pic"/>
<ul><li>first</li><li>second</li></ul>
<pre>line 1
  line 2</pre>
</body></html>`),
		},
		{
			"OEBPS/text/ch2.xhtml",
			[]byte(`<html><body><p>Two</p></body></html>`),
		},
		{
			"OEBPS/images/pic.png",
			pic.Bytes(),
		},
	}

	path := filepath.Join(t.TempDir(), "test.epub")

	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}

	zw := zip.NewWriter(f)

	for _, file := range files {
		w, err := zw.Create(file.name)
		if err != nil {
			t.Fatal(err)
		}

		if _, err := w.Write(file.body); err != nil {
			t.Fatal(err)
		}
	}

	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	return path
}

func TestOpenBook(t *testing.T) {
	b, err := OpenBook(writeTestEPUB(t))
	if err != nil {
		t.Fatal(err)
	}

	defer b.Close()

	if b.Title != "Test Book" {
		t.Errorf("title = %q", b.Title)
	}

	wantSpine := []string{"OEBPS/text/ch 1.xhtml", "OEBPS/text/ch2.xhtml"}
	if strings.Join(b.Spine, "|") != strings.Join(wantSpine, "|") {
		t.Errorf("spine = %q, want %q", b.Spine, wantSpine)
	}

	want := []TOCEntry{
		{
			Title: "One",
			Depth: 0,
			Spine: 0,
		},
		{
			Title: "One A",
			Depth: 1,
			Spine: 0,
		},
		{
			Title: "Two",
			Depth: 0,
			Spine: 1,
		},
	}

	if len(b.TOC) != len(want) {
		t.Fatalf("toc = %+v, want %+v", b.TOC, want)
	}

	for i := range want {
		if b.TOC[i] != want[i] {
			t.Errorf("toc[%d] = %+v, want %+v", i, b.TOC[i], want[i])
		}
	}
}

func TestChapterLayout(t *testing.T) {
	b, err := OpenBook(writeTestEPUB(t))
	if err != nil {
		t.Fatal(err)
	}

	defer b.Close()

	data, err := b.ReadFile(b.Spine[0])
	if err != nil {
		t.Fatal(err)
	}

	blocks, err := ParseChapter(b.Spine[0], data)
	if err != nil {
		t.Fatal(err)
	}

	pics := newPictures(b, true)
	lines := Layout(blocks, 20, 30, pics.sizer(10, 20))

	var text []string
	imageRows := 0

	for _, ln := range lines {
		if ln.Image != nil {
			if ln.Image.Path != "OEBPS/images/pic.png" {
				t.Errorf("image path = %q", ln.Image.Path)
			}

			imageRows++
			continue
		}

		var sb strings.Builder
		for _, s := range ln.Segs {
			sb.WriteString(s.Text)
		}

		text = append(text, sb.String())
	}

	if imageRows != 4 {
		t.Errorf("image rows = %d, want 4 (80px tall / 20px cells)", imageRows)
	}

	want := []string{
		"Chapter One",
		"",
		"Hello wide world,",
		"this is a test",
		"paragraph.",
		"",
		"",
		"  • first",
		"  • second",
		"",
		"line 1",
		"  line 2",
	}

	if strings.Join(text, "\n") != strings.Join(want, "\n") {
		t.Errorf("layout:\n%s\nwant:\n%s", strings.Join(text, "\n"), strings.Join(want, "\n"))
	}

	for _, ln := range lines {
		w := 0
		for _, s := range ln.Segs {
			w += len([]rune(s.Text))
		}

		if w > 20 {
			t.Errorf("line wider than 20: %+v", ln.Segs)
		}
	}
}

func TestFit(t *testing.T) {
	tests := []struct {
		w, h, boxC, boxR, cw, ch int
		cols, rows               int
	}{
		{100, 100, 80, 40, 10, 20, 10, 5},
		{1600, 800, 80, 40, 10, 20, 80, 20},
		{800, 1600, 80, 40, 10, 20, 40, 40},
		{100, 100, 80, 40, 0, 0, 80, 40},
	}

	for _, tt := range tests {
		c, r := fit(tt.w, tt.h, tt.boxC, tt.boxR, tt.cw, tt.ch)
		if c != tt.cols || r != tt.rows {
			t.Errorf("fit(%d,%d in %dx%d, cell %dx%d) = %d,%d want %d,%d",
				tt.w, tt.h, tt.boxC, tt.boxR, tt.cw, tt.ch, c, r, tt.cols, tt.rows)
		}
	}
}
