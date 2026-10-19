// Package program copies values larger than LLVM inlines through pointers
// that may alias, so the backend references memmove and the build links the
// standalone helper. Account 0 (writable, 545 bytes) holds a value: its
// buffer[529] and used u64le. The instruction's first byte selects a copy.
package program

import "gosvm/solana"

type value struct {
	buffer [529]byte
	used   uint64
}

func load(v *value, data []byte) {
	for i := uint64(0); i < 529; i++ {
		v.buffer[i] = data[i]
	}
	v.used = 0
	for j := uint64(0); j < 8; j++ {
		v.used = v.used | uint64(data[529+j])<<(j*8)
	}
}

func store(v *value, data []byte) {
	for i := uint64(0); i < 529; i++ {
		data[i] = v.buffer[i]
	}
	for j := uint64(0); j < 8; j++ {
		data[529+j] = byte(v.used >> (j * 8))
	}
}

// assign copies through pointers the compiler cannot prove distinct.
func assign(dst, src *value) { *dst = *src }

func Process(c solana.Context) uint64 {
	data := solana.Data(c, 0)
	ix := solana.Instruction(c)
	if len(data) != 537 || len(ix) != 1 {
		return 1
	}
	a := new(value)
	b := new(value)
	load(a, data)
	assign(b, a)
	b.buffer[0] = b.buffer[0] + ix[0]
	b.used++
	if ix[0] == 0 {
		assign(b, b)
	}
	assign(a, b)
	store(a, data)
	return 0
}
