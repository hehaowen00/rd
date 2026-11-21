package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Position is where a book was left: a chapter, and how far through it.
type Position struct {
	Chapter  int     `json:"chapter"`
	Fraction float64 `json:"fraction"`
	Progress float64 `json:"progress"` // through the whole book, 0-1
}

// Positions are the saved reading positions, keyed by absolute file path.
type Positions struct {
	file string
	m    map[string]Position
}

func LoadPositions() *Positions {
	p := &Positions{m: map[string]Position{}}

	dir := stateDir()
	if dir == "" {
		return p
	}

	p.file = filepath.Join(dir, "positions.json")

	if data, err := os.ReadFile(p.file); err == nil {
		_ = json.Unmarshal(data, &p.m)
	}

	return p
}

// stateDir is where the reader keeps what it remembers between runs.
func stateDir() string {
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}

		dir = filepath.Join(home, ".local", "state")
	}

	return filepath.Join(dir, "reader")
}

func (p *Positions) Get(path string) (Position, bool) {
	pos, ok := p.m[path]
	return pos, ok
}

// Set records a position and writes the file only if it changed.
func (p *Positions) Set(path string, pos Position) error {
	if old, ok := p.m[path]; ok && old == pos {
		return nil
	}

	p.m[path] = pos

	if p.file == "" {
		return nil
	}

	data, err := json.MarshalIndent(p.m, "", "  ")
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(p.file), 0o755); err != nil {
		return err
	}

	tmp := p.file + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}

	return os.Rename(tmp, p.file)
}
