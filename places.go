package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Places are folders saved to jump back to. They are kept one path to a line
// in $XDG_STATE_HOME/reader/places, so the file can be edited by hand; a line
// starting with # is a note.

func placesFile() string {
	dir := stateDir()
	if dir == "" {
		return ""
	}

	return filepath.Join(dir, "places")
}

// LoadPlaces is the saved list, in the order it is written down. A folder
// that is missing stays in it: a drive that is not plugged in is still a place.
func LoadPlaces() []string {
	data, err := os.ReadFile(placesFile())
	if err != nil {
		return nil
	}

	var out []string

	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || slices.Contains(out, line) {
			continue
		}

		out = append(out, line)
	}

	return out
}

func SavePlaces(dirs []string) error {
	file := placesFile()
	if file == "" {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}

	data := strings.Join(dirs, "\n")
	if data != "" {
		data += "\n"
	}

	tmp := file + ".tmp"
	if err := os.WriteFile(tmp, []byte(data), 0o644); err != nil {
		return err
	}

	return os.Rename(tmp, file)
}

func isDir(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

// maxBookScan bounds the walk for the book finder, so starting it in a huge
// tree such as / stays quick.
const maxBookScan = 100_000

var skipDirs = []string{"node_modules", "vendor", "target", "dist", "build", "__pycache__"}

// findBooks lists the EPUBs under root, as paths relative to it, shallower
// first. Hidden folders are skipped unless hidden is set.
func findBooks(root string, hidden bool) []string {
	var out []string

	seen := 0

	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		seen++
		if seen > maxBookScan {
			return filepath.SkipAll
		}

		if err != nil {
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}

			return nil
		}

		name := d.Name()

		if d.IsDir() {
			if path != root && (!hidden && strings.HasPrefix(name, ".") || slices.Contains(skipDirs, name)) {
				return filepath.SkipDir
			}

			return nil
		}

		if (hidden || !strings.HasPrefix(name, ".")) && isBookFile(name) {
			if rel, err := filepath.Rel(root, path); err == nil {
				out = append(out, rel)
			}
		}

		return nil
	})

	slices.SortStableFunc(out, func(a, b string) int {
		return depthOf(a) - depthOf(b)
	})

	return out
}

func depthOf(rel string) int { return strings.Count(rel, string(filepath.Separator)) }

// found is one result of the finder: a book, or a folder with books in it.
type found struct {
	rel string
	dir bool
}

// findShelves is findBooks with the folders that hold them, at any depth,
// listed too: each folder before the books, shallower first.
func findShelves(root string, hidden bool) []found {
	books := findBooks(root, hidden)
	seen := map[string]bool{}

	var dirs []found

	for _, b := range books {
		for d := filepath.Dir(b); d != "." && !seen[d]; d = filepath.Dir(d) {
			seen[d] = true
			dirs = append(dirs, found{rel: d, dir: true})
		}
	}

	slices.SortStableFunc(dirs, func(a, b found) int {
		if n := depthOf(a.rel) - depthOf(b.rel); n != 0 {
			return n
		}

		return strings.Compare(strings.ToLower(a.rel), strings.ToLower(b.rel))
	})

	out := dirs
	for _, b := range books {
		out = append(out, found{rel: b})
	}

	return out
}
