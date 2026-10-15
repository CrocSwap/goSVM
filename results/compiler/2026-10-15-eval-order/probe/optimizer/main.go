// Command optimizer shows the one gc behaviour goSVM does not match: with
// inlining, gc's SSA back end may fault on a nil pointer read early (by the
// order pass) after a later inlined call panics. Build it with default flags
// and with -gcflags=-l: the first prints "w" for the first two lines, the
// second a nil dereference everywhere (the order pass's order, which goSVM
// follows).
package main

import "fmt"

type T struct{ a, b uint64 }

var (
	gp         *T
	gq         *uint64
	pw         bool
	v1, v2, v3 uint64
	sink       uint64
)

func step() {
	if pw {
		panic("w")
	}
}

func wGp() uint64                 { step(); gp = &T{v2, v2 + 1}; return v3 }
func wGqv() uint64                { step(); *gq = v2; return v3 }
func any8(a any, b uint64) uint64 { return uint64(a.(uint8))*1000 + b }
func b2u(b bool) uint64 {
	if b {
		return 1
	}
	return 0
}

//go:noinline
func wGpN() uint64 { step(); gp = &T{v2, v2 + 1}; return v3 }

func try(name string, f func()) {
	defer func() { fmt.Println(name, recover()) }()
	f()
}

func main() {
	pw = true
	try("any8(uint8(gp.a), wGp())", func() { gp = nil; sink = any8(uint8(gp.a), wGp()) })
	try("b2u(*gq == v1) + wGqv()", func() { gq = nil; sink = b2u(*gq == v1) + wGqv() })
	try("any8(uint8(gp.a), wGpN())", func() { gp = nil; sink = any8(uint8(gp.a), wGpN()) })
	try("x := gp.a; x + wGp()", func() { gp = nil; x := gp.a; sink = x + wGp() })
}
