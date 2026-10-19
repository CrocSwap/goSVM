package pda

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"

	"gosvm/sdk/fixture"
	"gosvm/solana"
)

// deriveAddress is sol_create_program_address for native tests: it decodes
// one encoded seed group and returns 1 for an on-curve result, as the
// syscall does.
func deriveAddress(calls *[]byte) func(seeds, program, output []byte) uint64 {
	return func(seeds, program, output []byte) uint64 {
		h := sha256.New()
		count, off := int(seeds[0]), 1
		var bump byte
		for j := 0; j < count; j++ {
			n := int(seeds[off])
			h.Write(seeds[off+1 : off+1+n])
			if n == 1 {
				bump = seeds[off+1]
			}
			off += 1 + n
		}
		if calls != nil {
			*calls = append(*calls, bump)
		}
		h.Write(program)
		h.Write([]byte("ProgramDerivedAddress"))
		var k [32]byte
		copy(k[:], h.Sum(nil))
		if fixture.OnCurve(k) {
			return 1
		}
		copy(output, k[:])
		return 0
	}
}

// testdata/find_vectors.json is the output of Rust solana-pubkey 3.0.0's
// try_find_program_address (results/compiler/2026-10-19-find-address/oracle):
// 400 pseudo-random programs with 0..15 seeds of 0..32 bytes, and 16 seeds.
func TestFindAddressMatchesSolana(t *testing.T) {
	b, err := os.ReadFile("testdata/find_vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors []struct {
		Program string   `json:"program"`
		Seeds   []string `json:"seeds"`
		Address *string  `json:"address"`
		Bump    byte     `json:"bump"`
	}
	if err = json.Unmarshal(b, &vectors); err != nil || len(vectors) != 401 {
		t.Fatal(len(vectors), err)
	}
	skipped := 0
	for i, v := range vectors {
		var program [32]byte
		p, _ := hex.DecodeString(v.Program)
		copy(program[:], p)
		var s Seeds
		raw := [][]byte{}
		for _, seed := range v.Seeds {
			d, _ := hex.DecodeString(seed)
			raw = append(raw, d)
			if s.AddBytes(d) != 0 {
				t.Fatal(i, "seed")
			}
		}
		before := append([]byte(nil), s.Bytes()...)
		var calls []byte
		c := solana.Context{DeriveAddress: deriveAddress(&calls)}
		address, bump, code := s.FindAddress(c, program)
		if !bytes.Equal(before, s.Bytes()) {
			t.Fatal(i, "FindAddress changed the builder")
		}
		host, hostBump, hostErr := fixture.FindProgramAddress(program, raw...)
		if v.Address == nil {
			if code != ErrSeeds || len(v.Seeds) != 16 || hostErr == nil {
				t.Fatal(i, code)
			}
			continue
		}
		want, _ := hex.DecodeString(*v.Address)
		if code != 0 || !bytes.Equal(address[:], want) || bump != v.Bump {
			t.Fatalf("vector %d: %x %d %d, want %s %d", i, address, bump, code, *v.Address, v.Bump)
		}
		if hostErr != nil || host != address || hostBump != bump {
			t.Fatalf("vector %d: host fixture %x %d %v", i, host, hostBump, hostErr)
		}
		// One attempt per bump, from 255 down: the on-curve ones are skipped.
		if len(calls) != 256-int(bump) || calls[0] != 255 || calls[len(calls)-1] != bump {
			t.Fatalf("vector %d: attempts %v", i, calls)
		}
		skipped += len(calls) - 1
	}
	if skipped < 100 {
		t.Fatalf("vectors skip only %d on-curve bumps", skipped)
	}
	t.Logf("%d vectors match solana-pubkey; %d on-curve bumps skipped", len(vectors), skipped)
}

func TestFindAddressStatuses(t *testing.T) {
	var s Seeds
	if s.AddBytes([]byte("vault")) != 0 {
		t.Fatal("seed")
	}
	program := [32]byte{9}
	// No off-curve result: 255 attempts, bumps 255..1, never 0.
	var bumps []byte
	c := solana.Context{DeriveAddress: func(seeds, _, out []byte) uint64 { bumps = append(bumps, seeds[len(seeds)-1]); out[0] = 1; return 1 }}
	address, bump, code := s.FindAddress(c, program)
	if code != ErrNoAddress || bump != 0 || address != ([32]byte{}) || len(bumps) != 255 || bumps[0] != 255 || bumps[254] != 1 {
		t.Fatal(code, bump, len(bumps))
	}
	// Any other status ends the search and propagates.
	calls := 0
	c.DeriveAddress = func(_, _, _ []byte) uint64 { calls++; return 77 }
	if _, _, code = s.FindAddress(c, program); code != 77 || calls != 1 {
		t.Fatal(code, calls)
	}
	if _, _, code = s.FindAddress(solana.Context{}, program); code != 2009 {
		t.Fatal("missing native callback", code)
	}
	var full Seeds
	for j := 0; j < 16; j++ {
		if full.AddByte(byte(j)) != 0 {
			t.Fatal(j)
		}
	}
	if _, _, code = full.FindAddress(c, program); code != ErrSeeds {
		t.Fatal("16 seeds leave no room for the bump", code)
	}
	// The found bump completes the signer seeds: Address agrees.
	c.DeriveAddress = deriveAddress(nil)
	found, bump, code := s.FindAddress(c, program)
	if code != 0 || s.AddByte(bump) != 0 {
		t.Fatal(code)
	}
	if again, code := s.Address(c, program); code != 0 || again != found {
		t.Fatal("Address with the found bump", code)
	}
}
