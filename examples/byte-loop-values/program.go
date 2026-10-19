// Package byteloopvalues is an ordinary-Go oracle for hand-written byte
// loops in the on-chain subset: fixed-length compares that stop at the first
// difference (modes 0 to 4), which get a word-at-a-time path on SBF, and
// little-endian and big-endian loads that OR shifted bytes into an integer
// (modes 5 to 9), which keep the plain loop. Operands are slices, arrays and
// array pointers, long enough, too short (a bounds trap, unless an earlier
// byte already decided) or nil. The same source runs natively and through
// goSVM; tests compare output bytes and status (or a panic) for every vector.
// See docs/GO_ARRAYS.md#hand-written-byte-loops.
//
// Run(out, in) reads a mode from in[0], two operand lengths from in[1] and
// in[2], a nil flag from in[3] and operand bytes from in[8:40] and in[40:72].
package byteloopvalues

// less32 orders two arrays like Rust's derived Ord: 1 less, 2 greater, 3
// equal.
func less32(a, b [32]byte) uint64 {
	for i := 0; i < 32; i++ {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return 1
			}
			return 2
		}
	}
	return 3
}

// prefix8 compares a slice with an array behind a pointer.
func prefix8(b []byte, d *[8]byte) bool {
	for i := 0; i < 8; i++ {
		if b[i] != d[i] {
			return false
		}
	}
	return true
}

// equal32 compares two slices.
func equal32(a, b []byte) bool {
	for i := 0; i < 32; i++ {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// firstDifference compares bytes 4..12 and reports the first index that
// differs, or 99.
func firstDifference(a, b []byte) uint64 {
	for i := 4; i < 13; i++ {
		if a[i] != b[i] {
			return uint64(i)
		}
	}
	return 99
}

// equalSigned compares int8 arrays; the if body counts entries.
func equalSigned(a, b [16]int8, entered *uint64) bool {
	same := true
	for i := uint8(0); i < 16; i++ {
		if a[i] != b[i] {
			*entered = *entered + 1
			same = false
			break
		}
	}
	return same
}

func le64(b []byte) uint64 {
	v := uint64(0)
	for j := 0; j < 8; j++ {
		v |= uint64(b[j]) << (8 * uint64(j))
	}
	return v
}

func le32(b [4]byte) uint32 {
	var v uint32
	for i := 0; i < 4; i++ {
		v = v | uint32(b[i])<<(i*8)
	}
	return v
}

// rate and threshold side by side, as sdk/system reads the Rent sysvar.
func pair(b []byte) (uint64, uint64) {
	r, t := uint64(0), uint64(0)
	for j := uint64(0); j < 8; j++ {
		r = r | uint64(b[j])<<(j*8)
		t = t | uint64(b[8+j])<<(j*8)
	}
	return r, t
}

// le24 loads three bytes through a pointer into a nonzero accumulator.
func le24(b *[3]byte) uint32 {
	v := uint32(0xab000000)
	for i := 0; i < 3; i++ {
		v |= uint32(b[i]) << (8 * i)
	}
	return v
}

// le16 loads eight bytes into 16 bits: the upper six shift out.
func le16(b []byte) uint16 {
	var v uint16
	for i := 0; i < 8; i++ {
		v |= uint16(b[i+5]) << (8 * i)
	}
	return v
}

func be64(b []byte) uint64 {
	v := uint64(0)
	for i := 0; i < 8; i++ {
		v |= uint64(b[i]) << (56 - 8*i)
	}
	return v
}

func put(out []byte, at int, v uint64) {
	for i := 0; i < 8; i++ {
		out[at+i] = byte(v >> (8 * uint64(i)))
	}
}

func Run(out, in []byte) uint64 {
	if len(in) < 72 {
		return 1
	}
	a := in[8 : 8+int(in[1]%33)]
	b := in[40 : 40+int(in[2]%33)]
	var x, y [32]byte
	copy(x[:], in[8:40])
	copy(y[:], in[40:72])
	switch in[0] {
	case 0:
		put(out, 0, less32(x, y))
	case 1:
		var d [8]byte
		copy(d[:], in[40:48])
		p := &d
		if in[3]&1 != 0 {
			p = nil
		}
		if prefix8(a, p) {
			put(out, 0, 1)
		}
	case 2:
		if equal32(a, b) {
			put(out, 0, 1)
		}
	case 3:
		put(out, 0, firstDifference(a, b))
	case 4:
		var s, t [16]int8
		for i := 0; i < 16; i++ {
			s[i], t[i] = int8(in[8+i]), int8(in[40+i])
		}
		var entered uint64
		if equalSigned(s, t, &entered) {
			put(out, 0, 1)
		}
		put(out, 8, entered)
	case 5:
		put(out, 0, le64(a))
	case 6:
		var w [4]byte
		copy(w[:], in[8:12])
		put(out, 0, uint64(le32(w)))
	case 7:
		r, t := pair(a)
		put(out, 0, r)
		put(out, 8, t)
	case 8:
		var w [3]byte
		copy(w[:], in[8:11])
		p := &w
		if in[3]&1 != 0 {
			p = nil
		}
		put(out, 0, uint64(le24(p)))
		put(out, 8, uint64(le16(a)))
	case 9:
		put(out, 0, be64(a))
	}
	return 0
}

// Process is the byte-slice entrypoint: s is the output, i the input.
func Process(s, i []byte) uint64 { return Run(s, i) }
