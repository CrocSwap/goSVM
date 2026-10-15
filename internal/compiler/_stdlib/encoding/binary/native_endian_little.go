// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// goSVM on-chain overlay of Go 1.22.0 encoding/binary/native_endian_little.go
// (SBF is little-endian). Change (marked "goSVM:"): Go embeds littleEndian
// in nativeEndian; struct embedding is not in the on-chain subset, so the
// promoted methods are written out and call littleEndian's.

package binary

type nativeEndian struct {
	littleEndian littleEndian // goSVM: embedded in Go
}

// NativeEndian is the native-endian implementation of [ByteOrder] and [AppendByteOrder].
var NativeEndian nativeEndian

// goSVM: littleEndian's methods, promoted in Go.

func (n nativeEndian) Uint16(b []byte) uint16 { return n.littleEndian.Uint16(b) }

func (n nativeEndian) PutUint16(b []byte, v uint16) { n.littleEndian.PutUint16(b, v) }

func (n nativeEndian) AppendUint16(b []byte, v uint16) []byte {
	return n.littleEndian.AppendUint16(b, v)
}

func (n nativeEndian) Uint32(b []byte) uint32 { return n.littleEndian.Uint32(b) }

func (n nativeEndian) PutUint32(b []byte, v uint32) { n.littleEndian.PutUint32(b, v) }

func (n nativeEndian) AppendUint32(b []byte, v uint32) []byte {
	return n.littleEndian.AppendUint32(b, v)
}

func (n nativeEndian) Uint64(b []byte) uint64 { return n.littleEndian.Uint64(b) }

func (n nativeEndian) PutUint64(b []byte, v uint64) { n.littleEndian.PutUint64(b, v) }

func (n nativeEndian) AppendUint64(b []byte, v uint64) []byte {
	return n.littleEndian.AppendUint64(b, v)
}
