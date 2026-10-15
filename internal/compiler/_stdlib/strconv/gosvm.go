// Copyright 2026 The goSVM Authors. Derived from the Go standard library;
// see ../LICENSE.

// goSVM on-chain overlay: helpers replacing the Go 1.22.0 sources' uses of
// package unsafe (docs/GO_STDLIB.md#intrinsics).

package strconv

// unsafeString returns a string sharing b's bytes, as
// unsafe.String(unsafe.SliceData(b), len(b)) does: b must not change later.
//
// goSVM: the compiler replaces the body (a copy, which has the same value)
// with a conversion that shares the bytes.
//
//gosvm:intrinsic unsafeString
func unsafeString(b []byte) string {
	return string(b)
}
