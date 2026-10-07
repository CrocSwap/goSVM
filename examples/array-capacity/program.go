// Package arraycapacity exercises the 1 KiB per-array boundary with real copies.
package arraycapacity

func position(i []byte) uint64 { return uint64(i[1]) | uint64(i[2])<<8 }
func probe(i []byte) uint64    { return uint64(i[5]) | uint64(i[6])<<8 }
func word(i []byte) uint64 {
	n := uint64(0)
	for j := uint64(0); j < 8; j++ {
		n = n | uint64(i[8+j])<<(j*8)
	}
	return n
}
func put(s []byte, off, value uint64) {
	for j := uint64(0); j < 8; j++ {
		s[off+j] = byte(value >> (j * 8))
	}
}
func bytes(s, i []byte) {
	var a [1024]byte
	a[position(i)] = i[3]
	copy := a
	a[position(i)] = i[4]
	put(s, 0, uint64(copy[position(i)]))
	put(s, 8, uint64(a[position(i)]))
	put(s, 16, uint64(copy[probe(i)]))
}
func words(s, i []byte) {
	var a [128]uint64
	a[position(i)] = word(i)
	copy := a
	a[position(i)] = a[position(i)] + 1
	put(s, 0, copy[position(i)])
	put(s, 8, a[position(i)])
	put(s, 16, copy[probe(i)])
}
func smallWords(s, i []byte) {
	var a [256]uint32
	a[position(i)] = uint32(word(i))
	copy := a
	a[position(i)] = a[position(i)] + 1
	put(s, 0, uint64(copy[position(i)]))
	put(s, 8, uint64(a[position(i)]))
	put(s, 16, uint64(copy[probe(i)]))
}
func flags(s, i []byte) {
	var a [1024]bool
	a[position(i)] = i[3] != 0
	copy := a
	a[position(i)] = !a[position(i)]
	s[0] = 0
	s[1] = 0
	s[2] = 0
	if copy[position(i)] {
		s[0] = 1
	}
	if a[position(i)] {
		s[1] = 1
	}
	if copy[probe(i)] {
		s[2] = 1
	}
}

// Check is the native oracle. SBF verification supplies one entrypoint per
// element kind so unrelated 1 KiB locals are not combined by LLVM inlining.
func Check(s, i []byte) uint64 {
	// A write before a possible array fault must roll back in SBF tests.
	s[3] = 37
	if i[0] == 0 {
		bytes(s, i)
	} else if i[0] == 1 {
		words(s, i)
	} else if i[0] == 2 {
		smallWords(s, i)
	} else {
		flags(s, i)
	}
	return 0
}
