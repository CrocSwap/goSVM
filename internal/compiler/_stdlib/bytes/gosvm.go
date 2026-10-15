// Copyright 2026 The goSVM Authors. Derived from the Go standard library;
// see ../LICENSE.

// goSVM on-chain overlay: a constant bytes.go needs from Go 1.22.0
// bytes/buffer.go, which is left out (docs/GO_STDLIB.md).

package bytes

// maxInt is from Go 1.22.0 bytes/buffer.go, which is left out.
const maxInt = int(^uint(0) >> 1)
