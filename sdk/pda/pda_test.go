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
