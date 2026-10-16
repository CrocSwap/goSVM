// Package num is a 128-bit number parsed from hexadecimal text, the way
// holiman/uint256's MustFromHex builds computed constants: package-level
// variables whose values are fully determined at compile time.
package num

import (
	"errors"
	"math/bits"
)

// U128 is a 128-bit unsigned integer.
type U128 struct{ Lo, Hi uint64 }

// Sentinel errors, compared by identity (errors.Is).
var (
	ErrEmpty  = errors.New("num: empty")
	ErrSyntax = errors.New("num: invalid digit")
	ErrRange  = errors.New("num: overflow")
)

func digit(c byte) (uint64, bool) {
	switch {
	case c >= '0' && c <= '9':
		return uint64(c - '0'), true
	case c >= 'a' && c <= 'f':
		return uint64(c-'a') + 10, true
	case c >= 'A' && c <= 'F':
		return uint64(c-'A') + 10, true
	}
	return 0, false
}

// Parse parses "0x" followed by up to 32 hexadecimal digits.
func Parse(s string) (*U128, error) {
	if len(s) < 3 || s[0] != '0' || s[1] != 'x' {
		return nil, ErrEmpty
	}
	if len(s) > 34 {
		return nil, ErrRange
	}
	var z U128
	for i := 2; i < len(s); i++ {
		d, ok := digit(s[i])
		if !ok {
			return nil, ErrSyntax
		}
		z.Hi = z.Hi<<4 | z.Lo>>60
		z.Lo = z.Lo<<4 | d
	}
	return &z, nil
}

// MustParse is Parse that panics on an error, as MustFromHex does.
func MustParse(s string) *U128 {
	z, err := Parse(s)
	if err != nil {
		panic(err)
	}
	return z
}

// Add returns x + y, wrapping.
func (x *U128) Add(y *U128) *U128 {
	lo, carry := bits.Add64(x.Lo, y.Lo, 0)
	hi, _ := bits.Add64(x.Hi, y.Hi, carry)
	return &U128{lo, hi}
}

// Mul64 sets x to x * m (wrapping) and returns x.
func (x *U128) Mul64(m uint64) *U128 {
	hi, lo := bits.Mul64(x.Lo, m)
	x.Hi = x.Hi*m + hi
	x.Lo = lo
	return x
}

// Hex spells x in hexadecimal without leading zeros.
func (x U128) Hex() string {
	const digits = "0123456789abcdef"
	var buf [32]byte
	n := 0
	for i := 0; i < 32; i++ {
		var nib uint64
		if i < 16 {
			nib = x.Hi >> (60 - 4*i) & 15
		} else {
			nib = x.Lo >> (60 - 4*(i-16)) & 15
		}
		if n == 0 && nib == 0 && i < 31 {
			continue
		}
		buf[n] = digits[nib]
		n++
	}
	return "0x" + string(buf[:n])
}

// Pow10 is 10^n, by repeated multiplication.
func Pow10(n int) *U128 {
	z := &U128{Lo: 1}
	for i := 0; i < n; i++ {
		z.Mul64(10)
	}
	return z
}

var (
	One  = MustParse("0x1")
	Max  = MustParse("0xffffffffffffffffffffffffffffffff")
	Q64  = MustParse("0x10000000000000000")
	E30  = Pow10(30)
	Sum  = One.Add(Q64)
	Half = U128{Lo: ^uint64(0) >> 1}
)

// Table aliases One: its first element is the same object.
var Table = []*U128{One, MustParse("0x10"), MustParse("0x100"), E30}
