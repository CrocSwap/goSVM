package main

type S struct {
	A any
	B int
}
type S2 struct{ a, b int }
type Named int

const Cn Named = 7

type PM struct {
	m map[int][]int
	n map[int]int
}

var pm *PM
var gm map[int][]int
var gt T
var garr [3]int

func setPMI() int {
	pm = &PM{m: map[int][]int{0: {9}, 2: {8}}, n: map[int]int{0: 9, 2: 8}}
	i = 2
	return 0
}
func setGMI() int                  { i = 2; return 0 }
func newGM() int                   { gm = map[int][]int{0: {7}}; return 0 }
func setGT() int                   { gt = T{5, 6}; return 0 }
func setSl(a []int) int            { a[0] = 10; return 0 }
func eqArr(a [2]int, _ int) [2]int { return a }
func (t *T) Set() int              { t.a = 10; return 0 }

type S3 struct{ a uint8 }
type NS string

var x8 uint8 = 1
var x64 uint64 = 1
var gm8 map[uint8][]int
var gm64 map[uint64][]int
var gms map[string][]int
var gmn map[NS][]int
var ns NS
var gp *T
var gmp map[*T][]int

func setX8() int     { x8 = 10; return 0 }
func setX64() int    { x64 = 10; return 0 }
func setNS() int     { ns = "zz"; return 0 }
func setGP() int     { gp = &T{}; return 0 }

func sv2(a []int) []int { return a }
