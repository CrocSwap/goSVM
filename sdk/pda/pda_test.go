package pda

import (
	"bytes"
	"encoding/binary"
	"gosvm/solana"
	"testing"
)

func TestExplicitSeedEncodingAndBounds(t *testing.T) {
	var s Seeds
	key := [32]byte{1, 2, 3}
	for _, code := range []uint64{s.AddBytes([]byte{7, 8}), s.AddKey(key), s.AddUint32(0x12345678), s.AddUint64(0x1122334455667788), s.AddByte(255)} {
		if code != 0 {
			t.Fatal(code)
		}
	}
	want := []byte{5, 2, 7, 8, 32}
	want = append(want, key[:]...)
	want = append(want, 4)
	var b [8]byte
	binary.LittleEndian.PutUint32(b[:4], 0x12345678)
	want = append(want, b[:4]...)
	want = append(want, 8)
	binary.LittleEndian.PutUint64(b[:], 0x1122334455667788)
	want = append(want, b[:]...)
	want = append(want, 1, 255)
	if !bytes.Equal(s.Bytes(), want) {
		t.Fatalf("%x want %x", s.Bytes(), want)
	}
	before := append([]byte(nil), s.Bytes()...)
	if s.AddBytes(make([]byte, 33)) != ErrSeeds || !bytes.Equal(before, s.Bytes()) {
		t.Fatal("long seed mutated builder")
	}
	var maximum Seeds
	for i := 0; i < 16; i++ {
		if maximum.AddKey(key) != 0 {
			t.Fatal(i)
		}
	}
	if len(maximum.Bytes()) != 529 || maximum.AddByte(0) != ErrSeeds {
		t.Fatal("seed-count bound")
	}
}

func TestAddressAndFailureOutput(t *testing.T) {
	var s Seeds
	if s.AddByte(1) != 0 {
		t.Fatal("seed")
	}
	program := [32]byte{9}
	want := [32]byte{3}
	c := solana.Context{Accounts: []solana.Account{{Key: want[:]}}}
	c.DeriveAddress = func(seeds, p, out []byte) uint64 {
		if !bytes.Equal(seeds, []byte{1, 1, 1}) || !bytes.Equal(p, program[:]) {
			t.Fatal(seeds, p)
		}
		copy(out, want[:])
		return 0
	}
	got, code := s.Address(c, program)
	if code != 0 || got != want {
		t.Fatal(got, code)
	}
	ok, code := s.Matches(c, 0, program)
	if !ok || code != 0 {
		t.Fatal(ok, code)
	}
	c.DeriveAddress = func(_, _, out []byte) uint64 { out[0] = 77; return 123 }
	got, code = s.Address(c, program)
	if code != 123 || got != ([32]byte{}) {
		t.Fatal("failed derivation wrote output", got, code)
	}
}

func TestFindSearchesBumpsDownward(t *testing.T) {
	var s Seeds
	if s.AddBytes([]byte("vault")) != 0 {
		t.Fatal("seed")
	}
	var tried []byte
	c := solana.Context{DeriveAddress: func(seeds, program, output []byte) uint64 {
		bump := seeds[len(seeds)-1]
		tried = append(tried, bump)
		if !bytes.Equal(seeds[:len(seeds)-2], []byte{2, 5, 'v', 'a', 'u', 'l', 't'}) || seeds[len(seeds)-2] != 1 || program[0] != 9 {
			t.Fatal(seeds, program)
		}
		if bump > 252 {
			return 1 // on curve
		}
		output[0] = bump
		return 0
	}}
	address, bump, code := s.Find(c, [32]byte{9})
	if code != 0 || bump != 252 || address[0] != 252 || !bytes.Equal(tried, []byte{255, 254, 253, 252}) {
		t.Fatal(address, bump, code, tried)
	}
	if !bytes.Equal(s.Bytes(), []byte{1, 5, 'v', 'a', 'u', 'l', 't'}) {
		t.Fatal("Find changed the builder", s.Bytes())
	}
	c.DeriveAddress = func(_, _, _ []byte) uint64 { return 1 }
	if _, _, code := s.Find(c, [32]byte{9}); code != ErrSeeds {
		t.Fatal("exhausted search", code)
	}
	calls := 0
	c.DeriveAddress = func(_, _, _ []byte) uint64 { calls++; return 77 }
	if _, _, code := s.Find(c, [32]byte{9}); code != 77 || calls != 1 {
		t.Fatal("other status must propagate", code, calls)
	}
	if _, _, code := s.Find(solana.Context{}, [32]byte{9}); code != 2009 {
		t.Fatal("missing native callback", code)
	}
	var full Seeds
	for i := 0; i < 16; i++ {
		full.AddByte(1)
	}
	if _, _, code := full.Find(c, [32]byte{9}); code != ErrSeeds {
		t.Fatal("no room for the bump", code)
	}
}
