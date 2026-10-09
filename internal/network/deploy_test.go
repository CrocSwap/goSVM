package network

import (
	"bytes"
	"crypto/ed25519"
	"testing"
)

// The general compiler reproduces the hand-built single-instruction message.
func TestSignMatchesInvokeEncoding(t *testing.T) {
	payer := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, 32))
	program := bytes.Repeat([]byte{9}, 32)
	hash := bytes.Repeat([]byte{3}, 32)
	want, wantSig := Transaction(payer, program, hash, []byte{1, 2, 3})
	got, gotSig, err := Sign(payer, hash, []Instruction{{program, []Meta{{payer.Public().(ed25519.PublicKey), true, false}}, []byte{1, 2, 3}}})
	if err != nil || !bytes.Equal(got, want) || gotSig != wantSig {
		t.Fatalf("%x\n%x %v", got, want, err)
	}
}

// Jupiter v6's program account on mainnet records this program-data address.
func TestProgramDataAddress(t *testing.T) {
	program, _ := DecodeBase58("JUP6LkbZbjS1jKKwapdHNy74zcZ3tLUZoi5QNyVTaV4")
	if got := Base58(ProgramDataAddress(program)); got != "4Ec7ZxZS6Sbdg5UGSLHbAnM7GQHp2eFd4KYWRexAipQT" {
		t.Fatal(got)
	}
}

func TestUpgradeDecisions(t *testing.T) {
	elf := []byte{1, 2, 3}
	if !Unchanged([]byte{1, 2, 3, 0, 0}, elf) || Unchanged([]byte{1, 2, 3, 0, 9}, elf) || Unchanged([]byte{1, 2}, elf) || Unchanged([]byte{1, 2, 4}, elf) {
		t.Fatal("unchanged detection")
	}
	for _, c := range []struct{ capacity, size, want int }{
		{100, 100, 0},
		{100, 50, 0},
		{100, 101, minimumExtend},
		{100, 100 + 20000, 20000},
		{maxAccountData - programDataHeader - 10, maxAccountData - programDataHeader - 5, 10},
	} {
		if got, err := ExtendBytes(c.capacity, c.size); err != nil || got != c.want {
			t.Fatal(c, got, err)
		}
	}
	if _, err := ExtendBytes(100, maxAccountData); err == nil {
		t.Fatal("accepted oversize program")
	}
}
