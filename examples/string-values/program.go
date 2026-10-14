// Package stringvalues is an ordinary-Go oracle for strings and error values
// in the on-chain subset: literals (raw, escaped, non-ASCII), len, indexing
// and slicing with Go's bounds panics, concatenation and +=, bytewise
// comparisons, conversions between strings, byte slices, rune slices and
// runes (invalid runes become U+FFFD), range over strings with Go's UTF-8
// decoding, switch on strings, strings in structs, arrays, slices, pointers,
// interfaces and generic code, error values through interfaces, and the heap
// cost of each allocating operation. The same source runs natively and
// through goSVM; tests compare output bytes and status (or a panic) for every
// vector. See docs/GO_STRINGS.md.
//
// Run(out, in) reads a mode from in[0] and writes results into out.
package stringvalues

// W writes little-endian values and length-prefixed strings into a byte
// slice held in a field.
type W struct {
	out []byte
	o   int
}

func (w *W) put(v uint64, width int) {
	for j := range width {
		w.out[w.o+j] = byte(v >> (8 * j))
	}
	w.o = w.o + width
}

func (w *W) u8(v uint64)  { w.put(v, 1) }
func (w *W) u32(v uint64) { w.put(v, 4) }
func (w *W) u64(v uint64) { w.put(v, 8) }

// str writes len(s) as one byte, then at most 40 of its bytes.
func (w *W) str(s string) {
	w.u8(uint64(len(s)))
	for j := 0; j < len(s) && j < 40; j++ {
		w.out[w.o] = s[j]
		w.o = w.o + 1
	}
}

func flag(b bool) uint64 {
	if b {
		return 1
	}
	return 0
}

// Constants and named string types.

const greeting = "héllo, 世界"

const raw = `a\nb"c` + "\n"

// Name is a named string type.
type Name string

const typed Name = "typed\x00\xff\t"

// Mode 0: literals, len, indexing and slicing.
func basics(w *W, in []byte) {
	s := greeting
	w.str(s)
	w.str(raw)
	w.str(string(typed))
	w.u64(uint64(len(s)))
	w.u8(uint64(s[int(in[1])%len(s)]))
	const k = "ab" + "cd"
	w.u64(uint64(len(k)))
	w.str(k[1:3])
	w.u8(uint64("xyz"[in[5]%3]))
	var e string
	w.u8(flag(e == ""))
	w.u64(uint64(len(e)))
	w.str("\a\b\f\n\r\t\v\\'\"\x7fé\U0001F600??=")
	w.str(string(typed[1:4] + Name(k)))
	// Go panics for an index or bounds out of range.
	i := int(in[2]) % 20
	w.u8(uint64(s[i]))
	lo, hi := int(in[3])%16, int(in[4])%16
	w.str(s[lo:])
	w.str(s[:hi])
	w.str(s[lo:hi])
	var x int8 = int8(in[6])
	w.u8(uint64(s[x&15]))
}

// toBytes, constBytes and toRunes return their conversions, so the result
// escapes in gc too and its capacity is the heap one (a non-escaping small
// conversion would use gc's 32-element stack buffer).
//
//go:noinline
func toBytes(s string) []byte { return []byte(s) }

//go:noinline
func constBytes() []byte { return []byte("constant!") }

//go:noinline
func toRunes(s string) []rune { return []rune(s) }

// Mode 1: concatenation, aliasing and immutability, byte conversions.
func concat(w *W, in []byte) {
	n, m := int(in[1])%24, int(in[2])%24
	a := string(in[3 : 3+n])
	b := string(in[30 : 30+m])
	c := a + b
	w.str(c)
	d := a + "-" + b + "!" + a
	w.str(d)
	w.str(a + (b + a) + (c + ""))
	// The conversion copied: changing in leaves a unchanged.
	in[3] = in[3] + 1
	w.str(a)
	bs := toBytes(c)
	w.u64(uint64(len(bs)))
	w.u64(uint64(cap(bs)))
	if len(bs) > 0 {
		bs[0] = 'Z'
	}
	w.str(c)
	w.str(string(bs))
	bs2 := append(bs, 'q')
	w.u64(uint64(cap(bs2)))
	w.u8(flag(len(bs) < cap(bs) && &bs[:cap(bs)][len(bs)] == &bs2[len(bs)]))
	cb := constBytes()
	w.u64(uint64(len(cb)))
	w.u64(uint64(cap(cb)))
	w.str(string(cb))
	empty := toBytes("")
	w.u8(flag(empty == nil))
	w.u64(uint64(cap(empty)))
	acc := ""
	for j := 0; j < int(in[4])%6; j++ {
		acc += string(in[40+j : 41+j])
		acc += "."
	}
	w.str(acc)
	w.str("" + a + "")
	buf := make([]byte, 4)
	k := copy(buf, d)
	w.u64(uint64(k))
	w.str(string(buf))
	ext := append([]byte("x"), c...)
	w.str(string(ext))
	ext = append(ext, "yz"...)
	w.str(string(ext))
	if len(c) > 2 {
		sub := c[1:]
		c = "replaced"
		w.str(sub)
	}
	w.u8(flag(c == a+b))
	// += on fields, elements and through pointers.
	r := Rec{Key: a}
	r.Key += "+"
	keys := []string{a, b}
	keys[in[5]%2] += "#" + b
	p := &keys[0]
	*p += "*"
	w.str(r.Key)
	w.str(keys[0])
	w.str(keys[1])
	// x op= y evaluates y before reading x, as gc orders a call operand:
	// bump changes the target first, and an out-of-range index aborts only
	// after mark has written its byte.
	t := "t"
	t += bump(&t)
	w.str(t)
	if in[6]%3 == 0 {
		keys[int(in[7]%3)] += mark(w)
		w.str(keys[0])
	}
}

func bump(p *string) string {
	*p = *p + "!"
	return "x"
}

func mark(w *W) string {
	w.u8(0xAA)
	return "m"
}

// Mode 2: bytewise comparisons.
func compare(w *W, in []byte) {
	n, m := int(in[1])%6, int(in[2])%6
	a := string(in[3 : 3+n])
	b := string(in[10 : 10+m])
	w.u8(flag(a == b))
	w.u8(flag(a != b))
	w.u8(flag(a < b))
	w.u8(flag(a <= b))
	w.u8(flag(a > b))
	w.u8(flag(a >= b))
	w.u8(flag(a < "b"))
	w.u8(flag("ab" <= a))
	x, y := Name(a), Name(b)
	w.u8(flag(x < y))
	w.u8(flag(x == y))
	ss := []string{a, b, "", "a", "ab", string(in[20:22]), a + b, b[:m/2]}
	for j := 1; j < len(ss); j++ {
		for k := j; k > 0 && ss[k] < ss[k-1]; k-- {
			ss[k], ss[k-1] = ss[k-1], ss[k]
		}
	}
	for _, s := range ss {
		w.str(s)
	}
}

// Mode 3: rune conversions.
func runeConv(w *W, in []byte) {
	for k := 0; k < 4; k++ {
		v := int32(uint32(in[1+4*k]) | uint32(in[2+4*k])<<8 | uint32(in[3+4*k])<<16 | uint32(in[4+4*k])<<24)
		switch in[17+k] % 8 {
		case 0:
			v = 0x10FFFF
		case 1:
			v = 0x110000
		case 2:
			v = 0xD800 + v&0x7FF
		case 3:
			v = v & 0x7FF
		case 4:
			v = v & 0xFFFF
		case 5:
			v = -1 - v&3
		}
		w.str(string(rune(v)))
		w.str(string(byte(v)))
	}
	n := int(in[25]) % 16
	s := string(in[26 : 26+n])
	rs := toRunes(s)
	w.u64(uint64(len(rs)))
	w.u64(uint64(cap(rs)))
	for _, r := range rs {
		w.u32(uint64(uint32(r)))
	}
	w.str(string(rs))
	rs2 := []rune{-1, 0xD800, 0xDFFF, 0x10FFFF, 0x110000, 'a', 0x7F, 0x80, 0x7FF, 0x800, 0xFFFF, 0x10000, rune(in[42])}
	w.str(string(rs2))
	w.str(string(rs2[:0]))
	w.str(string(rune(in[43])) + string(in[44]))
	cr := toRunes("aé世\U0001F600\xff")
	w.u64(uint64(len(cr)))
	w.u64(uint64(cap(cr)))
	var none []rune
	w.str(string(none))
}

// Mode 4: range over strings, including invalid UTF-8.
func ranges(w *W, in []byte) {
	n := int(in[1]) % 40
	s := string(in[2 : 2+n])
	for i, r := range s {
		w.u8(uint64(i))
		w.u32(uint64(uint32(r)))
	}
	count := 0
	for range s {
		count++
	}
	w.u64(uint64(count))
	for i := range s {
		if i > 10 {
			break
		}
		w.u8(uint64(i))
	}
	var j int
	var r rune
	for j, r = range s {
		if r == 0xFFFD {
			continue
		}
		w.u8(uint64(j))
	}
	w.u8(uint64(j))
	w.u32(uint64(uint32(r)))
	for i, c := range "aé世\U0001F600\xff\xed\xa0\x80" {
		w.u8(uint64(i))
		w.u32(uint64(uint32(c)))
	}
	var rv any
	for _, rv = range typed {
		w.u8(flag(rv == any(rune(0))))
	}
	if x, ok := rv.(rune); ok {
		w.u32(uint64(uint32(x)))
	}
}

// Rec has string fields.
type Rec struct {
	Key  string
	N    uint64
	Tags [2]string
}

// Mode 5: switch, structs, arrays, slices and pointers.
func containers(w *W, in []byte) {
	keys := [5]string{"alpha", "beta", "", "gamma", "é"}
	k := keys[in[1]%5]
	switch k {
	case "alpha":
		w.u8(1)
	case "beta", "gamma":
		w.u8(2)
	case "":
		w.u8(3)
	default:
		w.u8(4)
	}
	switch t := string(in[2:4]); t {
	case "ab":
		w.u8(5)
	case string(in[4:6]):
		w.u8(6)
	case k:
		w.u8(7)
	default:
		w.str(t)
	}
	r := Rec{Key: k, N: 7}
	r.Tags[0] = k + k
	p := &r.Key
	*p = *p + "!"
	w.str(r.Key)
	w.str(r.Tags[0])
	r2 := r
	r2.Tags[1] = "x"
	w.str(r.Tags[1])
	var arr, arr2 [3]string
	arr[in[3]%3] = k
	arr2[in[4]%3] = keys[in[5]%5]
	w.u8(flag(arr == arr2))
	w.u8(flag(arr != arr2))
	var list []string
	for j := 0; j < int(in[6])%12; j++ {
		list = append(list, keys[j%5])
	}
	w.u64(uint64(len(list)))
	w.u64(uint64(cap(list)))
	for _, s := range list {
		w.u8(uint64(len(s)))
	}
	ps := new(string)
	*ps = "new"
	w.str(*ps)
	rp := &Rec{Key: "heap"}
	rp.Key = rp.Key + k
	w.str(rp.Key)
	recs := []Rec{r, {Key: "b"}}
	w.str(recs[in[7]%2].Key)
	w.str(list[in[8]%4]) // Go panics when the index is out of range
}

// Errors and interfaces.

// MyErr is an error through a pointer receiver.
type MyErr struct {
	Code uint64
	Msg  string
}

func (e *MyErr) Error() string { return "myerr: " + e.Msg }

// CodeErr is an error through a value receiver.
type CodeErr uint32

func (c CodeErr) Error() string { return "code " + string(rune('0'+c%10)) }

// Stringer is a string-returning interface.
type Stringer interface{ String() string }

// Label is a named string with a method.
type Label string

func (l Label) String() string { return "<" + string(l) + ">" }

func check(x byte) error {
	switch x % 4 {
	case 0:
		return nil
	case 1:
		return &MyErr{Code: uint64(x), Msg: "bad"}
	case 2:
		return CodeErr(x)
	}
	var e *MyErr
	return e // a non-nil error holding a nil *MyErr
}

func describe(err error) (uint64, error) {
	if err == nil {
		return 0, nil
	}
	return uint64(len(err.Error())), err
}

// Mode 6: error values and strings in interfaces.
func errorsMode(w *W, in []byte) {
	for k := 0; k < 4; k++ {
		err := check(in[1+k])
		w.u8(flag(err == nil))
		if err != nil && in[1+k]%4 != 3 {
			w.str(err.Error())
			n, again := describe(err)
			w.u8(n)
			w.u8(flag(again == err))
		}
		if me, ok := err.(*MyErr); ok {
			w.u8(flag(me == nil))
		}
		switch e := err.(type) {
		case nil:
			w.u8(10)
		case CodeErr:
			w.u8(uint64(e))
		case *MyErr:
			w.u8(20)
		}
	}
	e1, e2 := check(in[5]), check(in[6])
	w.u8(flag(e1 == e2))
	w.u8(flag(e1 != nil && e1 == CodeErr(in[5])))
	var a any = string(in[9:12])
	var b any = "abc"
	w.u8(flag(a == b))
	w.u8(flag(a == "abc"))
	if s, ok := a.(string); ok {
		w.str(s)
	}
	var st Stringer = Label(string(in[12:14]))
	w.str(st.String())
	switch v := a.(type) {
	case Label:
		w.str(string(v))
	case string:
		w.str(v + "?")
	}
	var c any = Label("abc")
	w.u8(flag(b == c))
	var d any = Rec{Key: string(in[14:15])}
	w.u8(flag(d == any(Rec{Key: "k"})))
	var ea any = check(in[15])
	if e, ok := ea.(error); ok {
		w.u8(flag(e == nil))
	}
	// Calling Error on a nil *MyErr dereferences nil: Go panics.
	if in[8]%2 == 1 {
		if e := check(in[7]); e != nil {
			w.str(e.Error())
		}
	}
	w.str(a.(string) + b.(string))
	if in[16]%3 == 0 {
		w.str(c.(string)) // Go panics: c holds a Label
	}
}

// Generic code over strings.

// Text is the set of string types.
type Text interface{ ~string }

func Join[T Text](xs []T, sep T) T {
	var out T
	for i, x := range xs {
		if i > 0 {
			out += sep
		}
		out += x
	}
	return out
}

func Max[T interface{ ~string | ~uint64 }](a, b T) T {
	if a > b {
		return a
	}
	return b
}

// Box holds any value.
type Box[T any] struct{ V T }

func (b Box[T]) Get() T { return b.V }

func Count[T comparable](xs []T, v T) int {
	n := 0
	for _, x := range xs {
		if x == v {
			n++
		}
	}
	return n
}

func Rev[T Text](s T) T {
	rs := []rune(s)
	for i, j := 0, len(rs)-1; i < j; i, j = i+1, j-1 {
		rs[i], rs[j] = rs[j], rs[i]
	}
	return T(string(rs))
}

func First[T Text](s T) byte { return s[0] }

// Mode 7: strings in generic code.
func generics(w *W, in []byte) {
	n := int(in[1]) % 8
	parts := []string{string(in[2:4]), "x", string(in[4 : 4+n%3])}
	w.str(Join(parts, ","))
	labels := []Label{"a", Label(string(in[5:6]))}
	w.str(string(Join(labels, Label("|"))))
	w.str(Max(string(in[6:8]), string(in[8:10])))
	w.u64(Max(uint64(in[6]), uint64(in[8])))
	box := Box[string]{V: string(in[10:12])}
	w.str(box.Get())
	var anyBox any = box
	if bb, ok := anyBox.(Box[string]); ok {
		w.str(bb.V)
	}
	w.u64(uint64(Count(parts, "x")))
	w.u64(uint64(Count([]Name{"a", "b", "a"}, Name(string(in[12:13])))))
	w.str(Rev(string(in[13 : 13+n])))
	w.str(string(Rev(Label("añb"))))
	w.u8(uint64(First(string(in[20 : 20+n%2])))) // Go panics for ""
}

// Mode 8: allocate repeatedly; out[0:2] counts completed iterations, so an
// out-of-memory abort (on chain) leaves the count of allocations that fit.
// Native Go never runs out of memory.
func heapUse(out []byte, in []byte) {
	n := int(in[1]) | int(in[2])<<8
	la, lb := int(in[4])%32+1, int(in[5])%32+1
	a := string(in[8 : 8+la])
	b := string(in[40 : 40+lb])
	total := 0
	for j := 0; j < n; j++ {
		out[0] = byte(j)
		out[1] = byte(j >> 8)
		switch in[3] {
		case 0:
			total = total + len(a+b)
		case 1:
			total = total + len(string(in[8:8+la]))
		case 2:
			total = total + len([]byte(a))
		case 3:
			total = total + len(string(rune(0x10000+j)))
		case 4:
			total = total + len([]rune(a))
		case 5:
			total = total + len(a+"")
		case 6:
			var v any = a
			total = total + len(v.(string))
		case 7:
			total = total + len([]byte("constant!"))
		}
	}
	out[0] = byte(n)
	out[1] = byte(n >> 8)
	out[2] = 1
	out[3] = byte(total)
}

// Run dispatches on in[0].
func Run(out, in []byte) uint64 {
	w := W{out: out}
	switch in[0] {
	case 0:
		basics(&w, in)
	case 1:
		concat(&w, in)
	case 2:
		compare(&w, in)
	case 3:
		runeConv(&w, in)
	case 4:
		ranges(&w, in)
	case 5:
		containers(&w, in)
	case 6:
		errorsMode(&w, in)
	case 7:
		generics(&w, in)
	case 8:
		heapUse(out, in)
		return 3
	default:
		return 7
	}
	return uint64(w.o)
}

// Process is the byte-slice entrypoint: s is the output, i the input.
func Process(s, i []byte) uint64 { return Run(s, i) }
