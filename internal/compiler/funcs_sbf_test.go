package compiler

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	funcvalues "gosvm/examples/func-values"
	"gosvm/internal/targets"
	"gosvm/internal/testvm"
)

// funcSBFSource is examples/func-values with a Context entrypoint: account
// 0 is the 1 KiB output and the instruction data is Run's input.
func funcSBFSource(t *testing.T) []byte {
	source := string(funcSource(t))
	source = strings.Replace(source, "package funcvalues\n", "package main\n\nimport \"gosvm/solana\"\n", 1)
	entry := "func Process(s, i []byte) uint64 { return Run(s, i) }"
	if !strings.Contains(source, entry) {
		t.Fatal("func-values entrypoint changed")
	}
	// Run returns the bytes written; the on-chain entrypoint returns 0.
	return []byte(strings.Replace(source, entry, "func Process(c solana.Context) uint64 { Run(solana.Data(c, 0), solana.Instruction(c)); return 0 }", 1))
}

// funcSBFCases are the vector cases (native expectations; a panic is an
// abort with rollback) and the closure-environment heap boundaries.
func funcSBFCases() ([]allocCase, int) {
	var cases []allocCase
	aborts := 0
	native := func(name string, in []byte) allocCase {
		c := allocCase{name: name, in: in, err: abortError, log: abortLog}
		out := make([]byte, 1024)
		failed := func() (failed bool) {
			defer func() { failed = recover() != nil }()
			funcvalues.Run(out, in)
			return false
		}()
		if failed {
			aborts++
		} else {
			c.output = out
		}
		return c
	}
	for j, in := range FuncVectorsForTest() {
		cases = append(cases, native(fmt.Sprintf("func-%03d-mode%d", j, in[0]), in))
	}
	for kind := byte(0); kind < 7; kind++ {
		limit := envLimit(kind)
		if limit < 0 {
			cases = append(cases, native(fmt.Sprintf("envs-kind%d-5000-fits", kind), envInput(kind, 5000)))
			continue
		}
		cases = append(cases, native(fmt.Sprintf("envs-kind%d-%d-fits", kind, limit), envInput(kind, limit)),
			allocCase{name: fmt.Sprintf("envs-kind%d-%d-oom", kind, limit+1), in: envInput(kind, limit+1), err: abortError, log: abortLog})
		aborts++
	}
	return cases, aborts
}

// closureCallSource measures calls through function values against a
// direct call of the same function. Every mode builds n elements: ks, fs
// (each the declared function step) and cs (each a closure capturing its
// k). Mode 0 calls step directly, mode 1 through fs[j] (the value is loaded
// per element, so the dispatch cannot be hoisted), mode 2 through cs[j]
// (a closure: the target ID and the captured k are loaded from its
// environment), and mode 3 inlines the body.
const closureCallSource = `package main

import "gosvm/solana"

func step(acc, k uint64) uint64 { return acc*3 + k }

func Process(c solana.Context) uint64 {
	in := solana.Instruction(c)
	out := solana.Data(c, 0)
	n := int(in[1])
	ks := make([]uint64, n)
	fs := make([]func(uint64, uint64) uint64, n)
	cs := make([]func(uint64) uint64, n)
	for j := range n {
		k := uint64(j) + uint64(in[2])
		ks[j] = k
		fs[j] = step
		cs[j] = func(acc uint64) uint64 { return acc*3 + k }
	}
	acc := uint64(in[4])
	switch in[0] {
	case 0:
		for j := range n {
			acc = step(acc, ks[j])
		}
	case 1:
		for j := range n {
			acc = fs[j](acc, ks[j])
		}
	case 2:
		for j := range n {
			acc = cs[j](acc)
		}
	default:
		for j := range n {
			acc = acc*3 + ks[j]
		}
	}
	for j := 0; j < 8; j++ {
		out[j] = byte(acc >> (8 * j))
	}
	return 0
}
`

func closureCallCase(mode, n byte) allocCase {
	in := make([]byte, 128)
	in[0], in[1], in[2], in[4] = mode, n, 5, 7
	acc := uint64(7)
	for j := byte(0); j < n; j++ {
		acc = acc*3 + uint64(j) + 5
	}
	out := make([]byte, 1024)
	for j := 0; j < 8; j++ {
		out[j] = byte(acc >> (8 * j))
	}
	return allocCase{name: fmt.Sprintf("call-mode%d-n%d", mode, n), in: in, output: out}
}

// TestFuncValuesSBF builds examples/func-values for fogo-v0 and solana-v3
// with the default 32 KiB heap and runs the vectors and closure-environment
// boundaries through the pinned runners: completions must leave native Go's
// output bytes; panics (including nil function calls) and out-of-memory
// must abort and roll back. It also measures the compute units of calls
// through function values and closures against a direct call. Skips without
// the v1.57 backend or a runner (make test). GOSVM_FUNC_RESULTS=<dir> keeps
// fixtures, ELFs, reports and a summary.
func TestFuncValuesSBF(t *testing.T) {
	if testing.Short() {
		t.Skip("SBF runs are skipped in -short mode")
	}
	llvm := modernLLVM()
	if llvm == "" {
		t.Skip("pinned platform-tools v1.57 backend required")
	}
	cases, aborts := funcSBFCases()
	source := funcSBFSource(t)
	keep := os.Getenv("GOSVM_FUNC_RESULTS")
	for _, profile := range []string{target.Fogo, target.Solana} {
		t.Run(profile, func(t *testing.T) {
			runner, err := testvm.RunnerFor("", profile)
			if err != nil {
				t.Skipf("runner unavailable: %v", err)
			}
			result := runAllocProgram(t, llvm, runner, profile, "func", source, 0, cases, keep)
			var calls []allocCase
			for _, mode := range []byte{0, 1, 2, 3} {
				calls = append(calls, closureCallCase(mode, 0), closureCallCase(mode, 100))
			}
			cu := runAllocProgram(t, llvm, runner, profile, "calls", []byte(closureCallSource), 0, calls, keep)
			perCall := func(mode int) float64 {
				return float64(cu[fmt.Sprintf("call-mode%d-n100", mode)].(uint64)-cu[fmt.Sprintf("call-mode%d-n0", mode)].(uint64)) / 100
			}
			summary := map[string]any{
				"profile": profile, "func_elf_sha256": result["elf_sha256"], "calls_elf_sha256": cu["elf_sha256"],
				"cases": len(cases), "aborts": aborts,
				"cu_per_iteration_direct": perCall(0), "cu_per_iteration_function_value": perCall(1),
				"cu_per_iteration_closure": perCall(2), "cu_per_iteration_inline": perCall(3),
			}
			for name, v := range result {
				if strings.HasPrefix(name, "envs-") {
					summary[name+"_cu"] = v
				}
			}
			maxCU := uint64(0)
			for name, v := range result {
				if strings.HasPrefix(name, "func-") && v.(uint64) > maxCU {
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
