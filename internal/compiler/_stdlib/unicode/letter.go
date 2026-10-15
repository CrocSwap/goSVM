// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package unicode provides data and functions to test some properties of
// Unicode code points.
//
// goSVM on-chain overlay of part of Go 1.22.0 unicode/letter.go: the
// constants, the table types, the case mappings (To, ToUpper, ToLower,
// ToTitle) and SimpleFold, which strings and bytes use. Change (marked
// "goSVM:"): to reads CaseRanges itself instead of taking the table as an
// argument, so the table stays read-only data (passing it would make goSVM
// copy it to the heap on every invocation). The property tables and their
// predicates (IsUpper, IsLetter, ...) and SpecialCase are left out
// (docs/GO_STDLIB.md).
package unicode

const (
	MaxRune         = '\U0010FFFF' // Maximum valid Unicode code point.
	ReplacementChar = '\uFFFD'     // Represents invalid code points.
	MaxASCII        = '\u007F'     // maximum ASCII value.
	MaxLatin1       = '\u00FF'     // maximum Latin-1 value.
)

// RangeTable defines a set of Unicode code points by listing the ranges of
// code points within the set. The ranges are listed in two slices
// to save space: a slice of 16-bit ranges and a slice of 32-bit ranges.
// The two slices must be in sorted order and non-overlapping.
// Also, R32 should contain only values >= 0x10000 (1<<16).
type RangeTable struct {
	R16         []Range16
	R32         []Range32
	LatinOffset int // number of entries in R16 with Hi <= MaxLatin1
}

// Range16 represents of a range of 16-bit Unicode code points. The range runs from Lo to Hi
// inclusive and has the specified stride.
type Range16 struct {
	Lo     uint16
	Hi     uint16
	Stride uint16
}

// Range32 represents of a range of Unicode code points and is used when one or
// more of the values will not fit in 16 bits. The range runs from Lo to Hi
// inclusive and has the specified stride. Lo and Hi must always be >= 1<<16.
type Range32 struct {
	Lo     uint32
	Hi     uint32
	Stride uint32
}

// CaseRange represents a range of Unicode code points for simple (one
// code point to one code point) case conversion.
// The range runs from Lo to Hi inclusive, with a fixed stride of 1. Deltas
// are the number to add to the code point to reach the code point for a
// different case for that character. They may be negative. If zero, it
// means the character is in the corresponding case. There is a special
// case representing sequences of alternating corresponding Upper and Lower
// pairs. It appears with a fixed Delta of
//
//	{UpperLower, UpperLower, UpperLower}
//
// The constant UpperLower has an otherwise impossible delta value.
type CaseRange struct {
	Lo    uint32
	Hi    uint32
	Delta d
}

// Indices into the Delta arrays inside CaseRanges for case mapping.
const (
	UpperCase = iota
	LowerCase
	TitleCase
	MaxCase
)

type d [MaxCase]rune // to make the CaseRanges text shorter

// If the Delta field of a [CaseRange] is UpperLower, it means
// this CaseRange represents a sequence of the form (say)
// [Upper] [Lower] [Upper] [Lower].
const (
	UpperLower = MaxRune + 1 // (Cannot be a valid delta.)
)

// to maps the rune using the specified case mapping.
// It additionally reports whether caseRange contained a mapping for r.
//
// goSVM: Go passes the table as caseRange []CaseRange; this reads
// CaseRanges, the only table goSVM's unicode has.
func to(_case int, r rune) (mappedRune rune, foundMapping bool) {
	if _case < 0 || MaxCase <= _case {
		return ReplacementChar, false // as reasonable an error as any
	}
	// binary search over ranges
	lo := 0
	hi := len(CaseRanges)
	for lo < hi {
		m := int(uint(lo+hi) >> 1)
		cr := CaseRanges[m]
		if rune(cr.Lo) <= r && r <= rune(cr.Hi) {
			delta := cr.Delta[_case]
			if delta > MaxRune {
				// In an Upper-Lower sequence, which always starts with
				// an UpperCase letter, the real deltas always look like:
				//	{0, 1, 0}    UpperCase (Lower is next)
				//	{-1, 0, -1}  LowerCase (Upper, Title are previous)
				// The characters at even offsets from the beginning of the
				// sequence are upper case; the ones at odd offsets are lower.
				// The correct mapping can be done by clearing or setting the low
				// bit in the sequence offset.
				// The constants UpperCase and TitleCase are even while LowerCase
				// is odd so we take the low bit from _case.
				return rune(cr.Lo) + ((r-rune(cr.Lo))&^1 | rune(_case&1)), true
			}
			return r + delta, true
		}
		if r < rune(cr.Lo) {
			hi = m
		} else {
			lo = m + 1
		}
	}
	return r, false
}

// To maps the rune to the specified case: [UpperCase], [LowerCase], or [TitleCase].
func To(_case int, r rune) rune {
	r, _ = to(_case, r) // goSVM: to(_case, r, CaseRanges)
	return r
}

// ToUpper maps the rune to upper case.
func ToUpper(r rune) rune {
	if r <= MaxASCII {
		if 'a' <= r && r <= 'z' {
			r -= 'a' - 'A'
		}
		return r
	}
	return To(UpperCase, r)
}

// ToLower maps the rune to lower case.
func ToLower(r rune) rune {
	if r <= MaxASCII {
		if 'A' <= r && r <= 'Z' {
			r += 'a' - 'A'
		}
		return r
	}
	return To(LowerCase, r)
}

// ToTitle maps the rune to title case.
func ToTitle(r rune) rune {
	if r <= MaxASCII {
		if 'a' <= r && r <= 'z' { // title case is upper case for ASCII
			r -= 'a' - 'A'
		}
		return r
	}
	return To(TitleCase, r)
}

// caseOrbit is defined in tables.go as []foldPair. Right now all the
// entries fit in uint16, so use uint16. If that changes, compilation
// will fail (the constants in the composite literal will not fit in uint16)
// and the types here can change to uint32.
type foldPair struct {
	From uint16
	To   uint16
}

// SimpleFold iterates over Unicode code points equivalent under
// the Unicode-defined simple case folding. Among the code points
// equivalent to rune (including rune itself), SimpleFold returns the
// smallest rune > r if one exists, or else the smallest rune >= 0.
// If r is not a valid Unicode code point, SimpleFold(r) returns r.
//
// For example:
//
//	SimpleFold('A') = 'a'
//	SimpleFold('a') = 'A'
//
//	SimpleFold('K') = 'k'
//	SimpleFold('k') = '\u212A' (Kelvin symbol, K)
//	SimpleFold('\u212A') = 'K'
//
//	SimpleFold('1') = '1'
//
//	SimpleFold(-2) = -2
func SimpleFold(r rune) rune {
	if r < 0 || r > MaxRune {
		return r
	}

	if int(r) < len(asciiFold) {
		return rune(asciiFold[r])
	}

	// Consult caseOrbit table for special cases.
	lo := 0
	hi := len(caseOrbit)
	for lo < hi {
		m := int(uint(lo+hi) >> 1)
		if rune(caseOrbit[m].From) < r {
			lo = m + 1
		} else {
			hi = m
		}
	}
	if lo < len(caseOrbit) && rune(caseOrbit[lo].From) == r {
		return rune(caseOrbit[lo].To)
	}

	// No folding specified. This is a one- or two-element
	// equivalence class containing rune and ToLower(rune)
	// and ToUpper(rune) if they are different from rune.
	if l := ToLower(r); l != r {
		return l
	}
	return ToUpper(r)
}
