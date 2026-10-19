package cpi

import (
	"bytes"
	"encoding/binary"
	"gosvm/sdk/pda"
	"gosvm/solana"
	"testing"
)

func TestBoundedMetasAndSignerGroups(t *testing.T) {
	var m Metas
	for i := uint64(0); i < MaxMetas; i++ {
		if m.Add(i, i%2 == 0, i%3 == 0) != 0 {
			t.Fatal(i)
		}
		// The first 16 records stay inline; the 17th moves them to the heap.
		if (m.wide != nil) != (i >= InlineMetas) || len(m.Bytes()) != int(i+1)*9 {
			t.Fatal("inline/heap records", i)
		}
	}
	b := append([]byte(nil), m.Bytes()...)
	if len(b) != 576 || m.Add(0, false, false) != ErrMetas || !bytes.Equal(b, m.Bytes()) {
		t.Fatal("meta bounds")
	}
	for i := 0; i < MaxMetas; i++ {
		flags := byte(0)
		if i%2 == 0 {
			flags |= 1
		}
		if i%3 == 0 {
			flags |= 2
		}
		if binary.LittleEndian.Uint64(b[i*9:]) != uint64(i) || b[i*9+8] != flags {
			t.Fatal("record encoding", i)
		}
	}
	var a, z pda.Seeds
	a.AddByte(9)
	z.AddBytes(nil)
	var groups Signers
	if groups.Add(&a) != 0 || groups.Add(&z) != 0 || !bytes.Equal(groups.Bytes(), []byte{2, 1, 1, 9, 1, 0}) {
		t.Fatal("signer groups", groups.Bytes())
	}
	if groups.Add(&a) != 0 || groups.Add(&z) != 0 || !bytes.Equal(groups.Bytes(), []byte{4, 1, 1, 9, 1, 0, 1, 1, 9, 1, 0}) {
		t.Fatal("four signer groups", groups.Bytes())
	}
	if groups.Add(&a) != ErrSigners || !bytes.Equal(groups.Bytes(), []byte{4, 1, 1, 9, 1, 0, 1, 1, 9, 1, 0}) {
		t.Fatal("fifth signer group", groups.Bytes())
	}
	var maximum pda.Seeds
	for i := 0; i < 16; i++ {
		maximum.AddKey([32]byte{})
	}
	var big Signers
	if big.Add(&maximum) != 0 || big.Add(&maximum) != ErrSigners {
		t.Fatal("combined signer bound")
	}
}

func TestInvokeValidationAndExactCallbackStatus(t *testing.T) {
	calls := 0
	c := solana.Context{Accounts: []solana.Account{{Executable: true}, {}}}
	c.InvokeInstruction = func(program uint64, metas, data, seeds []byte) uint64 {
		calls++
		if program != 0 || binary.LittleEndian.Uint64(metas) != 1 || metas[8] != 3 || !bytes.Equal(data, []byte{42}) || len(seeds) != 0 {
			t.Fatal(program, metas, data, seeds)
		}
		return 0x1200000000
	}
	var m Metas
	m.Add(1, true, true)
	if code := Invoke(c, 0, &m, []byte{42}, nil); code != 0x1200000000 || calls != 1 {
		t.Fatal(code, calls)
	}
	for _, tc := range []struct {
		p       uint64
		m, d, s []byte
		want    uint64
	}{
		{2, m.Bytes(), nil, nil, 2005}, {1, m.Bytes(), nil, nil, 2001},
		{0, []byte{0}, nil, nil, 2006}, {0, bytes.Repeat([]byte{0}, 65*9), nil, nil, 2006}, {0, bytes.Repeat([]byte{0}, 577), nil, nil, 2006},
		{0, []byte{2, 0, 0, 0, 0, 0, 0, 0, 0}, nil, nil, 2006}, {0, []byte{1, 0, 0, 0, 0, 0, 0, 0, 4}, nil, nil, 2006},
		{0, nil, make([]byte, 10241), nil, 2008}, {0, nil, nil, []byte{3}, 2007}, {0, nil, nil, []byte{1, 1, 33}, 2007},
		{0, nil, nil, []byte{1, 1, 2, 7}, 2007}, {0, nil, nil, []byte{0, 1}, 2007},
		{0, nil, nil, []byte{5, 0, 0, 0, 0, 0}, 2007}, {0, nil, nil, []byte{4, 0, 0, 0}, 2007},
	} {
		if got := solana.Invoke(c, tc.p, tc.m, tc.d, tc.s); got != tc.want {
			t.Fatalf("got %d want %d", got, tc.want)
		}
	}
	if calls != 1 {
		t.Fatal("invalid inputs reached callback", calls)
	}
	// The maxima reach the callback unchanged: 64 metas and four groups.
	var wide Metas
	for i := uint64(0); i < MaxMetas; i++ {
		wide.Add(i%2, false, false)
	}
	var seeds pda.Seeds
	seeds.AddByte(7)
	var four Signers
	for i := 0; i < MaxSigners; i++ {
		four.Add(&seeds)
	}
	c.InvokeInstruction = func(_ uint64, metas, _, signers []byte) uint64 {
		calls++
		if !bytes.Equal(metas, wide.Bytes()) || !bytes.Equal(signers, four.Bytes()) || len(metas) != 576 || signers[0] != 4 {
			t.Fatal(len(metas), signers)
		}
		return 0
	}
	if code := Invoke(c, 0, &wide, nil, &four); code != 0 || calls != 2 {
		t.Fatal(code, calls)
	}
}
