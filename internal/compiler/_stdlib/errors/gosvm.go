// Copyright 2026 The goSVM Authors. Derived from the Go standard library;
// see ../LICENSE.

// goSVM on-chain overlay: a helper replacing the Go 1.22.0 sources' use of
// internal/reflectlite (docs/GO_STDLIB.md#intrinsics).

package errors

// comparableType reports whether x's dynamic type is comparable
// (reflectlite.TypeOf(x).Comparable() for a non-nil x).
//
// goSVM: the compiler replaces the body with a test of x's dynamic type
// against the program's uncomparable dynamic types.
//
//gosvm:intrinsic comparable
func comparableType(x any) bool {
	return true
}
