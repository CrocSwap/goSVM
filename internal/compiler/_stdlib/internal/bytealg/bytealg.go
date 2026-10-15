// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// goSVM on-chain overlay of Go 1.22.0 internal/bytealg/bytealg.go. Changes
// (marked "goSVM:"): no internal/cpu offsets (assembly only); MaxLen is 0,
// as on every platform without an assembly Index, so strings and bytes take
// their generic paths; the Rabin-Karp searches compare candidates with
// equal instead of string(x) == string(y), which gc does without
// allocating but goSVM would copy; MakeNoZero is make (the bytes are zeroed,
// which callers never observe), with the same length and capacity.

package bytealg

// MaxLen is the maximum length of the string to be searched for (argument b)
// in Index. If MaxLen is not 0, make sure MaxLen >= 4.
var MaxLen int

// PrimeRK is the prime base used in Rabin-Karp algorithm.
const PrimeRK = 16777619

// HashStr returns the hash and the appropriate multiplicative
// factor for use in Rabin-Karp algorithm.
func HashStr[T string | []byte](sep T) (uint32, uint32) {
	hash := uint32(0)
	for i := 0; i < len(sep); i++ {
		hash = hash*PrimeRK + uint32(sep[i])
	}
	var pow, sq uint32 = 1, PrimeRK
	for i := len(sep); i > 0; i >>= 1 {
		if i&1 != 0 {
			pow *= sq
		}
		sq *= sq
	}
	return hash, pow
}

// HashStrRev returns the hash of the reverse of sep and the
// appropriate multiplicative factor for use in Rabin-Karp algorithm.
func HashStrRev[T string | []byte](sep T) (uint32, uint32) {
	hash := uint32(0)
	for i := len(sep) - 1; i >= 0; i-- {
		hash = hash*PrimeRK + uint32(sep[i])
	}
	var pow, sq uint32 = 1, PrimeRK
	for i := len(sep); i > 0; i >>= 1 {
		if i&1 != 0 {
			pow *= sq
		}
		sq *= sq
	}
	return hash, pow
}

// IndexRabinKarp uses the Rabin-Karp search algorithm to return the index of the
// first occurrence of sep in s, or -1 if not present.
func IndexRabinKarp[T string | []byte](s, sep T) int {
	// Rabin-Karp search
	hashss, pow := HashStr(sep)
	n := len(sep)
	var h uint32
	for i := 0; i < n; i++ {
		h = h*PrimeRK + uint32(s[i])
	}
	if h == hashss && equal(s[:n], sep) { // goSVM: string(s[:n]) == string(sep)
		return 0
	}
	for i := n; i < len(s); {
		h *= PrimeRK
		h += uint32(s[i])
		h -= pow * uint32(s[i-n])
		i++
		if h == hashss && equal(s[i-n:i], sep) { // goSVM: string(s[i-n:i]) == string(sep)
			return i - n
		}
	}
	return -1
}

// LastIndexRabinKarp uses the Rabin-Karp search algorithm to return the last index of the
// occurrence of sep in s, or -1 if not present.
func LastIndexRabinKarp[T string | []byte](s, sep T) int {
	// Rabin-Karp search from the end of the string
	hashss, pow := HashStrRev(sep)
	n := len(sep)
	last := len(s) - n
	var h uint32
	for i := len(s) - 1; i >= last; i-- {
		h = h*PrimeRK + uint32(s[i])
	}
	if h == hashss && equal(s[last:], sep) { // goSVM: string(s[last:]) == string(sep)
		return last
	}
	for i := last - 1; i >= 0; i-- {
		h *= PrimeRK
		h += uint32(s[i])
		h -= pow * uint32(s[i+n])
		if h == hashss && equal(s[i:i+n], sep) { // goSVM: string(s[i:i+n]) == string(sep)
			return i
		}
	}
	return -1
}

// equal reports whether a and b hold the same bytes.
//
// goSVM: string(a) == string(b) without the conversions' copies.
func equal[T string | []byte](a, b T) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// MakeNoZero makes a slice of length and capacity n without zeroing the bytes.
// It is the caller's responsibility to ensure uninitialized bytes
// do not leak to the end user.
//
// goSVM: the runtime's mallocgc without zeroing; make gives the same length
// and capacity (the invocation heap is zero-filled anyway).
func MakeNoZero(n int) []byte {
	return make([]byte, n)
}
