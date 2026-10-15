package stdlibvalues

import "bytes"

// BytesOps is the number of operations of the bytes group.
const BytesOps = 9

// slices returns a and b as byte slices: fresh copies, nil for an empty
// string when k says so, or a with spare capacity.
func slices(v Input, k int) ([]byte, []byte) {
	a, b := []byte(v.a), []byte(v.b)
	if k&1 == 1 && len(v.a) == 0 {
		a = nil
	}
	if k&2 == 2 && len(v.b) == 0 {
		b = nil
	}
	if k&4 == 4 {
		a = append(make([]byte, 0, len(a)+7), a...)
	}
	return a, b
}

func bytesGroup(w *W, v Input) {
	a, b := slices(v, v.h2)
	r := rune(v.h0) | rune(v.h1&0x1f)<<16
	if v.h2&8 == 8 {
		r = -r
	}
	switch v.op % BytesOps {
	case 0: // predicates and comparisons
		w.flag(bytes.Equal(a, b))
		w.flag(bytes.Equal(a, a[:len(a):len(a)]))
		w.int(bytes.Compare(a, b))
		w.int(bytes.Compare(a, a))
		w.flag(bytes.Contains(a, b))
		w.flag(bytes.ContainsAny(a, v.b))
		w.flag(bytes.ContainsRune(a, r))
		w.flag(bytes.ContainsFunc(a, isSep))
		w.flag(bytes.HasPrefix(a, b))
		w.flag(bytes.HasSuffix(a, b))
		w.flag(bytes.EqualFold(a, b))
		w.flag(bytes.EqualFold(a, bytes.ToUpper(a)))
	case 1: // indexes and counts
		w.int(bytes.Count(a, b))
		w.int(bytes.Index(a, b))
		w.int(bytes.LastIndex(a, b))
		w.int(bytes.IndexAny(a, v.b))
		w.int(bytes.LastIndexAny(a, v.b))
		w.int(bytes.IndexRune(a, r))
		w.int(bytes.IndexFunc(a, isSep))
		w.int(bytes.LastIndexFunc(a, isSep))
		c := byte(v.h3)
		if len(b) > 0 {
			c = b[0]
		}
		w.int(bytes.IndexByte(a, c))
		w.int(bytes.LastIndexByte(a, c))
	case 2: // splitting (results share a's bytes; their capacities are written)
		w.bss(bytes.Split(a, b))
		w.bss(bytes.SplitN(a, b, v.n%8))
		w.bss(bytes.SplitAfter(a, b))
		w.bss(bytes.SplitAfterN(a, b, v.m%8))
		w.bss(bytes.Fields(a))
		w.bss(bytes.FieldsFunc(a, isSep))
	case 3: // joining and cutting
		parts := bytes.Split(a, []byte(" "))
		w.bs(bytes.Join(parts, b))
		w.bs(bytes.Join(nil, b))
		w.bs(bytes.Join(parts[:1], b))
		before, after, found := bytes.Cut(a, b)
		w.bs(before)
		w.bs(after)
		w.flag(found)
		rest, ok := bytes.CutPrefix(a, b)
		w.bs(rest)
		w.flag(ok)
		rest, ok = bytes.CutSuffix(a, b)
		w.bs(rest)
		w.flag(ok)
	case 4: // case and mapping
		w.bs(bytes.ToUpper(a))
		w.bs(bytes.ToLower(a))
		w.bs(bytes.ToTitle(a))
		w.bs(bytes.ToValidUTF8(a, b))
		w.bs(bytes.Map(mapping(v.n), a))
		w.bs(bytes.Clone(a))
		w.flag(bytes.Clone(nil) == nil)
		rs := bytes.Runes(a)
		w.int(len(rs))
		w.int(cap(rs))
		for _, x := range rs {
			w.u64(uint64(x))
		}
	case 5: // trimming
		w.bs(bytes.Trim(a, v.b))
		w.bs(bytes.TrimLeft(a, v.b))
		w.bs(bytes.TrimRight(a, v.b))
		w.bs(bytes.TrimSpace(a))
		w.bs(bytes.TrimPrefix(a, b))
		w.bs(bytes.TrimSuffix(a, b))
		w.bs(bytes.TrimFunc(a, isSep))
		w.bs(bytes.TrimLeftFunc(a, isSep))
		w.bs(bytes.TrimRightFunc(a, isSep))
	case 6: // replacing
		w.bs(bytes.Replace(a, b, []byte("<>"), v.n%6))
		w.bs(bytes.Replace(a, b, nil, -1))
		w.bs(bytes.ReplaceAll(a, b, []byte("é")))
		w.bs(bytes.Replace(a, nil, []byte("|"), v.m%5))
	case 7: // Repeat, as for strings
		count := v.n % 12
		switch v.h3 % 4 {
		case 1:
			count = -1 - v.h0
		case 2:
			if len(a) >= 4 {
				count = int(^uint(0)>>1)/(1+v.h0%3) + v.h1%3
			}
		}
		w.u8(0x52)
		w.bs(bytes.Repeat(a, count))
	case 8: // results alias their input: writes through them are visible
		t := bytes.TrimSpace(a)
		if len(t) > 0 {
			t[0] = '#'
		}
		f := bytes.Fields(a)
		if len(f) > 1 {
			f[0] = append(f[0], '!') // capacity is limited: a new array
			f[1][0] = '@'
		}
		w.bs(a)
		w.bss(f)
	}
}
