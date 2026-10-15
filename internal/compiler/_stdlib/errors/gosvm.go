// Copyright 2026 The goSVM Authors. Derived from the Go standard library;
// see ../LICENSE.

// goSVM on-chain overlay: helpers replacing the Go 1.22.0 sources' uses of
// panic and internal/reflectlite (docs/GO_STDLIB.md#intrinsics).

package errors

// trap aborts the invocation where Go panics with msg.
//
// goSVM: panic is not in the on-chain subset; indexing msg at len(msg)
// always fails, so this uses the bounds trap (docs/ERROR_CONTRACT.md). The
// transaction fails and rolls back, as a Go panic would.
func trap(msg string) {
	_ = msg[len(msg)]
}

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
