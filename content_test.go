package main

import (
	"strings"
	"testing"
)

// gaps renders blocks as their text, with "" for each blank line between them.
func gaps(t *testing.T, body string) []string {
	t.Helper()

	blocks, err := ParseChapter("c.xhtml", []byte("<html><body>"+body+"</body></html>"))
	if err != nil {
		t.Fatal(err)
	}

	var out []string

	for _, b := range blocks {
		if b.SpaceBefore {
			out = append(out, "")
		}

		var sb strings.Builder
		for _, s := range b.Spans {
			sb.WriteString(s.Text)
		}

		out = append(out, strings.Join(strings.Fields(sb.String()), " "))
	}

	return out
}

func TestParagraphBreaks(t *testing.T) {
	tests := []struct {
		name, body string
		want       []string
	}{
		{"p", "<p>one</p><p>two</p>", []string{"one", "", "two"}},
		{"text-only divs", "<div>one</div><div>two <i>three</i></div>", []string{"one", "", "two three"}},
		{
			"div holding paragraphs adds no extra gap",
			"<div><p>one</p><p>two</p></div><div><p>three</p></div>",
			[]string{"one", "", "two", "", "three"},
		},
		{"double br", "<p>one<br/><br/>two<br/><br/><br/>three</p>", []string{"one", "", "two", "", "three"}},
		{"single br is a line break", "<p>one<br/>two</p>", []string{"one", "two"}},
		{"bare text and double br", "one<br/><br/>two", []string{"one", "", "two"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := gaps(t, tt.body)
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
