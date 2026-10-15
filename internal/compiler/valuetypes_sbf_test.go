package compiler

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	valuetypes "gosvm/examples/value-types"
	"gosvm/internal/targets"
	"gosvm/internal/testvm"
)

// valueTypesCUSource measures the new forms against their hand-written
// equivalents: mode 0 compares U128 structs with ==, mode 1 compares their
// fields, mode 2 updates fields of an array of structs, mode 3 the same
// fields in two scalar arrays; mode 4 panics with a constant string after
// the loop and mode 5 with a 64-byte string.
const valueTypesCUSource = `package main

import "gosvm/solana"

type U128 struct{ Lo, Hi uint64 }

type Pair struct {
	K uint32
	V uint64
}

func Process(c solana.Context) uint64 {
	in := solana.Instruction(c)
	out := solana.Data(c, 0)
	n := int(in[1])
	acc := uint64(0)
	var us [8]U128
	for j := range us {
		us[j] = U128{uint64(j % 3), uint64(in[2])}
	}
	var ps [8]Pair
	var ks [8]uint32
	var vs [8]uint64
	for j := range n {
		k := uint64(j) ^ uint64(in[3])
		switch in[0] {
		case 0:
			if us[j%8] == us[(j+1)%8] {
				acc++
			}
		case 1:
			a, b := us[j%8], us[(j+1)%8]
			if a.Lo == b.Lo && a.Hi == b.Hi {
				acc++
			}
		case 2:
			ps[j%8].V += k
			ps[j%8].K++
		default:
			vs[j%8] += k
			ks[j%8]++
		}
	}
	for j := range ps {
		acc ^= (ps[j].V + uint64(ps[j].K) + vs[j] + uint64(ks[j])) << j
	}
	if in[0] == 4 {
		panic("overflow")
	}
	if in[0] == 5 {
		panic(string(in[8:72]))
	}
	for j := 0; j < 8; j++ {
		out[j] = byte(acc >> (8 * j))
	}
	return 0
}
`

func valueTypesCUCase(mode, n byte) allocCase {
	in := make([]byte, 128)
	in[0], in[1], in[2], in[3] = mode, n, 9, 5
	for j := 8; j < 72; j++ {
		in[j] = 'a' + byte(j%26)
	}
	name := fmt.Sprintf("cu-mode%d-n%d", mode, n)
	if mode >= 4 {
		line := "Program log: panic: overflow"
		if mode == 5 {
			line = "Program log: panic: " + string(in[8:72])
		}
		return allocCase{name: name, in: in, err: abortError, log: abortLog, panicLog: line}
	}
	var us [8][2]uint64
	for j := range us {
		us[j] = [2]uint64{uint64(j % 3), 9}
	}
	var ks [8]uint32
	var vs [8]uint64
	acc := uint64(0)
	for j := 0; j < int(n); j++ {
		k := uint64(j) ^ 5
		switch mode {
		case 0, 1:
			if us[j%8] == us[(j+1)%8] {
				acc++
			}
		default:
			vs[j%8] += k
			ks[j%8]++
		}
	}
	for j := range vs {
		acc ^= (vs[j] + uint64(ks[j])) << j
	}
	out := make([]byte, 1024)
	for j := 0; j < 8; j++ {
		out[j] = byte(acc >> (8 * j))
	}
	return allocCase{name: name, in: in, output: out}
}

// valueTypesCases runs each vector natively: a completion must leave native
// Go's output; a panic must abort, roll back and, for a panic call, log its
// line (the program is compiled as package main).
func valueTypesCases(vectors [][]byte) (cases []allocCase, aborts, logged int) {
	for j, in := range vectors {
		c := allocCase{name: fmt.Sprintf("value-%03d-mode%d", j, in[0]), in: in, units: comboUnits, err: abortError, log: abortLog}
		out := make([]byte, 1024)
		func() {
			defer func() {
				if r := recover(); r != nil {
					aborts++
					if line := ExpectedPanicLine(r, "main"); line != "" {
						c.panicLog = "Program log: " + line
						logged++
					}
				}
			}()
			valuetypes.Run(out, append([]byte(nil), in...))
			c.output = out
		}()
		cases = append(cases, c)
	}
	return cases, aborts, logged
}

// TestValueTypesSBF builds examples/value-types for fogo-v0 and solana-v3
// and runs its vectors through the pinned runners: completions must leave
// native Go's output bytes; panics must abort with rollback, and each panic
// call must log its line exactly once. Skips without the v1.57 backend or a
// runner (make test). GOSVM_INT_RESULTS=<dir> keeps fixtures, ELFs, reports
// and a summary under value-types/.
func TestValueTypesSBF(t *testing.T) {
	if testing.Short() {
		t.Skip("SBF runs are skipped in -short mode")
	}
	llvm := modernLLVM()
	if llvm == "" {
		t.Skip("pinned platform-tools v1.57 backend required")
	}
	cases, aborts, logged := valueTypesCases(ValueTypesVectorsForTest())
	source := comboSBFSource(t, valueTypesSource(t), "valuetypes")
	keep := os.Getenv("GOSVM_INT_RESULTS")
	if keep != "" {
		keep = filepath.Join(keep, "value-types")
	}
	for _, profile := range []string{target.Fogo, target.Solana} {
		t.Run(profile, func(t *testing.T) {
			runner, err := testvm.RunnerFor("", profile)
			if err != nil {
				t.Skipf("runner unavailable: %v", err)
			}
			result := runAllocProgram(t, llvm, runner, profile, "value-types", source, 0, cases, keep)
			maxCU := uint64(0)
			for _, c := range cases {
				if v, ok := result[c.name].(uint64); ok && c.output != nil && v > maxCU {
					maxCU = v
				}
			}
			var probes []allocCase
			for mode := byte(0); mode < 6; mode++ {
				probes = append(probes, valueTypesCUCase(mode, 0), valueTypesCUCase(mode, 128))
			}
			cu := runAllocProgram(t, llvm, runner, profile, "value-types-cu", []byte(valueTypesCUSource), 0, probes, keep)
			per := func(mode int) float64 {
				return float64(cu[fmt.Sprintf("cu-mode%d-n128", mode)].(uint64)-cu[fmt.Sprintf("cu-mode%d-n0", mode)].(uint64)) / 128
			}
			summary := map[string]any{
				"profile": profile, "elf_sha256": result["elf_sha256"], "cu_elf_sha256": cu["elf_sha256"],
				"cases": len(cases), "aborts": aborts, "panic_log_lines": logged, "max_completed_cu": maxCU,
				"cu_per_iteration_struct_eq": per(0), "cu_per_iteration_field_eq": per(1),
				"cu_per_iteration_struct_array_update": per(2), "cu_per_iteration_scalar_arrays_update": per(3),
				"cu_panic_constant_after_empty_loop": cu["cu-mode4-n0"], "cu_panic_64_byte_string_after_empty_loop": cu["cu-mode5-n0"],
				"cu_completed_mode2_empty_loop": cu["cu-mode2-n0"],
			}
			encoded, _ := json.MarshalIndent(summary, "", "  ")
			t.Logf("%s: %d cases (%d aborts with rollback, %d with a panic log line) match native Go\n%s", profile, len(cases), aborts, logged, encoded)
			if keep != "" {
				if err = os.WriteFile(filepath.Join(keep, profile, "summary.json"), append(encoded, '\n'), 0644); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
