package compiler_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	rangevalues "gosvm/examples/range-values"
	uint16values "gosvm/examples/uint16-values"
	"gosvm/internal/compiler"
	"gosvm/internal/sbftest"
	"gosvm/internal/targets"
	"gosvm/internal/testvm"
)

type sbfCase struct {
	Name         string   `json:"name"`
	Initial      string   `json:"initial"`
	Instructions []string `json:"instructions"`
	Expected     string   `json:"expected"`
	Error        uint64   `json:"error,omitempty"`
}

// sbfCases turns native (ordinary Go) results into schema-1 SVM fixtures. A
// nonzero status is a custom error with the state rolled back; vectors that
// panic natively are covered by the host differential tests instead, because
// schema-1 fixtures only express custom errors.
func sbfCases(prefix string, process func([]byte, []byte) uint64, vectors [][2][]byte, every int) []sbfCase {
	var cases []sbfCase
	for j, v := range vectors {
		if j%every != 0 {
			continue
		}
		state := append([]byte(nil), v[0]...)
		code, panicked := func() (code uint64, panicked bool) {
			defer func() { panicked = recover() != nil }()
			return process(state, v[1]), false
		}()
		if panicked || code > 0xffffffff {
			continue
		}
		c := sbfCase{Name: fmt.Sprintf("%s-%04d", prefix, j), Initial: hex.EncodeToString(v[0]), Instructions: []string{hex.EncodeToString(v[1])}, Expected: hex.EncodeToString(state)}
		if code != 0 {
			c.Error, c.Expected = code, c.Initial
		}
		cases = append(cases, c)
	}
	return cases
}

// The range and uint16 fixtures run as real SBF programs for both modern
// targets, committed and simulated, against native-Go expectations. Skips when
// platform-tools v1.57 is absent, and skips the runs (after building) when the
// managed runners are not installed.
func TestRangeUint16SBF(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("SBF runs are skipped in -short mode")
	}
	llvm := compiler.ModernLLVMForTest()
	if llvm == "" {
		t.Skip("pinned platform-tools v1.57 backend required")
	}
	fixtures := []struct {
		name    string
		source  string
		process func([]byte, []byte) uint64
		vectors [][2][]byte
		every   int
	}{
		{"range", "../../examples/range-values/program.go", rangevalues.Process, compiler.RangeVectorsForTest(), 1},
		{"uint16", "../../examples/uint16-values/program.go", uint16values.Process, compiler.Uint16VectorsForTest(), 4},
	}
	for _, profile := range []string{target.Fogo, target.Solana} {
		for _, fixture := range fixtures {
			t.Run(profile+"/"+fixture.name, func(t *testing.T) {
				source, err := os.ReadFile(fixture.source)
				if err != nil {
					t.Fatal(err)
				}
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
				cases := sbfCases(fixture.name, fixture.process, fixture.vectors, fixture.every)
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
				t.Logf("%s %s: %d SBF cases (%d custom errors) passed", profile, fixture.name, len(cases), errors)
			})
		}
	}
}
