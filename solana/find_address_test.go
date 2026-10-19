package solana

import (
	"bytes"
	"testing"
)

// fakeDerive accepts an address whose bump byte is at most accept; it records
// every seed group it sees.
func fakeDerive(accept byte, seen *[][]byte) func(seeds, program, output []byte) uint64 {
	return func(seeds, program, output []byte) uint64 {
		*seen = append(*seen, bytes.Clone(seeds))
		bump := seeds[len(seeds)-1]
		if bump > accept {
			return 1 // on curve
		}
		for j := range output {
			output[j] = program[j] ^ bump
		}
		return 0
	}
}

func TestFindAddressSearchesFrom255(t *testing.T) {
	program := bytes.Repeat([]byte{7}, 32)
	seeds := []byte{2, 1, 'a', 2, 'b', 'c'}
	var seen [][]byte
	c := Context{DeriveAddress: fakeDerive(252, &seen)}
	output := make([]byte, 33)
	if code := FindAddress(c, seeds, program, output); code != 0 {
		t.Fatal(code)
	}
	if output[32] != 252 || output[0] != 7^252 || len(seen) != 4 {
		t.Fatalf("output %x after %d tries", output, len(seen))
	}
	// The bump is a third seed of one byte; the caller's group is untouched.
	if !bytes.Equal(seen[0], []byte{3, 1, 'a', 2, 'b', 'c', 1, 255}) || !bytes.Equal(seeds, []byte{2, 1, 'a', 2, 'b', 'c'}) {
		t.Fatalf("group %x seeds %x", seen[0], seeds)
	}
}

func TestFindAddressNoBumpAndErrors(t *testing.T) {
	program := make([]byte, 32)
	var seen [][]byte
	output := bytes.Repeat([]byte{9}, 33)
	c := Context{DeriveAddress: fakeDerive(0, &seen)}
	if code := FindAddress(c, []byte{0}, program, output); code != 1 || len(seen) != 255 || seen[254][2] != 1 {
		t.Fatalf("code %d after %d tries", code, len(seen))
	}
	if !bytes.Equal(output, bytes.Repeat([]byte{9}, 33)) {
		t.Fatal("no-bump search wrote output")
	}
	c.DeriveAddress = func(_, _, out []byte) uint64 { out[0] = 1; return 77 }
	if code := FindAddress(c, []byte{0}, program, output); code != 77 || output[0] != 9 {
		t.Fatal("derivation status not propagated", code)
	}
	fifteen := []byte{15}
	for j := 0; j < 15; j++ {
		fifteen = append(fifteen, 32)
		fifteen = append(fifteen, bytes.Repeat([]byte{byte(j)}, 32)...)
	}
	sixteen := append([]byte{16}, append(bytes.Clone(fifteen[1:]), 0)...)
	c.DeriveAddress = fakeDerive(255, &seen)
	if code := FindAddress(c, fifteen, program, output); code != 0 || output[32] != 255 {
		t.Fatal("15 seeds of 32 bytes", code)
	}
	for _, v := range []struct {
		seeds, program, output []byte
	}{
		{sixteen, program, output},
		{[]byte{1, 33}, program, output},
		{[]byte{1, 2, 'a'}, program, output},
		{[]byte{0, 0}, program, output},
		{nil, program, output},
		{[]byte{0}, program[:31], output},
		{[]byte{0}, program, output[:32]},
		{[]byte{0}, program, make([]byte, 34)},
	} {
		if code := FindAddress(c, v.seeds, v.program, v.output); code != 2007 {
			t.Fatalf("seeds %x program %d output %d: code %d", v.seeds, len(v.program), len(v.output), code)
		}
	}
	if code := FindAddress(Context{}, []byte{0}, program, output); code != 2009 {
		t.Fatal("missing callback", code)
	}
}
