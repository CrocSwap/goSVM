package solana

import (
	"bytes"
	"testing"
)

func TestReturnDataBoundsCopiesAndUnavailable(t *testing.T) {
	var stored []byte
	calls := 0
	c := Context{WriteReturnData: func(data []byte) { calls++; stored = data }}
	for _, n := range []int{0, 1, 20, 1023, 1024} {
		data := bytes.Repeat([]byte{91}, n)
		if code := SetReturnData(c, data); code != 0 || !bytes.Equal(data, stored) {
			t.Fatal(n, code, stored)
		}
		if n > 0 {
			data[0] = 42
			if stored[0] != 91 {
				t.Fatal("setter retained caller buffer")
			}
			stored[0] = 123
			if data[0] != 42 {
				t.Fatal("callback can change caller buffer")
			}
		}
	}
	before := append([]byte(nil), stored...)
	if code := SetReturnData(c, make([]byte, 1025)); code != 2017 || calls != 5 || !bytes.Equal(stored, before) {
		t.Fatal(code, calls)
	}
	if code := SetReturnData(Context{}, nil); code != 2009 {
		t.Fatal(code)
	}
	if code := SetReturnData(Context{}, make([]byte, 1025)); code != 2017 {
		t.Fatal(code)
	}
	if code := SetReturnData(c, nil); code != 0 || len(stored) != 0 || calls != 6 {
		t.Fatal(code, calls)
	}
}
