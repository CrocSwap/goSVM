package compiler

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gosvm/internal/targets"
	"gosvm/internal/testvm"
)

// realLibsSBFProgram is examples/real-libraries (either form) with a
// Context entrypoint: account 0 is the 1 KiB output and the instruction data
// is Run's input.
func realLibsSBFProgram(t *testing.T, program *Program) *Program {
	t.Helper()
	p := *program
	p.Packages = append([]PackageSources(nil), program.Packages...)
	patched := false
	for i, unit := range p.Packages {
		if unit.Path != p.Entry {
			continue
		}
		sources := append([]Source(nil), unit.Sources...)
		for j, src := range sources {
			if filepath.Base(src.Name) != "reallibs.go" {
				continue
			}
			source := string(src.Data)
			entry := "func Process(s, i []byte) uint64 { return Run(s, i) }"
			if !strings.Contains(source, entry) || !strings.Contains(source, "import (\n") {
				t.Fatal("real-libraries entrypoint changed")
			}
			source = strings.Replace(source, "import (\n", "import (\n\t\"gosvm/solana\"\n", 1)
			source = strings.Replace(source, entry, "func Process(c solana.Context) uint64 { Run(solana.Data(c, 0), solana.Instruction(c)); return 0 }", 1)
			sources[j].Data = []byte(source)
			patched = true
		}
		p.Packages[i].Sources = sources
	}
	if !patched {
		t.Fatal("real-libraries entry package not found")
	}
	return &p
}

// realLibsBench is one representative call of the bench group (Bench* in
// examples/real-libraries/bench.go) for the CU table.
type realLibsBench struct {
	name string
	op   int
	in   []byte
}

// benchVector builds a bench-group vector: words a..e, n0, n1, flags and
// text.
func benchVector(op int, words []*big.Int, n0, n1 uint64, flags byte, text string) []byte {
	in := make([]byte, 256)
	in[0], in[1], in[3] = 7, byte(op), flags
	for j, x := range words {
		if x != nil {
			putWord(in, 32+32*j, x)
		}
	}
	for k := 0; k < 8; k++ {
		in[8+k] = byte(n0 >> (8 * k))
		in[16+k] = byte(n1 >> (8 * k))
	}
	in[2] = byte(len(text))
	copy(in[192:], text)
	return in
}

// sqrtAtTick approximates the Q64.96 sqrt price of a tick (for inputs only).
func sqrtAtTick(tick int64) *big.Int {
	f := new(big.Float).SetPrec(256).SetFloat64(1)
	base := new(big.Float).SetPrec(256).SetFloat64(1.0001)
	n := tick
	if n < 0 {
		n = -n
	}
	for b := base; n > 0; n >>= 1 {
		if n&1 == 1 {
			f.Mul(f, b)
		}
		b = new(big.Float).SetPrec(256).Mul(b, b)
	}
	if tick < 0 {
		f.Quo(new(big.Float).SetPrec(256).SetFloat64(1), f)
	}
	f.Sqrt(f)
	f.Mul(f, new(big.Float).SetPrec(256).SetInt(new(big.Int).Lsh(big.NewInt(1), 96)))
	x, _ := f.Int(nil)
	return x
}

// realLibsBenches are the representative inputs of the CU table.
func realLibsBenches() []realLibsBench {
	rng := rand.New(rand.NewSource(20261016))
	bits := func(n int) *big.Int {
		x := new(big.Int).Rand(rng, new(big.Int).Lsh(big.NewInt(1), uint(n)))
		return x.SetBit(x, n-1, 1) // exactly n bits
	}
	dec := func(s string) *big.Int { x, _ := new(big.Int).SetString(s, 10); return x }
	price := sqrtAtTick(0)
	liq := dec("1000000000000000000000000") // 1e24
	a256, b256, c256 := bits(256), bits(256), bits(255)
	var benches []realLibsBench
	add := func(name string, op int, words []*big.Int, n0, n1 uint64, flags byte, text string) {
		benches = append(benches, realLibsBench{name, op, benchVector(op, words, n0, n1, flags, text)})
	}
	add("none (decode and write only)", 0, []*big.Int{a256, b256, c256}, 0, 0, 0, "")
	add("uint256 Add (256-bit)", 1, []*big.Int{a256, b256}, 0, 0, 0, "")
	add("uint256 Mul (256 x 256-bit)", 2, []*big.Int{a256, b256}, 0, 0, 0, "")
	add("uint256 Mul (128 x 128-bit)", 2, []*big.Int{bits(128), bits(128)}, 0, 0, 0, "")
	add("uint256 Div (256 / 64-bit)", 3, []*big.Int{a256, bits(64)}, 0, 0, 0, "")
	add("uint256 Div (256 / 192-bit, Knuth)", 4, []*big.Int{a256, bits(192)}, 0, 0, 0, "")
	add("uint256 Div (256 / 128-bit, Knuth)", 4, []*big.Int{a256, bits(128)}, 0, 0, 0, "")
	add("uint256 MulDivOverflow (200 x 200 / 150-bit)", 5, []*big.Int{bits(200), bits(200), bits(150)}, 0, 0, 0, "")
	add("uint256 MulMod (256-bit operands)", 6, []*big.Int{a256, b256, c256}, 0, 0, 0, "")
	add("uint256 Exp (256-bit base, 64-bit exponent)", 7, []*big.Int{a256, bits(64)}, 0, 0, 0, "")
	add("uint256 Exp (256-bit base, 256-bit exponent)", 7, []*big.Int{a256, b256}, 0, 0, 0, "")
	add("uint256 Sqrt (256-bit)", 8, []*big.Int{a256}, 0, 0, 0, "")
	add("uint256 Sqrt (64-bit)", 8, []*big.Int{bits(64)}, 0, 0, 0, "")
	add("uint256 Log10 (200-bit)", 9, []*big.Int{bits(200)}, 0, 0, 0, "")
	add("uint256 Dec (78 digits)", 10, []*big.Int{a256}, 0, 0, 0, "")
	add("uint256 SetFromHex (32 hex digits, = one MustFromHex)", 11, nil, 0, 0, 0, "0xfffcb933bd6fad37aa2d162d1a594001")
	add("uint256 SetFromDecimal (49 digits, = MaxSqrtRatioU256)", 12, nil, 0, 0, 0, "1461446703485210103287273052203988822378723970342")
	add("int256 Mul (128-bit signed)", 13, []*big.Int{bits(127), new(big.Int).Neg(bits(120))}, 0, 0, 0, "")
	add("int256 Quo (256 / 128-bit signed)", 14, []*big.Int{new(big.Int).Neg(bits(250)), bits(128)}, 0, 0, 0, "")
	add("int256 SetFromDec (37 digits, = one MustFromDec)", 15, nil, 0, 0, 0, "3402992956809132418596140100660247210")
	add("GetSqrtRatioAtTickV2 (tick 1)", 16, nil, 1, 0, 0, "")
	add("GetSqrtRatioAtTickV2 (tick 500000)", 16, nil, 500000, 0, 0, "")
	add("GetSqrtRatioAtTickV2 (tick -887272, MinTick)", 16, nil, uint64(1<<64-887272), 0, 0, "")
	add("GetTickAtSqrtRatioV2 (price 1)", 17, []*big.Int{price}, 0, 0, 0, "")
	add("GetTickAtSqrtRatioV2 (tick ~ 500000)", 17, []*big.Int{sqrtAtTick(500000)}, 0, 0, 0, "")
	add("GetTickAtSqrtRatioV2 (MaxSqrtRatio - 1)", 17, []*big.Int{new(big.Int).Sub(maxSqrtRatio, big.NewInt(1))}, 0, 0, 0, "")
	add("utils.MulDivV2 (512-bit product)", 18, []*big.Int{new(big.Int).Lsh(liq, 96), bits(100), bits(160)}, 0, 0, 0, "")
	add("GetAmount0DeltaV2 (round up)", 19, []*big.Int{sqrtAtTick(-1000), price, liq}, 0, 0, 0, "")
	add("GetNextSqrtPriceFromInput (zeroForOne)", 20, []*big.Int{price, dec("1000000000000000000000"), liq}, 0, 0, 2, "")
	add("ComputeSwapStep (exact in, within the range)", 21, []*big.Int{price, sqrtAtTick(-1000), liq, dec("1000000000000000000")}, 0, 3000, 0, "")
	add("ComputeSwapStep (exact in, reaches the target)", 21, []*big.Int{price, sqrtAtTick(-10), liq, dec("1000000000000000000000000")}, 0, 3000, 0, "")
	add("ComputeSwapStep (exact out)", 21, []*big.Int{price, sqrtAtTick(1000), liq, new(big.Int).Neg(dec("1000000000000000000"))}, 0, 500, 0, "")
	add("uint128 MulWrap (128-bit)", 22, []*big.Int{bits(128), bits(128)}, 0, 0, 0, "")
	add("uint128 QuoRem (128 / 100-bit)", 23, []*big.Int{bits(128), bits(100)}, 0, 0, 0, "")
	add("uint128 QuoRem (128 / 64-bit)", 23, []*big.Int{bits(128), bits(64)}, 0, 0, 0, "")
	return benches
}

// realLibsInitProbes are programs that import one library's packages and
// call nothing on the measured path: their idle invocation costs the
// entrypoint plus the packages' initialization (init_cu: above "none").
// The replica probes declare, in their own package, initializers copied
// verbatim from uniswapv3-sdk-uint256 v0.5.5 (utils/tick_math.go,
// int_types.go, sqrtprice_math.go, constants/constants.go): their cost
// above their base probe (own_cu) breaks the utils initialization down.
var realLibsInitProbes = []struct{ name, imports, decls, use, base string }{
	{"none", ``, ``, `0`, ""},
	{"uint256", `"github.com/holiman/uint256"`, ``, `uint256.NewInt(uint64(in[1])).Uint64()`, "none"},
	{"int256", `"github.com/KyberNetwork/int256"`, ``, `int256.NewInt(int64(in[1])).Uint64()`, "none"},
	{"uint128", `"lukechampine.com/uint128"`, ``, `uint128.From64(uint64(in[1])).Lo`, "none"},
	{"constants (and uint256)", `"github.com/KyberNetwork/uniswapv3-sdk-uint256/constants"`, ``, `uint64(constants.FeeLow) + uint64(in[1])`, "uint256"},
	{"utils (all four packages)", `"github.com/KyberNetwork/uniswapv3-sdk-uint256/utils"`, ``, `uint64(utils.MaxTick) + uint64(in[1])`, "none"},
	// Small applications: their ELF sizes (the call is linked, not made).
	{"app: ComputeSwapStep only", `"github.com/KyberNetwork/int256"
	"github.com/KyberNetwork/uniswapv3-sdk-uint256/utils"`, `
func swap(in []byte) uint64 {
	var current, target, liquidity, next, amountIn, amountOut, fee utils.Uint256
	current.SetBytes(in[1:21])
	target.SetBytes(in[21:41])
	liquidity.SetBytes(in[41:57])
	remaining := int256.NewInt(int64(in[57]))
	if utils.ComputeSwapStep(&current, &target, &liquidity, remaining, 3000, &next, &amountIn, &amountOut, &fee) != nil {
		return 0
	}
	return next[0] + amountIn[0] + amountOut[0] + fee[0]
}
`, `swap(in)`, "none"},
	{"app: tick math only", `"github.com/KyberNetwork/uniswapv3-sdk-uint256/utils"`, `
func ticks(in []byte) uint64 {
	var price utils.Uint160
	if utils.GetSqrtRatioAtTickV2(int(int8(in[1]))*1000, &price) != nil {
		return 0
	}
	tick, _ := utils.GetTickAtSqrtRatioV2(&price)
	return uint64(tick)
}
`, `ticks(in)`, "none"},
	{"replica: tick_math.go sqrtConst1-21 and MaxUint256 (22 MustFromHex)", `"github.com/holiman/uint256"`, `
var (
	sqrtConst1  = uint256.MustFromHex("0xfffcb933bd6fad37aa2d162d1a594001")
	sqrtConst2  = uint256.MustFromHex("0x100000000000000000000000000000000")
	sqrtConst3  = uint256.MustFromHex("0xfff97272373d413259a46990580e213a")
	sqrtConst4  = uint256.MustFromHex("0xfff2e50f5f656932ef12357cf3c7fdcc")
	sqrtConst5  = uint256.MustFromHex("0xffe5caca7e10e4e61c3624eaa0941cd0")
	sqrtConst6  = uint256.MustFromHex("0xffcb9843d60f6159c9db58835c926644")
	sqrtConst7  = uint256.MustFromHex("0xff973b41fa98c081472e6896dfb254c0")
	sqrtConst8  = uint256.MustFromHex("0xff2ea16466c96a3843ec78b326b52861")
	sqrtConst9  = uint256.MustFromHex("0xfe5dee046a99a2a811c461f1969c3053")
	sqrtConst10 = uint256.MustFromHex("0xfcbe86c7900a88aedcffc83b479aa3a4")
	sqrtConst11 = uint256.MustFromHex("0xf987a7253ac413176f2b074cf7815e54")
	sqrtConst12 = uint256.MustFromHex("0xf3392b0822b70005940c7a398e4b70f3")
	sqrtConst13 = uint256.MustFromHex("0xe7159475a2c29b7443b29c7fa6e889d9")
	sqrtConst14 = uint256.MustFromHex("0xd097f3bdfd2022b8845ad8f792aa5825")
	sqrtConst15 = uint256.MustFromHex("0xa9f746462d870fdf8a65dc1f90e061e5")
	sqrtConst16 = uint256.MustFromHex("0x70d869a156d2a1b890bb3df62baf32f7")
	sqrtConst17 = uint256.MustFromHex("0x31be135f97d08fd981231505542fcfa6")
	sqrtConst18 = uint256.MustFromHex("0x9aa508b5b7a84e1c677de54f3e99bc9")
	sqrtConst19 = uint256.MustFromHex("0x5d6af8dedb81196699c329225ee604")
	sqrtConst20 = uint256.MustFromHex("0x2216e584f5fa1ea926041bedfe98")
	sqrtConst21 = uint256.MustFromHex("0x48a170391f7dc42444e8fa2")

	MaxUint256 = uint256.MustFromHex("0xffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")
)
`, `sqrtConst1[0] + uint64(in[1])`, "uint256"},
	{"replica: Uint128Max, Uint160Max, MaxUint160 (3 MustFromHex)", `"github.com/holiman/uint256"`, `
var (
	Uint128Max = uint256.MustFromHex("0xffffffffffffffffffffffffffffffff")
	Uint160Max = uint256.MustFromHex("0xffffffffffffffffffffffffffffffffffffffff")
)

var MaxUint160 = uint256.MustFromHex("0xffffffffffffffffffffffffffffffffffffffff")
`, `Uint128Max[0] + uint64(in[1])`, "uint256"},
	{"replica: MaxSqrtRatioU256 (MustFromDecimal, 49 digits)", `"github.com/holiman/uint256"`, `
var MaxSqrtRatioU256 = uint256.MustFromDecimal("1461446703485210103287273052203988822378723970342")
`, `MaxSqrtRatioU256[0] + uint64(in[1])`, "uint256"},
	{"replica: magicSqrt10001, magicTickLow, magicTickHigh (3 int256.MustFromDec)", `"github.com/KyberNetwork/int256"`, `
var (
	magicSqrt10001 = int256.MustFromDec("255738958999603826347141")
	magicTickLow   = int256.MustFromDec("3402992956809132418596140100660247210")
	magicTickHigh  = int256.MustFromDec("291339464771989622907027621153398088495")
)
`, `magicSqrt10001[0] + uint64(in[1])`, "int256"},
	{"replica: constants.TickSpacings (a 7-entry map)", ``, `
type FeeAmount uint64

const (
	FeeLowest FeeAmount = 100
	FeeLow    FeeAmount = 500
	FeeMedium FeeAmount = 3000
	FeeHigh   FeeAmount = 10000

	// These FeeTiers are used in other dexes such as BaseSwapV3, ArbiDexV3.
	Fee80   FeeAmount = 80
	Fee450  FeeAmount = 450
	Fee2500 FeeAmount = 2500

	FeeMax FeeAmount = 1000000
)

// The default factory tick spacings by fee amount.
var TickSpacings = map[FeeAmount]int{
	FeeLowest: 1,
	Fee80:     1,
	FeeLow:    10,
	Fee450:    10,
	FeeMedium: 60,
	Fee2500:   60,
	FeeHigh:   200,
}
`, `uint64(TickSpacings[FeeLow]) + uint64(in[1])`, "none"},
	{"replica: constants.Q96U256 (uint256 Exp(2, 96))", `"github.com/holiman/uint256"`, `
var Q96U256 = new(uint256.Int).Exp(uint256.NewInt(2), uint256.NewInt(96))
`, `Q96U256[1] + uint64(in[1])`, "uint256"},
}

func realLibsInitSource(imports, decls, use string) string {
	source := "package probe\n\nimport (\n\t\"gosvm/solana\"\n"
	if imports != "" {
		source += "\t" + imports + "\n"
	}
	return source + ")\n" + decls + `
func Process(c solana.Context) uint64 {
	in := solana.Instruction(c)
	if in[0] == 1 {
		out := solana.Data(c, 0)
		out[0] = byte(` + use + `)
	}
	return 0
}
`
}

// TestRealLibrariesSBF builds examples/real-libraries for fogo-v0 and
// solana-v3, against the unmodified modules with trimming opted in and
// against the trimmed module copies, and runs the vectors through the pinned
// runners: every output must match the unmodified libraries run natively,
// and every native panic must abort and roll back (with its log line). It
// also measures the bench operations' CU, each library's per-invocation
// initialization and the ELF sizes of both forms.
// GOSVM_REALLIBS_RESULTS=<dir> keeps fixtures, ELFs, reports and a summary
// per target and form.
func TestRealLibrariesSBF(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("SBF runs are skipped in -short mode")
	}
	llvm := modernLLVM()
	if llvm == "" {
		t.Skip("pinned platform-tools v1.57 backend required")
	}
	env := realLibs(t)
	vectors := realLibsSBFVectors()
	benches := realLibsBenches()
	for _, b := range benches {
		vectors = append(vectors, b.in)
	}
	idle := make([]byte, 256)
	idle[0] = 0xff
	vectors = append(vectors, idle)
	want := nativeRealLibs(t, env.oracle, vectors)
	var cases []allocCase
	aborts, panics := 0, 0
	benchCase := map[string]string{}
	for j, in := range vectors {
		name := fmt.Sprintf("v%04d-g%d-op%d", j, in[0], in[1])
		if j >= len(vectors)-len(benches)-1 && j < len(vectors)-1 {
			b := benches[j-(len(vectors)-len(benches)-1)]
			name = fmt.Sprintf("bench-%02d", j-(len(vectors)-len(benches)-1))
			benchCase[b.name] = name
		}
		if j == len(vectors)-1 {
			name = "idle"
		}
		c := allocCase{name: name, in: in, units: globalUnits, err: abortError, log: abortLog}
		result := want[j]
		if line, ok := strings.CutPrefix(result, "L:"); ok {
			lineHex, _, _ := strings.Cut(line, " ")
			text, err := hex.DecodeString(lineHex)
			if err != nil {
				t.Fatal(err)
			}
			c.log = "Program log: " + string(text)
			aborts++
			panics++
		} else if strings.HasPrefix(result, "P:") {
			aborts++
			c.noPanicLog = true
		} else {
			_, hexOut, _ := strings.Cut(strings.TrimSpace(result), ":")
			out, err := hex.DecodeString(hexOut)
			if err != nil || len(out) != 1024 {
				t.Fatalf("vector %d: native output %q: %v", j, result, err)
			}
			c.output = out
		}
		if strings.HasPrefix(name, "bench-") && c.output == nil {
			t.Fatalf("bench %s fails natively: %q", name, result)
		}
		cases = append(cases, c)
	}
	keep := os.Getenv("GOSVM_REALLIBS_RESULTS")
	for _, profile := range []string{target.Fogo, target.Solana} {
		for _, v := range env.variants() {
			t.Run(profile+"/"+v.name, func(t *testing.T) {
				realLibsSBF(t, env, v, profile, llvm, keep, cases, benches, benchCase, aborts, panics)
			})
		}
	}
}

func realLibsSBF(t *testing.T, env *realLibsEnv, v realLibsVariant, profile, llvm, keep string, cases []allocCase, benches []realLibsBench, benchCase map[string]string, aborts, panics int) {
	program := realLibsSBFProgram(t, v.program)
	if keep != "" {
		keep = filepath.Join(keep, v.name)
	}
	runner, err := testvm.RunnerFor("", profile)
	if err != nil {
		t.Skipf("runner unavailable: %v (install the pinned runners; see docs/DEVELOPMENT.md)", err)
	}
	// A fixture suite holds at most 1,024 cases: run the same ELF
	// (built identically each time) in parts.
	result := map[string]any{}
	for part, from := 0, 0; from < len(cases); part, from = part+1, from+1000 {
		to := min(from+1000, len(cases))
		r := runGlobalProgram(t, llvm, runner, profile, fmt.Sprintf("real-libraries-%d", part), program, cases[from:to], keep)
		if result["elf_sha256"] != nil && r["elf_sha256"] != result["elf_sha256"] {
			t.Fatalf("part %d built a different ELF", part)
		}
		for k, v := range r {
			result[k] = v
		}
	}
	var cus []uint64
	for name, v := range result {
		if strings.HasPrefix(name, "v") {
			cus = append(cus, v.(uint64))
		}
	}
	sort.Slice(cus, func(i, j int) bool { return cus[i] < cus[j] })
	none := result[benchCase[benches[0].name]].(uint64)
	table := []map[string]any{}
	for _, b := range benches {
		cu := result[benchCase[b.name]].(uint64)
		table = append(table, map[string]any{"operation": b.name, "cu": cu, "call_cu": int64(cu) - int64(none)})
	}
	// Initialization: one program per library or replica.
	inits := []map[string]any{}
	initCU := map[string]uint64{}
	for j, probe := range realLibsInitProbes {
		dir := filepath.Join(v.dir, "probes", fmt.Sprintf("p%02d", j))
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "program.go"), []byte(realLibsInitSource(probe.imports, probe.decls, probe.use)), 0644); err != nil {
			t.Fatal(err)
		}
		p, err := ReadProgramWithOptions(dir, ReadOptions{Trim: v.trim})
		if err != nil {
			t.Fatal(err)
		}
		idleCase := allocCase{name: "idle", in: make([]byte, 8), output: make([]byte, 1024)}
		r := runGlobalProgram(t, llvm, runner, profile, fmt.Sprintf("init-%02d", j), p, []allocCase{idleCase}, keep)
		cu := r["idle"].(uint64)
		initCU[probe.name] = cu
		entry := map[string]any{"probe": probe.name, "cu": cu, "init_cu": int64(cu) - int64(initCU["none"]),
			"elf_bytes": r["elf_bytes"], "elf_sha256": r["elf_sha256"]}
		if probe.base != "" {
			entry["base"] = probe.base
			entry["own_cu"] = int64(cu) - int64(initCU[probe.base])
		}
		inits = append(inits, entry)
	}
	summary := map[string]any{"profile": profile, "form": v.name, "elf_sha256": result["elf_sha256"], "elf_bytes": result["elf_bytes"],
		"cases": len(cases), "aborts": aborts, "panic_lines": panics,
		"idle_cu": result["idle"], "median_vector_cu": cus[len(cus)/2], "max_vector_cu": cus[len(cus)-1],
		"bench": table, "init": inits}
	encoded, _ := json.MarshalIndent(summary, "", "  ")
	t.Logf("%s (%s): %d cases (%d aborts with rollback, %d panic lines) match native Go\n%s", profile, v.name, len(cases), aborts, panics, encoded)
	if keep != "" {
		if err = os.WriteFile(filepath.Join(keep, profile, "summary.json"), append(encoded, '\n'), 0644); err != nil {
			t.Fatal(err)
		}
	}
}
