package compiler_test

import (
	"bytes"
	"context"
	"debug/elf"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	genericvalues "gosvm/examples/generic-values"
	"gosvm/internal/compiler"
	"gosvm/internal/sbftest"
	"gosvm/internal/targets"
	"gosvm/internal/testvm"
)

// The generic oracle runs as a real SBF program for both modern targets,
// committed and simulated, against native-Go expectations. Skips when
// platform-tools v1.57 is absent, and skips the runs (after building) when the
// managed runners are not installed.
func TestGenericSBF(t *testing.T) {
	if testing.Short() {
		t.Skip("SBF runs are skipped in -short mode")
	}
	llvm := compiler.ModernLLVMForTest()
	if llvm == "" {
		t.Skip("pinned platform-tools v1.57 backend required")
	}
	source, err := os.ReadFile("../../examples/generic-values/program.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, profile := range []string{target.Fogo, target.Solana} {
		t.Run(profile, func(t *testing.T) {
			dir := t.TempDir()
			if err = os.MkdirAll(filepath.Join(dir, "testdata"), 0755); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(dir, "program.go"), source, 0644); err != nil {
				t.Fatal(err)
			}
			elf := filepath.Join(dir, "program.so")
			if err = compiler.Build(dir, elf, llvm, profile); err != nil {
				t.Fatal(err)
			}
			runner, err := testvm.RunnerFor("", profile)
			if err != nil {
				t.Skipf("built the %s ELF; runner unavailable: %v", profile, err)
			}
			cases := sbfCases("generic", genericvalues.Process, compiler.GenericVectorsForTest(), 4)
			errors := 0
			for _, c := range cases {
				if c.Error != 0 {
					errors++
				}
			}
			if len(cases) < 200 || errors == 0 || errors == len(cases) {
				t.Fatalf("fixture needs successes and custom errors: %d cases, %d errors", len(cases), errors)
			}
			suite, err := json.Marshal(map[string]any{"schema": 1, "cases": cases})
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(dir, "testdata", "sbf.json"), suite, 0644); err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			err = sbftest.RunFast(context.Background(), dir, elf, &out, sbftest.FastOptions{Runner: runner, Profile: profile, PlatformTools: target.ModernTools, ReportPath: filepath.Join(dir, "report.json")})
			if err != nil {
				t.Fatalf("%v\n%s", err, out.String())
			}
			t.Logf("%s generic: %d SBF cases (%d custom errors) passed", profile, len(cases), errors)
		})
	}
}

const genericHelpers = `package p
type Wide interface{ ~uint32 | ~uint64 }
type Pair[T Wide] struct{ A, B T }
func Min[T Wide](a, b T) T {
	if a < b {
		return a
	}
	return b
}
func (p *Pair[T]) Order() T { p.A, p.B = Min(p.A, p.B), p.A^p.B^Min(p.A, p.B); return p.B - p.A }
func Process(s, i []byte) uint64 {
	p := Pair[uint64]{uint64(s[0]) << 40, uint64(s[1])}
	q := Pair[uint32]{uint32(s[2]) << 20, uint32(s[3])}
	return p.Order() + uint64(q.Order()) + Min(uint64(len(i)), 7) + uint64(Min(uint32(s[4]), 9))
}
`

const handwrittenHelpers = `package p
type Pair64 struct{ A, B uint64 }
type Pair32 struct{ A, B uint32 }
func min64(a, b uint64) uint64 {
	if a < b {
		return a
	}
	return b
}
func min32(a, b uint32) uint32 {
	if a < b {
		return a
	}
	return b
}
func (p *Pair64) Order() uint64 { p.A, p.B = min64(p.A, p.B), p.A^p.B^min64(p.A, p.B); return p.B - p.A }
func (p *Pair32) Order() uint32 { p.A, p.B = min32(p.A, p.B), p.A^p.B^min32(p.A, p.B); return p.B - p.A }
func Process(s, i []byte) uint64 {
	p := Pair64{uint64(s[0]) << 40, uint64(s[1])}
	q := Pair32{uint32(s[2]) << 20, uint32(s[3])}
	return p.Order() + uint64(q.Order()) + min64(uint64(len(i)), 7) + uint64(min32(uint32(s[4]), 9))
}
`

// Monomorphization costs nothing: generic helpers instantiated for two widths
// compile to the same SBF machine code as hand-written per-type copies, so
// size and compute units are identical.
func TestGenericMatchesHandwrittenSBF(t *testing.T) {
	if testing.Short() {
		t.Skip("SBF builds are skipped in -short mode")
	}
	llvm := compiler.ModernLLVMForTest()
	if llvm == "" {
		t.Skip("pinned platform-tools v1.57 backend required")
	}
	text := func(profile, source string) []byte {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "program.go"), []byte(source), 0644); err != nil {
			t.Fatal(err)
		}
		output := filepath.Join(dir, "program.so")
		if err := compiler.Build(dir, output, llvm, profile); err != nil {
			t.Fatal(err)
		}
		f, err := elf.Open(output)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		section := f.Section(".text")
		if section == nil {
			t.Fatal("no .text section")
		}
		b, err := section.Data()
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	for _, profile := range []string{target.Fogo, target.Solana} {
		generic, handwritten := text(profile, genericHelpers), text(profile, handwrittenHelpers)
		if !bytes.Equal(generic, handwritten) {
			t.Errorf("%s: generic .text (%d bytes) differs from hand-written (%d bytes)", profile, len(generic), len(handwritten))
			continue
		}
		t.Logf("%s: identical .text, %d bytes", profile, len(generic))
	}
}
