package compiler

import (
	"bytes"
	"fmt"
	"math/big"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// Real third-party libraries (examples/real-libraries,
// results/compiler/2026-10-16-real-libraries): holiman/uint256 v1.3.1,
// KyberNetwork/int256 v0.1.4, the KyberNetwork/uniswapv3-sdk-uint256 v0.5.5
// math and lukechampine.com/uint128 v1.3.0, consumed as module
// dependencies. Natively the example runs against the unmodified modules;
// goSVM compiles it against the same unmodified modules with trimming
// opted in for them (docs/GO_PACKAGES.md#trimming), and, for the
// historical results, against trimmed copies (trim/trim.json: upstream
// minus the declarations goSVM rejects, every kept declaration
// byte-identical) resolved through ordinary go.mod replacements. All need
// the modules in the Go module cache; the tests skip, saying so, when they
// are missing.

const realLibsExample = "../../examples/real-libraries"

// realLibsEnv is the shared setup: the native oracle and the program in its
// two forms. The main form is the example itself, compiled against the
// unmodified modules from the module cache with trimming opted in for the
// four library modules (realLibsTrim, docs/GO_PACKAGES.md#trimming). The
// historical form (results/compiler/2026-10-16-real-libraries) compiles it
// against the trimmed module copies of examples/real-libraries/trim.
type realLibsEnv struct {
	dir     string // scratch directory holding everything below
	oracle  string // native oracle binary
	onchain string // module directory goSVM compiles against the trimmed copies
	program *Program
	// upstream is the example module with trimming opted in; upstreamDir
	// is a scratch module with its go.mod and go.sum (for probes).
	upstream    *Program
	upstreamDir string
	skip        string // why the prerequisites are missing
	err         error  // resolving the on-chain module failed
}

// realLibsTrim are the trim patterns of the example (its gosvm.json
// "trim"): the four library modules, never the example's own package.
var realLibsTrim = []string{"github.com/holiman/uint256", "github.com/KyberNetwork/int256", "github.com/KyberNetwork/uniswapv3-sdk-uint256/...", "lukechampine.com/uint128"}

// realLibsVariant is one form of the program.
type realLibsVariant struct {
	name    string
	program *Program
	dir     string // module directory for probes
	trim    []string
}

// variants are the two forms of the program, the main one first.
func (env *realLibsEnv) variants() []realLibsVariant {
	return []realLibsVariant{{"opt-in", env.upstream, env.upstreamDir, realLibsTrim}, {"trimmed-copies", env.program, env.onchain, nil}}
}

var (
	realLibsOnce  sync.Once
	realLibsSetup realLibsEnv
)

// goCommand runs go with the module settings of an offline user build.
func goCommand(dir string, args ...string) *exec.Cmd {
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOPROXY=off", "GOFLAGS=", "GOTOOLCHAIN=local")
	return cmd
}

func realLibs(t *testing.T) *realLibsEnv {
	t.Helper()
	realLibsOnce.Do(func() {
		env := &realLibsSetup
		dir, err := os.MkdirTemp("", "gosvm-real-libraries-")
		if err != nil {
			env.skip = err.Error()
			return
		}
		env.dir = dir
		example, err := filepath.Abs(realLibsExample)
		if err != nil {
			env.skip = err.Error()
			return
		}
		// The module graph must be complete in the module cache.
		if out, err := goCommand(example, "list", "-deps", ".", "./oracle").CombinedOutput(); err != nil {
			env.skip = fmt.Sprintf("the library modules are not in the Go module cache; run `go mod download` in examples/real-libraries (%v: %s)", err, bytes.TrimSpace(out))
			return
		}
		trimmed := filepath.Join(dir, "trimmed")
		if out, err := goCommand(example, "run", "./trim", "-out", trimmed).CombinedOutput(); err != nil {
			env.skip = fmt.Sprintf("trimming the library modules failed: %v: %s", err, out)
			return
		}
		env.oracle = filepath.Join(dir, "oracle")
		args := []string{"build", "-o", env.oracle}
		if runtime.GOOS == "darwin" {
			args = append(args, "-ldflags=-linkmode=external")
		}
		if out, err := goCommand(example, append(args, "./oracle")...).CombinedOutput(); err != nil {
			env.skip = fmt.Sprintf("building the native oracle failed: %v: %s", err, out)
			return
		}
		if runtime.GOOS == "darwin" {
			if out, err := exec.Command("codesign", "--force", "--sign", "-", env.oracle).CombinedOutput(); err != nil {
				env.skip = fmt.Sprintf("codesign oracle: %v: %s", err, out)
				return
			}
		}
		// The on-chain module: the example's package and go.mod/go.sum,
		// with each library replaced by its trimmed copy.
		env.onchain = filepath.Join(dir, "onchain")
		if err := os.MkdirAll(env.onchain, 0755); err != nil {
			env.skip = err.Error()
			return
		}
		entries, err := os.ReadDir(example)
		if err != nil {
			env.skip = err.Error()
			return
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !(strings.HasSuffix(name, ".go") || name == "go.mod" || name == "go.sum") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(example, name))
			if err != nil {
				env.skip = err.Error()
				return
			}
			if name == "go.mod" {
				for _, r := range [][2]string{{"github.com/holiman/uint256", "uint256"}, {"github.com/KyberNetwork/int256", "int256"},
					{"lukechampine.com/uint128", "uint128"}, {"github.com/KyberNetwork/uniswapv3-sdk-uint256", "uniswapv3"}} {
					data = append(data, fmt.Sprintf("\nreplace %s => %s\n", r[0], filepath.Join(trimmed, r[1]))...)
				}
			}
			if err := os.WriteFile(filepath.Join(env.onchain, name), data, 0644); err != nil {
				env.skip = err.Error()
				return
			}
		}
		// A resolution failure here is a goSVM failure, not a missing
		// prerequisite: the tests fail with it.
		env.program, env.err = ReadProgram(env.onchain)
		if env.err != nil {
			return
		}
		env.upstream, env.err = ReadProgramWithOptions(example, ReadOptions{Trim: realLibsTrim})
		if env.err != nil {
			return
		}
		env.upstreamDir = filepath.Join(dir, "upstream")
		if err := os.MkdirAll(env.upstreamDir, 0755); err != nil {
			env.skip = err.Error()
			return
		}
		for _, name := range []string{"go.mod", "go.sum"} {
			data, err := os.ReadFile(filepath.Join(example, name))
			if err == nil {
				err = os.WriteFile(filepath.Join(env.upstreamDir, name), data, 0644)
			}
			if err != nil {
				env.skip = err.Error()
				return
			}
		}
	})
	env := &realLibsSetup
	if env.skip != "" {
		t.Skip(env.skip)
	}
	if env.err != nil {
		t.Fatal(env.err)
	}
	return env
}

// realLibsDriver is valueTypesDriver for 256-byte vectors.
const realLibsDriver = `
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
static u8 driver_out[1024];
static void print_out(void) {for (u64 j=0;j<1024;j++) printf("%02x",driver_out[j]); puts("");}
void sol_log_(const u8 *p,u64 n) {printf("L:");for (u64 j=0;j<n;j++) printf("%02x",p[j]);printf(" ");}
void abort(void) {printf("P:"); print_out(); fflush(stdout); _Exit(0);}
static u8 nibble(char c){return c<='9'?c-'0':c-'a'+10;}
int main(void){char ih[513];u8 in[256];
while(scanf("%512s",ih)==1){
 for(u64 j=0;j<256;j++)in[j]=(nibble(ih[j*2])<<4)|nibble(ih[j*2+1]);
 memset(driver_out,0,sizeof driver_out);
 memset(gosvm_host_heap,0,sizeof gosvm_host_heap);
 printf("%llu:",go_Process(GOSVM_SLICE(driver_out,1024),GOSVM_SLICE(in,256)));print_out();
}return 0;}`

// realLibsGroupOps is the number of operations of each group of Run, in
// examples/real-libraries (U256ArithOps, U256UnaryOps, U256TextOps, I256Ops,
// TickOps, MathOps, U128Ops, BenchOps).
var realLibsGroupOps = [8]int{32, 18, 14, 26, 6, 12, 20, 24}

// Uniswap v3 constants for the operand generators.
var (
	minSqrtRatio    = big.NewInt(4295128739)
	maxSqrtRatio, _ = new(big.Int).SetString("1461446703485210103287273052203988822378723970342", 10)
	two256          = new(big.Int).Lsh(big.NewInt(1), 256)
)

const (
	minTick = -887272
	maxTick = 887272
)

// putWord stores x mod 2^256 as four little-endian words at in[o:].
func putWord(in []byte, o int, x *big.Int) {
	v := new(big.Int).Mod(x, two256)
	b := v.FillBytes(make([]byte, 32)) // big-endian
	for j := 0; j < 32; j++ {
		in[o+j] = b[31-j]
	}
}

// edgeValue is one of the 256-bit edge cases: 0, 1, small values, the
// maximum and values around powers of two at word and Uniswap boundaries,
// the Uniswap price limits, and negative (two's complement) small values.
func edgeValue(rng *rand.Rand) *big.Int {
	one := big.NewInt(1)
	switch rng.Intn(6) {
	case 0:
		return big.NewInt(int64(rng.Intn(5)))
	case 1:
		k := []uint{1, 31, 32, 63, 64, 65, 95, 96, 97, 127, 128, 129, 159, 160, 161, 191, 192, 193, 254, 255, 256}[rng.Intn(21)]
		x := new(big.Int).Lsh(one, k)
		return x.Add(x, big.NewInt(int64(rng.Intn(3)-1)))
	case 2:
		return new(big.Int).Sub(two256, big.NewInt(int64(1+rng.Intn(3)))) // max, max-1, max-2
	case 3:
		x := []*big.Int{minSqrtRatio, maxSqrtRatio}[rng.Intn(2)]
		return new(big.Int).Add(x, big.NewInt(int64(rng.Intn(5)-2)))
	case 4:
		return new(big.Int).Sub(two256, big.NewInt(int64(1+rng.Intn(1000)))) // small negatives
	}
	// A power of two times a small odd value.
	x := new(big.Int).Lsh(big.NewInt(int64(2*rng.Intn(50)+1)), uint(rng.Intn(250)))
	return x
}

// randomValue is a random value of random bit length, sometimes sparse.
func randomValue(rng *rand.Rand) *big.Int {
	bitsLen := 1 + rng.Intn(256)
	switch rng.Intn(4) {
	case 0:
		bitsLen = []int{64, 128, 160, 192, 256}[rng.Intn(5)]
	case 1:
		x := new(big.Int)
		for j := 0; j < 1+rng.Intn(4); j++ {
			x.SetBit(x, rng.Intn(256), 1)
		}
		return x
	}
	b := make([]byte, (bitsLen+7)/8)
	rng.Read(b)
	x := new(big.Int).SetBytes(b)
	return x.Rsh(x, uint(len(b)*8-bitsLen))
}

func operand(rng *rand.Rand) *big.Int {
	if rng.Intn(5) < 2 {
		return edgeValue(rng)
	}
	return randomValue(rng)
}

// sqrtPrice is a price mostly inside [MinSqrtRatio, MaxSqrtRatio).
func sqrtPrice(rng *rand.Rand) *big.Int {
	switch rng.Intn(9) {
	case 0:
		return edgeValue(rng)
	case 8:
		return big.NewInt(int64(rng.Intn(2))) // a zero (or one) price: division by zero
	case 1:
		x := []*big.Int{minSqrtRatio, maxSqrtRatio}[rng.Intn(2)]
		return new(big.Int).Add(x, big.NewInt(int64(rng.Intn(7)-3)))
	case 2:
		return new(big.Int).Lsh(big.NewInt(1), 96) // price 1
	}
	span := new(big.Int).Sub(maxSqrtRatio, minSqrtRatio)
	bitsLen := 33 + rng.Intn(span.BitLen()-32)
	x := new(big.Int).Rand(rng, new(big.Int).Lsh(big.NewInt(1), uint(bitsLen)))
	x.Add(x, minSqrtRatio)
	if x.Cmp(maxSqrtRatio) >= 0 {
		x.Sub(maxSqrtRatio, big.NewInt(1))
	}
	return x
}

// liquidity is a uint128 liquidity, sometimes 0 or beyond 128 bits.
func liquidity(rng *rand.Rand) *big.Int {
	switch rng.Intn(10) {
	case 0:
		return big.NewInt(int64(rng.Intn(2)))
	case 1:
		return new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 128), big.NewInt(int64(rng.Intn(2))))
	case 2:
		return operand(rng)
	}
	return new(big.Int).Rand(rng, new(big.Int).Lsh(big.NewInt(1), uint(1+rng.Intn(128))))
}

// amount is a signed amount: exact input (non-negative) or output.
func amount(rng *rand.Rand) *big.Int {
	x := new(big.Int).Rand(rng, new(big.Int).Lsh(big.NewInt(1), uint(1+rng.Intn(200))))
	if rng.Intn(8) == 0 {
		x = edgeValue(rng)
	}
	if rng.Intn(2) == 0 {
		x.Neg(x)
	}
	return x
}

// textValue is hex, decimal, JSON or scientific text, valid or not.
func textValue(rng *rand.Rand, n *big.Int) string {
	digits := func(alphabet string, k int) string {
		var b strings.Builder
		for j := 0; j < k; j++ {
			b.WriteByte(alphabet[rng.Intn(len(alphabet))])
		}
		return b.String()
	}
	var s string
	switch rng.Intn(14) {
	case 0:
		s = "0x" + n.Text(16)
	case 1:
		s = "0x" + strings.ToUpper(n.Text(16))
	case 2:
		s = n.Text(10)
	case 3:
		s = "-" + n.Text(10)
	case 4:
		s = "0x" + strings.Repeat("0", 1+rng.Intn(3)) + n.Text(16)
	case 5:
		s = strings.Repeat("0", rng.Intn(3)) + n.Text(10) + strings.Repeat("0", rng.Intn(40))
	case 6:
		s = `"` + []string{"0x" + n.Text(16), n.Text(10)}[rng.Intn(2)] + `"`
	case 7:
		s = []string{"", "0x", "0X1", "x12", "0x0", "0", "-0", "+5", "null", `""`, `"0x"`, "1e3", "12e70", "1e", "e5", "0x1g", " 1", "1 "}[rng.Intn(18)]
	case 8:
		s = "0x" + digits("0123456789abcdefABCDEFg", rng.Intn(70))
	case 9:
		s = digits("0123456789", rng.Intn(82))
	case 10:
		s = digits("0123456789", 1+rng.Intn(20)) + "e" + digits("0123456789", 1+rng.Intn(2))
	case 11:
		s = "1" + strings.Repeat("0", 76+rng.Intn(4)) // around 2^256
	case 12:
		s = []string{"115792089237316195423570985008687907853269984665640564039457584007913129639935", "115792089237316195423570985008687907853269984665640564039457584007913129639936",
			"57896044618658097711785492504343953926634992332820282019728792003956564819967", "-57896044618658097711785492504343953926634992332820282019728792003956564819968",
			"-57896044618658097711785492504343953926634992332820282019728792003956564819969"}[rng.Intn(5)]
	default:
		s = digits("0123456789abcdefx-+\" .e", rng.Intn(30))
	}
	if len(s) > 64 {
		s = s[:64]
	}
	return s
}

// realLibsVector builds one vector for group g and operation op.
func realLibsVector(rng *rand.Rand, g, op int) []byte {
	in := make([]byte, 256)
	rng.Read(in)
	in[0], in[1] = byte(g), byte(op)
	for o := 32; o < 192; o += 32 {
		putWord(in, o, operand(rng))
	}
	small := []uint64{0, 1, 2, 63, 64, 65, 127, 128, 129, 255, 256, 257, 1 << 63, 1<<64 - 1}
	for o := 8; o < 32; o += 8 {
		if rng.Intn(2) == 0 {
			v := small[rng.Intn(len(small))]
			for k := 0; k < 8; k++ {
				in[o+k] = byte(v >> (8 * k))
			}
		}
	}
	switch g {
	case 0:
		if rng.Intn(4) == 0 {
			copy(in[64:96], in[32:64]) // b == a
		}
	case 2, 3:
		s := textValue(rng, operand(rng))
		if g == 3 && rng.Intn(3) == 0 {
			s = textValue(rng, new(big.Int).Rsh(operand(rng), uint(rng.Intn(256))))
		}
		in[2] = byte(len(s) + 65*rng.Intn(3))
		copy(in[192:256], s)
	case 4:
		putWord(in, 32, sqrtPrice(rng))
	case 5:
		putWord(in, 32, sqrtPrice(rng))
		putWord(in, 64, sqrtPrice(rng))
		if op == 0 || op == 1 || op == 2 || op == 3 || op == 4 {
			// MulDiv: products around 2^256 and divisors of every size.
			putWord(in, 32, operand(rng))
			putWord(in, 64, operand(rng))
			putWord(in, 96, operand(rng))
		} else {
			putWord(in, 96, liquidity(rng))
		}
		if op == 7 || op == 8 {
			putWord(in, 64, amount(rng)) // amountIn / amountOut (b)
			if rng.Intn(2) == 0 {
				putWord(in, 64, new(big.Int).Abs(amount(rng)))
			}
		}
		if op == 9 {
			putWord(in, 32, liquidity(rng))
			putWord(in, 64, amount(rng))
		}
		if op == 10 || op == 11 {
			putWord(in, 128, amount(rng))
			if rng.Intn(10) == 0 {
				copy(in[64:96], in[32:64]) // target == current
			}
		}
	case 6:
		for o := 32; o < 96; o += 32 {
			if rng.Intn(3) == 0 {
				// Small values: products and sums that do not overflow.
				putWord(in, o, new(big.Int).Rand(rng, new(big.Int).Lsh(big.NewInt(1), uint(1+rng.Intn(70)))))
			}
		}
	}
	return in
}

// realLibsTickSweep covers the tick range: every tick within 40 of each
// end and of zero, and 2,000 ticks spread evenly across [MinTick, MaxTick].
func realLibsTickSweep() [][]byte {
	var ticks []int64
	for d := int64(-40); d <= 40; d++ {
		ticks = append(ticks, minTick+40+d, maxTick-40+d, d)
	}
	ticks = append(ticks, minTick-1, maxTick+1, -1<<63, 1<<63-1)
	for j := int64(0); j < 2000; j++ {
		ticks = append(ticks, minTick+j*(maxTick-minTick)/1999)
	}
	var vectors [][]byte
	for j, tk := range ticks {
		for _, op := range []byte{0, 2} {
			in := make([]byte, 256)
			in[0], in[1] = 4, op
			// tick() with n%8 == 0 uses the value itself; ticks are
			// multiples of 8 only by chance, so use the full-range branch
			// encoding: n%8 in 4..7 maps n>>3 into [MinTick, MaxTick].
			n := uint64(tk-minTick)<<3 | 4
			switch {
			case tk >= minTick-4 && tk < minTick:
				n = uint64(tk-(minTick-4))<<3 | 1
			case tk > maxTick && tk <= maxTick+4:
				n = uint64(tk-(maxTick-4))<<3 | 2
			case tk < minTick || tk > maxTick:
				n = uint64(tk) &^ 7 // the raw tick (a multiple of 8, far out of range)
			}
			for k := 0; k < 8; k++ {
				in[8+k] = byte(n >> (8 * k))
			}
			in[16] = byte(j)
			vectors = append(vectors, in)
		}
	}
	return vectors
}

// realLibsVectors covers every operation of every group perOp times, and
// the Uniswap operations (groups 4 and 5) four times as often.
func realLibsVectors(seed int64, perOp int) [][]byte {
	rng := rand.New(rand.NewSource(seed))
	var vectors [][]byte
	for g, ops := range realLibsGroupOps {
		n := perOp
		if g == 4 || g == 5 {
			n *= 4
		}
		for op := 0; op < ops; op++ {
			for j := 0; j < n; j++ {
				vectors = append(vectors, realLibsVector(rng, g, op))
			}
		}
	}
	return vectors
}

// realLibsSBFVectors are the vectors the SBF tests run.
func realLibsSBFVectors() [][]byte {
	v := realLibsVectors(20261017, 5)
	sweep := realLibsTickSweep()
	for j := 0; j < len(sweep); j += 37 {
		v = append(v, sweep[j])
	}
	return v
}

// nativeRealLibs runs vectors through the batch oracle.
func nativeRealLibs(t *testing.T, oracle string, vectors [][]byte) []string {
	t.Helper()
	var input strings.Builder
	for _, in := range vectors {
		fmt.Fprintf(&input, "%x\n", in)
	}
	cmd := exec.Command(oracle)
	cmd.Stdin = strings.NewReader(input.String())
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("oracle: %v", err)
	}
	lines := strings.SplitAfter(string(out), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) != len(vectors) {
		t.Fatalf("oracle printed %d lines for %d vectors", len(lines), len(vectors))
	}
	return lines
}

// TestRealLibrariesDifferential compiles examples/real-libraries for the host,
// against the unmodified modules with trimming opted in and against the
// trimmed module copies, and compares every vector's output, result and
// panic with the unmodified modules run natively.
func TestRealLibrariesDifferential(t *testing.T) {
	env := realLibs(t)
	vectors := append(realLibsVectors(20261016, 80), realLibsTickSweep()...)
	vectors = append(vectors, realLibsSBFVectors()...)
	want := nativeRealLibs(t, env.oracle, vectors)
	for _, v := range env.variants() {
		t.Run(v.name, func(t *testing.T) { realLibsDifferential(t, env, v.program, vectors, want) })
	}
}

func realLibsDifferential(t *testing.T, env *realLibsEnv, program *Program, vectors [][]byte, want []string) {
	c, err := CompileProgram(program)
	if err != nil {
		t.Fatal(err)
	}
	bin := hostCompile(t, c, DefaultHeapBytes, realLibsDriver, false)
	panics, runs := compareHostVectors(t, bin, vectors, want)
	total := 0
	for _, n := range panics {
		total += n
	}
	for g, ops := range realLibsGroupOps {
		for op := 0; op < ops; op++ {
			if runs[[2]int{g, op}] == 0 {
				t.Errorf("group %d op %d never ran", g, op)
			}
		}
	}
	// Panics are exercised: uint256's Must* parsers, int256's division by
	// zero and uint128's checked arithmetic.
	for _, g := range []int{2, 3, 6} {
		if panics[g] == 0 {
			t.Errorf("group %d: no vector panics", g)
		}
	}
	// The Uniswap operations both succeed and fail (GetAmount0DeltaV2
	// cannot fail: its quotient is below its numerator): the offset of each
	// one's error record (0xee for nil) in the output.
	outcomes := map[string][2]int{}
	for _, c := range []struct {
		name      string
		group, op int
		offset    int
	}{
		{"GetSqrtRatioAtTickV2", 4, 0, 40}, {"GetTickAtSqrtRatioV2", 4, 1, 8}, {"MostSignificantBit", 4, 3, 8},
		{"MulDivV2", 5, 0, 64}, {"MulDivRoundingUpV2", 5, 2, 32}, {"GetAmount1DeltaV2", 5, 6, 32},
		{"GetNextSqrtPriceFromInput", 5, 7, 32}, {"GetNextSqrtPriceFromOutput", 5, 8, 32}, {"AddDeltaInPlace", 5, 9, 32},
		{"ComputeSwapStep", 5, 10, 128}, {"ComputeSwapStep (pool fees)", 5, 11, 128},
	} {
		var ok, failed int
		for j, in := range vectors {
			if int(in[0]) != c.group || int(in[1]) != c.op {
				continue
			}
			_, hexOut, _ := strings.Cut(strings.TrimSpace(want[j]), ":")
			if len(hexOut) < 2*c.offset+2 {
				continue
			}
			if hexOut[2*c.offset:2*c.offset+2] == "ee" {
				ok++
			} else {
				failed++
			}
		}
		if ok == 0 || failed == 0 {
			t.Errorf("%s: %d successes, %d errors", c.name, ok, failed)
		}
		outcomes[c.name] = [2]int{ok, failed}
	}
	t.Logf("Uniswap outcomes (successes, errors): %v", outcomes)
	// One fresh oracle process per vector for a sample agrees with the
	// batch oracle (the libraries' package variables are never written).
	for j := 0; j < len(vectors); j += 97 {
		got := nativeRealLibs(t, env.oracle, vectors[j:j+1])[0]
		if got != want[j] {
			t.Fatalf("vector %d: fresh oracle %q, batch %q", j, got, want[j])
		}
	}
	t.Logf("%d vectors (%d panics: %v) match native Go", len(vectors), total, panics)
}

// realLibsWhole pins why each unmodified library package does not compile
// on chain as a whole: the first rejection goSVM reports when a program
// imports it straight from the module cache (by default every file of a
// reachable package must satisfy the subset; docs/GO_PACKAGES.md), which
// names the trimming opt-in; with it, each compiles.
var realLibsWhole = []struct{ path, use, at, want string }{
	{"github.com/holiman/uint256", "uint256.NewInt(1).Uint64()", "github.com/holiman/uint256@v1.3.1/conversion.go:8:2", stdlibImportRule("database/sql")},
	{"github.com/KyberNetwork/int256", "int256.NewInt(1).Uint64()", "github.com/!kyber!network/int256@v0.1.4/conversion.go:6:2", stdlibImportRule("math/big")},
	{"lukechampine.com/uint128", "uint128.From64(1).Lo", "lukechampine.com/uint128@v1.3.0/uint128.go:6:2", stdlibImportRule("fmt")},
	{"github.com/KyberNetwork/uniswapv3-sdk-uint256/constants", "uint64(constants.FeeLow)", "github.com/!kyber!network/uniswapv3-sdk-uint256@v0.5.5/constants/constants.go:4:2", stdlibImportRule("math/big")},
	{"github.com/KyberNetwork/uniswapv3-sdk-uint256/utils", "uint64(utils.MaxTick)", "github.com/!kyber!network/uniswapv3-sdk-uint256@v0.5.5/utils/calldata.go:4:2", stdlibImportRule("math/big")},
}

func TestRealLibrariesWholePackages(t *testing.T) {
	env := realLibs(t)
	example, err := filepath.Abs(realLibsExample)
	if err != nil {
		t.Fatal(err)
	}
	out, err := goCommand(example, "env", "GOMODCACHE").Output()
	if err != nil {
		t.Fatal(err)
	}
	modcache := strings.TrimSpace(string(out))
	for j, c := range realLibsWhole {
		dir := filepath.Join(env.dir, "whole", fmt.Sprintf("p%d", j))
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"go.mod", "go.sum"} {
			data, err := os.ReadFile(filepath.Join(example, name))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, name), data, 0644); err != nil {
				t.Fatal(err)
			}
		}
		src := fmt.Sprintf("package probe\n\nimport %q\n\nfunc Process(s, i []byte) uint64 { return %s }\n", c.path, c.use)
		if err := os.WriteFile(filepath.Join(dir, "program.go"), []byte(src), 0644); err != nil {
			t.Fatal(err)
		}
		got := ""
		p, err := ReadProgram(dir)
		if err == nil {
			_, err = Check(p)
		}
		if err != nil {
			got = err.Error()
		}
		// The rejection names the opt-in (docs/GO_PACKAGES.md#trimming).
		want := filepath.Join(modcache, filepath.FromSlash(c.at)) + ": " + c.want + fmt.Sprintf(trimHintText, c.path)
		if got != want {
			t.Errorf("%s:\ngot  %q\nwant %q", c.path, got, want)
		}
		// With the opt-in, the unmodified package compiles.
		p, err = ReadProgramWithOptions(dir, ReadOptions{Trim: realLibsTrim})
		if err == nil {
			_, err = CompileProgram(p)
		}
		if err != nil {
			t.Errorf("%s with trimming: %v", c.path, err)
		}
	}
}
