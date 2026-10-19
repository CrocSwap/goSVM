package compiler

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gosvm/internal/targets"
	"gosvm/internal/testvm"
	"gosvm/sdk/fixture"
)

// findAddressSource writes FindAddress (mode 0) or a CreateAddress loop over
// bumps 255..1 (mode 1) into account 0: the status as 8 LE bytes, then the
// 32-byte address and the bump. Input: [mode][program 32][seed group].
const findAddressSource = `package main

import "gosvm/solana"

func Process(c solana.Context) uint64 {
	in := solana.Instruction(c)
	out := solana.Data(c, 0)
	program, seeds := in[1:33], in[33:]
	var res [33]byte
	code := uint64(1)
	if in[0] == 0 {
		code = solana.FindAddress(c, seeds, program, res[:])
	} else {
		group := make([]byte, len(seeds)+2)
		copy(group, seeds)
		group[0]++
		group[len(seeds)] = 1
		for bump := 255; bump >= 1; bump-- {
			group[len(seeds)+1] = byte(bump)
			if solana.CreateAddress(c, group, program, res[:32]) == 0 {
				res[32] = byte(bump)
				code = 0
				break
			}
		}
	}
	for i := 0; i < 8; i++ {
		out[i] = byte(code >> (8 * uint64(i)))
	}
	copy(out[8:41], res[:])
	return 0
}
`

func findAddressGroup(seeds [][]byte) []byte {
	g := []byte{byte(len(seeds))}
	for _, s := range seeds {
		g = append(append(g, byte(len(s))), s...)
	}
	return g
}

// TestFindAddressSBF checks solana.FindAddress (sol_try_find_program_address)
// against the host find_program_address oracle and a CreateAddress loop on
// fogo-v0 and solana-v3, and records both CUs. Malformed groups return 2007
// without reaching the syscall. GOSVM_FIND_RESULTS=<dir> keeps the run.
func TestFindAddressSBF(t *testing.T) {
	if testing.Short() {
		t.Skip("SBF runs are skipped in -short mode")
	}
	llvm := modernLLVM()
	if llvm == "" {
		t.Skip("pinned platform-tools v1.57 backend required")
	}
	type vector struct {
		name  string
		seeds [][]byte
	}
	var vectors []vector
	vectors = append(vectors, vector{"empty", nil}, vector{"one-byte", [][]byte{{1}}})
	for j := 0; j < 24; j++ {
		key := bytes.Repeat([]byte{byte(j * 11)}, 32)
		vectors = append(vectors, vector{fmt.Sprintf("hyperlane-%02d", j), [][]byte{[]byte("hyperlane"), []byte("-"), []byte("process_authority"), []byte("-"), key}})
	}
	var fifteen [][]byte
	for j := 0; j < 15; j++ {
		fifteen = append(fifteen, bytes.Repeat([]byte{byte(j)}, 32))
	}
	vectors = append(vectors, vector{"fifteen-max", fifteen})
	var cases []allocCase
	bumps := map[string]byte{}
	for j, v := range vectors {
		program := bytes.Repeat([]byte{byte(j + 1)}, 32)
		var id fixture.Key
		copy(id[:], program)
		want, bump, err := fixture.FindProgramAddress(id, v.seeds...)
		if err != nil {
			t.Fatal(v.name, err)
		}
		bumps[v.name] = bump
		out := make([]byte, 1024)
		copy(out[8:40], want[:])
		out[40] = bump
		for mode := byte(0); mode < 2; mode++ {
			in := append(append([]byte{mode}, program...), findAddressGroup(v.seeds)...)
			cases = append(cases, allocCase{name: fmt.Sprintf("%s-mode%d", v.name, mode), in: in, units: 1_400_000, output: out})
		}
	}
	sixteen := append(append([][]byte{}, fifteen...), []byte{0})
	for _, bad := range []struct {
		name  string
		group []byte
	}{
		{"sixteen-seeds", findAddressGroup(sixteen)},
		{"seed-33-bytes", findAddressGroup([][]byte{bytes.Repeat([]byte{1}, 33)})},
		{"trailing-byte", append(findAddressGroup([][]byte{{1}}), 0)},
		{"short-seed", []byte{1, 5, 1}},
	} {
		out := make([]byte, 1024)
		binary.LittleEndian.PutUint64(out, 2007)
		in := append(append([]byte{0}, bytes.Repeat([]byte{3}, 32)...), bad.group...)
		cases = append(cases, allocCase{name: "malformed-" + bad.name, in: in, output: out})
	}
	keep := os.Getenv("GOSVM_FIND_RESULTS")
	for _, profile := range []string{target.Fogo, target.Solana} {
		t.Run(profile, func(t *testing.T) {
			runner, err := testvm.RunnerFor("", profile)
			if err != nil {
				t.Skipf("runner unavailable: %v", err)
			}
			result := runAllocProgram(t, llvm, runner, profile, "find-address", []byte(findAddressSource), 0, cases, keep)
			var saved []int
			rows := map[string]any{}
			for _, v := range vectors {
				find, loop := result[v.name+"-mode0"].(uint64), result[v.name+"-mode1"].(uint64)
				tries := 256 - int(bumps[v.name])
				// Same syscall price per bump either way.
				if int(loop)-int(find) <= 0 {
					t.Fatalf("%s: FindAddress %d CU, CreateAddress loop %d CU", v.name, find, loop)
				}
				saved = append(saved, int(loop)-int(find))
				rows[v.name] = map[string]any{"bump": bumps[v.name], "tries": tries, "find_cu": find, "loop_cu": loop}
			}
			sort.Ints(saved)
			summary := map[string]any{"profile": profile, "elf_sha256": result["elf_sha256"], "cases": len(cases),
				"median_cu_saved": saved[len(saved)/2], "min_cu_saved": saved[0], "max_cu_saved": saved[len(saved)-1], "vectors": rows}
			encoded, _ := json.MarshalIndent(summary, "", "  ")
			t.Logf("%s: %d cases match find_program_address\n%s", profile, len(cases), strings.TrimSpace(string(encoded)))
			if keep != "" {
				if err = os.WriteFile(filepath.Join(keep, profile, "summary.json"), append(encoded, '\n'), 0644); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
