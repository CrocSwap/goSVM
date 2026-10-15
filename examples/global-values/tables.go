package globalvalues

import (
	"gosvm/examples/global-values/alpha"
	"gosvm/examples/global-values/beta"
	"gosvm/examples/global-values/log"
	"gosvm/examples/global-values/zeta"
)

// Byte-order singletons: empty structs with value methods, as in
// encoding/binary. Never written: read-only, and they take no storage.
type bigEndian struct{}

func (bigEndian) Uint16(b []byte) uint16 { return uint16(b[1]) | uint16(b[0])<<8 }
func (bigEndian) Uint32(b []byte) uint32 {
	return uint32(b[3]) | uint32(b[2])<<8 | uint32(b[1])<<16 | uint32(b[0])<<24
}

type littleEndian struct{}

func (littleEndian) Uint16(b []byte) uint16 { return uint16(b[0]) | uint16(b[1])<<8 }

var BigEndian bigEndian
var LittleEndian littleEndian

// Read-only lookup tables (constant initializers, never written).
var pows64 = [...]uint64{
	1e00, 1e01, 1e02, 1e03, 1e04, 1e05, 1e06, 1e07, 1e08, 1e09,
	1e10, 1e11, 1e12, 1e13, 1e14, 1e15, 1e16, 1e17, 1e18, 1e19,
}

type powerOf2 struct {
	bits  uint16
	limbs [4]uint64
	neg   bool
}

var table = []powerOf2{
	{1, [4]uint64{2, 0, 0, 0}, false},
	{64, [4]uint64{0, 1, 0, 0}, false},
	{bits: 128, limbs: [4]uint64{2: 1}},
	{bits: 255, limbs: [4]uint64{3: 1 << 63}, neg: true},
	{},
	{bits: 7, limbs: [4]uint64{128, 0, 0, 0}},
}

var negatives = [4]int16{-1, -300, 7, -32768}
var flags = [3]bool{true, false, true}
var zeroTable [5]uint32
var names = []string{"add", "sub", "mul", "div"}

type Config struct {
	Limit uint64
	Shift uint8
	On    bool
	Mask  [2]uint32
}

var config = Config{Limit: 1 << 40, Shift: 3, On: true, Mask: [2]uint32{0xff, 0xf0f0}}

// Written state.
var counter uint64
var lastNext uint64
var point *uint64
var acc []uint64
var stash func() uint64

func script(ops []byte) uint64 {
	for k := 0; k+1 < len(ops) && k < 64; k += 2 {
		arg := uint64(ops[k+1])
		switch ops[k] % 13 {
		case 0:
			counter += arg
		case 1:
			writable[arg&3] = writable[arg&3]*3 + arg
		case 2:
			wp[arg&3]++
		case 3:
			view[arg&1] += arg
		case 4:
			lastNext = next()
		case 5:
			registry[names[arg&3]] += arg
		case 6:
			alpha.S.Bump(arg)
		case 7:
			beta.Swap(beta.Ops[arg%3])
		case 8:
			*cell += uint32(arg)
		case 9:
			built = built + string(rune('a'+arg%26))
		case 10:
			log.Mark(byte(arg))
		case 11:
			point = &writable[arg&3]
			*point ^= arg
		case 12:
			stash = func() uint64 { return counter * 2 }
			counter = stash() + 1
		}
	}
	for _, v := range writable {
		u64(v)
	}
	u64(view[0] + view[1])
	u64(counter)
	u64(lastNext)
	u64(registry["add"])
	u64(registry["sub"])
	u64(registry["mul"])
	u64(registry["div"])
	u64(registry["a"])
	u64(uint64(len(registry)))
	u64(alpha.S.N)
	u64(alpha.Counter)
	u64(alpha.S.Total())
	u64(beta.Default(5, 3))
	u64(uint64(*cell))
	str(built)
	u64(uint64(len(log.Trace)))
	u64(uint64(log.Last))
	if point != nil {
		u64(*point)
	}
	return counter
}

func tables(in []byte) uint64 {
	i := in[0]
	u64(pows64[i%24]) // 20 entries: 20..23 panic
	e := table[i%8]   // 6 entries: 6 and 7 panic
	u64(uint64(e.bits))
	for _, l := range e.limbs {
		u64(l)
	}
	u64(flag(e.neg))
	sum := uint64(0)
	for j, p := range table {
		sum += uint64(j) * uint64(p.bits)
	}
	u64(sum)
	str(names[in[1]%5]) // 4 entries
	u64(uint64(negatives[in[2]&3]))
	u64(uint64(int64(negatives[in[2]&3]) * int64(in[3])))
	u64(flag(flags[in[3]%3]))
	u64(uint64(zeroTable[in[4]%5]))
	u64(config.Limit >> config.Shift)
	u64(uint64(config.Mask[in[5]&1]))
	u64(uint64(BigEndian.Uint32(in[6:10])))
	u64(uint64(LittleEndian.Uint16(in[6:8])))
	u64(uint64(greeting[in[10]%16])) // 12 bytes: 12..15 panic
	u64(uint64(len(table)) + uint64(cap(table)))
	u64(uint64(beta.Fib[in[11]%9])) // 8 entries
	c := config
	c.Limit++
	u64(c.Limit - config.Limit)
	return uint64(i)
}

func functions(in []byte) uint64 {
	for j := 0; j < int(in[0]%8); j++ {
		lastNext = next()
	}
	u64(lastNext)
	u64(beta.Ops[in[1]%4](uint64(in[2]), uint64(in[3]))) // 3 entries
	u64(beta.Default(uint64(in[2]), uint64(in[3])))
	old := beta.Swap(beta.Ops[2])
	u64(old(10, 4))
	u64(beta.Default(10, 4))
	u64(hook(uint64(in[4])))
	u64(zeta.Hook(uint64(in[5])))
	u64(uint64(zeta.Calls))
	alpha.Bound(uint64(in[6]))
	u64(alpha.S.N)
	u64(uint64(alpha.S.Hist[in[6]&3]))
	h := handlers[in[7]%4] // 1 and 2 are set: 0 and 3 are nil
	u64(h(uint64(in[8])))
	if in[9] == 255 {
		u64(nilFn())
	}
	stash = func() uint64 { counter++; return counter }
	u64(stash() + stash())
	return counter
}

func find(name string) (uint64, error) {
	if v, ok := registry[name]; ok {
		return v, nil
	}
	if name == "" {
		return 0, ErrCode
	}
	return 0, ErrNotFound
}

func classify(err error) uint64 {
	switch e := err.(type) {
	case nil:
		return 0
	case *Err:
		if e == ErrOther {
			return 2
		}
		return 1
	case CodeErr:
		return 10 + uint64(e.code)
	}
	return 99
}

func errorsAndMaps(in []byte) uint64 {
	keys := []string{"a", "b", "zz", "", "c"}
	for _, k := range keys[:1+in[0]%5] {
		v, err := find(k)
		u64(v)
		u64(classify(err))
		u64(flag(err == ErrNotFound))
		u64(flag(err == ErrCode))
	}
	var e error = ErrOther
	u64(classify(e))
	u64(flag(e == error(ErrOther)))
	registry[greeting[:in[1]%6]] = uint64(in[2])
	u64(uint64(len(registry)))
	delete(registry, "b")
	u64(uint64(len(registry)))
	_, ok := registry["b"]
	u64(flag(ok))
	anything = registry["a"] + 1
	u64(anything.(uint64))
	if in[3] == 255 {
		nilMap[1] = 2
	}
	u64(uint64(nilMap[3]))
	if in[4] == 255 {
		u64(uint64(anything.(uint32)))
	}
	return uint64(len(registry))
}

func crossPackage(in []byte) uint64 {
	alpha.P.Bump(uint64(in[0]))
	*alpha.Q = *alpha.Q + uint64(in[1])
	p := &beta.Mutable[in[2]&3]
	*p = *p * 2
	beta.Mutable[in[3]%5]++ // 4 entries
	beta.Default = beta.Ops[1]
	zeta.Hook = func(x uint64) uint64 { return x + zeta.Base }
	u64(zeta.Hook(uint64(in[4])))
	u64(hook(uint64(in[4]))) // still the original closure
	u64(alpha.S.N)
	u64(*alpha.S.Last)
	u64(alpha.Counter)
	u64(alpha.P.Total())
	for _, v := range beta.Mutable {
		u64(uint64(v))
	}
	u64(beta.Default(9, 4))
	alpha.S.Tags = append(alpha.S.Tags, beta.Names[in[5]%3])
	u64(uint64(len(alpha.S.Tags)))
	str(alpha.S.Tags[len(alpha.S.Tags)-1])
	log.Mark('x')
	str(string(log.Trace))
	return alpha.Counter
}

func heap(in []byte) uint64 {
	n := int(in[0]) + int(in[1])<<8
	for j := 0; j < n; j++ {
		acc = append(acc, uint64(j))
	}
	u64(uint64(len(acc)))
	u64(uint64(cap(acc)))
	for j := 0; j < int(in[2]); j++ {
		log.Mark(byte(j))
	}
	u64(uint64(len(log.Trace)))
	u64(uint64(cap(log.Trace)))
	return uint64(len(acc))
}
