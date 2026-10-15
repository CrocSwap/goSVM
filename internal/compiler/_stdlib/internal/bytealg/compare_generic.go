// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// goSVM on-chain overlay of Go 1.22.0 internal/bytealg/compare_generic.go
// (SBF has no assembly Compare). Changes (marked "goSVM:"): the goto to
// samebytes is a skipped loop (goto is not in the on-chain subset), and the
// runtime's string comparison, linked by name, is left out.

package bytealg

func Compare(a, b []byte) int {
	l := len(a)
	if len(b) < l {
		l = len(b)
	}
	// goSVM: Go jumps to samebytes when l == 0 || &a[0] == &b[0]; the loop
	// below then compares nothing.
	if !(l == 0 || &a[0] == &b[0]) {
		for i := 0; i < l; i++ {
			c1, c2 := a[i], b[i]
			if c1 < c2 {
				return -1
			}
			if c1 > c2 {
				return +1
			}
		}
	}
	// samebytes:
	if len(a) < len(b) {
		return -1
	}
	if len(a) > len(b) {
		return +1
	}
	return 0
}
