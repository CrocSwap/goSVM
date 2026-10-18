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

func TestGetReturnDataCopiesPrefixAndSetter(t *testing.T) {
	setter := bytes.Repeat([]byte{7}, 32)
	for _, size := range []int{0, 1, 31, 32, 1024} {
		data := make([]byte, size)
		for i := range data {
			data[i] = byte(i*13 + 5)
		}
		c := Context{ReadReturnData: func() ([]byte, []byte) { return data, setter }}
		for _, capacity := range []int{0, 1, 32, 1023, 1024, 2048} {
			output := bytes.Repeat([]byte{91}, capacity)
			program := bytes.Repeat([]byte{91}, 32)
			if got := GetReturnData(c, output, program); got != uint64(size) {
				t.Fatal(size, capacity, got)
			}
			n := min(size, capacity)
			if !bytes.Equal(output[:n], data[:n]) || !bytes.Equal(output[n:], bytes.Repeat([]byte{91}, capacity-n)) {
				t.Fatal("output", size, capacity)
			}
			want := bytes.Repeat([]byte{91}, 32)
			if n != 0 {
				want = setter
			}
			if !bytes.Equal(program, want) {
				t.Fatal("program", size, capacity)
			}
		}
	}
	program := make([]byte, 32)
	if got := GetReturnData(Context{}, make([]byte, 8), program); got != 0 || !bytes.Equal(program, make([]byte, 32)) {
		t.Fatal("missing callback should read as no return data", got)
	}
	for _, n := range []int{0, 31, 33} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatal("program length", n, "did not trap")
				}
			}()
			GetReturnData(Context{}, nil, make([]byte, n))
		}()
	}
}
