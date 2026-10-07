// Package pointervalues supplies the ordinary-Go oracle for borrowed storage.
package pointervalues

import "gosvm/examples/pointer-values/model"

func next(s []byte) uint64                 { s[0] = s[0] + 1; return uint64(s[0]) }
func index(s []byte, n uint64) uint64      { s[1] = s[1] + 1; return n }
func snapshot(b model.Box) model.Box       { return b }
func mutate(b *model.Box, s []byte) uint64 { b.N = b.N + 7; return next(s) }
func get(i []byte) uint64 {
	var n uint64
	for j := uint64(0); j < 8; j++ {
		n = n | uint64(i[8+j])<<(j*8)
	}
	return n
}
func put(s []byte, off, n uint64) {
	for j := uint64(0); j < 8; j++ {
		s[off+j] = byte(n >> (j * 8))
	}
}
func Process(s, i []byte) uint64 {
	s[0] = i[2]
	var p *uint64
	var b *model.Box
	var a *model.Key
	switch i[0] {
	case 1: // Assignment dereferences after the RHS effect.
		s[4], *p = byte(next(s)), next(s)
		return 0
	case 2:
		b.N = next(s)
		return 0
	case 3:
		s[3] = byte(next(s))
		return *p
	case 4: // Implicit value receiver dereference and argument evaluation.
		return b.Value(next(s))
	case 5: // Method-expression wrapper checks after explicit argument evaluation.
		return (*model.Box).Value(b, next(s))
	case 6:
		q := &b.N
		return *q
	case 7: // Go checks nil even when taking the address of an indirection.
		q := &*p
		if q != nil {
			return 107
		}
		return 0
	case 8:
		s[4], a[index(s, uint64(i[1]))] = byte(next(s)), byte(next(s))
		return 0
	case 9:
		k := model.Key{1, 2, 3, 4}
		ka := &k
		ka[index(s, get(i))] = byte(next(s))
		s[4] = k[0]
		return 0
	case 10: // Pointer receiver may explicitly handle nil.
		n := b.Nullable(next(s))
		if b.Add(1) != nil {
			return 110
		}
		put(s, 8, n)
		return 0
	case 11:
		s[3] = byte(next(s))
		return 9
	case 13:
		literal := &model.Box{N: get(i)}
		alias := literal
		literal.Child.N = next(s)
		alias.N++
		put(s, 8, literal.N+literal.Child.N)
		return 0
	case 14:
		n := get(i)
		m := n + 1
		lp := &n
		old := lp
		lp, *lp = &m, next(s)
		if old != &n || lp != &m {
			return 114
		}
		put(s, 8, n)
		put(s, 16, m)
		return 0
	case 15:
		box := model.Box{}
		q := &box.Child
		q.N = next(s)
		child := &(*q).N
		*child++
		put(s, 8, box.Child.N)
		return 0
	case 12: // Constant len/cap must not dereference a nil array pointer.
		s[4] = byte(len(a) + cap(a))
		return 0
	}
	value := get(i)
	box := model.Box{Key: model.Key{1, 2, 3, 4}, N: value, Child: model.Small{N: uint64(i[3])}}
	original := box
	q, n := model.Forward(&box.N, next(s))
	*q = *q + n
	self := box.Self()
	if self != &box || q != &box.N {
		return 101
	}
	saved := box
	copied := snapshot(box).Value(mutate(&box, s))
	if copied != saved.N+uint64(s[0])+uint64(saved.Key[0])+saved.Child.N {
		return 102
	}
	field := box.Add(n)
	*field = *field + 1
	child := &self.Child.N
	*child = *child + 5
	key := &box.Key
	key.Set(uint64(i[1])%4, byte(next(s)))
	(*model.Key).Set(key, 3, byte(next(s)))
	var counter model.Counter = model.Counter(value)
	r := counter.Increase(n)
	if r != counter {
		return 103
	}
	copy := box
	copy.Add(19)
	if copy.N != box.N+19 || original.N != value {
		return 104
	}
	// Explicit value and pointer method expressions; the value copy is stable.
	total := model.Box.Value(box, n) + (*model.Box).Value(&box, n) + copied + uint64(counter)
	*(&box.Key[0]) = byte(next(s))
	*(&s[5]) = byte(next(s))
	put(s, 8, total)
	put(s, 16, box.N+box.Child.N+uint64(box.Key[0])+uint64(box.Key[3]))
	return 0
}
