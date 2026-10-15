// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// goSVM on-chain overlay of IsSpace from Go 1.22.0 unicode/graphic.go, with
// isExcludingLatin and is16 from unicode/letter.go specialized to
// White_Space's table. Change (marked "goSVM:"): Go passes the table
// (White_Space, a *RangeTable) to isExcludingLatin and is16; goSVM reads the
// read-only whiteSpaceR16 directly. White_Space has no R32 entries.

package unicode

// IsSpace reports whether the rune is a space character as defined
// by Unicode's White Space property; in the Latin-1 space
// this is
//
//	'\t', '\n', '\v', '\f', '\r', ' ', U+0085 (NEL), U+00A0 (NBSP).
//
// Other definitions of spacing characters are set by category
// Z and property [Pattern_White_Space].
func IsSpace(r rune) bool {
	// This property isn't the same as Z; special-case it.
	if uint32(r) <= MaxLatin1 {
		switch r {
		case '\t', '\n', '\v', '\f', '\r', ' ', 0x85, 0xA0:
			return true
		}
		return false
	}
	return isWhiteSpaceExcludingLatin(r) // goSVM: isExcludingLatin(White_Space, r)
}

// isWhiteSpaceExcludingLatin is isExcludingLatin(White_Space, r).
func isWhiteSpaceExcludingLatin(r rune) bool {
	// Compare as uint32 to correctly handle negative runes.
	if off := whiteSpaceLatinOffset; len(whiteSpaceR16) > off && uint32(r) <= uint32(whiteSpaceR16[len(whiteSpaceR16)-1].Hi) {
		return isWhiteSpace16(off, uint16(r)) // goSVM: is16(r16[off:], uint16(r))
	}
	// goSVM: White_Space.R32 is empty.
	return false
}

// linearMax is the maximum size table for linear search for non-Latin1 rune.
// Derived by running 'go test -calibrate'.
const linearMax = 18

// isWhiteSpace16 is is16(whiteSpaceR16[off:], r).
func isWhiteSpace16(off int, r uint16) bool {
	if len(whiteSpaceR16)-off <= linearMax || r <= MaxLatin1 {
		for i := off; i < len(whiteSpaceR16); i++ {
			range_ := whiteSpaceR16[i]
			if r < range_.Lo {
				return false
			}
			if r <= range_.Hi {
				return range_.Stride == 1 || (r-range_.Lo)%range_.Stride == 0
			}
		}
		return false
	}

	// binary search over ranges
	lo := off
	hi := len(whiteSpaceR16)
	for lo < hi {
		m := int(uint(lo+hi) >> 1)
		range_ := whiteSpaceR16[m]
		if range_.Lo <= r && r <= range_.Hi {
			return range_.Stride == 1 || (r-range_.Lo)%range_.Stride == 0
		}
		if r < range_.Lo {
			hi = m
		} else {
			lo = m + 1
		}
	}
	return false
}
