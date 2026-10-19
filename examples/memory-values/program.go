// Package memoryvalues is an ordinary-Go oracle for bulk memory moves in the
// on-chain subset: copy between and within byte slices (overlapping in both
// directions, exactly aliased, empty, nil, and from strings, the heap and
// read-only data), copy and append on slices of other element types, struct
// and array assignment through pointers that may alias, zeroing, and string
// building, and loops that zero a slice to its end. On the modern SBF
// profiles goSVM hands moves of at least a
// threshold number of bytes to the VM's sol_memmove_ and sol_memset_
// syscalls (docs/GO_ALLOCATION.md#bulk-memory-on-sbf); the same source is
// compiled natively, as host C and for SBF, and tests compare the output
// bytes (or a panic) for every vector.
//
// Run(out, in) reads a mode from in[0]. out is OutBytes long and arrives
// filled with Pattern; the first WorkBytes are the data the modes move, the
// rest hold results. Run returns 0 unless it panics.
package memoryvalues

const (
	OutBytes  = 3072
	WorkBytes = 3008
	InBytes   = 64
	Modes     = 9
)

// Pattern is out's initial byte j.
func Pattern(j uint64) byte { return byte(j*131 + j>>8 + 7) }

// text is read-only data: 64 distinct bytes repeated to 2,048.
const t64 = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ+/"
const t256 = t64 + t64 + t64 + t64
const text = t256 + t256 + t256 + t256 + t256 + t256 + t256 + t256

// table is a read-only package-level array.
var table = [16]uint64{1, 2, 3, 5, 8, 13, 21, 34, 55, 89, 144, 233, 377, 610, 987, 1597}

type Pair struct {
	A uint64
	B uint32
}

type B3 [3]byte
type W5 [5]uint64

// Blocks of the sizes LLVM copies with memcpy/memmove and zeroes with
// memset rather than inline stores.
type Blk47 [47]byte
type Blk64 [64]byte
type Blk100 [100]byte
type Blk333 [333]byte
type Blk1000 [1000]byte
type Wide6 struct {
	W [6]uint64
}
type Wide16 struct {
	W [16]uint64
	T uint32
}
type Wide125 struct {
	W [125]uint64
}

func u16(in []byte, i uint64) uint64 { return uint64(in[i]) | uint64(in[i+1])<<8 }

func put64(b []byte, v uint64) {
	for j := uint64(0); j < 8; j++ {
		b[j] = byte(v >> (8 * j))
	}
}

func get64(b []byte) uint64 {
	v := uint64(0)
	for j := uint64(0); j < 8; j++ {
		v |= uint64(b[j]) << (8 * j)
	}
	return v
}

// Run dispatches on in[0]; in[1] is a variant and in[2:10] four u16
// parameters a, b, c, d.
func Run(out, in []byte) uint64 {
	work := out[:WorkBytes]
	res := out[WorkBytes:]
	a, b, c, d := u16(in, 2), u16(in, 4), u16(in, 6), u16(in, 8)
	switch in[0] {
	case 0:
		moveWithin(work, res, in[1], a, b, c, d)
	case 1:
		moveHeap(work, res, in[1], a, b, c, d)
	case 2:
		moveReadOnly(work, res, in[1], a, b, c)
	case 3:
		moveTyped(work, res, in[1], a, b, c)
	case 4:
		appendMoves(work, res, in[1], a, b, c, d)
	case 5:
		assignBlocks(work, res, in[1], a, b, c)
	case 6:
		zeroBlocks(work, res, in[1], a, b)
	case 7:
		buildStrings(work, res, a, b, c, d)
	case 8:
		zeroLoops(work, res, in[1], a, b, c)
	}
	return 0
}

// moveWithin copies work[s:s+m] to work[dst:dst+n]: overlapping either way,
// exactly aliased, or empty; out-of-range bounds panic before any move.
func moveWithin(work, res []byte, variant byte, dst, s, n, m uint64) {
	var r int
	switch variant % 4 {
	case 0:
		r = copy(work[dst:dst+n], work[s:s+m])
	case 1:
		r = copy(work[dst:], work[s:s+m]) // destination to the end
	case 2:
		v := work[dst : dst+n]
		r = copy(v, v) // exact alias
	case 3:
		var none []byte
		r = copy(work[dst:dst+n], none) + copy(none, work[s:s+m])
	}
	put64(res[0:8], uint64(r))
}

// moveHeap copies between heap slices and work in both directions.
func moveHeap(work, res []byte, variant byte, dst, s, n, m uint64) {
	h := make([]byte, m)
	for j := uint64(0); j < m; j++ {
		h[j] = byte(j*7) ^ variant
	}
	r := 0
	if variant%2 == 0 {
		r = copy(work[dst:dst+n], h[s%(m+1):])
	} else {
		r = copy(h, work[s:s+n])
		r += copy(work[dst:dst+m], h)
	}
	put64(res[0:8], uint64(r))
	put64(res[8:16], uint64(len(h)))
}

// moveReadOnly copies from a string constant and from a []byte conversion.
func moveReadOnly(work, res []byte, variant byte, dst, s, n uint64) {
	r := 0
	switch variant % 3 {
	case 0:
		r = copy(work[dst:dst+n], text[s:])
	case 1:
		r = copy(work[dst:dst+n], text[s%2048:(s+n)%2049])
	case 2:
		var w [16]uint64
		r = copy(w[:], table[s%17:])
		for j := uint64(0); j < 16; j++ {
			put64(work[dst+8*j:dst+8*j+8], w[j])
		}
	}
	put64(res[0:8], uint64(r))
}

// moveTyped decodes a slice of another element type from work, copies it
// over itself (overlapping either way) and encodes it back.
func moveTyped(work, res []byte, variant byte, length, dst, s uint64) {
	length = length % 61
	r := 0
	switch variant % 6 {
	case 0:
		v := make([]uint64, length)
		for j := range v {
			v[j] = get64(work[8*j:])
		}
		r = copy(v[dst%(length+1):], v[s%(length+1):])
		for j := range v {
			put64(work[8*j:], v[j])
		}
	case 1:
		v := make([]uint32, length)
		for j := range v {
			v[j] = uint32(get64(work[4*j:]))
		}
		r = copy(v[dst%(length+1):], v[s%(length+1):])
		for j := range v {
			x := v[j]
			work[4*j], work[4*j+1], work[4*j+2], work[4*j+3] = byte(x), byte(x>>8), byte(x>>16), byte(x>>24)
		}
	case 2:
		v := make([]uint16, length)
		for j := range v {
			v[j] = uint16(work[2*j]) | uint16(work[2*j+1])<<8
		}
		r = copy(v[dst%(length+1):], v[s%(length+1):])
		for j := range v {
			work[2*j], work[2*j+1] = byte(v[j]), byte(v[j]>>8)
		}
	case 3:
		v := make([]B3, length)
		for j := range v {
			copy(v[j][:], work[3*j:])
		}
		r = copy(v[dst%(length+1):], v[s%(length+1):])
		for j := range v {
			copy(work[3*j:], v[j][:])
		}
	case 4:
		v := make([]Pair, length)
		for j := range v {
			v[j] = Pair{get64(work[12*j:]), uint32(get64(work[12*j+8:]))}
		}
		r = copy(v[dst%(length+1):], v[s%(length+1):])
		for j := range v {
			put64(work[12*j:], v[j].A)
			x := v[j].B
			work[12*j+8], work[12*j+9], work[12*j+10], work[12*j+11] = byte(x), byte(x>>8), byte(x>>16), byte(x>>24)
		}
	case 5:
		v := make([]W5, length)
		for j := range v {
			for k := 0; k < 5; k++ {
				v[j][k] = get64(work[40*j+8*k:])
			}
		}
		r = copy(v[dst%(length+1):], v[s%(length+1):])
		for j := range v {
			for k := 0; k < 5; k++ {
				put64(work[40*j+8*k:], v[j][k])
			}
		}
	}
	put64(res[0:8], uint64(r))
}

// appendMoves exercises append's moves: deleting a range in place
// (overlapping forward move), inserting by appending a slice of itself, and
// growth that copies the old elements to a new array.
func appendMoves(work, res []byte, variant byte, n, capacity, i, j uint64) {
	if capacity < n {
		capacity = n
	}
	b := make([]byte, n, capacity)
	copy(b, work)
	switch variant % 4 {
	case 0:
		b = append(b[:i], b[j:]...) // delete b[i:j]
	case 1:
		b = append(b[:j], b[i:]...) // may overlap the tail or grow
	case 2:
		b = append(b, work[i:i+j]...)
	case 3:
		w := make([]uint64, n%200, capacity%200+n%200)
		for k := range w {
			w[k] = get64(work[8*k:])
		}
		w = append(w[:i%(uint64(len(w))+1)], w[j%(uint64(len(w))+1):]...)
		w = append(w, w...)
		for k := range w {
			if 8*k+8 <= len(work) {
				put64(work[8*k:], w[k])
			}
		}
		put64(res[16:24], uint64(len(w)))
		put64(res[24:32], uint64(cap(w)))
		return
	}
	put64(res[0:8], uint64(len(b)))
	put64(res[8:16], uint64(cap(b)))
	copy(work[1024:], b)
}

func assign47(d, s *Blk47)          { *d = *s }
func assign64(d, s *Blk64)          { *d = *s }
func assign100(d, s *Blk100)        { *d = *s }
func assign333(d, s *Blk333)        { *d = *s }
func assign1000(d, s *Blk1000)      { *d = *s }
func assignW6(d, s *Wide6)          { *d = *s }
func assignW16(d, s *Wide16)        { *d = *s }
func assignW125(d, s *Wide125)      { *d = *s }
func swap333(p, q *Blk333)          { *p, *q = *q, *p }
func through(d **Blk100, s *Blk100) { **d = *s }

// assignBlocks loads blocks from work, assigns them through pointers that
// may alias (i == j is a self-assignment), and stores them back.
func assignBlocks(work, res []byte, variant byte, i, j, k uint64) {
	i, j, k = i%3, j%3, k%3
	switch variant % 9 {
	case 0:
		v := make([]Blk47, 3)
		for x := range v {
			copy(v[x][:], work[47*x:])
		}
		assign47(&v[i], &v[j])
		v[k] = v[i]
		for x := range v {
			copy(work[47*x:], v[x][:])
		}
	case 1:
		v := make([]Blk64, 3)
		for x := range v {
			copy(v[x][:], work[64*x+1:])
		}
		assign64(&v[i], &v[j])
		t := v[k]
		v[i] = t
		for x := range v {
			copy(work[64*x:], v[x][:])
		}
	case 2:
		v := make([]Blk100, 3)
		for x := range v {
			copy(v[x][:], work[100*x+3:])
		}
		assign100(&v[i], &v[j])
		p := &v[k]
		through(&p, &v[i])
		for x := range v {
			copy(work[100*x:], v[x][:])
		}
	case 3:
		v := make([]Blk333, 3)
		for x := range v {
			copy(v[x][:], work[333*x:])
		}
		assign333(&v[i], &v[j])
		swap333(&v[k], &v[j])
		for x := range v {
			copy(work[333*x+5:], v[x][:])
		}
	case 4:
		v := make([]Blk1000, 3)
		for x := range v {
			copy(v[x][:], work[1000*x:])
		}
		assign1000(&v[i], &v[j])
		v[k] = v[i]
		for x := range v {
			copy(work[1000*x:], v[x][:])
		}
	case 5:
		v := make([]Wide6, 3)
		for x := range v {
			for y := 0; y < 6; y++ {
				v[x].W[y] = get64(work[48*x+8*y:])
			}
		}
		assignW6(&v[i], &v[j])
		v[k] = v[i]
		for x := range v {
			for y := 0; y < 6; y++ {
				put64(work[48*x+8*y:], v[x].W[y])
			}
		}
	case 6:
		v := make([]Wide16, 3)
		for x := range v {
			for y := 0; y < 16; y++ {
				v[x].W[y] = get64(work[136*x+8*y:])
			}
			v[x].T = uint32(work[136*x+128])
		}
		assignW16(&v[i], &v[j])
		t := v[k]
		v[j] = t
		for x := range v {
			for y := 0; y < 16; y++ {
				put64(work[136*x+8*y:], v[x].W[y])
			}
			work[136*x+128] = byte(v[x].T)
		}
	case 7:
		v := make([]Wide125, 3)
		for x := range v {
			for y := 0; y < 125; y++ {
				v[x].W[y] = get64(work[8*y+x:])
			}
		}
		assignW125(&v[i], &v[j])
		v[k] = v[i]
		for x := range v {
			for y := 0; y < 125; y++ {
				put64(work[8*y+x*1000:], v[x].W[y])
			}
		}
	case 8:
		// Locals: copies between stack values, not the heap.
		var x, y Blk333
		copy(x[:], work[i*100:])
		copy(y[:], work[j*100+7:])
		if k == 0 {
			x = y
		} else if k == 1 {
			y = x
		}
		z := x
		copy(work[0:], z[:])
		copy(work[400:], y[:])
	}
}

// zeroBlocks zeroes blocks of several sizes and alignments in place.
func zeroBlocks(work, res []byte, variant byte, i, j uint64) {
	i, j = i%3, j%3
	switch variant % 6 {
	case 0:
		v := make([]Blk100, 3)
		for x := range v {
			copy(v[x][:], work[100*x:])
		}
		v[i] = Blk100{}
		for x := range v {
			copy(work[100*x:], v[x][:])
		}
	case 1:
		v := make([]Blk333, 3)
		for x := range v {
			copy(v[x][:], work[333*x:])
		}
		p := &v[j]
		*p = Blk333{}
		v[i] = Blk333{1, 2, 3}
		for x := range v {
			copy(work[333*x:], v[x][:])
		}
	case 2:
		v := make([]Wide16, 3)
		for x := range v {
			for y := 0; y < 16; y++ {
				v[x].W[y] = get64(work[136*x+8*y:])
			}
		}
		v[i] = Wide16{}
		v[j].W = [16]uint64{}
		for x := range v {
			for y := 0; y < 16; y++ {
				put64(work[136*x+8*y:], v[x].W[y])
			}
		}
	case 3:
		v := make([]Wide125, 2)
		for y := 0; y < 125; y++ {
			v[0].W[y] = get64(work[8*y:])
			v[1].W[y] = get64(work[8*y+1000:])
		}
		v[i%2] = Wide125{}
		for y := 0; y < 125; y++ {
			put64(work[8*y:], v[0].W[y])
			put64(work[8*y+1000:], v[1].W[y])
		}
	case 4:
		// A fresh zero local on every iteration.
		for x := uint64(0); x < i+1; x++ {
			var z Blk1000
			z[x] = byte(x + 1)
			copy(work[x*900+j:], z[:])
		}
	case 5:
		h := make([]Blk64, 3)
		for x := range h {
			copy(h[x][:], work[64*x+2:])
		}
		h[i] = Blk64{}
		for x := range h {
			copy(work[64*x:], h[x][:])
		}
	}
}

// buildStrings concatenates and converts strings built from work.
func buildStrings(work, res []byte, a, n, b, m uint64) {
	s := string(work[a : a+n])
	t := string(work[b : b+m])
	u := s + t + s
	r := copy(work[1500:], u)
	v := []byte(t + s)
	r += copy(work[100:], v)
	put64(res[0:8], uint64(r))
	put64(res[8:16], uint64(len(u)))
}

// zeroLoops runs the zeroing loops goSVM emits as one memset on SBF
// (docs/GO_ALLOCATION.md#zeroing-loops), and near misses it must compile
// as written: a start past the end, a signed start that traps, a nonzero
// store, a value variable.
func zeroLoops(work, res []byte, variant byte, start, n, k uint64) {
	switch variant % 9 {
	case 0:
		tail := work[start : start+n]
		for i := range tail {
			tail[i] = 0
		}
	case 1:
		sub := work[:n]
		for j := start; j < uint64(len(sub)); j++ {
			sub[j] = 0
		}
	case 2:
		v := make([]uint32, n%700)
		for x := range v {
			v[x] = uint32(get64(work[4*x:]))
		}
		w := v[k%(uint64(len(v))+1):]
		for i := range w {
			w[i] = 0
		}
		for x := range v {
			put64(work[4*x:], uint64(v[x]))
		}
	case 3:
		sub := work[start%7 : n]
		for j := uint(start); j < uint(len(sub)); j++ {
			sub[j] = 0
		}
	case 4:
		b := make([]bool, n%1000)
		for x := range b {
			b[x] = work[x]&1 == 1
		}
		for i := range b {
			b[i] = false
		}
		for x := range b {
			if b[x] {
				work[x] = 1
			}
		}
		put64(res[0:8], uint64(len(b)))
	case 5:
		// Signed: a start below zero traps at its first store, as in Go.
		sub := work[:n]
		for j := int(start) - 5; j < len(sub); j++ {
			sub[j] = 0
		}
	case 6:
		tail := work[start : start+n]
		for i := range tail {
			tail[i] = 1
		}
	case 7:
		tail := work[start : start+n]
		for i, v := range tail {
			tail[i] = v & 0
		}
	case 8:
		var none []byte
		for i := range none {
			none[i] = 0
		}
		w := make([]uint64, 0, n%100)
		for j := uint64(0); j < uint64(len(w)); j++ {
			w[j] = 0
		}
		put64(res[0:8], uint64(cap(w)))
	}
}

func Process(s, i []byte) uint64 { return Run(s, i) }
