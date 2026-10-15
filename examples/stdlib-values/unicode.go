package stdlibvalues

import (
	"math"
	"strings"
	"unicode"
	"unicode/utf8"
)

// UnicodeOps is the number of operations of the unicode group.
const UnicodeOps = 5

// runeFrom is a rune from two 16-bit values: any code point, surrogates,
// values above MaxRune and negative values.
func runeFrom(lo, hi int) rune {
	switch hi % 5 {
	case 0:
		return rune(lo & 0x7f)
	case 1:
		return rune(lo)
	case 2:
		return rune(lo) | rune(hi>>3&0x1f)<<16
	case 3:
		return -rune(lo) - 1
	}
	return rune(lo) | rune(hi)<<16
}

func unicodeGroup(w *W, v Input) {
	in := v.raw
	switch v.op % UnicodeOps {
	case 0: // rune classification and case mapping
		for _, r := range []rune{runeFrom(v.h0, v.h1), runeFrom(v.h2, v.h3), rune(v.h0), 0x212A, 0x17F, 0x1E9E, 0x3A3, 0x1C5, 0x10400, unicode.MaxRune, unicode.ReplacementChar} {
			w.u64(uint64(unicode.ToUpper(r)))
			w.u64(uint64(unicode.ToLower(r)))
			w.u64(uint64(unicode.ToTitle(r)))
			w.u64(uint64(unicode.To(v.n%4, r)))
			w.u64(uint64(unicode.SimpleFold(r)))
			w.flag(unicode.IsSpace(r))
		}
		for _, r := range v.a {
			w.u64(uint64(unicode.SimpleFold(r)))
			w.flag(unicode.IsSpace(r))
		}
		w.int(unicode.UpperCase + unicode.LowerCase<<2 + unicode.TitleCase<<4 + unicode.MaxCase<<6)
		w.u64(uint64(unicode.MaxASCII) + uint64(unicode.MaxLatin1)<<8 + uint64(unicode.UpperLower)<<32)
		w.str(unicode.Version)
		w.int(len(unicode.CaseRanges))
		cr := unicode.CaseRanges[v.h0%len(unicode.CaseRanges)]
		w.u64(uint64(cr.Lo) | uint64(cr.Hi)<<32)
		w.u64(uint64(cr.Delta[unicode.UpperCase]))
	case 1: // decoding
		p := in[8 : 8+v.h0%49]
		s := string(p)
		r, size := utf8.DecodeRune(p)
		w.u64(uint64(r))
		w.int(size)
		r, size = utf8.DecodeRuneInString(s)
		w.u64(uint64(r))
		w.int(size)
		r, size = utf8.DecodeLastRune(p)
		w.u64(uint64(r))
		w.int(size)
		r, size = utf8.DecodeLastRuneInString(s)
		w.u64(uint64(r))
		w.int(size)
		w.flag(utf8.FullRune(p))
		w.flag(utf8.FullRuneInString(s))
		w.int(utf8.RuneCount(p))
		w.int(utf8.RuneCountInString(s))
		w.flag(utf8.Valid(p))
		w.flag(utf8.ValidString(s))
		for j := 0; j < len(p) && j < 8; j++ {
			w.flag(utf8.RuneStart(p[j]))
			w.flag(utf8.FullRune(p[j:]))
		}
	case 2: // encoding
		r := runeFrom(v.h0, v.h1)
		w.int(utf8.RuneLen(r))
		w.flag(utf8.ValidRune(r))
		buf := make([]byte, utf8.UTFMax)
		n := utf8.EncodeRune(buf, r)
		w.bs(buf[:n])
		w.bs(utf8.AppendRune(fresh(v.b), r))
		w.bs(utf8.AppendRune(nil, runeFrom(v.h2, v.h3)))
		w.u64(uint64(utf8.RuneError) + uint64(utf8.RuneSelf)<<24 + uint64(utf8.MaxRune)<<32 + uint64(utf8.UTFMax)<<56)
	case 3: // EncodeRune into a buffer that is too small panics
		r := runeFrom(v.h0, v.h1)
		buf := make([]byte, v.h2%4)
		w.u8(0x55)
		w.int(utf8.EncodeRune(buf, r))
		w.bs(buf)
	case 4: // case-insensitive comparison of related strings
		upper, lower := strings.ToUpper(v.a), strings.ToLower(v.a)
		w.flag(strings.EqualFold(upper, lower))
		w.flag(strings.EqualFold(v.a, v.b))
		w.flag(strings.EqualFold(strings.ToTitle(v.b), v.b))
		w.str(upper)
		w.str(lower)
	}
}

// MathOps is the number of operations of the math group.
const MathOps = 1

func mathGroup(w *W, v Input) {
	// Every integer limit, in typed and untyped constant expressions.
	w.int(math.MaxInt)
	w.int(math.MinInt)
	w.u64(uint64(math.MaxInt8) | uint64(math.MaxInt16)<<8 | uint64(math.MaxInt32)<<24)
	var lows = [3]int64{math.MinInt8, math.MinInt16, math.MinInt32}
	w.u64(uint64(lows[0]) ^ uint64(lows[1]) ^ uint64(lows[2]))
	w.u64(math.MaxInt64)
	min64 := int64(math.MinInt64)
	w.u64(uint64(min64))
	w.u64(math.MaxUint)
	w.u64(math.MaxUint8 + math.MaxUint16<<8 + math.MaxUint32<<24)
	w.u64(math.MaxUint64)
	var i8 int8 = math.MinInt8
	var u16 uint16 = math.MaxUint16
	var i32 int32 = math.MaxInt32
	x := int64(v.x)
	w.u64(uint64(i8) + uint64(u16) + uint64(i32))
	w.flag(x == math.MinInt64 || x == math.MaxInt64)
	w.flag(v.y > math.MaxUint32)
	w.int(int(v.y % math.MaxInt16))
}
