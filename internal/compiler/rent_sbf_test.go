package compiler

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"

	"gosvm/internal/targets"
	"gosvm/internal/testvm"
	"gosvm/sdk/system"
)

// rentSource calls the SDK's rent calculation (sdk/system/rent.go) and writes
// (lamports, status) as two little-endian uint64s into account 0.
// Input: [mode][rate 8][threshold bits 8][size 8]. Mode 0 is
// MinimumBalanceFor(rate, threshold, size); mode 1 is MinimumBalance(size)
// from the runner's Rent sysvar.
const rentSource = `package rentprobe

import (
	"gosvm/sdk/system"
	"gosvm/solana"
)

func get(in []byte, at int) uint64 {
	v := uint64(0)
	for i := 0; i < 8; i++ {
		v |= uint64(in[at+i]) << (8 * uint64(i))
	}
	return v
}

func put(out []byte, at int, v uint64) {
	for i := 0; i < 8; i++ {
		out[at+i] = byte(v >> (8 * uint64(i)))
	}
}

func Process(c solana.Context) uint64 {
	in := solana.Instruction(c)
	out := solana.Data(c, 0)
	var lamports, code uint64
	if in[0] == 0 {
		lamports, code = system.MinimumBalanceFor(get(in, 1), get(in, 9), get(in, 17))
	} else {
		lamports, code = system.MinimumBalance(c, get(in, 17))
	}
	put(out, 0, lamports)
	put(out, 8, code)
	return 0
}
`

// rentModule writes rentSource into dir as an SDK-2 module, with the SDK
// snapshot of examples/tutorial (which must match sdk/).
func rentModule(t *testing.T, dir string) {
	t.Helper()
	files := map[string][]byte{
		"program.go": []byte(rentSource),
		"go.mod":     []byte("module example.org/rentprobe\n\ngo 1.22\n\nrequire gosvm v0.0.0\n\nreplace gosvm => ./.gosvm/sdk\n"),
	}
	snapshot := "../../examples/tutorial/.gosvm"
	err := filepath.WalkDir(snapshot, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel("../../examples/tutorial", path)
		files[rel], err = os.ReadFile(path)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	for name, b := range files {
		path := filepath.Join(dir, name)
		if err = os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, b, 0644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestRentSBF runs system.MinimumBalanceFor on fogo-v0 and solana-v3 over the
// fast path's boundaries (thresholds 2.0 and 1.0, n = (128+size)*rate on
// either side of 2^53), the exact path and every error, and MinimumBalance
// with each runner's Rent sysvar, against the host results.
// GOSVM_RENT_RESULTS=<dir> keeps the run.
func TestRentSBF(t *testing.T) {
	if testing.Short() {
		t.Skip("SBF runs are skipped in -short mode")
	}
	llvm := modernLLVM()
	if llvm == "" {
		t.Skip("pinned platform-tools v1.57 backend required")
	}
	input := func(mode byte, rate, bits, size uint64) []byte {
		in := make([]byte, 25)
		in[0] = mode
		binary.LittleEndian.PutUint64(in[1:], rate)
		binary.LittleEndian.PutUint64(in[9:], bits)
		binary.LittleEndian.PutUint64(in[17:], size)
		return in
	}
	output := func(lamports, code uint64) []byte {
		out := make([]byte, 1024)
		binary.LittleEndian.PutUint64(out, lamports)
		binary.LittleEndian.PutUint64(out[8:], code)
		return out
	}
	two, one := math.Float64bits(2), math.Float64bits(1)
	thresholds := []uint64{two, one, two + 1, one - 1, math.Float64bits(2.5), math.Float64bits(0.5), 0, 1 << 63,
		math.Float64bits(-1), math.Float64bits(math.Inf(1)), math.Float64bits(math.NaN())}
	var cases []allocCase
	for _, size := range []uint64{0, 82, 165, 10240, system.MaxDataLen, system.MaxDataLen + 1, math.MaxUint64} {
		rates := []uint64{0, 3480, 6960, math.MaxUint64}
		if size <= system.MaxDataLen {
			exact := (uint64(1)<<53 - 1) / (128 + size)
			wrap := math.MaxUint64 / (128 + size)
			rates = append(rates, exact, exact+1, wrap, wrap+1)
		}
		for _, rate := range rates {
			for j, bits := range thresholds {
				lamports, code := system.MinimumBalanceFor(rate, bits, size)
				cases = append(cases, allocCase{name: fmt.Sprintf("for-size%d-rate%d-t%02d", size, rate, j), in: input(0, rate, bits, size), output: output(lamports, code)})
			}
		}
	}
	// The runners' Rent sysvars: LiteSVM 0.8.2 (fogo-v0) has 3,480 lamports
	// per byte-year at threshold 2.0, LiteSVM 0.13.0 (solana-v3) 6,960 at 1.0.
	rent := map[string][2]uint64{target.Fogo: {3480, two}, target.Solana: {6960, one}}
	keep := os.Getenv("GOSVM_RENT_RESULTS")
	for _, profile := range []string{target.Fogo, target.Solana} {
		t.Run(profile, func(t *testing.T) {
			cases := cases
			for _, size := range []uint64{0, 165, system.MaxDataLen, system.MaxDataLen + 1} {
				lamports, code := system.MinimumBalanceFor(rent[profile][0], rent[profile][1], size)
				cases = append(cases, allocCase{name: fmt.Sprintf("sysvar-size%d", size), in: input(1, 0, 0, size), output: output(lamports, code)})
			}
			runner, err := testvm.RunnerFor("", profile)
			if err != nil {
				t.Skipf("runner unavailable: %v", err)
			}
			dir := t.TempDir()
			if keep != "" {
				dir = filepath.Join(keep, profile, "rent")
				if err = os.MkdirAll(dir, 0755); err != nil {
					t.Fatal(err)
				}
			}
			rentModule(t, dir)
			result := runAllocDir(t, llvm, runner, profile, "rent", dir, 0, cases)
			t.Logf("%s: %d cases match the host (ELF %s); MinimumBalance(165) %v CU, MinimumBalanceFor(3480, 2.5, 165) %v CU",
				profile, len(cases), result["elf_sha256"], result["sysvar-size165"], result["for-size165-rate3480-t04"])
		})
	}
}
