// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Avoid IndexByte and IndexByteString on Plan 9 because it uses
// SSE instructions on x86 machines, and those are classified as
// floating point instructions, which are illegal in a note handler.

// goSVM on-chain overlay: Go 1.22.0 internal/bytealg/indexbyte_generic.go, unchanged;
// SBF has no assembly implementation, so the generic one applies (its
// build constraint is dropped).

package bytealg

func IndexByte(b []byte, c byte) int {
	for i, x := range b {
		if x == c {
			return i
		}
	}
	return -1
}

func IndexByteString(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}
