package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestPlacesRoundTrip(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	if got := LoadPlaces(); len(got) != 0 {
		t.Fatalf("fresh LoadPlaces = %v", got)
	}

	if err := SavePlaces([]string{"/a", "/b"}); err != nil {
		t.Fatal(err)
	}

	if got := LoadPlaces(); !slices.Equal(got, []string{"/a", "/b"}) {
		t.Errorf("LoadPlaces = %v", got)
	}
}

func TestFindBooks(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{"a.epub", "x/b.EPUB", "x/notes.txt", ".hidden/c.epub", "node_modules/d.epub"} {
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(full, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	want := []string{"a.epub", filepath.Join("x", "b.EPUB")}
	if got := findBooks(root, false); !slices.Equal(got, want) {
		t.Errorf("findBooks = %v, want %v", got, want)
	}
}

func TestFindShelves(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{"a.epub", "x/y/b.epub", "x/z/c.epub", "empty/notes.txt"} {
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(full, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var got []string
	for _, f := range findShelves(root, false) {
		s := f.rel
		if f.dir {
			s += "/"
		}

		got = append(got, filepath.ToSlash(s))
	}

	want := []string{"x/", "x/y/", "x/z/", "a.epub", "x/y/b.epub", "x/z/c.epub"}
	if !slices.Equal(got, want) {
		t.Errorf("findShelves = %v, want %v", got, want)
	}
}
