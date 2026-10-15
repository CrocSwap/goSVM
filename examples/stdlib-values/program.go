// Package stdlibvalues is an ordinary-Go oracle for the standard-library
// packages available on chain (docs/GO_STDLIB.md): errors, math,
// encoding/binary, unicode/utf8, unicode, strings, bytes and strconv, and
// for the two language features they needed, variadic functions and
// pointers to pointers. Every
// supported function is called on inputs taken from the vector, and every
// result (values, lengths and capacities of returned slices, error texts and
// errors.Is/As answers) is written to the output. The same source runs
// natively against Go's standard library and through goSVM against its
// on-chain overlay; tests compare output bytes and status (or a panic) for
// every vector.
//
// Run(out, in) reads a group from in[0] and an operation from in[1]; in[2]
// and in[3] are the lengths (mod 49) of the strings a = in[8:] and b =
// in[56:]; in[4] and in[5] are small signed integers; in[104:112] and
// in[112:120] are 64-bit little-endian values; in[120:128] are four 16-bit
// values (runes, sizes, bases).
package stdlibvalues

// W writes values into out, dropping what does not fit.
type W struct {
	out []byte
	o   int
}

func (w *W) u8(v byte) {
	if w.o < len(w.out) {
		w.out[w.o] = v
		w.o++
	}
}

func (w *W) u64(v uint64) {
	for j := 0; j < 8; j++ {
		w.u8(byte(v >> (8 * j)))
	}
}

func (w *W) int(v int) { w.u64(uint64(v)) }

func (w *W) flag(b bool) {
	if b {
		w.u8(1)
	} else {
		w.u8(0)
	}
}

// str writes the length (two bytes) and then the bytes of s.
func (w *W) str(s string) {
	w.u8(byte(len(s)))
	w.u8(byte(len(s) >> 8))
	for j := 0; j < len(s); j++ {
		w.u8(s[j])
	}
}

// bs writes a byte slice: nil-ness, length, capacity and bytes.
func (w *W) bs(b []byte) {
	w.flag(b == nil)
	w.int(len(b))
	w.int(cap(b))
	for _, c := range b {
		w.u8(c)
	}
}

func (w *W) strs(ss []string) {
	w.flag(ss == nil)
	w.int(len(ss))
	for _, s := range ss {
		w.str(s)
	}
}

func (w *W) bss(bb [][]byte) {
	w.flag(bb == nil)
	w.int(len(bb))
	for _, b := range bb {
		w.bs(b)
	}
}

// err writes whether err is nil, and its text otherwise.
func (w *W) err(err error) {
	if err == nil {
		w.u8(0xee)
		return
	}
	w.u8(1)
	w.str(err.Error())
}

func le64(b []byte) uint64 {
	var v uint64
	for j := 7; j >= 0; j-- {
		v = v<<8 | uint64(b[j])
	}
	return v
}

// Input holds the decoded vector.
type Input struct {
	raw        []byte
	op         int
	a, b       string
	n, m       int
	x, y       uint64
	h0, h1, h2 int
	h3         int
}

// Groups Run dispatches on.
const Groups = 10

func Process(s, i []byte) uint64 { return Run(s, i) }

// Run decodes the vector and runs one operation of one group.
func Run(out, in []byte) uint64 {
	w := &W{out: out}
	la, lb := int(in[2]%49), int(in[3]%49)
	v := Input{raw: in, op: int(in[1]), a: string(in[8 : 8+la]), b: string(in[56 : 56+lb]),
		n: int(int8(in[4])), m: int(int8(in[5])), x: le64(in[104:]), y: le64(in[112:]),
		h0: int(in[120]) | int(in[121])<<8, h1: int(in[122]) | int(in[123])<<8,
		h2: int(in[124]) | int(in[125])<<8, h3: int(in[126]) | int(in[127])<<8}
	switch in[0] % Groups {
	case 0:
		stringsGroup(w, v)
	case 1:
		bytesGroup(w, v)
	case 2:
		strconvGroup(w, v)
	case 3:
		binaryGroup(w, v)
	case 4:
		errorsGroup(w, v)
	case 5:
		unicodeGroup(w, v)
	case 6:
		mathGroup(w, v)
	case 7:
		builderGroup(w, v)
	case 8:
		quoteGroup(w, v)
	case 9:
		langGroup(w, v)
	}
	return uint64(w.o)
}

// fresh returns a copy of s with capacity len(s). It is used for slices
// that are appended to, instead of []byte(s): gc gives a non-escaping
// conversion a 32-byte stack buffer, which goSVM does not (a documented
// difference, docs/GO_STRINGS.md#allocation).
func fresh(s string) []byte {
	b := make([]byte, len(s))
	copy(b, s)
	return b
}
