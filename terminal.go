package main

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// The terminal is asked what it can do and what it is themed to before tcell
// takes the keyboard, since the replies arrive on the same input. OSC 10, 11
// and 4 ask for the foreground, the background and the sixteen; a DA1 request
// follows every question, and terminals answer in order, so the DA1 reply
// marks the end. Without it there is no knowing when the last answer has
// arrived, and an answer read after the tty goes back to normal is echoed
// onto the shell prompt on the way out.
func terminalQuery() string {
	var q strings.Builder
	q.WriteString(kittyQuery + "\x1b]10;?\x07\x1b]11;?\x07")

	for i := range 16 {
		fmt.Fprintf(&q, "\x1b]4;%d;?\x07", i)
	}

	q.WriteString("\x1b[c")

	return q.String()
}

var da1Reply = regexp.MustCompile(`\x1b\[\?[0-9;]*c`)

// askTerminal sends terminalQuery and returns everything the terminal
// answered, or nothing when its answers did not all arrive in time.
func askTerminal(timeout time.Duration) []byte {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil
	}

	defer tty.Close()

	fd := int(tty.Fd())

	old, err := term.MakeRaw(fd)
	if err != nil {
		return nil
	}

	defer term.Restore(fd, old)

	if _, err := tty.WriteString(terminalQuery()); err != nil {
		return nil
	}

	// select(2) rather than a read deadline: a deadline on a tty is not
	// supported everywhere, and where it is not the read fails at once,
	// leaving the answers in the buffer to land on the shell later.
	deadline := time.Now().Add(timeout)
	var buf []byte
	chunk := make([]byte, 4096)

	for !da1Reply.Match(buf) {
		left := time.Until(deadline)
		if left <= 0 {
			return nil
		}

		fds := &unix.FdSet{}
		fds.Set(fd)
		tv := unix.NsecToTimeval(int64(left))

		n, err := unix.Select(fd+1, fds, nil, nil, &tv)
		if err == unix.EINTR {
			continue
		}

		if err != nil || n == 0 {
			return nil
		}

		read, err := tty.Read(chunk)
		buf = append(buf, chunk[:read]...)

		if err != nil {
			return nil
		}
	}

	return buf
}

// The two defaults sit outside the numbered slots: the answer for the default
// foreground and the one for palette slot 10 come back under the same digits.
const (
	slotFg = -1
	slotBg = -2
)

// termColours is what the terminal said: the palette slots by number, and the
// two defaults under slotFg and slotBg.
type termColours map[int]tcell.Color

// oscReply matches one answer: "10;rgb:...", "11;rgb:..." or "4;7;rgb:...".
// The channels come back in 1 to 4 hex digits, whichever the terminal likes.
var oscReply = regexp.MustCompile(`\x1b\]([0-9]{1,2});(?:([0-9]{1,2});)?` +
	`rgb:([0-9a-fA-F]+)/([0-9a-fA-F]+)/([0-9a-fA-F]+)`)

// parseColours reads every colour answer in what the terminal sent back.
func parseColours(buf []byte) termColours {
	out := termColours{}

	for _, m := range oscReply.FindAllStringSubmatch(string(buf), -1) {
		var key int

		switch m[1] {
		case "4":
			n, err := strconv.Atoi(m[2])
			if err != nil || n > 15 {
				continue
			}

			key = n
		case "10":
			key = slotFg
		case "11":
			key = slotBg
		default:
			continue
		}

		out[key] = tcell.NewRGBColor(hexChannel(m[3]), hexChannel(m[4]), hexChannel(m[5]))
	}

	return out
}

// hexChannel is one channel of an "rgb:" answer scaled to 8 bits from however
// many digits the terminal used: ffff, fff, ff and f all mean full.
func hexChannel(s string) int32 {
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return 0
	}

	switch len(s) {
	case 1:
		return int32(v) * 17
	case 2:
		return int32(v)
	case 3:
		return int32(v >> 4)
	default:
		return int32(v >> 8)
	}
}
