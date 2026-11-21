// Package fuzzy is the matcher every list that filters as you type shares:
// the picker's sources, and what the command line lists under its input.
package fuzzy

import (
	"math"
	"sort"
	"strings"
	"unicode"
)

// Fuzzy matching, the way a picker wants it: the query's characters must appear
// in order somewhere in the candidate, and the closer they sit to each other
// and to the starts of words, the better the match scores.
//
// This is fzf's algorithm — Smith-Waterman with affine gaps, scoring every
// alignment and keeping the best. The one-pass leftmost version this replaced
// was smaller but took the first alignment it found rather than the good one,
// which is not a tuning problem: for the query "git.go" it matched the g and i
// of "plugins", spent the rest of the query on the tail of the path, and put
// plugins/term/term_test.go above plugins/git/git.go. No bonus fixes that, only
// looking at the other alignment does.
const (
	scoreMatch       = 16 // every matched character is worth having
	bonusConsecutive = 16 // ...and as much again next to the last one
	bonusWordStart   = 10 // after a separator, or a capital in camelCase
	penaltyGapStart  = 3  // opening a run of skipped characters
	penaltyGapExtend = 1  // and each one after that
	penaltySkipped   = 1  // per character skipped before the first match
	firstCharBonus   = 2  // the query's first character counts its bonus twice
)

// BonusConsecutive and BonusWordStart are what a matched letter is worth for
// sitting next to the last and for starting a word, for a list that adds its
// own boosts and wants them on the same scale.
const (
	BonusConsecutive = bonusConsecutive
	BonusWordStart   = bonusWordStart
)

// unreachable is a score no alignment can have, for the cells of the table that
// have none. Halved so adding a bonus to it cannot overflow.
const unreachable = math.MinInt / 2

// Score is how well text matches query, and whether it matches at all. An
// empty query matches everything with a score of zero, which leaves the
// candidates in the order they came.
func Score(query, text string) (int, bool) {
	return newMatcher(query).score(text)
}

// A matcher is one query against many candidates. The query is lowered once and
// the working tables are kept and grown rather than allocated per candidate:
// this runs over every path in the project on every keystroke, and the
// allocation was most of what it cost.
type matcher struct {
	q     []rune
	runes []rune // the candidate, and the same lowered
	lower []rune
	bonus []int // what matching at each position is worth beyond the match
	row   []int // the table's current row, and the one above it
	prev  []int
}

func newMatcher(query string) *matcher {
	return &matcher{
		q: []rune(strings.ToLower(query)),
	}
}

// score is the best alignment of the query in text, and whether there is one.
func (m *matcher) score(text string) (int, bool) {
	if len(m.q) == 0 {
		return 0, true
	}

	m.runes, m.lower = m.runes[:0], m.lower[:0]

	for _, r := range text {
		m.runes = append(m.runes, r)
		m.lower = append(m.lower, unicode.ToLower(r))
	}

	// the window worth looking at: from the first place the query could start
	// to the last place it could end. Everything outside it is in no alignment,
	// and for a long path that is most of it
	first, last, ok := m.window()
	if !ok {
		return 0, false
	}

	lower := m.lower[first:last]

	m.bonus = grow(m.bonus, len(lower))

	for j := range lower {
		m.bonus[j] = 0
		if isWordStart(m.runes, first+j) {
			m.bonus[j] = bonusWordStart
		}
	}

	m.row = grow(m.row, len(lower))
	m.prev = grow(m.prev, len(lower))

	for i := range m.q {
		// row holds the best score of an alignment of the query so far whose
		// last character sits at j; two rows are enough, since a cell only ever
		// looks at the one above it
		m.row, m.prev = m.prev, m.row

		// gap is the same for alignments that reached j by skipping — affine,
		// so a long skip costs less per character than several short ones
		gap := unreachable

		for j := range lower {
			if j >= 2 {
				gap = max(gap-penaltyGapExtend, m.prev[j-2]-penaltyGapStart)
			}

			if lower[j] != m.q[i] {
				m.row[j] = unreachable
				continue
			}

			switch {
			case i == 0:
				// no predecessor, and where the query's first character lands
				// says most about whether this is the candidate meant — so its
				// bonus counts double, and the further in it sits the less it
				// is worth
				m.row[j] = scoreMatch + m.bonus[j]*firstCharBonus - min(first+j, 20)*penaltySkipped
			case j == 0:
				m.row[j] = unreachable // nothing before it to have matched
			default:
				// next to the last match, or after a gap — a run of characters
				// typed as they appear is the strongest thing there is, so it
				// outweighs the word start it might otherwise have earned
				m.row[j] = max(
					m.prev[j-1]+max(m.bonus[j], bonusConsecutive),
					gap+m.bonus[j],
				) + scoreMatch
			}
		}
	}

	best := unreachable

	for _, s := range m.row {
		best = max(best, s)
	}

	if best == unreachable {
		return 0, false
	}

	// a short candidate matching the whole query beats a long one that buries it
	return best - len(m.runes)/8, true
}

// window is the stretch of the candidate any alignment lies in: from where the
// query's first character first appears to where its last one last does. It is
// also the match test — a query that cannot be walked through in order is not
// in there at all.
func (m *matcher) window() (first, last int, ok bool) {
	qi := 0

	for i, r := range m.lower {
		if r == m.q[qi] {
			if qi == 0 {
				first = i
			}

			qi++
			if qi == len(m.q) {
				break
			}
		}
	}

	if qi < len(m.q) {
		return 0, 0, false
	}

	for i := len(m.lower) - 1; i >= first; i-- {
		if m.lower[i] == m.q[len(m.q)-1] {
			return first, i + 1, true
		}
	}

	return 0, 0, false
}

// grow returns a slice of that length, reusing the one given when it is big
// enough. The contents are whatever was there before: every caller writes each
// cell before reading it.
func grow(s []int, n int) []int {
	if cap(s) >= n {
		return s[:n]
	}

	return make([]int, n)
}

// isWordStart reports whether the rune at i begins a word: the first character,
// one after a separator, or a capital in the middle of camelCase.
func isWordStart(t []rune, i int) bool {
	if i == 0 {
		return true
	}

	prev := t[i-1]

	switch {
	case prev == '/' || prev == '\\' || prev == '_' || prev == '-' ||
		prev == '.' || prev == ' ' || prev == ':':
		return true
	case unicode.IsLower(prev) && unicode.IsUpper(t[i]):
		return true
	}

	return false
}

// Filter keeps the candidates that match, best first. Ties keep the order
// they were given in, so a source's own idea of importance survives.
func Filter[T any](query string, items []T, text func(T) string) []T {
	ranked, _ := Rank(query, items, text, nil)
	return ranked
}

// Rank is Filter with a bonus added to each candidate's score, which
// also returns the candidates that matched in the order they were given. A
// query that only grows can only lose candidates, so those are all the next
// keystroke has to look at.
func Rank[T any](query string, items []T, text func(T) string, bonus func(T) int) (ranked, matched []T) {
	type scored struct {
		item  T
		score int
	}

	m := newMatcher(query)

	var out []scored

	for _, it := range items {
		score, ok := m.score(text(it))
		if !ok {
			continue
		}

		if bonus != nil {
			score += bonus(it)
		}

		out = append(out, scored{
			it,
			score,
		})
	}

	matched = make([]T, 0, len(out))

	for _, s := range out {
		matched = append(matched, s.item)
	}

	sort.SliceStable(out, func(a, b int) bool {
		return out[a].score > out[b].score
	})

	ranked = make([]T, 0, len(out))

	for _, s := range out {
		ranked = append(ranked, s.item)
	}

	return ranked, matched
}

// Positions are the characters of text, by rune, that a query's characters
// land on, for a list to pick them out: each query character taken at the
// first place it fits, a word start ahead preferred to a letter in the middle
// of one. It is for showing a match, not scoring it, and may light up other
// letters than the alignment Score chose; nil when the query does not match.
func Positions(query, text string) []int {
	q := []rune(strings.ToLower(query))
	t := []rune(text)
	lower := []rune(strings.ToLower(text))
	out := make([]int, 0, len(q))
	from := 0

	for qi, c := range q {
		at := -1

		for i := from; i < len(lower); i++ {
			if lower[i] != c {
				continue
			}

			if at < 0 {
				at = i
			}

			// a word start is taken over an earlier mid-word match, as long
			// as the rest of the query still fits after it
			if isWordStart(t, i) && fits(q[qi+1:], lower[i+1:]) {
				at = i
				break
			}
		}

		if at < 0 {
			return nil
		}

		out = append(out, at)
		from = at + 1
	}

	return out
}

// fits says whether q can be walked through t in order.
func fits(q, t []rune) bool {
	i := 0

	for _, r := range t {
		if i < len(q) && r == q[i] {
			i++
		}
	}

	return i == len(q)
}
