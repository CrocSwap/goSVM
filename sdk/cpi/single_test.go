package cpi

import (
	"bytes"
	"gosvm/sdk/pda"
	"gosvm/solana"
	"math/rand"
	"testing"
)

func TestSingleSignerEncodingAndRejectedAdds(t *testing.T) {
	rng := rand.New(rand.NewSource(20261006))
	for trial := 0; trial < 10000; trial++ {
		var seeds pda.Seeds
		var compact SingleSigner
		want := []byte{1, 0}
		for i := 0; i < trial%21; i++ {
			b := make([]byte, rng.Intn(35))
			rng.Read(b)
			before := bytes.Clone(compact.Bytes())
			a, code := seeds.AddBytes(b), compact.AddBytes(b)
			if a != code {
				t.Fatalf("trial %d addition %d: %d != %d", trial, i, a, code)
			}
			if code == 0 {
				want[1]++
				want = append(append(want, byte(len(b))), b...)
			} else if !bytes.Equal(before, compact.Bytes()) {
				t.Fatal("rejected addition changed encoding")
			}
		}
		var general Signers
		if general.Add(&seeds) != 0 || !bytes.Equal(general.Bytes(), compact.Bytes()) || !bytes.Equal(want, compact.Bytes()) {
			t.Fatalf("trial %d: independent/general encoding mismatch", trial)
		}
	}
	var compact SingleSigner
	for i := 0; i < 16; i++ {
		if compact.AddBytes(bytes.Repeat([]byte{byte(i)}, 32)) != 0 {
			t.Fatal("maximum seed rejected")
		}
	}
	before := bytes.Clone(compact.Bytes())
	if len(before) != 530 || compact.AddByte(7) != pda.ErrSeeds || !bytes.Equal(before, compact.Bytes()) {
		t.Fatal("maximum capacity or rejection")
	}
}

func TestInvokeSingleSignedUnsignedAndGuards(t *testing.T) {
	var seeds SingleSigner
	seeds.AddByte(42)
	var metas Metas
	metas.Add(1, true, true)
	calls := 0
	expected := []byte{1, 1, 1, 42}
	c := solana.Context{Accounts: []solana.Account{{Executable: true}, {}}}
	c.InvokeInstruction = func(program uint64, m, data, signer []byte) uint64 {
		calls++
		if program != 0 || !bytes.Equal(m, metas.Bytes()) || !bytes.Equal(data, []byte{9}) || !bytes.Equal(signer, expected) {
			t.Fatal("invoke encoding", program, m, data, signer)
		}
		return 0x1200000000
	}
	if code := InvokeSingle(c, 0, &metas, []byte{9}, &seeds); code != 0x1200000000 || calls != 1 {
		t.Fatal("signed status", code, calls)
	}
	expected = nil
	if code := InvokeSingle(c, 0, &metas, []byte{9}, nil); code != 0x1200000000 || calls != 2 {
		t.Fatal("unsigned nil signer", code, calls)
	}
	if InvokeSingle(c, 0, nil, nil, &seeds) != ErrMetas || InvokeSingle(c, 2, &metas, nil, &seeds) != 2005 ||
		InvokeSingle(c, 1, &metas, nil, &seeds) != 2001 || InvokeSingle(c, 0, &metas, make([]byte, 10241), &seeds) != 2008 || calls != 2 {
		t.Fatal("invoke guards")
	}
	var invalid Metas
	invalid.Add(2, false, false)
	if InvokeSingle(c, 0, &invalid, nil, &seeds) != 2006 || calls != 2 {
		t.Fatal("meta index guard")
	}
	c.InvokeInstruction = nil
	if InvokeSingle(c, 0, &metas, nil, &seeds) != 2009 {
		t.Fatal("missing native callback")
	}
}
