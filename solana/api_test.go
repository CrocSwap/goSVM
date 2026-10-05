package solana

import (
	"bytes"
	"crypto/sha256"
	"testing"
)

func TestSHA256Regions(t *testing.T) {
	calls := 0
	c := Context{Hash: func(b []byte) []byte { calls++; h := sha256.Sum256(b); return h[:] }}
	input := []byte("--abc--")
	output := bytes.Repeat([]byte{255}, 40)
	if code := SHA256(c, input, 2, 3, output, 4); code != 0 {
		t.Fatal(code)
	}
	want := sha256.Sum256([]byte("abc"))
	if !bytes.Equal(output[4:36], want[:]) || output[3] != 255 || output[36] != 255 {
		t.Fatal("incorrect digest or overwrite")
	}
	before := bytes.Clone(output)
	for _, v := range [][3]uint64{{8, 0, 0}, {0, 8, 0}, {0, 1, 9}, {^uint64(0), 1, 0}, {1, ^uint64(0), 0}, {0, 1, ^uint64(0)}} {
		if code := SHA256(c, input, v[0], v[1], output, v[2]); code != 2003 {
			t.Fatal(v, code)
		}
	}
	if calls != 1 || !bytes.Equal(before, output) {
		t.Fatal("invalid region hashed or wrote bytes")
	}
}
