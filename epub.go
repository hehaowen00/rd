package main

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/net/html"
)

// Book is an opened EPUB or CBZ: its reading order (spine) and table of
// contents.
type Book struct {
	zr    *zip.ReadCloser
	files map[string]*zip.File

	Title string
	Spine []string // zip paths of the chapter documents, in reading order
	TOC   []TOCEntry
	// Pages is set for a comic: the page images of each chapter, whose
	// Spine entry is then the folder they are in.
	Pages [][]string
}

var errNoPages = errors.New("no page images in this archive")

// comicArchiveExts are zips read as a folder of page images.
var comicArchiveExts = []string{".cbz", ".zip"}

// isBookFile is whether the library lists and opens name.
func isBookFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".epub" || slices.Contains(comicArchiveExts, ext)
}

// TOCEntry is one line of the table of contents.
type TOCEntry struct {
	Title string
	Depth int
	Spine int // index into Book.Spine, or -1 if the target is not in the spine
}

// OpenBook opens and indexes the EPUB at filename.
func OpenBook(filename string) (*Book, error) {
	zr, err := zip.OpenReader(filename)
	if err != nil {
		return nil, err
	}
	if slices.Contains(comicArchiveExts, strings.ToLower(filepath.Ext(filename))) {
		b, err := openComic(zr)
		if err != nil {
			zr.Close()
		}
		return b, err
	}
	b := &Book{zr: zr, files: make(map[string]*zip.File, len(zr.File))}
	for _, f := range zr.File {
		b.files[f.Name] = f
	}
	if err := b.index(); err != nil {
		zr.Close()
		return nil, err
	}
	return b, nil
}

// Close releases the underlying archive.
func (b *Book) Close() error { return b.zr.Close() }

// ReadFile returns the contents of a file inside the archive.
func (b *Book) ReadFile(name string) ([]byte, error) {
	f, ok := b.files[name]
	if !ok {
		return nil, fmt.Errorf("%s: not found in epub", name)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

type opfItem struct {
	ID         string `xml:"id,attr"`
	Href       string `xml:"href,attr"`
	MediaType  string `xml:"media-type,attr"`
	Properties string `xml:"properties,attr"`
}

type opfPackage struct {
	Metadata struct {
		Titles []string `xml:"title"`
	} `xml:"metadata"`
	Manifest struct {
		Items []opfItem `xml:"item"`
	} `xml:"manifest"`
	Spine struct {
		Toc      string `xml:"toc,attr"`
		ItemRefs []struct {
			IDRef string `xml:"idref,attr"`
		} `xml:"itemref"`
	} `xml:"spine"`
}

func (b *Book) index() error {
	var container struct {
		Rootfiles []struct {
			FullPath string `xml:"full-path,attr"`
		} `xml:"rootfiles>rootfile"`
	}
	if err := b.decodeXML("META-INF/container.xml", &container); err != nil {
		return err
	}
	if len(container.Rootfiles) == 0 {
		return fmt.Errorf("container.xml lists no rootfile")
	}
	opfPath := container.Rootfiles[0].FullPath

	var pkg opfPackage
	if err := b.decodeXML(opfPath, &pkg); err != nil {
		return err
	}
	if len(pkg.Metadata.Titles) > 0 {
		b.Title = strings.TrimSpace(pkg.Metadata.Titles[0])
	}

	items := make(map[string]opfItem, len(pkg.Manifest.Items))
	for _, it := range pkg.Manifest.Items {
		items[it.ID] = it
	}
	for _, ref := range pkg.Spine.ItemRefs {
		if it, ok := items[ref.IDRef]; ok {
			b.Spine = append(b.Spine, resolveHref(opfPath, it.Href))
		}
	}
	if len(b.Spine) == 0 {
		return fmt.Errorf("epub spine is empty")
	}

	// Prefer the EPUB 3 nav document, fall back to the EPUB 2 NCX.
	for _, it := range pkg.Manifest.Items {
		if hasToken(it.Properties, "nav") {
			b.TOC = b.parseNav(resolveHref(opfPath, it.Href))
			break
		}
	}
	if len(b.TOC) == 0 {
		if it, ok := items[pkg.Spine.Toc]; ok {
			b.TOC = b.parseNCX(resolveHref(opfPath, it.Href))
		}
	}
	if len(b.TOC) == 0 {
		for i, p := range b.Spine {
			b.TOC = append(b.TOC, TOCEntry{Title: path.Base(p), Spine: i})
		}
	}
	return nil
}

func (b *Book) decodeXML(name string, v any) error {
	data, err := b.ReadFile(name)
	if err != nil {
		return err
	}
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false
	dec.CharsetReader = func(_ string, r io.Reader) (io.Reader, error) { return r, nil }
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("parsing %s: %w", name, err)
	}
	return nil
}

func (b *Book) spineIndex(target string) int {
	for i, p := range b.Spine {
		if p == target {
			return i
		}
	}
	return -1
}

type navPoint struct {
	Label   string `xml:"navLabel>text"`
	Content struct {
		Src string `xml:"src,attr"`
	} `xml:"content"`
	Children []navPoint `xml:"navPoint"`
}

func (b *Book) parseNCX(ncxPath string) []TOCEntry {
	var ncx struct {
		Points []navPoint `xml:"navMap>navPoint"`
	}
	if err := b.decodeXML(ncxPath, &ncx); err != nil {
		return nil
	}
	var out []TOCEntry
	var walk func(ps []navPoint, depth int)
	walk = func(ps []navPoint, depth int) {
		for _, p := range ps {
			out = append(out, TOCEntry{
				Title: collapseSpace(p.Label),
				Depth: depth,
				Spine: b.spineIndex(resolveHref(ncxPath, p.Content.Src)),
			})
			walk(p.Children, depth+1)
		}
	}
	walk(ncx.Points, 0)
	return out
}

func (b *Book) parseNav(navPath string) []TOCEntry {
	data, err := b.ReadFile(navPath)
	if err != nil {
		return nil
	}
	doc, err := html.Parse(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	var navs []*html.Node
	var find func(n *html.Node)
	find = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "nav" {
			navs = append(navs, n)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			find(c)
		}
	}
	find(doc)
	if len(navs) == 0 {
		return nil
	}
	nav := navs[0]
	for _, n := range navs {
		if attr(n, "epub:type") == "toc" || attr(n, "type") == "toc" {
			nav = n
			break
		}
	}

	var out []TOCEntry
	var walk func(n *html.Node, depth int)
	walk = func(n *html.Node, depth int) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode {
				continue
			}
			switch c.Data {
			case "a", "span":
				if n.Data == "li" {
					out = append(out, TOCEntry{
						Title: collapseSpace(textContent(c)),
						Depth: depth,
						Spine: b.spineIndex(resolveHref(navPath, attr(c, "href"))),
					})
				}
			case "ol", "ul":
				d := depth
				if n.Data == "li" {
					d++
				}
				walk(c, d)
			default:
				walk(c, depth)
			}
		}
	}
	walk(nav, 0)
	return out
}

// resolveHref resolves a (possibly percent-encoded) href relative to the
// document at base, dropping any fragment, and returns a zip path.
func resolveHref(base, href string) string {
	if i := strings.IndexByte(href, '#'); i >= 0 {
		href = href[:i]
	}
	if u, err := url.PathUnescape(href); err == nil {
		href = u
	}
	if href == "" {
		return base
	}
	if strings.HasPrefix(href, "/") {
		return path.Clean(href[1:])
	}
	return path.Join(path.Dir(base), href)
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key || (a.Namespace != "" && a.Namespace+":"+a.Key == key) {
			return a.Val
		}
	}
	return ""
}

func textContent(n *html.Node) string {
	var sb strings.Builder
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			sb.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return sb.String()
}

func collapseSpace(s string) string { return strings.Join(strings.Fields(s), " ") }

func hasToken(list, tok string) bool {
	for _, f := range strings.Fields(list) {
		if f == tok {
			return true
		}
	}
	return false
}
