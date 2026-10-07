// Package sliceviews is the native-Go oracle for bounded views and capacities.
package sliceviews

import "gosvm/examples/slice-views/model"

func next(s []byte, n uint64) uint64 { s[0]++; return n }
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
	switch i[0] {
	case 1:
		v := s[next(s, uint64(i[1])):next(s, uint64(i[3]))]
		put(s, 8, uint64(len(v)))
		put(s, 16, uint64(cap(v)))
		return 0
	case 2:
		v := s[next(s, uint64(i[1])):next(s, uint64(i[3])):next(s, get(i))]
		put(s, 8, uint64(len(v)))
		put(s, 16, uint64(cap(v)))
		return 0
	case 3:
		v := s[:4:6]
		w := v[next(s, uint64(i[1])):next(s, get(i))]
		put(s, 8, uint64(len(w)))
		put(s, 16, uint64(cap(w)))
		return 0
	case 4:
		var n []byte
		v := n[next(s, uint64(i[1])):next(s, uint64(i[3])):next(s, get(i))]
		if v != nil {
			return 104
		}
		return 0
	case 5:
		var k *model.Key
		v := k[next(s, 0):next(s, uint64(i[1]))]
		return uint64(len(v))
	case 6:
		var k [0]byte
		v := k[:]
		if v == nil || len(v) != 0 || cap(v) != 0 {
			return 106
		}
		return 0
	case 7:
		var n []byte
		if n != nil || nil != n || len(n) != 0 || cap(n) != 0 {
			return 107
		}
		v := []byte(nil)
		if v != nil {
			return 117
		}
		return 0
	case 8:
		v := s[4:8:12]
		pointer := &v[next(s, get(i))]
		*pointer = byte(next(s, 31))
		return 0
	case 10:
		var k [1024]byte
		k[0], k[1023] = 37, 255
		v := k[next(s, uint64(i[1])):next(s, get(i))]
		if len(v) != 0 {
			v[uint64(len(v))-1] = i[4]
		}
		put(s, 8, uint64(len(v)))
		put(s, 16, uint64(cap(v)))
		s[4] = k[1023]
		return 0
	case 11:
		var k [1024]byte
		v := k[0:0:next(s, get(i))]
		w := v[:uint64(cap(v))]
		if cap(w) != 0 {
			w[uint64(cap(w))-1] = i[4]
		}
		put(s, 8, uint64(len(w)))
		put(s, 16, uint64(cap(w)))
		s[4] = k[1023]
		return 0
	case 9:
		v := s[:1:1]
		s[3] = byte(next(s, 9))
		if len(v) != 1 {
			return 109
		}
		return 9
	}
	// Copying an array owns separate storage; slicing it keeps storage identity.
	var box model.Box
	for j := uint64(0); j < 32; j++ {
		box.Key[j] = byte(j + uint64(i[3]))
	}
	original := box.Key
	v := box.Window(4, 12, 16)
	v[0] = i[4]
	w, capacity := model.Forward(v, 2)
	reslice := w[:uint64(cap(w))]
	reslice[9] = i[5]
	if box.Key[4] != i[4] || box.Key[15] != i[5] || capacity != 12 {
		return 101
	}
	if original[4] != byte(4+uint64(i[3])) {
		return 102
	}
	alias := &box.Key[7]
	*alias = byte(next(s, 37))
	outer := model.FromKey(&box.Key, 3)
	copied := box.Key
	copyView := copied[3:]
	copyView[4] = byte(next(s, 41))
	if box.Key[7] != 37 || copied[7] != 41 {
		return 103
	}
	// Conversion preserves the borrow, pointer, length and capacity.
	converted := []byte(outer)
	converted[1] = byte(next(s, 43))
	narrow := s[4:8:12]
	expanded := narrow[:uint64(cap(narrow))]
	expanded[7] = i[6]
	account := model.Tail(s, 4)
	account[1] = box.Key[4]
	put(s, 8, uint64(len(v))+uint64(cap(v))*256+uint64(w[1])*65536+uint64(copyView[4])*16777216)
	put(s, 16, uint64(box.Key[4])+uint64(outer[12])*256+uint64(original[4])*65536+uint64(s[5])*16777216)
	return 0
}
