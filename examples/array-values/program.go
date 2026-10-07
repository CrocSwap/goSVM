// Package arrayvalues exercises the experimental scalar-array subset. It is a
// compiler fixture, not a swap program or a generated schema-2 application.
package arrayvalues

type Key [32]byte
type OtherKey [32]byte
type Small byte
type Flags [3]bool
type Box struct {
	Key  Key
	Tail uint64
}

func bump(s []byte) byte {
	s[0] = s[0] + 1
	return s[0]
}

func keyLiteral(s []byte) Key {
	// Evaluate values in lexical order, not index order; leave gaps zeroed.
	return Key{31: bump(s), 0: bump(s), bump(s)}
}

func change(k Key, v byte) Key {
	k[0] = v
	k[31] = k[31] + 1
	return k
}

func empty(s []byte) [0]byte {
	bump(s)
	return [0]byte{}
}

func raw(k [32]byte) [32]byte { return k }

// Process uses the legacy 24-byte state / 16-byte instruction adapter. The
// instruction selects normal semantics (0), bounds failures (1/2/3), or a
// deliberate handler error (4), or full-width indexing (5).
// Every successful checksum derives from arrays.
func Process(s, i []byte) uint64 {
	position := uint64(i[1])
	s[0] = i[2]
	var zero Key
	if i[0] == 1 {
		s[3] = 17
		zero[position] = bump(s)
		return 0
	}
	if i[0] == 2 {
		s[3] = 19
		s[4] = zero[position]
		return 0
	}
	if i[0] == 3 {
		var z [0]byte
		s[3] = 23
		z[position] = bump(s)
		return 0
	}
	if i[0] == 4 {
		s[3] = 29
		zero[position] = bump(s)
		return 9
	}
	if i[0] == 5 {
		position = 0
		for j := uint64(0); j < 8; j++ {
			position = position | uint64(i[8+j])<<(j*8)
		}
		s[3] = 31
		zero[position] = bump(s)
		return 0
	}
	original := keyLiteral(s)
	copy := change(original, i[3])
	converted := OtherKey(original)
	anonymous := raw([32]byte(original))
	if converted != OtherKey(original) || anonymous != [32]byte(original) || original == copy {
		return 1
	}
	var allZero [32]byte
	if zero != Key(allZero) {
		return 2
	}
	short := [...]uint32{2: uint32(bump(s)), 0: uint32(bump(s)), uint32(bump(s))}
	wide := [2]uint64{uint64(i[4]), 18446744073709551615}
	wide[0] = wide[0] + wide[1]
	narrow := [2]Small{Small(i[5]), 255}
	narrow[0] = narrow[0] + narrow[1]
	flags := Flags{true, false, true}
	flags[uint64(i[6])%3] = i[7] != 0
	box := Box{Key: original, Tail: uint64(i[8])}
	saved := box
	box.Key[position] = i[9]
	if saved.Key != original || original[31] != byte(i[2]+1) {
		return 3
	}
	// Calls in nonconstant len/cap operands must still run, even for length zero.
	n := uint64(len(empty(s))) + uint64(cap(empty(s)))
	n = n + uint64(len(original)) + uint64(cap(original)) + uint64(len(short))
	n = n + uint64(original[0]) + uint64(original[1]) + uint64(original[31])
	n = n + uint64(copy[0]) + uint64(copy[31]) + uint64(box.Key[position]) + saved.Tail
	n = n + uint64(short[0]) + uint64(short[1]) + uint64(short[2]) + wide[0] + uint64(narrow[0])
	if flags[0] {
		n = n + 7
	}
	if flags[1] {
		n = n + 11
	}
	if flags[2] {
		n = n + 13
	}
	s[1] = copy[0]
	s[2] = box.Key[position]
	s[5] = byte(short[2])
	s[6] = byte(narrow[0])
	for j := uint64(0); j < 8; j++ {
		s[8+j] = byte(n >> (j * 8))
	}
	return 0
}
