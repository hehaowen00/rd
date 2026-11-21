package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// Every command carries q=2 so the terminal never answers onto the input
// stream tcell is reading.

const kittyChunk = 4096

func kittyTransmit(id int, png []byte) string {
	b := base64.StdEncoding.EncodeToString(png)
	var sb strings.Builder

	for first := true; len(b) > 0; first = false {
		n := min(kittyChunk, len(b))
		more := 0

		if n < len(b) {
			more = 1
		}

		if first {
			fmt.Fprintf(&sb, "\x1b_Ga=t,f=100,t=d,i=%d,q=2,m=%d;%s\x1b\\", id, more, b[:n])
		} else {
			fmt.Fprintf(&sb, "\x1b_Gm=%d;%s\x1b\\", more, b[:n])
		}

		b = b[n:]
	}

	return sb.String()
}

// kittyPlacementID names the single placement each picture has, so placing it
// again on the next frame moves it rather than adding another.
const kittyPlacementID = 1

type box struct{ x, y, w, h int }

// kittyPlace draws the src rectangle of picture id into cols×rows cells at
// (row, col), leaving the cursor where it was.
func kittyPlace(id, row, col, cols, rows int, src box) string {
	return fmt.Sprintf(
		"\x1b[%d;%dH\x1b_Ga=p,i=%d,p=%d,x=%d,y=%d,w=%d,h=%d,c=%d,r=%d,z=-1,C=1,q=2\x1b\\",
		row+1, col+1, id, kittyPlacementID, src.x, src.y, src.w, src.h, cols, rows)
}

func kittyUnplace(id int) string {
	return fmt.Sprintf("\x1b_Ga=d,d=i,i=%d,p=%d,q=2\x1b\\", id, kittyPlacementID)
}

func kittyForget(id int) string {
	return fmt.Sprintf("\x1b_Ga=d,d=I,i=%d,q=2\x1b\\", id)
}

// The terminal is asked whether it draws kitty pictures before tcell takes the
// keyboard, since the reply arrives on the same input. A DA1 request follows
// the query; terminals answer in order, so the DA1 reply marks the end.
const kittyQuery = "\x1b_Gi=31,s=1,v=1,a=q,t=d,f=24;AAAA\x1b\\\x1b[c"

var (
	kittyReply = regexp.MustCompile(`\x1b_Gi=31;OK`)
	da1Reply   = regexp.MustCompile(`\x1b\[\?[0-9;]*c`)
)

// DetectGraphics reports whether the terminal speaks the kitty graphics
// protocol. READER_GRAPHICS=kitty forces it on for multiplexers that pass
// pictures through but swallow the query.
func DetectGraphics(timeout time.Duration) bool {
	if os.Getenv("READER_GRAPHICS") == "kitty" {
		return true
	}

	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return false
	}

	defer tty.Close()

	fd := int(tty.Fd())

	old, err := term.MakeRaw(fd)
	if err != nil {
		return false
	}

	defer term.Restore(fd, old)

	if _, err := tty.WriteString(kittyQuery); err != nil {
		return false
	}

	deadline := time.Now().Add(timeout)
	var buf []byte
	chunk := make([]byte, 1024)

	for !da1Reply.Match(buf) {
		left := time.Until(deadline)
		if left <= 0 {
			return false
		}

		fds := &unix.FdSet{}
		fds.Set(fd)
		tv := unix.NsecToTimeval(int64(left))

		n, err := unix.Select(fd+1, fds, nil, nil, &tv)
		if err == unix.EINTR {
			continue
		}

		if err != nil || n == 0 {
			return false
		}

		read, err := tty.Read(chunk)
		buf = append(buf, chunk[:read]...)

		if err != nil {
			return false
		}
	}

	return kittyReply.Match(buf)
}

// cellAspect is how much taller than wide a cell is assumed to be when the
// terminal does not report its pixel size.
const cellAspect = 2

// fit is the size in cells of an imgW×imgH picture: its natural size, or
// scaled down to the box keeping its shape.
func fit(imgW, imgH, boxCols, boxRows, cellW, cellH int) (cols, rows int) {
	if imgW <= 0 || imgH <= 0 {
		return 1, 1
	}

	if cellW <= 0 || cellH <= 0 {
		cols = boxCols
		rows = imgH * cols / (imgW * cellAspect)

		if rows > boxRows {
			rows = boxRows
			cols = imgW * rows * cellAspect / imgH
		}

		return max(min(cols, boxCols), 1), max(min(rows, boxRows), 1)
	}

	cols = (imgW + cellW - 1) / cellW
	rows = (imgH + cellH - 1) / cellH

	if cols <= boxCols && rows <= boxRows {
		return max(cols, 1), max(rows, 1)
	}

	if cols*boxRows > rows*boxCols {
		rows = rows * boxCols / cols
		cols = boxCols
	} else {
		cols = cols * boxRows / rows
		rows = boxRows
	}

	return max(min(cols, boxCols), 1), max(min(rows, boxRows), 1)
}
