// Package controlvalues is an ordinary-Go oracle for returns and control flow.
package controlvalues

type Status uint64

const Failed Status = 9

type Key [4]byte
type Box struct {
	Key Key
	N   uint64
}

func next(s []byte) byte              { s[0] = s[0] + 1; return s[0] }
func pair(s []byte) (uint64, byte)    { return uint64(next(s)), next(s) }
func forward(s []byte) (uint64, byte) { return pair(s) }
func consume(a uint64, b byte) uint64 { return a*256 + uint64(b) }
func boxes(k Key, n uint64) (Box, Key, Status) {
	b := Box{Key: k, N: n}
	k[0] = k[0] + 1
	return b, k, 0
}
func put(s []byte, off, n uint64) {
	for j := uint64(0); j < 8; j++ {
		s[off+j] = byte(n >> (j * 8))
	}
}
func Process(s, i []byte) uint64 {
	s[0] = i[2]
	// RHS calls run before any assignment; bounds failure can follow the first
	// committed local write. Actual SBF must still roll the transaction back.
	if i[0] == 1 {
		s[4], s[uint64(i[1])] = next(s), next(s)
		return 0
	}
	if i[0] == 2 {
		var a Key
		s[4], a[uint64(i[1])] = next(s), next(s)
		return uint64(a[0])
	}
	if i[0] == 3 {
		s[3] = next(s)
		return uint64(Failed)
	}
	a, b := pair(s)
	a, c := a+uint64(next(s)), next(s)
	a, b = uint64(b), byte(a)
	var d, e = forward(s)
	var zero, tail uint64
	zero, tail = tail, zero
	total := consume(pair(s)) + d + uint64(e) + a + uint64(b) + uint64(c) + zero + tail
	// Capture indexes before RHSs; swaps use saved old values.
	pos := uint64(i[1]) % 4
	k := Key{1, 2, 3, 4}
	pos, k[pos] = 3, byte(next(s))
	original := k
	box, copied, status := boxes(k, uint64(i[3]))
	box.Key[0] = byte(next(s))
	copied[1] = byte(next(s))
	if k != original || status != 0 {
		return 101
	}
	_, _ = pair(s)
	total = total + box.N + uint64(box.Key[0]) + uint64(copied[0]) + uint64(copied[1]) + pos
	// A matching case skips later case calls, including multi-expression cases.
	switch value := next(s); value {
	case next(s), next(s):
		total = total + 500
	default:
		total = total + uint64(next(s))
	case byte(value + 3):
		total = total + 700
	}
	switch uint64(i[5]) % 3 {
	case uint64(next(s)) % 3:
		total = total + 31
	case uint64(next(s)) % 3, uint64(next(s)) % 3:
		total = total + 37
	default:
		total = total + 41
	}
	switch {
	case i[4] == 0:
		total = total + 11
	case i[4] < 128:
		total = total + 13
	default:
		total = total + 17
	}
	switch original {
	case Key{}:
		total = total + 19
	default:
		total = total + 23
	}
	// Nested continue targets the inner loop; continue in switch targets the
	// outer loop and must execute its post statement exactly once.
	for j := uint64(0); j < 8; j++ {
		switch j {
		case 1, 3:
			continue
		case 5:
			break
		default:
			total = total + j
		}
		for inner := uint64(0); inner < 3; inner++ {
			if inner == 1 {
				continue
			}
			total = total + inner
		}
	}
	for remaining := uint64(3); remaining != 0; remaining = remaining - 1 {
		if remaining == 2 {
			continue
		}
		total = total + remaining
	}
	s[1], s[2] = s[2], s[1]
	put(s, 8, total)
	put(s, 16, uint64(s[0]))
	return 0
}
