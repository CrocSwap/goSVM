package protocol

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestWireWords(t *testing.T) {
	for offset := uint64(0); offset < 10; offset++ {
		for _, value := range []uint64{0, 1, 0x123456789abcdef0, ^uint64(0)} {
			got := bytes.Repeat([]byte{0xa5}, 32)
			want := bytes.Clone(got)
			binary.LittleEndian.PutUint64(want[offset:], value)
			store(got, offset, value)
			if !bytes.Equal(got, want) || load(got, offset) != value {
				t.Fatalf("word at %d value %x", offset, value)
			}
		}
	}
}

func TestFixedRegions(t *testing.T) {
	a := make([]byte, 73)
	for i := range a {
		a[i] = byte(i*17 + 5)
	}
	for offset := uint64(0); offset < 9; offset++ {
		b := bytes.Repeat([]byte{0xa5}, 55)
		want := bytes.Clone(b)
		copy(want[3:35], a[offset:offset+32])
		copy32(b, 3, a, offset)
		if !bytes.Equal(b, want) || !same(a, offset, b, 3) {
			t.Fatalf("region at %d", offset)
		}
		// A mismatch at any byte, including each word boundary, must reject.
		for i := 3; i < 35; i++ {
			b[i] ^= 1
			if same(a, offset, b, 3) {
				t.Fatalf("ignored mismatch at %d", i)
			}
			b[i] ^= 1
		}
	}
	// The protocol copies between distinct regions of the same state account.
	b := bytes.Clone(a)
	want := bytes.Clone(a)
	copy(want[:32], want[40:72])
	copy32(b, 0, b, 40)
	if !bytes.Equal(b, want) {
		t.Fatal("same-account copy")
	}
}
