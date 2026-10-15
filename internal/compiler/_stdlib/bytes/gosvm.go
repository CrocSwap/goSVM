// Copyright 2026 The goSVM Authors. Derived from the Go standard library;
// see ../LICENSE.

// goSVM on-chain overlay: a helper replacing the Go 1.22.0 sources' uses of
// panic (docs/GO_STDLIB.md#intrinsics).

package bytes

// trap aborts the invocation where Go panics with msg.
//
// goSVM: panic is not in the on-chain subset; indexing msg at len(msg)
// always fails, so this uses the bounds trap (docs/ERROR_CONTRACT.md). The
// transaction fails and rolls back, as a Go panic would.
func trap(msg string) {
	_ = msg[len(msg)]
}

// maxInt is from Go 1.22.0 bytes/buffer.go, which is left out.
const maxInt = int(^uint(0) >> 1)
