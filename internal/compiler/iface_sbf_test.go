package compiler

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	ifacevalues "gosvm/examples/iface-values"
	"gosvm/internal/targets"
	"gosvm/internal/testvm"
)

// ifaceSBFSource is examples/iface-values with a Context entrypoint: account
// 0 is the 1 KiB output and the instruction data is Run's input.
func ifaceSBFSource(t *testing.T) []byte {
	source := string(ifaceSource(t))
	source = strings.Replace(source, "package ifacevalues\n", "package main\n\nimport \"gosvm/solana\"\n", 1)
	entry := "func Process(s, i []byte) uint64 { return Run(s, i) }"
	if !strings.Contains(source, entry) {
		t.Fatal("iface-values entrypoint changed")
	}
	// Run returns the bytes written; the on-chain entrypoint returns 0.
	return []byte(strings.Replace(source, entry, "func Process(c solana.Context) uint64 { Run(solana.Data(c, 0), solana.Instruction(c)); return 0 }", 1))
}

// ifaceSBFCases are the vector cases (native expectations; a panic is an
// abort with rollback) and the boxing out-of-memory boundaries.
func ifaceSBFCases() ([]allocCase, int) {
	var cases []allocCase
	aborts := 0
	native := func(name string, in []byte) allocCase {
		c := allocCase{name: name, in: in, err: abortError, log: abortLog}
		out := make([]byte, 1024)
		failed := func() (failed bool) {
			defer func() { failed = recover() != nil }()
			ifacevalues.Run(out, in)
			return false
		}()
		if failed {
			aborts++
		} else {
			c.output = out
		}
		return c
	}
	for j, in := range IfaceVectorsForTest() {
		cases = append(cases, native(fmt.Sprintf("iface-%03d-mode%d", j, in[0]), in))
	}
	for kind := byte(0); kind < 6; kind++ {
		limit := boxLimits[kind]
		if limit < 0 {
			cases = append(cases, native(fmt.Sprintf("boxes-kind%d-5000-fits", kind), boxInput(kind, 5000)))
			continue
		}
		cases = append(cases, native(fmt.Sprintf("boxes-kind%d-%d-fits", kind, limit), boxInput(kind, limit)),
			allocCase{name: fmt.Sprintf("boxes-kind%d-%d-oom", kind, limit+1), in: boxInput(kind, limit+1), err: abortError, log: abortLog})
		aborts++
	}
	return cases, aborts
}

// dispatchSource measures a dynamic call against a direct call of the same
// method. Every mode builds n elements of ts []T and is []I (each T boxed);
// mode 0 then calls ts[j].M directly, mode 1 calls is[j].M dynamically
// (the type ID is loaded per element, so the test cannot be hoisted), and
// mode 2 inlines the method body. With in[3] == 1 the odd elements of is
// hold a U, the second type ID the dispatcher tests.
const dispatchSource = `package main

import "gosvm/solana"

type I interface{ M(x uint64) uint64 }
type T struct{ k uint64 }
type U struct{ k uint64 }

func (t T) M(x uint64) uint64 { return x*3 + t.k }
func (u U) M(x uint64) uint64 { return x + u.k }

func Process(c solana.Context) uint64 {
	in := solana.Instruction(c)
	out := solana.Data(c, 0)
	n := int(in[1])
	ts := make([]T, n)
	is := make([]I, n)
	for j := range n {
		ts[j] = T{k: uint64(j) + uint64(in[2])}
		if in[3] == 1 && j%2 == 1 {
			is[j] = U{k: ts[j].k}
		} else {
			is[j] = ts[j]
		}
	}
	acc := uint64(in[4])
	switch in[0] {
	case 0:
		for j := range n {
			acc = ts[j].M(acc)
		}
	case 1:
		for j := range n {
			acc = is[j].M(acc)
		}
	default:
		for j := range n {
			acc = acc*3 + ts[j].k
		}
	}
	for j := 0; j < 8; j++ {
		out[j] = byte(acc >> (8 * j))
	}
	return 0
}
`

// dispatchCase runs mode over n elements; u puts U values at the odd
// elements of the interface slice.
func dispatchCase(mode, n byte, u bool) allocCase {
	in := make([]byte, 128)
	in[0], in[1], in[2], in[4] = mode, n, 5, 7
	acc := uint64(7)
	for j := byte(0); j < n; j++ {
		k := uint64(j) + 5
		if u && mode == 1 && j%2 == 1 {
			acc = acc + k
		} else {
			acc = acc*3 + k
		}
	}
	name := fmt.Sprintf("dispatch-mode%d-n%d", mode, n)
	if u {
		in[3] = 1
		name = fmt.Sprintf("dispatch-mode%d-second-n%d", mode, n)
	}
	out := make([]byte, 1024)
	for j := 0; j < 8; j++ {
		out[j] = byte(acc >> (8 * j))
	}
	return allocCase{name: name, in: in, output: out}
}

// TestInterfaceSBF builds examples/iface-values for fogo-v0 and solana-v3
// with the default 32 KiB heap and runs the vectors and boxing boundaries
// through the pinned runners: completions must leave native Go's output
// bytes; panics and out-of-memory must abort and roll back. It also measures
// the compute units of a dynamic call against a direct call. Skips without
// the v1.57 backend or a runner (make test). GOSVM_IFACE_RESULTS=<dir> keeps
// fixtures, ELFs, reports and a summary.
func TestInterfaceSBF(t *testing.T) {
	if testing.Short() {
		t.Skip("SBF runs are skipped in -short mode")
	}
	llvm := modernLLVM()
	if llvm == "" {
		t.Skip("pinned platform-tools v1.57 backend required")
	}
	cases, aborts := ifaceSBFCases()
	source := ifaceSBFSource(t)
	keep := os.Getenv("GOSVM_IFACE_RESULTS")
	for _, profile := range []string{target.Fogo, target.Solana} {
		t.Run(profile, func(t *testing.T) {
			runner, err := testvm.RunnerFor("", profile)
			if err != nil {
				t.Skipf("runner unavailable: %v", err)
			}
			result := runAllocProgram(t, llvm, runner, profile, "iface", source, 0, cases, keep)
			var dispatch []allocCase
			for _, mode := range []byte{0, 1, 2} {
				dispatch = append(dispatch, dispatchCase(mode, 0, false), dispatchCase(mode, 100, false))
			}
			dispatch = append(dispatch, dispatchCase(1, 0, true), dispatchCase(1, 100, true))
			cu := runAllocProgram(t, llvm, runner, profile, "dispatch", []byte(dispatchSource), 0, dispatch, keep)
			perCall := func(name string) float64 {
				return float64(cu[name+"n100"].(uint64)-cu[name+"n0"].(uint64)) / 100
			}
			summary := map[string]any{
				"profile": profile, "iface_elf_sha256": result["elf_sha256"], "dispatch_elf_sha256": cu["elf_sha256"],
				"cases": len(cases), "aborts": aborts,
				"cu_per_iteration_direct": perCall("dispatch-mode0-"), "cu_per_iteration_dynamic": perCall("dispatch-mode1-"),
				"cu_per_iteration_dynamic_second_type": perCall("dispatch-mode1-second-"), "cu_per_iteration_no_call": perCall("dispatch-mode2-"),
			}
			for name, v := range result {
				if strings.HasPrefix(name, "boxes-") {
					summary[name+"_cu"] = v
				}
			}
			maxCU := uint64(0)
			for name, v := range result {
				if strings.HasPrefix(name, "iface-") && v.(uint64) > maxCU {
					maxCU = v.(uint64)
				}
			}
			summary["max_vector_cu"] = maxCU
			encoded, _ := json.MarshalIndent(summary, "", "  ")
			t.Logf("%s: %d cases (%d aborts with rollback) match native Go and the heap model\n%s", profile, len(cases), aborts, encoded)
			if keep != "" {
				if err = os.WriteFile(filepath.Join(keep, profile, "summary.json"), append(encoded, '\n'), 0644); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
