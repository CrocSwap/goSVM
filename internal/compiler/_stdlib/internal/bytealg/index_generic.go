// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// goSVM on-chain overlay of Go 1.22.0 internal/bytealg/index_generic.go (SBF
// has no assembly Index). MaxLen is 0, so strings and bytes never call these.
// Change (marked "goSVM:"): Go panics "unimplemented"; goSVM traps.

package bytealg

const MaxBruteForce = 0

// Index returns the index of the first instance of b in a, or -1 if b is not present in a.
// Requires 2 <= len(b) <= MaxLen.
func Index(a, b []byte) int {
	trap("unimplemented") // goSVM: panic("unimplemented")
	return 0
}

// IndexString returns the index of the first instance of b in a, or -1 if b is not present in a.
// Requires 2 <= len(b) <= MaxLen.
func IndexString(a, b string) int {
	trap("unimplemented") // goSVM: panic("unimplemented")
	return 0
}

// Cutover reports the number of failures of IndexByte we should tolerate
// before switching over to Index.
// n is the number of bytes processed so far.
// See the bytes.Index implementation for details.
func Cutover(n int) int {
	trap("unimplemented") // goSVM: panic("unimplemented")
	return 0
}

// trap aborts the invocation where Go panics with msg.
//
// goSVM: panic is not in the on-chain subset; indexing msg at len(msg)
// always fails, so this uses the bounds trap (docs/ERROR_CONTRACT.md).
func trap(msg string) {
	_ = msg[len(msg)]
}
