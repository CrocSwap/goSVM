package compiler

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"math/bits"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gosvm/internal/sbftest"
	"gosvm/internal/targets"
	"gosvm/internal/testvm"
)

// Probe record layout and modes; see testdata/bitsprobe/program.go.
const (
	probeRecord   = 48
	probeVectors  = 0
	probeHand     = 1
	probeBits     = 2
	probeBaseline = 3
)

// probeRef is the native math/bits oracle for probe op codes.
func probeRef(op, a, b, c uint64) (uint64, uint64) {
	switch op {
	case 0:
		return bits.Mul64(a, b)
	case 1:
		return bits.Div64(a, b, c)
	case 2:
		return bits.Add64(a, b, c)
	case 3:
		return uint64(bits.LeadingZeros64(a)), 0
	case 4:
		return bits.Sub64(a, b, c)
	case 5:
		return bits.Rem64(a, b, c), 0
	case 6:
		q, r := bits.Div32(uint32(a), uint32(b), uint32(c))
		return uint64(q), uint64(r)
	case 7:
		return uint64(bits.Rem32(uint32(a), uint32(b), uint32(c))), 0
	case 8:
		return uint64(bits.TrailingZeros64(a)), uint64(bits.OnesCount64(a))
	case 9:
		return bits.Reverse64(a), bits.RotateLeft64(a, -3)
	case 10:
		s, carry := bits.Add32(uint32(a), uint32(b), uint32(c))
		h, l := bits.Mul32(uint32(a), uint32(b))
		return uint64(s) | uint64(carry)<<32, uint64(h)<<32 | uint64(l)
	}
	panic("unknown probe op")
}

func probePanics(op, a, b, c uint64) (panics bool) {
	defer func() { panics = recover() != nil }()
	probeRef(op, a, b, c)
	return false
}

// mulDivExact is floor(a*b/d), or 0 when d == 0 or the quotient overflows:
// the contract of full-swap's mulDiv, computed with big integers.
func mulDivExact(a, b, d uint64) uint64 {
	if d == 0 {
		return 0
	}
	q := new(big.Int).Mul(new(big.Int).SetUint64(a), new(big.Int).SetUint64(b))
	q.Quo(q, new(big.Int).SetUint64(d))
	if !q.IsUint64() {
		return 0
	}
	return q.Uint64()
}

func base58(b []byte) string {
	const alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
	n := new(big.Int).SetBytes(b)
	var out []byte
	for n.Sign() > 0 {
		m := new(big.Int)
		n.DivMod(n, big.NewInt(58), m)
		out = append([]byte{alphabet[m.Int64()]}, out...)
	}
	for _, c := range b {
		if c != 0 {
			break
		}
		out = append([]byte{'1'}, out...)
	}
	return string(out)
}

func probeKey(s string) []byte { h := sha256.Sum256([]byte(s)); return h[:] }

type probeRow struct{ op, a, b, c uint64 }

// probeCase is one scenario: the account's records and one step per
// transaction of up to eight instructions over consecutive record ranges.
type probeCase struct {
	name       string
	rows       []probeRow
	mode       uint64
	perIx      int
	abort      bool
	expectRows func(r probeRow) (uint64, uint64)
}

func probeData(rows []probeRow, result func(probeRow) (uint64, uint64)) []byte {
	d := make([]byte, len(rows)*probeRecord)
	for j, r := range rows {
		o := d[j*probeRecord:]
		binary.LittleEndian.PutUint64(o, r.op)
		binary.LittleEndian.PutUint64(o[8:], r.a)
		binary.LittleEndian.PutUint64(o[16:], r.b)
		binary.LittleEndian.PutUint64(o[24:], r.c)
		if result != nil {
			r0, r1 := result(r)
			binary.LittleEndian.PutUint64(o[32:], r0)
			binary.LittleEndian.PutUint64(o[40:], r1)
		}
	}
	return d
}

// bitsProbeFixtures renders the cases as gosvm-svm-fixtures-v1. Error steps
// assert the rolled-back (initial) state.
func bitsProbeFixtures(cases []probeCase, abortError json.RawMessage) ([]byte, error) {
	program := base58(probeKey("gosvm math/bits probe program; local tests only"))
	data := base58(probeKey("gosvm math/bits probe records"))
	state := func(d []byte) sbftest.FixtureState {
		return sbftest.FixtureState{Data: hex.EncodeToString(d), Owner: program, Lamports: 1_000_000_000_000}
	}
	initial := state(nil)
	suite := sbftest.FixtureSuite{Format: sbftest.FixtureFormat, ProgramID: program, PayerSeed: hex.EncodeToString(probeKey("gosvm math/bits probe payer; never fund")),
		Accounts: []sbftest.FixtureAccount{{Name: "records", Address: data, Initial: &initial}}}
	for _, c := range cases {
		before := probeData(c.rows, nil)
		after := before
		if !c.abort {
			after = probeData(c.rows, c.expectRows)
		}
		fc := sbftest.FixtureCase{Name: c.name, Overrides: []sbftest.FixtureOverride{{Name: "records", State: state(before)}}}
		step := sbftest.FixtureStep{Expect: sbftest.FixtureExpectation{Error: json.RawMessage("null"), Accounts: []sbftest.FixtureCheck{{Account: "records"}}}}
		expected := state(after)
		step.Expect.Accounts[0].State = &expected
		if c.abort {
			step.Expect.Error = abortError
		}
		for first := 0; first < len(c.rows); first += c.perIx {
			n := c.perIx
			if first+n > len(c.rows) {
				n = len(c.rows) - first
			}
			ix := make([]byte, 24)
			binary.LittleEndian.PutUint64(ix, uint64(first))
			binary.LittleEndian.PutUint64(ix[8:], uint64(n))
			binary.LittleEndian.PutUint64(ix[16:], c.mode)
			step.Instructions = append(step.Instructions, sbftest.FixtureInstruction{Program: "program", Accounts: []sbftest.FixtureMeta{{Account: "records", Writable: true}}, Data: hex.EncodeToString(ix)})
		}
		if len(step.Instructions) > 8 {
			return nil, fmt.Errorf("%s: %d instructions", c.name, len(step.Instructions))
		}
		fc.Steps = []sbftest.FixtureStep{step}
		suite.Cases = append(suite.Cases, fc)
	}
	return json.MarshalIndent(suite, "", " ")
}

// quoteInputs are mulDiv(y, net, x+net) arguments as full-swap's Quote passes
// them, for pool reserves x, y and net input net <= x:
//   - narrow: y*net < 2^64 (the hand-written version's hi == 0 fast path);
//   - wide: reserves up to 1e18, y*net >= 2^64;
//   - skewed: wide, with a small input reserve x (1e6..4e9) against a large
//     output reserve y (1e15..1.8e19), so the divisor x+net is small.
var quoteClasses = []string{"narrow", "wide", "skewed"}

func quoteInputs(rng *rand.Rand, n int, class string) []probeRow {
	var rows []probeRow
	for len(rows) < n {
		x := uint64(1_000_000) + rng.Uint64()%1_000_000_000_000_000_000
		y := uint64(1_000_000) + rng.Uint64()%1_000_000_000_000_000_000
		switch class {
		case "narrow":
			x, y = x%4_000_000_000+1, y%4_000_000_000+1
		case "skewed":
			x, y = uint64(1_000_000)+rng.Uint64()%4_000_000_000, uint64(1_000_000_000_000_000)+rng.Uint64()%17_446_744_073_709_551_615
		}
		net := uint64(1) + rng.Uint64()%x
		if hi, _ := bits.Mul64(y, net); (hi != 0) != (class != "narrow") || x+net < x || hi >= x+net {
			continue
		}
		rows = append(rows, probeRow{0, y, net, x + net})
	}
	return rows
}

// bitsProbeCases returns the vector and trap scenarios for testdata/bitsprobe
// and the mulDiv CU scenarios for testdata/bitscu.
func bitsProbeCases() (probe, cu []probeCase) {
	rng := rand.New(rand.NewSource(20261014))
	vectorRef := func(r probeRow) (uint64, uint64) { return probeRef(r.op, r.a, r.b, r.c) }
	edges := bitsEdges(64)
	pick := func() uint64 {
		switch rng.Intn(3) {
		case 0:
			return rng.Uint64()
		case 1:
			return rng.Uint64() >> uint(rng.Intn(64))
		}
		return edges[rng.Intn(len(edges))]
	}
	var vectors []probeRow
	for op := uint64(0); op <= 10; op++ {
		for j := 0; j < 400; j++ {
			a, b, c := pick(), pick(), pick()
			if op == 2 || op == 4 {
				c = []uint64{0, 1, 2, ^uint64(0), rng.Uint64()}[j%5]
			}
			if (op == 1 || op == 5 || op == 6 || op == 7) && j%3 != 2 {
				// Mostly valid divisions, including hi = y-1 and normalized y.
				mask := ^uint64(0)
				if op >= 6 {
					mask = 1<<32 - 1
					a, b, c = a&mask, b&mask, c&mask
				}
				if c == 0 {
					c = 1
				}
				if j%3 == 0 {
					a = c - 1
				} else {
					a %= c
				}
			}
			if probePanics(op, a, b, c) {
				continue
			}
			vectors = append(vectors, probeRow{op, a, b, c})
		}
	}
	cases := []probeCase{}
	for first := 0; first < len(vectors); first += 800 {
		end := first + 800
		if end > len(vectors) {
			end = len(vectors)
		}
		cases = append(cases, probeCase{name: fmt.Sprintf("vectors-%02d", len(cases)), rows: vectors[first:end], mode: probeVectors, perIx: 100, expectRows: vectorRef})
	}
	// Each trap is its own transaction: Go panics, SBF aborts and rolls back.
	for _, r := range []probeRow{
		{1, 0, 5, 0}, {1, 7, 5, 7}, {1, 8, 5, 7}, {1, ^uint64(0), 0, ^uint64(0)}, {1, 1 << 63, 0, 1 << 63},
		{5, 3, 9, 0}, {5, 0, 0, 0},
		{6, 0, 5, 0}, {6, 7, 5, 7}, {6, 1<<32 - 1, 0, 1<<32 - 1},
		{7, 3, 9, 0},
	} {
		if !probePanics(r.op, r.a, r.b, r.c) {
			panic(fmt.Sprintf("probe trap %+v does not panic in Go", r))
		}
		cases = append(cases, probeCase{name: fmt.Sprintf("trap-op%d-%x-%x-%x", r.op, r.a, r.b, r.c), rows: []probeRow{r}, mode: probeVectors, perIx: 1, abort: true})
	}
	// Rem64 does not trap on quotient overflow (hi >= y), unlike Div64.
	rem := []probeRow{{5, 7, 5, 7}, {5, ^uint64(0), ^uint64(0), 3}, {5, 1 << 63, 1, 1 << 63}, {7, 7, 5, 7}, {7, 1<<32 - 1, 9, 2}}
	cases = append(cases, probeCase{name: "rem-overflow", rows: rem, mode: probeVectors, perIx: 8, expectRows: vectorRef})
	// CU comparison: identical loops over the same inputs; per call =
	// (case - baseline) / records.
	for _, label := range quoteClasses {
		rows := quoteInputs(rng, 64, label)
		for _, m := range []struct {
			name string
			mode uint64
		}{{"hand", probeHand}, {"bits", probeBits}, {"baseline", probeBaseline}} {
			m := m
			cu = append(cu, probeCase{name: "cu-" + label + "-" + m.name, rows: rows, mode: m.mode, perIx: 64, expectRows: func(r probeRow) (uint64, uint64) {
				if m.mode == probeBaseline {
					return r.a ^ r.b ^ r.c, 0
				}
				return mulDivExact(r.a, r.b, r.c), 0
			}})
		}
	}
	return cases, cu
}

// TestBitsSBF builds testdata/bitsprobe and testdata/bitscu for fogo-v0 and
// solana-v3 and runs math/bits vectors, traps (with rollback) and the mulDiv
// CU comparison through the pinned runners. It skips without the v1.57
// backend or a runner. GOSVM_BITS_RESULTS=<dir> keeps the fixtures, ELFs,
// runner reports and a summary per target.
func TestBitsSBF(t *testing.T) {
	llvm := modernLLVM()
	if llvm == "" {
		t.Skip("pinned platform-tools v1.57 backend required")
	}
	keep := os.Getenv("GOSVM_BITS_RESULTS")
	probe, cu := bitsProbeCases()
	for _, profile := range []string{target.Fogo, target.Solana} {
		t.Run(profile, func(t *testing.T) {
			runner, err := testvm.RunnerFor("", profile)
			if err != nil {
				t.Skipf("runner unavailable: %v", err)
			}
			summary := map[string]any{"profile": profile}
			probeCU := runBitsProgram(t, llvm, runner, profile, "bitsprobe", probe, keep)
			vectors, traps := 0, 0
			for _, c := range probe {
				if c.abort {
					traps++
				} else {
					vectors += len(c.rows)
				}
			}
			summary["bitsprobe_elf_sha256"], summary["vectors"], summary["trap_cases"] = probeCU["elf_sha256"], vectors, traps
			cuCU := runBitsProgram(t, llvm, runner, profile, "bitscu", cu, keep)
			summary["bitscu_elf_sha256"] = cuCU["elf_sha256"]
			for _, width := range quoteClasses {
				base := cuCU["cu-"+width+"-baseline"].(uint64)
				summary["mulDiv_"+width+"_baseline_tx_cu"] = base
				for _, kind := range []string{"hand", "bits"} {
					total := cuCU["cu-"+width+"-"+kind].(uint64)
					summary["mulDiv_"+width+"_"+kind+"_tx_cu"] = total
					summary["mulDiv_"+width+"_"+kind+"_cu_per_call"] = float64(total-base) / 64
				}
			}
			encoded, _ := json.MarshalIndent(summary, "", "  ")
			t.Logf("%s", encoded)
			if keep != "" {
				if err = os.WriteFile(filepath.Join(keep, profile, "summary.json"), append(encoded, '\n'), 0644); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

// runBitsProgram builds testdata/<name> for the profile, runs the cases and
// returns each case's CU and the ELF hash.
func runBitsProgram(t *testing.T, llvm, runner, profile, name string, cases []probeCase, keep string) map[string]any {
	t.Helper()
	source := mustRead(t, filepath.Join("testdata", name, "program.go"))
	dir := t.TempDir()
	if keep != "" {
		dir = filepath.Join(keep, profile, name)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "program.go"), source, 0644); err != nil {
		t.Fatal(err)
	}
	elf := filepath.Join(dir, "program.so")
	if err := Build(dir, elf, llvm, profile); err != nil {
		t.Fatal(err)
	}
	// Traps call the VM abort syscall: the runner reports a program failure
	// with no custom code, and the transaction rolls back.
	fixtures, err := bitsProbeFixtures(cases, json.RawMessage(`{"InstructionError":[0,"ProgramFailedToComplete"]}`))
	if err != nil {
		t.Fatal(err)
	}
	fixturePath := filepath.Join(dir, "fixtures.json")
	if err = os.WriteFile(fixturePath, fixtures, 0644); err != nil {
		t.Fatal(err)
	}
	p, err := target.Resolve(profile)
	if err != nil {
		t.Fatal(err)
	}
	report := filepath.Join(dir, "svm-results.json")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	var log strings.Builder
	if err = sbftest.RunGeneral(ctx, elf, fixturePath, report, io.Writer(&log), sbftest.FastOptions{Runner: runner, Profile: p.Name, PlatformTools: p.Tools}); err != nil {
		t.Fatalf("%s: %v\n%s", name, err, log.String())
	}
	var parsed struct {
		Cases []sbftest.FixtureCaseResult `json:"cases"`
	}
	if err = json.Unmarshal(mustRead(t, report), &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed.Cases) != len(cases) {
		t.Fatalf("%s: ran %d of %d cases", name, len(parsed.Cases), len(cases))
	}
	out := map[string]any{"elf_sha256": fmt.Sprintf("%x", sha256.Sum256(mustRead(t, elf)))}
	for _, c := range parsed.Cases {
		out[c.Name] = c.Steps[0].CU
	}
	return out
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
