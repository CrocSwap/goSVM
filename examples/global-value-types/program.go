// Package globalvaluetypes is an ordinary-Go oracle for package-level
// variables combined with struct and array comparison, type aliases, arrays
// of composite elements and panic:
//
//   - package-level arrays of structs and of arrays, both read-only data
//     (never written, constant initializers) and writable (the globals
//     block), including tables above the 1,024-byte per-value array limit
//     (a uint256-style [60]Int, and two above the 4 KiB SBF stack frame)
//     that are only indexed in place;
//   - mutation in place (G[i].F = v, G[i][j]++, op= on elements and fields)
//     of this package's variables and of another package's (pkg.G[i].F);
//   - == and != on struct and array globals, switch on a struct global,
//     and globals compared through interfaces;
//   - type aliases (of another package's types and of unnamed types) as
//     the types of package-level variables;
//   - a panic in a function that initializers also call (Run calls it with
//     input-dependent values), with Go's log line.
//
// The same source runs natively (a fresh process per vector, as every SBF
// invocation starts from freshly initialized variables) and through goSVM;
// tests compare output bytes, status and, for panics, the log line. Panics
// during initialization itself abort every invocation, so they are tested
// with separate probe programs. See docs/GO_GLOBALS.md and docs/GO_TYPES.md.
//
// Run(out, in) reads a mode from in[0] and writes results into out.
package globalvaluetypes

import "gosvm/examples/global-value-types/wide"

// Aliases of another package's types and of unnamed types.
type (
	Int  = wide.Int
	Pair = wide.Pair
	Cell = wide.Cell
	Grid = [4][4]uint64
	Row  = [4]uint64
)

// Rec is a struct with struct and array fields.
type Rec struct {
	Key  Pair
	Tag  [2]uint8
	Hits uint64
}

// Code is a named integer panic value.
type Code uint8

// W writes little-endian values into the output; the writer itself is a
// package-level variable holding the output slice.
type W struct {
	out []byte
	o   int
}

var w W

func put(v uint64, width int) {
	for j := range width {
		w.out[w.o+j] = byte(v >> (8 * j))
	}
	w.o = w.o + width
}

func u8(v uint64)  { put(v, 1) }
func u64(v uint64) { put(v, 8) }

func flag(b bool) uint64 {
	if b {
		return 1
	}
	return 0
}

// Read-only data: never written, constant initializers.
//
// pows is holiman/uint256's table shape: 60 Ints, 1,920 bytes, above the
// per-value array limit; it is only indexed, so it is admitted.
var pows = [60]Int{{1}, {10}, {100}, {1000}, 19: {0x8ac7230489e80000}, 20: {0x6bc75e2d63100000, 5}, 59: {0, 0, 0, 0x1000}}
var pairs = [4]Pair{{K: 1, V: 2}, {K: 3, V: 4}, {K: 1, V: 2}, {K: 7}}
var origin = Pair{K: 1, V: 2}
var cube = [2][2][2]uint16{{{1, 2}, {3, 4}}, {{5, 6}, {7, 8}}}
var cells = [3]Cell{{P: Pair{K: 1}, W: [2]uint16{1, 2}}, {P: Pair{K: 1}, W: [2]uint16{1, 2}}, {W: [2]uint16{0, 9}}}
var zeroRec Rec
var identity Grid = Grid{{1}, {0, 1}, {0, 0, 1}, {0, 0, 0, 1}}

// big is 4,800 bytes of read-only data, above the 4 KiB SBF stack frame; it
// is only indexed.
var big = [300]Pair{7: {K: 7, V: 49}, 150: {V: 1 << 40}, 299: {K: 1, V: 1}}

// Writable: the globals block.
var grid Grid
var recs [4]Rec
var acc Int
var cells2 [3]Cell
var rows = [2]Row{{1, 2, 3, 4}, {5, 6, 7, 8}}
var ptrs = [2]*Pair{&slots[0], &slots[1]}
var slots [2]Pair
var lists = [2][]uint64{{1, 2, 3}, {4}}
var boxed any = origin
var boxedRow any = Row{1, 2, 3, 4}
var ifaces = [3]any{Pair{K: 1, V: 2}, Int{1}, Code(3)}
var current Pair

// ledger is 5,120 bytes of the globals block, above the per-value limit and
// the 4 KiB SBF stack frame; it is only indexed in place.
var ledger [160]Int

// Initializers that call a function that can panic.
var limit = wide.Checked(40)
var table = build()

func build() [4]Rec {
	var t [4]Rec
	for i := range t {
		t[i].Key = pairs[i]
		t[i].Tag[1] = uint8(wide.Checked(uint64(i)))
		t[i].Hits = uint64(i) * 3
	}
	return t
}

func init() {
	for i := range recs {
		recs[i].Key = pairs[i]
		recs[i].Tag[1] = uint8(i)
		recs[i].Hits = uint64(i) * 3
	}
	grid[1][2]++
	grid[3][0] += 7
	wide.Table[3].V += 5
	wide.Grid[2][1]++
	cells2[1] = cells[0]
	current = wide.Seed
}

func Process(s, i []byte) uint64 { return Run(s, i) }

// Run dispatches on in[0].
func Run(out, in []byte) uint64 {
	w = W{out: out}
	switch in[0] {
	case 0:
		return dump()
	case 1:
		return mutate(in[1:])
	case 2:
		return lookups(in[1:])
	case 3:
		return compare(in[1:])
	case 4:
		return interfaces(in[1:])
	case 5:
		return panics(in[1:])
	}
	return 0
}

func pair(p Pair) {
	u64(uint64(p.K))
	u64(p.V)
}

func rec(r Rec) {
	pair(r.Key)
	u8(uint64(r.Tag[0]))
	u8(uint64(r.Tag[1]))
	u64(r.Hits)
}

// dump writes every variable's value after initialization.
func dump() uint64 {
	for i := range grid {
		for _, v := range grid[i] {
			u8(v)
		}
	}
	for _, r := range recs {
		rec(r)
	}
	for _, r := range table {
		rec(r)
	}
	for _, p := range wide.Table {
		pair(p)
	}
	for i := range wide.Grid {
		for j := range wide.Grid[i] {
			u8(uint64(wide.Grid[i][j]))
		}
	}
	u64(limit)
	u64(wide.Steps)
	pair(current)
	pair(*ptrs[1])
	u64(uint64(len(lists[0])))
	u64(lists[1][0])
	u64(uint64(cells2[1].W[1]))
	u64(flag(recs == table))
	u64(flag(recs[2].Key == origin))
	u64(flag(cells2[1] == cells[1]))
	return 1
}

// mutate applies a script of in-place updates, then writes the state.
func mutate(in []byte) uint64 {
	n := int(in[0] % 40)
	for j := 0; j < n; j++ {
		a, b := in[1+j]&3, in[2+j]&3
		switch in[2+j] % 9 {
		case 0:
			grid[a][b]++
		case 1:
			grid[a][b] += uint64(in[3+j])
		case 2:
			recs[a].Key.V = uint64(in[3+j])
		case 3:
			recs[a].Tag[b&1]++
		case 4:
			recs[a].Hits *= 3
		case 5:
			wide.Table[a].V -= uint64(b)
			wide.Grid[a%3][b%3]++
		case 6:
			rows[a&1][b] = grid[b][a]
			ptrs[a&1].V += uint64(b)
		case 7:
			acc[b] ^= pows[in[3+j]%60][b]
			ledger[in[3+j]%160][b] += uint64(j) + 1
			wide.Bump(uint64(in[3+j]) << 60)
		default:
			recs[a], recs[b] = recs[b], recs[a]
			lists[a&1][0]++
		}
	}
	for i := range grid {
		for _, v := range grid[i] {
			u64(v)
		}
	}
	for _, r := range recs {
		rec(r)
	}
	for _, p := range wide.Table {
		pair(p)
	}
	for _, r := range rows {
		for _, v := range r {
			u64(v)
		}
	}
	pair(slots[0])
	pair(slots[1])
	for _, v := range acc {
		u64(v)
	}
	for _, v := range wide.Counter {
		u64(v)
	}
	u64(lists[0][0] + lists[1][0])
	sum := uint64(0)
	for i := range ledger {
		sum = sum*31 + ledger[i][0] + ledger[i][1]<<8 + ledger[i][2]<<16 + ledger[i][3]<<24
	}
	u64(sum)
	for i := range wide.Grid {
		for j := range wide.Grid[i] {
			u8(uint64(wide.Grid[i][j]))
		}
	}
	// Comparisons after mutation.
	u64(flag(recs == table))
	u64(flag(grid == identity))
	u64(flag(rows[0] == rows[1]))
	u64(flag(wide.Table == pairs))
	return 2
}

// lookups reads the read-only tables in place; an index past a table is a
// Go runtime error (no log line).
func lookups(in []byte) uint64 {
	i := int(in[0])
	if in[1] < 200 {
		i %= 60
	}
	x := pows[i]
	for _, v := range x {
		u64(v)
	}
	u64(pows[(i+1)%60][1])
	u64(uint64(cube[in[2]&1][in[3]&1][in[4]&1]))
	u64(uint64(pairs[in[5]&3].K))
	u64(uint64(cells[in[6]%3].W[1]))
	u64(identity[in[7]&3][in[8]&3])
	u64(uint64(len(pows)))
	u64(uint64(big[i%300].K) + big[in[9]%8+in[10]%2*143].V)
	u64(uint64(len(big)) + uint64(len(ledger)))
	n := uint64(0)
	for j := range pows {
		n += pows[j][0] >> 32
	}
	u64(n)
	return 3
}

// compare uses == and != on struct and array globals and a struct switch.
func compare(in []byte) uint64 {
	a, b := in[0]&3, in[1]&3
	u64(flag(pairs[a] == pairs[b]))
	u64(flag(pairs[a] == origin))
	u64(flag(wide.Table[a] != pairs[b]))
	u64(flag(wide.Origin == origin))
	u64(flag(recs[a] == table[b]))
	u64(flag(recs[a] != zeroRec))
	u64(flag(cells[0] == cells[1]))
	u64(flag(cells[a%3] == cells2[b%3]))
	u64(flag(cube[0] == cube[1]))
	u64(flag(cube[a&1][b&1] == [2]uint16{1, 2}))
	u64(flag(pows[a] == pows[b]))
	u64(flag(rows[0] == Row{1, 2, 3, 4}))
	u64(flag(acc == Int{}))
	u64(flag(current == wide.Seed))
	u64(flag(*ptrs[0] == slots[0]))
	switch pairs[a] {
	case origin:
		u8(1)
	case wide.Table[1]:
		u8(2)
	case Pair{K: 7}:
		u8(3)
	default:
		u8(4)
	}
	recs[a].Key = Pair{K: uint32(in[2] & 3), V: uint64(in[3] & 3)}
	u64(flag(recs[a].Key == origin))
	u64(flag(recs == table))
	return 4
}

// interfaces compares globals through interface values and stores struct
// and array values into interface globals.
func interfaces(in []byte) uint64 {
	a, b := in[0]&3, in[1]&3
	u64(flag(boxed == any(pairs[a])))
	u64(flag(boxed == any(recs[b].Key)))
	u64(flag(boxedRow == any(rows[0])))
	u64(flag(boxedRow == any(identity[a])))
	u64(flag(ifaces[0] == any(origin)))
	u64(flag(ifaces[1] == any(pows[a])))
	u64(flag(ifaces[2] == any(Code(b))))
	if in[2]&1 == 1 {
		boxed = recs[a].Key
	} else {
		boxed = cells[a%3]
	}
	switch v := boxed.(type) {
	case Pair:
		pair(v)
	case Cell:
		pair(v.P)
		u64(uint64(v.W[1]))
	}
	u64(flag(boxed == any(origin)))
	u64(flag(boxed == ifaces[0]))
	var x any = grid
	u64(flag(x == any(Grid{})))
	ifaces[b%3] = rows[a&1]
	u64(flag(ifaces[b%3] == boxedRow))
	return 5
}

// panics calls the function the initializers call, with input-dependent
// values, and panics with values derived from globals.
func panics(in []byte) uint64 {
	u64(wide.Checked(uint64(in[0])))
	u64(wide.Steps)
	switch in[1] % 4 {
	case 1:
		if recs[in[2]&3] == table[in[3]&3] {
			panic(Code(in[2] & 3))
		}
	case 2:
		if pairs[in[2]&3] == origin {
			panic("origin pair")
		}
	case 3:
		var x any = ifaces[0]
		var y any = []byte{1}
		if in[2]&1 == 1 {
			x = y
		}
		// Equal dynamic types that are uncomparable: a runtime error.
		u64(flag(x == y))
	}
	return 6
}
