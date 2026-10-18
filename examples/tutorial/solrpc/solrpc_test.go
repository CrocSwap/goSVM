package solrpc

import (
	"crypto/ed25519"
	"crypto/sha256"
	"testing"

	"filippo.io/edwards25519"
)

func TestBase58(t *testing.T) {
	if got := SystemProgram.String(); got != "11111111111111111111111111111111" {
		t.Fatalf("System program = %s", got)
	}
	for i := 0; i < 100; i++ {
		seed := sha256.Sum256([]byte{byte(i)})
		key := KeyOf(ed25519.NewKeyFromSeed(seed[:]))
		key[0] = byte(i % 3) // include leading zero bytes
		back, err := ParseKey(key.String())
		if err != nil || back != key {
			t.Fatalf("round trip %x: %v", key, err)
		}
	}
}

func TestFindProgramAddressIsOffCurve(t *testing.T) {
	program := KeyOf(ed25519.NewKeyFromSeed(make([]byte, 32)))
	for i := 0; i < 50; i++ {
		key, bump, err := FindProgramAddress(program, []byte("counter"), []byte{byte(i)})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := new(edwards25519.Point).SetBytes(key[:]); err == nil {
			t.Fatalf("address %s with bump %d is on the curve", key, bump)
		}
	}
}
