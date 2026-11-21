package utils

// Bitset is a set of small non-negative ints. It grows as bits are set.
type Bitset []uint64

func (s Bitset) Has(i int) bool {
	w := i >> 6
	return w < len(s) && s[w]&(1<<(i&63)) != 0
}

func (s *Bitset) Set(i int) {
	w := i >> 6
	for len(*s) <= w {
		*s = append(*s, 0)
	}

	(*s)[w] |= 1 << (i & 63)
}

func (s Bitset) Clear(i int) {
	w := i >> 6
	if w < len(s) {
		s[w] &^= 1 << (i & 63)
	}
}

func (s *Bitset) Toggle(i int) {
	if s.Has(i) {
		s.Clear(i)
	} else {
		s.Set(i)
	}
}
