package stdlibvalues

import (
	"strings"
	"unicode"
)

// StringsOps is the number of operations of the strings group.
const StringsOps = 8

// isSep is a FieldsFunc/TrimFunc predicate: spaces, punctuation-like ASCII
// and runes above U+FFFF.
func isSep(r rune) bool {
	return r == ',' || r == '-' || r == '.' || unicode.IsSpace(r) || r > 0xFFFF
}

// mapping is a Map function chosen by k: upper-casing, dropping a rune
// (a negative result), invalid results and multi-byte replacements.
func mapping(k int) func(rune) rune {
	switch k & 3 {
	case 0:
		return unicode.ToUpper
	case 1:
		return func(r rune) rune {
			if r == 'a' || r == 0xe9 {
				return -1
			}
			return r
		}
	case 2:
		return func(r rune) rune {
			if r < 0x80 {
				return r + 0x100 // two-byte encodings
			}
			return 0xD800 // a surrogate: encodes as U+FFFD
		}
	}
	return func(r rune) rune { return r ^ 1 }
}

func stringsGroup(w *W, v Input) {
	a, b := v.a, v.b
	r := rune(v.h0) | rune(v.h1&0x1f)<<16
	if v.h2&1 == 1 {
		r = -r
	}
	switch v.op % StringsOps {
	case 0: // predicates and comparisons
		w.int(strings.Compare(a, b))
		w.flag(strings.Contains(a, b))
		w.flag(strings.ContainsAny(a, b))
		w.flag(strings.ContainsRune(a, r))
		w.flag(strings.ContainsFunc(a, isSep))
		w.flag(strings.HasPrefix(a, b))
		w.flag(strings.HasSuffix(a, b))
		w.flag(strings.EqualFold(a, b))
		w.flag(strings.EqualFold(a, strings.ToUpper(a)))
		w.flag(strings.EqualFold(strings.ToLower(b), b))
	case 1: // indexes and counts
		w.int(strings.Count(a, b))
		w.int(strings.Index(a, b))
		w.int(strings.LastIndex(a, b))
		w.int(strings.IndexAny(a, b))
		w.int(strings.LastIndexAny(a, b))
		w.int(strings.IndexRune(a, r))
		w.int(strings.IndexFunc(a, isSep))
		w.int(strings.LastIndexFunc(a, isSep))
		c := byte(v.h3)
		if len(b) > 0 {
			c = b[0]
		}
		w.int(strings.IndexByte(a, c))
		w.int(strings.LastIndexByte(a, c))
	case 2: // splitting
		w.strs(strings.Split(a, b))
		w.strs(strings.SplitN(a, b, v.n%8))
		w.strs(strings.SplitAfter(a, b))
		w.strs(strings.SplitAfterN(a, b, v.m%8))
		w.strs(strings.Fields(a))
		w.strs(strings.FieldsFunc(a, isSep))
	case 3: // joining and cutting
		parts := strings.Split(a, " ")
		w.str(strings.Join(parts, b))
		w.str(strings.Join(nil, b))
		w.str(strings.Join(parts[:1], b))
		before, after, found := strings.Cut(a, b)
		w.str(before)
		w.str(after)
		w.flag(found)
		rest, ok := strings.CutPrefix(a, b)
		w.str(rest)
		w.flag(ok)
		rest, ok = strings.CutSuffix(a, b)
		w.str(rest)
		w.flag(ok)
	case 4: // case and mapping
		w.str(strings.ToUpper(a))
		w.str(strings.ToLower(a))
		w.str(strings.ToTitle(a))
		w.str(strings.ToValidUTF8(a, b))
		w.str(strings.Map(mapping(v.n), a))
		w.str(strings.Clone(a))
	case 5: // trimming
		w.str(strings.Trim(a, b))
		w.str(strings.TrimLeft(a, b))
		w.str(strings.TrimRight(a, b))
		w.str(strings.TrimSpace(a))
		w.str(strings.TrimPrefix(a, b))
		w.str(strings.TrimSuffix(a, b))
		w.str(strings.TrimFunc(a, isSep))
		w.str(strings.TrimLeftFunc(a, isSep))
		w.str(strings.TrimRightFunc(a, isSep))
	case 6: // replacing
		w.str(strings.Replace(a, b, "<>", v.n%6))
		w.str(strings.Replace(a, b, "", -1))
		w.str(strings.ReplaceAll(a, b, "é"))
		w.str(strings.Replace(a, "", "|", v.m%5))
	case 7: // Repeat: a count from the vector, including negative counts and
		// counts whose result length overflows (both panic)
		count := v.n % 12
		switch v.h3 % 4 {
		case 1:
			count = -1 - v.h0
		case 2:
			if len(a) >= 4 { // so that the length overflows (a huge allocation would not panic)
				count = int(^uint(0)>>1)/(1+v.h0%3) + v.h1%3
			}
		}
		w.u8(0x52)
		w.str(strings.Repeat(a, count))
	}
}
