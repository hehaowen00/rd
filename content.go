package main

import (
	"bytes"
	"fmt"
	"strings"

	"golang.org/x/net/html"
)

// Style is a set of text attributes.
type Style uint8

const (
	Bold Style = 1 << iota
	Italic
	Underline
	Code
	Dim
)

// Span is a run of text sharing one style.
type Span struct {
	Text  string
	Style Style
}

// BlockKind distinguishes the kinds of laid-out blocks.
type BlockKind int

const (
	BlockText BlockKind = iota
	BlockImage
	BlockRule
)

// Block is a paragraph-level unit of chapter content.
type Block struct {
	Kind        BlockKind
	Spans       []Span
	Indent      int    // left indent in cells
	Prefix      string // list marker drawn in the indent of the first line
	Pre         bool   // preformatted: no reflow
	Heading     int    // 1-6, or 0
	Image       string // zip path for BlockImage
	Alt         string
	SpaceBefore bool // a blank line separates this block from the previous one
	PageBreak   bool // the block starts a new page
}

// listIndent is the hanging indent for list items; markers are right-aligned in it.
const listIndent = 4

type ctx struct {
	style   Style
	indent  int
	pre     bool
	heading int
}

type converter struct {
	base   string
	blocks []Block

	cur        []Span
	curCtx     ctx
	prefix     string
	gap        bool
	listCounts []int // -1 for unordered lists, else the next ordinal
}

// ParseChapter converts an XHTML chapter into blocks. base is the chapter's
// zip path, used to resolve image references.
func ParseChapter(base string, data []byte) ([]Block, error) {
	doc, err := html.Parse(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	c := &converter{base: base}
	c.walk(doc, ctx{})
	c.flush(false)
	if len(c.blocks) > 0 {
		c.blocks[0].SpaceBefore = false
	}
	return c.blocks, nil
}

// flush ends the current text block. keepEmpty emits a blank line even when
// there is no text (used for empty lines in <pre>).
func (c *converter) flush(keepEmpty bool) {
	if c.hasText() || (keepEmpty && c.curCtx.pre) {
		c.blocks = append(c.blocks, Block{
			Kind:        BlockText,
			Spans:       c.cur,
			Indent:      c.curCtx.indent,
			Prefix:      c.prefix,
			Pre:         c.curCtx.pre,
			Heading:     c.curCtx.heading,
			SpaceBefore: c.gap,
		})
		c.gap = false
		c.prefix = ""
	}
	c.cur = nil
}

// hasText is whether the current block has anything but whitespace.
func (c *converter) hasText() bool {
	for _, s := range c.cur {
		if strings.TrimSpace(s.Text) != "" {
			return true
		}
	}

	return false
}

// blockTags are the elements that start blocks of their own.
var blockTags = map[string]bool{
	"p": true, "div": true, "section": true, "article": true, "blockquote": true,
	"pre": true, "ul": true, "ol": true, "li": true, "dl": true, "dd": true, "dt": true,
	"table": true, "tr": true, "figure": true, "aside": true, "header": true,
	"footer": true, "main": true, "nav": true, "hr": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
}

// hasBlockChild is whether any element inside n starts a block.
func hasBlockChild(n *html.Node) bool {
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		if ch.Type == html.ElementNode && (blockTags[ch.Data] || hasBlockChild(ch)) {
			return true
		}
	}

	return false
}

func (c *converter) text(s string, cx ctx) {
	if len(c.cur) == 0 {
		c.curCtx = cx
	}
	if !cx.pre {
		c.cur = append(c.cur, Span{Text: s, Style: cx.style})
		return
	}
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		if i > 0 {
			c.curCtx = cx
			c.flush(true)
			c.curCtx = cx
		}
		if ln != "" {
			if len(c.cur) == 0 {
				c.curCtx = cx
			}
			c.cur = append(c.cur, Span{Text: strings.ReplaceAll(ln, "\t", "    "), Style: cx.style})
		}
	}
}

// paragraph marks a block boundary with a blank line around it.
func (c *converter) paragraph() {
	c.flush(false)
	if len(c.blocks) > 0 {
		c.gap = true
	}
}

func (c *converter) walk(n *html.Node, cx ctx) {
	switch n.Type {
	case html.TextNode:
		c.text(n.Data, cx)
		return
	case html.DocumentNode:
		c.children(n, cx)
		return
	case html.ElementNode:
	default:
		return
	}

	switch n.Data {
	case "head", "script", "style", "title":
		return
	case "br":
		if cx.pre {
			c.text("\n", cx)
			return
		}

		// a <br> on an empty line, as in text<br/><br/>text, is a blank
		// line between paragraphs
		if !c.hasText() && len(c.blocks) > 0 {
			c.gap = true
		}

		c.flush(false)
		return
	case "hr":
		c.paragraph()
		c.blocks = append(c.blocks, Block{Kind: BlockRule, SpaceBefore: c.gap})
		c.gap = true
		return
	case "img", "image":
		src := attr(n, "src")
		if src == "" {
			src = attr(n, "href")
		}
		if src == "" {
			src = attr(n, "xlink:href")
		}
		if src == "" {
			return
		}
		c.paragraph()
		c.blocks = append(c.blocks, Block{
			Kind:        BlockImage,
			Image:       resolveHref(c.base, src),
			Alt:         attr(n, "alt"),
			SpaceBefore: c.gap,
		})
		c.gap = true
		return
	}

	switch n.Data {
	case "b", "strong", "th":
		cx.style |= Bold
	case "i", "em", "cite", "var", "dfn":
		cx.style |= Italic
	case "u", "ins":
		cx.style |= Underline
	case "code", "tt", "kbd", "samp":
		cx.style |= Code
	case "small", "sub", "sup":
		cx.style |= Dim
	}

	switch n.Data {
	case "p", "figure", "figcaption", "table", "dl", "aside", "header", "footer":
		c.paragraph()
		c.children(n, cx)
		c.paragraph()
	case "h1", "h2", "h3", "h4", "h5", "h6":
		c.paragraph()
		cx.heading = int(n.Data[1] - '0')
		cx.style |= Bold
		c.children(n, cx)
		c.paragraph()
	case "blockquote":
		c.paragraph()
		cx.indent += 2
		cx.style |= Italic
		c.children(n, cx)
		c.paragraph()
	case "pre":
		c.paragraph()
		cx.pre = true
		cx.style |= Code
		c.children(n, cx)
		c.paragraph()
	case "ul", "ol":
		top := len(c.listCounts) == 0
		if top {
			c.paragraph()
		} else {
			c.flush(false)
		}
		count := -1
		if n.Data == "ol" {
			count = 1
		}
		c.listCounts = append(c.listCounts, count)
		c.children(n, cx)
		c.listCounts = c.listCounts[:len(c.listCounts)-1]
		if top {
			c.paragraph()
		} else {
			c.flush(false)
		}
	case "li":
		c.flush(false)
		marker := "•"
		if k := len(c.listCounts) - 1; k >= 0 && c.listCounts[k] >= 0 {
			marker = fmt.Sprintf("%d.", c.listCounts[k])
			c.listCounts[k]++
		}
		c.prefix = marker + " "
		cx.indent += listIndent
		c.children(n, cx)
		c.flush(false)
	case "dd":
		c.flush(false)
		cx.indent += listIndent
		c.children(n, cx)
		c.flush(false)
	case "div":
		// a div holding only text is a paragraph by another name
		if !hasBlockChild(n) {
			c.paragraph()
			c.children(n, cx)
			c.paragraph()

			return
		}

		c.flush(false)
		c.children(n, cx)
		c.flush(false)
	case "section", "article", "body", "main", "nav", "tr", "dt", "caption":
		c.flush(false)
		c.children(n, cx)
		c.flush(false)
	case "td":
		c.children(n, cx)
		c.text("  ", cx)
	default:
		c.children(n, cx)
	}
}

func (c *converter) children(n *html.Node, cx ctx) {
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		c.walk(ch, cx)
	}
}
