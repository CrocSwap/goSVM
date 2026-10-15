package compiler

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	compoundvalues "gosvm/examples/compound-values"
	"gosvm/internal/targets"
	"gosvm/internal/testvm"
)

// compoundCUSource measures compound assignment against the spelled-out
// form: mode 0 runs acc += k, mode 1 acc = acc + k, mode 2 m[j%8] += k and
// mode 3 m[j%8] = m[j%8] + k (two map lookups per iteration instead of one).
const compoundCUSource = `package main

import "gosvm/solana"

func Process(c solana.Context) uint64 {
	in := solana.Instruction(c)
	out := solana.Data(c, 0)
	n := int(in[1])
	acc := uint64(in[2])
	m := make(map[uint64]uint64, 8)
	for j := range n {
		k := uint64(j) ^ uint64(in[3])
		switch in[0] {
		case 0:
			acc += k
		case 1:
			acc = acc + k
		case 2:
			m[uint64(j%8)] += k
		default:
			m[uint64(j%8)] = m[uint64(j%8)] + k
		}
	}
	for j := uint64(0); j < 8; j++ {
		acc ^= m[j] << j
	}
	for j := 0; j < 8; j++ {
		out[j] = byte(acc >> (8 * j))
	}
	return 0
}
`

func compoundCUCase(mode, n byte) allocCase {
	in := make([]byte, 128)
	in[0], in[1], in[2], in[3] = mode, n, 9, 5
	acc := uint64(9)
	var m [8]uint64
	for j := 0; j < int(n); j++ {
		k := uint64(j) ^ 5
		if mode < 2 {
			acc += k
		} else {
			m[j%8] += k
		}
	}
	for j := uint64(0); j < 8; j++ {
		acc ^= m[j] << j
	}
	out := make([]byte, 1024)
	for j := 0; j < 8; j++ {
		out[j] = byte(acc >> (8 * j))
	}
	return allocCase{name: fmt.Sprintf("cu-mode%d-n%d", mode, n), in: in, output: out}
}

// compoundSBFSource is examples/compound-values with a Context entrypoint:
// account 0 is the 1 KiB output and the instruction data is Run's input.
func compoundSBFSource(t *testing.T) []byte {
	return comboSBFSource(t, compoundSource(t), "compoundvalues")
}

// TestCompoundSBF builds examples/compound-values for fogo-v0 and solana-v3
// and runs its vectors through the pinned runners: completions must leave
// native Go's output bytes; panics (division by zero, negative shifts,
// bounds, nil pointers and nil maps) must abort and roll back. It also
// measures compound assignment against the spelled-out form. Skips without
// the v1.57 backend or a runner (make test). GOSVM_INT_RESULTS=<dir> keeps
// fixtures, ELFs, reports and a summary under compound/.
func TestCompoundSBF(t *testing.T) {
	if testing.Short() {
		t.Skip("SBF runs are skipped in -short mode")
	}
	llvm := modernLLVM()
	if llvm == "" {
		t.Skip("pinned platform-tools v1.57 backend required")
	}
	cases, aborts := comboCases("compound", CompoundVectorsForTest(), compoundvalues.Run, 0, nil, nil)
	source := compoundSBFSource(t)
	keep := os.Getenv("GOSVM_INT_RESULTS")
	if keep != "" {
		keep = filepath.Join(keep, "compound")
	}
	for _, profile := range []string{target.Fogo, target.Solana} {
		t.Run(profile, func(t *testing.T) {
			runner, err := testvm.RunnerFor("", profile)
			if err != nil {
				t.Skipf("runner unavailable: %v", err)
			}
			result := runAllocProgram(t, llvm, runner, profile, "compound", source, 0, cases, keep)
			var probes []allocCase
			for mode := byte(0); mode < 4; mode++ {
				probes = append(probes, compoundCUCase(mode, 0), compoundCUCase(mode, 128))
			}
			cu := runAllocProgram(t, llvm, runner, profile, "compound-cu", []byte(compoundCUSource), 0, probes, keep)
			per := func(mode int) float64 {
				return float64(cu[fmt.Sprintf("cu-mode%d-n128", mode)].(uint64)-cu[fmt.Sprintf("cu-mode%d-n0", mode)].(uint64)) / 128
			}
			summary := map[string]any{
				"profile": profile, "compound_elf_sha256": result["elf_sha256"], "cu_elf_sha256": cu["elf_sha256"],
				"cases": len(cases), "aborts": aborts,
				"cu_per_iteration_local_compound": per(0), "cu_per_iteration_local_spelled": per(1),
				"cu_per_iteration_map_compound": per(2), "cu_per_iteration_map_spelled": per(3),
			}
			encoded, _ := json.MarshalIndent(summary, "", "  ")
			t.Logf("%s: %d cases (%d aborts with rollback) match native Go\n%s", profile, len(cases), aborts, encoded)
			if keep != "" {
				if err = os.WriteFile(filepath.Join(keep, profile, "summary.json"), append(encoded, '\n'), 0644); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
