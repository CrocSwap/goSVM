package solana

import (
	"bytes"
	"testing"
)

func TestSecp256k1RecoverBoundsStatusesAndCopy(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 64)
	var gotID uint64
	calls := 0
	c := Context{RecoverSecp256k1: func(hash []byte, id uint64, signature []byte) ([]byte, uint64) {
		calls++
		gotID = id
		if id == 9 {
			return nil, 2
		}
		if id == 10 {
			return key[:63], 0
		}
		return key, 0
	}}
	hash, signature := make([]byte, 32), make([]byte, 64)
	for _, lengths := range [][3]int{{31, 64, 64}, {33, 64, 64}, {32, 63, 64}, {32, 65, 64}, {32, 64, 63}, {32, 64, 65}} {
		output := bytes.Repeat([]byte{91}, lengths[2])
		if code := Secp256k1Recover(c, make([]byte, lengths[0]), 0, make([]byte, lengths[1]), output); code != 2018 || calls != 0 || !bytes.Equal(output, bytes.Repeat([]byte{91}, lengths[2])) {
			t.Fatal(lengths, code, calls)
		}
	}
	output := bytes.Repeat([]byte{91}, 64)
	if code := Secp256k1Recover(c, hash, 9, signature, output); code != 2 || !bytes.Equal(output, bytes.Repeat([]byte{91}, 64)) {
		t.Fatal("runtime status should propagate with output unchanged", code)
	}
	if code := Secp256k1Recover(c, hash, 10, signature, output); code != 2018 || !bytes.Equal(output, bytes.Repeat([]byte{91}, 64)) {
		t.Fatal("short native key", code)
	}
	if code := Secp256k1Recover(c, hash, 3, signature, output); code != 0 || gotID != 3 || !bytes.Equal(output, key) {
		t.Fatal(code, gotID)
	}
	output[0] = 1
	if key[0] != 7 {
		t.Fatal("output aliases the callback key")
	}
	if code := Secp256k1Recover(Context{}, hash, 0, signature, output); code != 2009 {
		t.Fatal(code)
	}
}
