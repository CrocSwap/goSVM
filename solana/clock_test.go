package solana

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestClockCurrentWordsAndFailedOutput(t *testing.T) {
	words := [5]uint64{9007199254740993, ^uint64(22), 7, 9, ^uint64(123455)}
	calls := 0
	c := Context{ReadClock: func(out []byte) uint64 {
		calls++
		if len(out) != 40 {
			t.Fatal(len(out))
		}
		for i, v := range words {
			binary.LittleEndian.PutUint64(out[8*i:], v)
		}
		return 0
	}}
	out := bytes.Repeat([]byte{91}, 40)
	for i := 0; i < 2; i++ {
		if code := Clock(c, out); code != 0 {
			t.Fatal(code)
		}
		for j, v := range words {
			if got := binary.LittleEndian.Uint64(out[j*8:]); got != v {
				t.Fatal(j, got, v)
			}
		}
		words[0]++
		words[4] = 1010
	}
	before := append([]byte(nil), out...)
	c.ReadClock = func(data []byte) uint64 { calls++; data[0] = 123; return 9876 }
	if code := Clock(c, out); code != 9876 || !bytes.Equal(before, out) {
		t.Fatal(code, out)
	}
	for _, n := range []int{0, 1, 39, 41, 80} {
		b := bytes.Repeat([]byte{91}, n)
		if code := Clock(c, b); code != 2016 || !bytes.Equal(b, bytes.Repeat([]byte{91}, n)) {
			t.Fatal(n, code)
		}
	}
	if calls != 3 || Clock(Context{}, out) != 2009 || !bytes.Equal(before, out) {
		t.Fatal(calls)
	}
}
