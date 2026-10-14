// Package uint16values is an ordinary-Go oracle for uint16 in the on-chain
// subset: wrapping arithmetic, shifts, conversions, comparisons and uint16
// storage in locals, parameters, results, fields, arrays and pointers.
package uint16values

type Port uint16

const Limit Port = 65000
const Big uint16 = 0xfff0

type Pair struct {
	Lo   uint16
	Hi   Port
	Tags [3]uint16
	N    uint32
}

func read16(s []byte, off uint64) uint16 { return uint16(s[off]) | uint16(s[off+1])<<8 }
func write16(s []byte, off uint64, v uint16) {
	s[off] = byte(v)
	s[off+1] = byte(v >> 8)
}
func split(v uint16) (uint8, uint8) { return uint8(v), uint8(v >> 8) }
func bump(p *uint16) uint16 {
	*p = *p + 1
	return *p
}
func (p Port) Next() Port  { return p + 1 }
func (p *Pair) Swap() Port { p.Lo, p.Hi = uint16(p.Hi), Port(p.Lo); return p.Hi }

func Process(s, i []byte) uint64 {
	a, b := read16(i, 1), read16(i, 3)
	c := i[5] // shift count; edge vectors use 0, 15, 16, 17 and 255
	mode := i[0] % 6
	if mode == 0 {
		// Wrapping arithmetic and bitwise operators.
		write16(s, 0, a+b)
		write16(s, 2, a-b)
		write16(s, 4, a*b)
		write16(s, 6, a&b)
		write16(s, 8, a|b)
		write16(s, 10, a^b)
		write16(s, 12, a&^b)
		write16(s, 14, -a)
		write16(s, 16, ^a)
		write16(s, 18, a/b)
		write16(s, 20, a%b)
		return 0
	}
	if mode == 1 {
		// Shifts by counts at and past the width yield zero.
		write16(s, 0, a<<c)
		write16(s, 2, a>>c)
		write16(s, 4, a<<uint64(c))
		write16(s, 6, a>>uint16(c))
		write16(s, 8, a<<15)
		write16(s, 10, a>>15)
		var w uint16 = 1
		w = w << (c % 20)
		write16(s, 12, w)
		write16(s, 14, Big>>(c%17))
		write16(s, 16, uint16(uint64(a)<<c))
		return 0
	}
	if mode == 2 {
		// Conversions to and from every unsigned width.
		wide := uint64(a)<<32 | uint64(b)<<8 | uint64(c)
		write16(s, 0, uint16(wide))
		write16(s, 2, uint16(uint32(wide>>8)))
		write16(s, 4, uint16(c)*257)
		s[6] = uint8(a)
		s[7] = byte(b >> 4)
		x := uint32(a) * uint32(b)
		write16(s, 8, uint16(x>>16))
		write16(s, 10, uint16(x))
		lo, hi := split(a + b)
		s[12], s[13] = hi, lo
		write16(s, 14, uint16(len(s))+a)
		write16(s, 16, uint16(uint8(a)+uint8(b)))
		return uint64(a) * uint64(b) % 5
	}
	if mode == 3 {
		// Comparisons, switch tags and named uint16 types with methods.
		flags := uint64(0)
		if a < b {
			flags = flags | 1
		}
		if a <= b {
			flags = flags | 2
		}
		if a == b {
			flags = flags | 4
		}
		if a != b {
			flags = flags | 8
		}
		if a > Big {
			flags = flags | 16
		}
		p := Port(a)
		if p >= Limit {
			flags = flags | 32
		}
		switch b {
		case 0:
			flags = flags | 64
		case 65535, 1:
			flags = flags | 128
		default:
			if b%2 == 0 {
				flags = flags | 256
			}
		}
		write16(s, 0, uint16(p.Next()))
		write16(s, 2, uint16(flags))
		return flags % 3
	}
	if mode == 4 {
		// Storage: fields, arrays, pointers, increments and decrements.
		var pair Pair
		pair.Lo = a
		pair.Hi = Port(b)
		pair.Tags[0] = a
		pair.Tags[1] = b
		pair.Tags[2]--
		pair.Lo++
		pair.Hi--
		pair.N = uint32(a) + uint32(b)
		hi := pair.Swap()
		ptr := &pair.Tags[1]
		*ptr = *ptr + uint16(c)
		*ptr++
		n := bump(&pair.Lo)
		local := a
		local++
		local--
		local--
		words := [4]uint16{a, b, 65535, 0}
		words[3]--
		words[2]++
		write16(s, 0, pair.Lo)
		write16(s, 2, uint16(pair.Hi))
		write16(s, 4, pair.Tags[0])
		write16(s, 6, pair.Tags[1])
		write16(s, 8, pair.Tags[2])
		write16(s, 10, uint16(pair.N>>1))
		write16(s, 12, uint16(hi)+n)
		write16(s, 14, local)
		write16(s, 16, words[2])
		write16(s, 18, words[3])
		if words != [4]uint16{a, b, 0, 65535} {
			return 1
		}
		return 0
	}
	// Division by zero still panics; quotient and remainder otherwise.
	write16(s, 0, a/b)
	write16(s, 2, a%b)
	return 0
}
