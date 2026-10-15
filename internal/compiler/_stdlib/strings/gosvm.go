// Copyright 2026 The goSVM Authors. Derived from the Go standard library;
// see ../LICENSE.

// goSVM on-chain overlay: helpers replacing the Go 1.22.0 sources' uses of
// panic and package unsafe (docs/GO_STDLIB.md#intrinsics).

package strings

// trap aborts the invocation where Go panics with msg.
//
// goSVM: panic is not in the on-chain subset; indexing msg at len(msg)
// always fails, so this uses the bounds trap (docs/ERROR_CONTRACT.md). The
// transaction fails and rolls back, as a Go panic would.
func trap(msg string) {
	_ = msg[len(msg)]
}

// unsafeString returns a string sharing b's bytes, as
// unsafe.String(unsafe.SliceData(b), len(b)) does: b must not change later.
//
// goSVM: the compiler replaces the body (a copy, which has the same value)
// with a conversion that shares the bytes, so the heap use matches Go's.
//
//gosvm:intrinsic unsafeString
func unsafeString(b []byte) string {
	return string(b)
}

// noescape returns p, hidden from escape analysis, as the runtime's
// noescape does for Builder.copyCheck: the result is only compared, never
// dereferenced, so storing it does not move the Builder to the heap.
//
// goSVM: the compiler replaces the body with "return p". The Go body
// returns nil so that Check's borrow analysis sees a result that refers to
// no storage, which is what makes the store harmless.
//
//gosvm:intrinsic noescape
func noescape(p *Builder) *Builder {
	return nil
}
