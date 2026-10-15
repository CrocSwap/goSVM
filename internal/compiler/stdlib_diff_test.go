package compiler

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	stdlibvalues "gosvm/examples/stdlib-values"
)

// Standard-library packages against native Go: examples/stdlib-values runs
// natively against Go 1.22.0's standard library and as host-compiled C
// against goSVM's overlay (32 KiB heap, as on chain by default); output
// bytes, results and panics must match for every vector. See
// docs/GO_STDLIB.md.

const stdlibExample = "../../examples/stdlib-values"

func stdlibProgram(t *testing.T) *Program {
	t.Helper()
	sources, err := ReadSources(stdlibExample)
	if err != nil {
		t.Fatal(err)
	}
	return &Program{Entry: "program", Packages: []PackageSources{{"program", sources}}}
}

func nativeStdlibCall(in []byte) (result string) {
	out := make([]byte, 1024)
	defer func() {
		if recover() != nil {
			result = fmt.Sprintf("P:%x\n", out)
		}
	}()
	code := stdlibvalues.Run(out, in)
	return fmt.Sprintf("%d:%x\n", code, out)
}

// stdlibGroupOps is the number of operations of each group of Run.
var stdlibGroupOps = [stdlibvalues.Groups]int{
	stdlibvalues.StringsOps, stdlibvalues.BytesOps, stdlibvalues.StrconvOps, stdlibvalues.BinaryOps,
	stdlibvalues.ErrorsOps, stdlibvalues.UnicodeOps, stdlibvalues.MathOps, stdlibvalues.BuilderOps,
	stdlibvalues.QuoteOps, stdlibvalues.LangOps,
}

// textPieces are fragments strings are built from: ASCII letters of both
// cases, spaces and separators, multi-byte runes with case mappings and
// special folds (Kelvin sign, long s, sharp s, final sigma), invalid and
// truncated UTF-8, and the UTF-8 edge sequences.
var textPieces = []string{
	"a", "b", "A", "B", "ab", "AB", "aB", "x", "z", "Z", " ", "  ", "\t", "\n", "\v", "\f", "\r",
	",", "-", ".", "..", "--", "_", "é", "É", "ß", "ẞ", "\u212a", "k", "K", "\u017f", "s", "S",
	"Σ", "σ", "ς", "ǅ", "Ǆ", "ǆ", "𐐀", "𐐨", "😀", "世界", "\u0085", "\u00a0", "\u2028", "\u3000",
	"\u200a", "\u1680", "\xff", "\xc3", "\xe2\x82", "\xed\xa0\x80", "\xf4\x90\x80\x80", "\x00", "\x7f",
	"\x80", "\xef\xbf\xbd", "<>", "|",
}

// numberPieces are fragments of numbers for strconv parsing, including
// limits of every bit size, prefixes, underscores, signs and junk.
var numberPieces = []string{
	"0", "1", "7", "9", "10", "127", "128", "255", "256", "-128", "-129", "32767", "32768", "65535",
	"65536", "2147483647", "2147483648", "-2147483648", "-2147483649", "4294967295", "4294967296",
	"9223372036854775807", "9223372036854775808", "-9223372036854775808", "-9223372036854775809",
	"18446744073709551615", "18446744073709551616", "99999999999999999999999", "0x", "0X", "0b", "0B",
	"0o", "0O", "0x7f", "0xFF", "0b101", "0o17", "017", "1_000", "_1", "1__0", "1_", "0x_1f", "+", "-",
	"+5", "-0", "z", "Z", "zz", "1z", "ff", "FF", "fF", " ", "true", "false", "TRUE", "True", "t", "f",
	"T", "F", "1", "0", "yes", "", "8", "a", "g", "\xff", "é",
}

// quotePieces are fragments of quoted and escaped text for strconv's
// quoting and unquoting functions.
var quotePieces = []string{
	`"`, `'`, "`", `\`, `\n`, `\t`, `\a`, `\b`, `\f`, `\r`, `\v`, `\\`, `\"`, `\'`, `\x41`, `\x4`,
	`\xff`, `\u00e9`, `\u00E`, `\U0001F600`, `\U00110000`, `\ud800`, `\101`, `\400`, `\777`, `\8`,
	`\q`, "a", "b", " ", "é", "😀", "\xff", "\x00", "\x7f", "\u2028", "\u00ad", "\n", `"a"`, `'a'`,
	"`a`", `"\n"`, `'\''`, `"\""`, "`\\`", `''`, `""`,
}

// piecesText concatenates random pieces up to max bytes.
func piecesText(rng *rand.Rand, pieces []string, max int) []byte {
	var b []byte
	n := rng.Intn(max + 1)
	for len(b) < n {
		p := pieces[rng.Intn(len(pieces))]
		if len(b)+len(p) > max {
			break
		}
		b = append(b, p...)
	}
	return b
}

// stdlibVector builds one vector for group g and operation op: a and b
// from the group's alphabet (b is often a piece of a, or empty), and random
// integers biased to small and edge values.
func stdlibVector(rng *rand.Rand, g, op int) []byte {
	in := make([]byte, 128)
	rng.Read(in)
	in[0], in[1] = byte(g), byte(op)
	pieces := textPieces
	switch g {
	case 2:
		pieces = numberPieces
	case 8:
		pieces = quotePieces
	}
	var a, b []byte
	switch {
	case g == 3 || g == 4 || g == 7:
		// binary, errors and Builder programs read raw bytes.
		a, b = in[8:8+rng.Intn(49)], piecesText(rng, pieces, 48)
		a = append([]byte(nil), a...)
	case g == 2 && rng.Intn(3) == 0:
		a = []byte(numberPieces[rng.Intn(len(numberPieces))])
		b = piecesText(rng, textPieces, 20)
	default:
		a = piecesText(rng, pieces, 48)
		b = piecesText(rng, pieces, 12)
	}
	switch rng.Intn(6) {
	case 0, 1:
		if len(a) > 0 {
			i := rng.Intn(len(a))
			j := i + rng.Intn(len(a)-i+1)
			b = append([]byte(nil), a[i:j]...)
		}
	case 2:
		b = nil
	}
	if rng.Intn(10) == 0 {
		a = nil
	}
	in[2], in[3] = byte(len(a)+49*rng.Intn(5)), byte(len(b)+49*rng.Intn(5))
	copy(in[8:56], a)
	copy(in[56:104], b)
	small := []byte{0, 1, 2, 3, 0xff, 0xfe, 0x80, 0x7f, 5, 0xfb}
	if rng.Intn(2) == 0 {
		in[4] = small[rng.Intn(len(small))]
	}
	if rng.Intn(2) == 0 {
		in[5] = small[rng.Intn(len(small))]
	}
	edges := []uint64{0, 1, 2, 7, 0x7f, 0x80, 0xff, 0x3fff, 0x4000, 1<<32 - 1, 1 << 32, 1<<63 - 1, 1 << 63, 1<<64 - 1}
	for _, o := range []int{104, 112} {
		if rng.Intn(2) == 0 {
			v := edges[rng.Intn(len(edges))]
			for k := 0; k < 8; k++ {
				in[o+k] = byte(v >> (8 * k))
			}
		}
	}
	for o := 120; o < 128; o += 2 {
		if rng.Intn(3) == 0 {
			in[o], in[o+1] = byte(rng.Intn(64)), 0
		}
	}
	if g == 5 && rng.Intn(3) == 0 {
		// Runes near interesting code points.
		points := []int{0x41, 0x61, 0xdf, 0x130, 0x131, 0x17f, 0x1c5, 0x212a, 0x2126, 0xd7ff, 0xd800, 0xdfff, 0xe000, 0xfffd, 0xffff, 0x10000, 0x10ffff}
		p := points[rng.Intn(len(points))] + rng.Intn(3) - 1
		in[120], in[121], in[122], in[123] = byte(p), byte(p>>8), byte(2+5*rng.Intn(3)), byte(p>>16)<<3
	}
	return in
}

// stdlibVectors covers every operation of every group perOp times.
func stdlibVectors(seed int64, perOp int) [][]byte {
	rng := rand.New(rand.NewSource(seed))
	var vectors [][]byte
	for g, ops := range stdlibGroupOps {
		for op := 0; op < ops; op++ {
			for j := 0; j < perOp; j++ {
				vectors = append(vectors, stdlibVector(rng, g, op))
			}
		}
	}
	return vectors
}

// StdlibVectorsForTest returns the vectors the SBF test runs.
func StdlibVectorsForTest() [][]byte { return stdlibVectors(20261025, 6) }

func compileStdlib(t *testing.T) []byte {
	t.Helper()
	c, err := CompileProgram(stdlibProgram(t))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// compareHostVectors runs vectors through the host binary and compares each
// line with want (native results); it returns the panics per group and the
// runs per group and operation.
func compareHostVectors(t *testing.T, bin string, vectors [][]byte, want []string) (map[int]int, map[[2]int]int) {
	t.Helper()
	var got, batch strings.Builder
	flush := func() {
		if batch.Len() > 0 {
			got.WriteString(runHost(t, bin, batch.String()))
			batch.Reset()
		}
	}
	panics, runs := map[int]int{}, map[[2]int]int{}
	for j, in := range vectors {
		line := fmt.Sprintf("%x\n", in)
		runs[[2]int{int(in[0]), int(in[1])}]++
		if strings.HasPrefix(want[j], "P:") {
			panics[int(in[0])]++
			flush()
			got.WriteString(runHost(t, bin, line))
			continue
		}
		batch.WriteString(line)
	}
	flush()
	gotLines := strings.SplitAfter(got.String(), "\n")
	for j := range want {
		if j >= len(gotLines) || gotLines[j] != want[j] {
			g := ""
			if j < len(gotLines) {
				g = gotLines[j]
			}
			t.Fatalf("vector %d (group %d op %d) %x:\nC  %.400q\nGo %.400q", j, vectors[j][0], vectors[j][1], vectors[j], g, want[j])
		}
	}
	return panics, runs
}

func TestStdlibDifferential(t *testing.T) {
	bin := hostCompile(t, compileStdlib(t), DefaultHeapBytes, allocDriver, false)
	vectors := append(stdlibVectors(20261014, 600), StdlibVectorsForTest()...)
	want := make([]string, len(vectors))
	for j, in := range vectors {
		want[j] = nativeStdlibCall(in)
	}
	panics, runs := compareHostVectors(t, bin, vectors, want)
	total := 0
	for _, n := range panics {
		total += n
	}
	// Every operation ran; the operations that test a panic must panic
	// for some vectors.
	for g, ops := range stdlibGroupOps {
		for op := 0; op < ops; op++ {
			if runs[[2]int{g, op}] == 0 {
				t.Errorf("group %d op %d never ran", g, op)
			}
		}
	}
	for _, g := range []int{0, 1, 2, 3, 4, 5, 7, 9} {
		if panics[g] == 0 {
			t.Errorf("group %d: no vector panics", g)
		}
	}
	t.Logf("%d vectors (%d panics: %v) match native Go", len(vectors), total, panics)
}
