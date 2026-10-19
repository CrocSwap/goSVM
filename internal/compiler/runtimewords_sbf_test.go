package compiler

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"os"
	"testing"

	"gosvm/internal/targets"
	"gosvm/internal/testvm"
	"gosvm/sdk/fixture"
)

// runtimeWordsSource drives the runtime routines that read, write and compare
// whole words on SBF (docs/BACKEND_INTERFACE.md#runtime-word-access). Account
// 0 is the 1 KiB program-owned output. Input: [mode][program 32][group...].
//
//	mode 0: CreateAddress into out[8:40] (status in out[0:8]); out[40:72] is
//	        pre-filled with 0xee and must survive a failed derivation.
//	mode 1: CreateAddress whose output is the group's own bytes (aliasing).
//	mode 2: ResizeAccount(0, 1024), CloseAccount(0, 0), Rent and IsPDA.
const runtimeWordsSource = `package main

import "gosvm/solana"

func put(out []byte, at int, v uint64) {
	for i := 0; i < 8; i++ {
		out[at+i] = byte(v >> (8 * uint64(i)))
	}
}

func Process(c solana.Context) uint64 {
	in := solana.Instruction(c)
	out := solana.Data(c, 0)
	program := in[1:33]
	group := make([]byte, len(in)-33)
	copy(group, in[33:])
	switch in[0] {
	case 0:
		for i := 8; i < 72; i++ {
			out[i] = 0xee
		}
		put(out, 0, solana.CreateAddress(c, group, program, out[8:40]))
	case 1:
		buf := make([]byte, len(group)+32)
		copy(buf, group)
		// The output overlaps the group's last 32 bytes (the key seed).
		code := solana.CreateAddress(c, buf[:len(group)], program, buf[len(group)-32:len(group)])
		put(out, 0, code)
		copy(out[8:40], buf[len(group)-32:len(group)])
	case 2:
		put(out, 0, solana.ResizeAccount(c, 0, 1024))
		put(out, 8, solana.CloseAccount(c, 0, 0))
		put(out, 16, solana.Rent(c, out[24:41]))
		if solana.IsPDA(c, 0, group, 7) {
			out[48] = 1
		} else {
			out[48] = 2
		}
	}
	return 0
}
`

func pdaHash(program []byte, seeds ...[]byte) fixture.Key {
	h := sha256.New()
	for _, s := range seeds {
		h.Write(s)
	}
	h.Write(program)
	h.Write([]byte("ProgramDerivedAddress"))
	var k fixture.Key
	copy(k[:], h.Sum(nil))
	return k
}

// TestRuntimeWordsSBF runs runtimeWordsSource on fogo-v0 and solana-v3 against
// host-computed results.
func TestRuntimeWordsSBF(t *testing.T) {
	if testing.Short() {
		t.Skip("SBF runs are skipped in -short mode")
	}
	llvm := modernLLVM()
	if llvm == "" {
		t.Skip("pinned platform-tools v1.57 backend required")
	}
	var cases []allocCase
	group := func(seeds ...[]byte) []byte {
		g := []byte{byte(len(seeds))}
		for _, s := range seeds {
			g = append(append(g, byte(len(s))), s...)
		}
		return g
	}
	key := bytes.Repeat([]byte{0x5a}, 32)
	for j := 0; j < 16; j++ {
		program := bytes.Repeat([]byte{byte(j + 1)}, 32)
		var id fixture.Key
		copy(id[:], program)
		// One off-curve and one on-curve bump per program.
		var good, bad int = -1, -1
		for b := 255; b >= 0 && (good < 0 || bad < 0); b-- {
			if fixture.OnCurve(pdaHash(program, []byte("vault"), key, []byte{byte(b)})) {
				if bad < 0 {
					bad = b
				}
			} else if good < 0 {
				good = b
			}
		}
		for _, b := range []int{good, bad} {
			g := group([]byte("vault"), key, []byte{byte(b)})
			out := make([]byte, 1024)
			for i := 8; i < 72; i++ {
				out[i] = 0xee
			}
			if b == good {
				want := pdaHash(program, []byte("vault"), key, []byte{byte(b)})
				copy(out[8:40], want[:])
			} else {
				binary.LittleEndian.PutUint64(out, 1)
			}
			cases = append(cases, allocCase{name: fmt.Sprintf("create-%02d-bump%03d", j, b), in: append(append([]byte{0}, program...), g...), output: out})
		}
		// Aliasing: the key seed is last, so the output overlaps it.
		g := group([]byte("vault"), []byte{byte(good)}, key)
		out := make([]byte, 1024)
		want := pdaHash(program, []byte("vault"), []byte{byte(good)}, key)
		if fixture.OnCurve(want) {
			binary.LittleEndian.PutUint64(out, 1)
			copy(out[8:40], key)
		} else {
			copy(out[8:40], want[:])
		}
		cases = append(cases, allocCase{name: fmt.Sprintf("alias-%02d", j), in: append(append([]byte{1}, program...), g...), output: out})
	}
	// The runners' Rent sysvars: LiteSVM 0.8.2 (fogo-v0) has 3,480 lamports
	// per byte-year at threshold 2.0, LiteSVM 0.13.0 (solana-v3) 6,960 at 1.0.
	rent := map[string][2]uint64{target.Fogo: {3480, 0x4000000000000000}, target.Solana: {6960, 0x3ff0000000000000}}
	keep := os.Getenv("GOSVM_WORDS_RESULTS")
	for _, profile := range []string{target.Fogo, target.Solana} {
		t.Run(profile, func(t *testing.T) {
			out := make([]byte, 1024)
			binary.LittleEndian.PutUint64(out[8:], 2013) // refund account is the source
			binary.LittleEndian.PutUint64(out[24:], rent[profile][0])
			binary.LittleEndian.PutUint64(out[32:], rent[profile][1])
			out[40] = 50
			out[48] = 2
			cases := append(cases, allocCase{name: "lifecycle", in: append(append([]byte{2}, key...), 's'), output: out})
			runner, err := testvm.RunnerFor("", profile)
			if err != nil {
				t.Skipf("runner unavailable: %v", err)
			}
			result := runAllocProgram(t, llvm, runner, profile, "runtime-words", []byte(runtimeWordsSource), 0, cases, keep)
			t.Logf("%s: %d cases match the host (ELF %s); create-00 %v CU, lifecycle %v CU", profile, len(cases), result["elf_sha256"], result[cases[0].name], result["lifecycle"])
		})
	}
}
