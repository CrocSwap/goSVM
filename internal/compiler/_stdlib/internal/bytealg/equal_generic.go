// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// goSVM on-chain overlay of Go 1.22.0 internal/bytealg/equal_generic.go.
// Change (marked "goSVM:"): Equal compares the bytes in a loop instead of
// string(a) == string(b), which gc compiles without allocating but goSVM
// would copy.

package bytealg

// Equal reports whether a and b
// are the same length and contain the same bytes.
// A nil argument is equivalent to an empty slice.
//
// Equal is equivalent to bytes.Equal.
// It is provided here for convenience,
// because some packages cannot depend on bytes.
func Equal(a, b []byte) bool {
	// Neither cmd/compile nor gccgo allocates for these string conversions.
	// There is a test for this in package bytes.
	return equal(a, b) // goSVM: string(a) == string(b)
}
