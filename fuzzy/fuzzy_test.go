package fuzzy

import (
	"fmt"
	"testing"
)

// TestScore: the query's characters must appear in order, and the
// closer-together, earlier, word-start matches score higher.
func TestScore(t *testing.T) {
	cs := []struct {
		query, text string
		want        bool
	}{
		{
			"",
			"anything",
			true,
		},
		{
			"abc",
			"abc",
			true,
		},
		{
			"abc",
			"a_b_c",
			true,
		},
		{
			"abc",
			"xxaxxbxxc",
			true,
		},
		{
			"abc",
			"acb",
			false,
		},
		{
			"abc",
			"ab",
			false,
		},
		{
			"ED",
			"editor",
			true,
		}, // matching ignores case
	}
	for _, c := range cs {
		_, ok := Score(c.query, c.text)
		if ok != c.want {
			t.Errorf("Score(%q, %q) matched=%v, want %v", c.query, c.text, ok, c.want)
		}
	}

	better := func(query, win, lose string) {
		t.Helper()
		a, okA := Score(query, win)
		b, okB := Score(query, lose)
		if !okA || !okB {
			t.Fatalf("%q should match both %q and %q", query, win, lose)
		}

		if a <= b {
			t.Errorf("%q: %q scored %d, %q scored %d — the first should win", query, win, a, lose, b)
		}
	}
	better("abc", "abc", "a_b_c")                                  // together beats spread out
	better("ed", "editor.go", "shredder.go")                       // at the front beats buried
	better("ff", "find_file", "offfff")                            // word starts beat the middle of a word
	better("main", "main.go", "internal/main_helpers_and_more.go") // short beats long
}

// TestFilter: matches only, best first, and ties keep their order.
func TestFilter(t *testing.T) {
	items := []string{"internal/editor/editor.go", "README.md", "editor_test.go", "main.go"}
	got := Filter("edt", items, func(s string) string {
		return s
	})
	if len(got) != 2 {
		t.Fatalf("matches: %v", got)
	}

	if got[0] != "editor_test.go" {
		t.Fatalf("best match %q, want editor_test.go", got[0])
	}

	// an empty query keeps everything, in order
	got = Filter("", items, func(s string) string {
		return s
	})
	if len(got) != len(items) || got[0] != items[0] {
		t.Fatalf("empty query: %v", got)
	}
}

// TestFuzzyPrefersTheBestAlignment: the query's characters are looked for in
// the best place they fit, not the first. Taking the leftmost match instead put
// every *_test.go above plugins/git/git.go for the query "git.go", by spending
// the g and i on "plugins" and the rest on the tail of the path.
func TestFuzzyPrefersTheBestAlignment(t *testing.T) {
	paths := []string{
		"plugins/term/term_test.go",
		"plugins/todo/find_test.go",
		"plugins/calc/calc_test.go",
		"editor/plugin_api_test.go",
		"plugins/git/git.go",
		"plugins/git/draw.go",
	}

	got := Filter("git.go", paths, func(s string) string {
		return s
	})
	if len(got) == 0 || got[0] != "plugins/git/git.go" {
		t.Fatalf("best match %v, want plugins/git/git.go first", got)
	}

	// the same for a query that is a run inside a longer name
	got = Filter("picker", []string{"plugins/picker/picker.go", "plugins/calc/calc/calc_test.go"},
		func(s string) string {
			return s
		})
	if got[0] != "plugins/picker/picker.go" {
		t.Fatalf("best match %q", got[0])
	}
}

func BenchmarkFilter(b *testing.B) {
	var paths []string

	for i := range 20000 {
		paths = append(paths, fmt.Sprintf("plugins/pkg%d/internal/some_long_file_name_%d.go", i%40, i))
	}

	b.ResetTimer()

	for b.Loop() {
		Filter("git.go", paths, func(s string) string {
			return s
		})
	}
}
