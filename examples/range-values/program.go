// Package rangevalues is an ordinary-Go oracle for Go 1.22 range loops in the
// on-chain subset. The same source is compiled natively and through goSVM;
// tests compare the 24-byte state and status (or a panic) for every vector.
package rangevalues

type Key [8]byte
type Words [4]uint16
type Box struct {
	Key Key
	N   uint64
}

// bump records every evaluation of a range operand in s[23].
func bump(s []byte) uint64 { s[23] = s[23] + 1; return uint64(s[23]) }
func pick(p *Key, s []byte) *Key {
	bump(s)
	return p
}
func copyOf(k Key, s []byte) Key {
	bump(s)
	return k
}
func boxed(b *Box, s []byte) *Box {
	bump(s)
	return b
}
func nilKey(s []byte) *Key {
	bump(s)
	var p *Key
	return p
}
func load(s []byte) Key {
	var k Key
	for j := range k {
		k[j] = s[j]
	}
	return k
}

func Process(s, i []byte) uint64 {
	mode := i[0] % 20
	n := uint64(i[1])
	if mode == 0 {
		// Byte slice: key/value, key only, value only and no variables.
		sum := byte(0)
		for j, v := range s[:n%24] {
			sum = sum + v*byte(j)
			s[j] = v ^ i[2]
		}
		for j := range s[n%24:] {
			s[uint64(j)+n%24] = s[uint64(j)+n%24] + 1
		}
		count := uint64(0)
		for _, v := range i {
			count = count + uint64(v)
		}
		for range s {
			count++
		}
		s[22] = sum
		return count % 7
	}
	if mode == 1 {
		// Integer counts: typed unsigned, untyped constant and len results.
		total := uint64(0)
		for j := range n % 40 {
			total = total + j*j
		}
		var small uint8 = i[2]
		for j := range small {
			total = total + uint64(j)
		}
		for j := range 5 {
			total = total + uint64(j)*100
		}
		for range len(s) {
			total++
		}
		for j := range uint16(i[3]) {
			total = total + uint64(j>>1)
		}
		for range n % 3 {
			total = total + 1000
		}
		for j := range uint32(i[4] % 9) {
			total = total + uint64(j)<<8
		}
		putTotal(s, total)
		return 0
	}
	if mode == 2 {
		// Array value: Go ranges over a copy; body writes are not observed.
		a := load(s)
		for j, v := range a {
			a[(uint64(j)+1)%8] = a[(uint64(j)+1)%8] + i[2]
			s[8+uint64(j)] = v
		}
		for j := range a {
			s[16+uint64(j)] = a[j]
		}
		return 0
	}
	if mode == 3 {
		// Pointer to array: reads go through the pointer and see body writes.
		a := load(s)
		p := &a
		for j, v := range p {
			p[(uint64(j)+1)%8] = p[(uint64(j)+1)%8] + i[2]
			s[8+uint64(j)] = v
		}
		for j, v := range &a {
			s[16+uint64(j)] = v + byte(uint64(j))
		}
		return 0
	}
	if mode == 4 {
		// Slice header and length are fixed at loop start; elements are shared.
		t := s[:n%24]
		count := uint64(0)
		for j, v := range t {
			if j == 0 {
				t = t[:0]
				if n%24 > 1 {
					s[1] = i[2]
				}
			}
			s[16+uint64(j)%8] = v
			count++
		}
		return count + uint64(len(t))*100
	}
	if mode == 5 {
		// Assignment forms keep the last value; zero iterations leave it.
		var k uint8 = 77
		for k = range uint8(n % 10) {
		}
		var v byte = 99
		for _, v = range s[:i[2]%5] {
		}
		var w uint64 = 5
		for w = range n % 4 {
			s[4] = s[4] + byte(w)
		}
		var x byte
		for _, x = range load(s) {
			s[5] = s[5] ^ x
		}
		s[0], s[1], s[2] = k, v, byte(w)
		return 0
	}
	if mode == 6 {
		// break and continue, including continue from a nested switch.
		count := uint64(0)
		for j, v := range s {
			switch v % 4 {
			case 0:
				continue
			case 1:
				count = count + 10
			}
			if uint64(j) == n%24 {
				break
			}
			if v == i[2] {
				continue
			}
			count++
		}
		for j := range n % 50 {
			if j == 30 {
				break
			}
			if j%3 == 0 {
				continue
			}
			count = count + 100
		}
		putTotal(s, count)
		return 0
	}
	if mode == 7 {
		// Empty and zero-length operands, and index-only ranges over nil
		// array pointers (len is constant; the pointer is never read).
		count := uint64(0)
		var zero [0]byte
		var none *[0]byte
		var missing *Key
		var empty []byte
		for range s[:0] {
			count++
		}
		for range 0 {
			count++
		}
		for range n * 0 {
			count++
		}
		for range empty {
			count++
		}
		for _, v := range zero {
			count = count + uint64(v)
		}
		for _, v := range none {
			count = count + uint64(v)
		}
		for j := range missing {
			count = count + 1 + uint64(j)
		}
		for range *missing {
			count = count + 100
		}
		putTotal(s, count)
		return 0
	}
	if mode == 8 {
		// A value variable reads through a nil pointer: Go panics.
		var missing *Key
		k := load(s)
		if n%2 == 0 {
			missing = &k
		}
		for _, v := range missing {
			s[0] = s[0] + v
		}
		return 0
	}
	if mode == 9 {
		// The operand is evaluated once; index-only ranges still run its calls.
		a := load(s)
		count := uint64(0)
		for j, v := range pick(&a, s) {
			a[j] = v + 1
			count++
		}
		for range pick(&a, s) {
			count++
		}
		for j := range copyOf(a, s) {
			count = count + uint64(j)
		}
		for range *nilKey(s) {
			count++
		}
		for j := range bump(s) % 4 {
			count = count + j
		}
		for _, v := range s[:bump(s)%8] {
			count = count + uint64(v)
		}
		s[0] = a[0]
		putTotal(s[8:], count)
		return 0
	}
	if mode == 10 {
		// uint16 counts and arrays of uint16 words.
		var w Words
		for j := range w {
			w[j] = uint16(s[2*uint64(j)]) | uint16(s[2*uint64(j)+1])<<8
		}
		total := uint16(0)
		for j, v := range w {
			total = total + v*uint16(j)
			w[j] = v - uint16(i[2])
		}
		for j := range uint16(n) {
			total = total ^ (j << 3)
		}
		for j, v := range &w {
			s[8+2*uint64(j)] = byte(v)
			s[9+2*uint64(j)] = byte(v >> 8)
		}
		s[0], s[1] = byte(total), byte(total>>8)
		return 0
	}
	if mode == 11 {
		// The int key: indexing, conversion, comparison and shift counts.
		var a Key
		for j := range a {
			if j >= 2 && j < 6 {
				a[j] = byte(1 << j)
			}
			if j != 7 {
				a[j] = a[j] | s[j]>>j
			}
		}
		for j, v := range a {
			s[8+uint64(j)] = v
			s[16+uint64(j)] = byte(uint64(j) * 3)
			if len(a) == j {
				return 9
			}
		}
		return 0
	}
	if mode == 12 {
		// Fresh iteration variables: assigning or taking addresses inside the
		// body changes only this iteration's copy.
		count := uint64(0)
		for j, v := range s[:8] {
			p := &v
			*p = *p + 1
			s[8+uint64(j)] = v
			j = 0
			s[j] = s[j] + 1
			count++
		}
		for j := range uint8(4) {
			q := &j
			*q = *q + 10
			s[16+uint64(j)%8] = j
		}
		return count
	}
	if mode == 13 {
		// Nested ranges and break exits only the innermost loop.
		count := uint64(0)
		for j := range uint8(n % 6) {
			for k, v := range s[:4] {
				if uint64(k) == uint64(j) {
					break
				}
				count = count + uint64(v) + uint64(j)
			}
		}
		putTotal(s, count)
		return 0
	}
	if mode == 14 {
		// Fields: arrays inside structs, by value and through a pointer.
		b := Box{Key: load(s), N: n}
		for j, v := range b.Key {
			b.Key[j] = 0
			s[8+uint64(j)] = v
		}
		pb := &b
		for j := range pb.Key {
			pb.Key[j] = byte(uint64(j) + pb.N)
		}
		for j, v := range boxed(pb, s).Key {
			s[16+uint64(j)] = v
		}
		return 0
	}
	if mode == 15 {
		// A range body that returns early, and the iteration variables are
		// not visible after the loop.
		for j, v := range s {
			if v == i[2] {
				return uint64(j) + 1
			}
		}
		return 0
	}
	if mode == 16 {
		// Keys are not adjusted by body writes to the slice variable.
		t := s[:8]
		for j, v := range t {
			t = s[8:]
			t[j] = v
		}
		return 0
	}
	if mode == 17 {
		// Bool arrays and uint32 arrays.
		var flags [6]bool
		for j := range flags {
			flags[j] = s[j]%2 == 1
		}
		var words [3]uint32
		for j, f := range flags {
			if f {
				words[uint64(j)%3] = words[uint64(j)%3] + uint32(s[j])<<8
			}
		}
		for j, w := range words {
			s[8+4*uint64(j)] = byte(w >> 8)
			s[9+4*uint64(j)] = byte(w >> 16)
		}
		return 0
	}
	if mode == 18 {
		// Index out of range inside a range body still panics.
		for j := range n % 30 {
			s[j] = s[j] + 1
		}
		return 0
	}
	// Large uint64 count with an early exit.
	count := uint64(0)
	for j := range ^uint64(0) {
		if j == n {
			break
		}
		count++
	}
	putTotal(s, count)
	return 0
}

func putTotal(s []byte, n uint64) {
	for j := range uint64(8) {
		s[j] = byte(n >> (j * 8))
	}
}
