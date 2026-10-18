package solana

import (
	"bytes"
	"testing"
)

func TestKeccak256BoundsAndCopy(t *testing.T) {
	var seen []byte
	c := Context{Keccak: func(input []byte) []byte {
		seen = append([]byte(nil), input...)
		return bytes.Repeat([]byte{byte(len(input))}, 32)
	}}
	input := []byte("0123456789")
	output := bytes.Repeat([]byte{91}, 40)
	if code := Keccak256(c, input, 2, 5, output, 8); code != 0 || string(seen) != "23456" ||
		!bytes.Equal(output[:8], bytes.Repeat([]byte{91}, 8)) || !bytes.Equal(output[8:], bytes.Repeat([]byte{5}, 32)) {
		t.Fatal(code, seen, output)
	}
	for _, bad := range [][4]uint64{{11, 0, 40, 0}, {2, 9, 40, 0}, {0, 0, 31, 0}, {0, 0, 40, 9}, {0, 0, 40, 41}} {
		seen = nil
		out := bytes.Repeat([]byte{91}, int(bad[2]))
		if code := Keccak256(c, input, bad[0], bad[1], out, bad[3]); code != 2003 || seen != nil || !bytes.Equal(out, bytes.Repeat([]byte{91}, int(bad[2]))) {
			t.Fatal(bad, code)
		}
	}
	if code := Keccak256(c, nil, 0, 0, make([]byte, 32), 0); code != 0 || len(seen) != 0 {
		t.Fatal("empty input", code)
	}
	short := Context{Keccak: func([]byte) []byte { return make([]byte, 31) }}
	if code := Keccak256(short, input, 0, 1, make([]byte, 32), 0); code != 2004 {
		t.Fatal(code)
	}
	if code := Keccak256(Context{}, input, 0, 1, make([]byte, 32), 0); code != 2009 {
		t.Fatal(code)
	}
}
