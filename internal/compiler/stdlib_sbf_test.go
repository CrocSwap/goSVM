package compiler

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	stdlibvalues "gosvm/examples/stdlib-values"
	"gosvm/internal/targets"
	"gosvm/internal/testvm"
)

// stdlibSBFProgram is examples/stdlib-values with a Context entrypoint:
// account 0 is the 1 KiB output and the instruction data is Run's input.
func stdlibSBFProgram(t *testing.T) *Program {
	t.Helper()
	p := stdlibProgram(t)
	for i, src := range p.Packages[0].Sources {
		if filepath.Base(src.Name) != "program.go" {
			continue
		}
		source := string(src.Data)
		entry := "func Process(s, i []byte) uint64 { return Run(s, i) }"
		if !strings.Contains(source, entry) || !strings.Contains(source, "\npackage stdlibvalues\n") {
			t.Fatal("stdlib-values entrypoint changed")
		}
		source = strings.Replace(source, "\npackage stdlibvalues\n", "\npackage stdlibvalues\n\nimport \"gosvm/solana\"\n", 1)
		source = strings.Replace(source, entry, "func Process(c solana.Context) uint64 { Run(solana.Data(c, 0), solana.Instruction(c)); return 0 }", 1)
		p.Packages[0].Sources[i].Data = []byte(source)
	}
	return p
}

// stdlibPayload follows the flag byte of every probe's input.
const stdlibPayload = "12345678hello, world! abc,def,ghi,jkl;  Kéß 0x1f 18446744073709551615"

// stdlibProbes measure the CU of common calls. Each body runs when the
// input's first byte is 1 and sets r; the same ELF with a first byte of 0
// skips it, so the difference is the call's cost, and the skipped run minus
// an empty program's is the cost of initializing the imported packages on
// every invocation. want computes r natively.
var stdlibProbes = []struct {
	name, imports, body string
	want                func(in []byte) uint64
}{
	{"none", ``, `r = uint64(in[1])`, func(in []byte) uint64 { return uint64(in[1]) }},
	{"strconv.ParseUint(8 digits)", `"strconv"`, `v, err := strconv.ParseUint(string(in[1:9]), 10, 64); if err == nil { r = v }`,
		func(in []byte) uint64 { v, _ := strconv.ParseUint(string(in[1:9]), 10, 64); return v }},
	{"strconv.ParseUint(20 digits)", `"strconv"`, `v, err := strconv.ParseUint(string(in[52:72]), 10, 64); if err == nil { r = v }`,
		func(in []byte) uint64 { v, _ := strconv.ParseUint(string(in[52:72]), 10, 64); return v }},
	{"strconv.Atoi(syntax error)", `"strconv"`, `_, err := strconv.Atoi(string(in[9:14])); if err != nil { r = uint64(len(err.Error())) }`,
		func(in []byte) uint64 { _, err := strconv.Atoi(string(in[9:14])); return uint64(len(err.Error())) }},
	{"strconv.FormatUint(20 digits)", `"strconv"`, `r = uint64(len(strconv.FormatUint(^uint64(in[1]), 10)))`,
		func(in []byte) uint64 { return uint64(len(strconv.FormatUint(^uint64(in[1]), 10))) }},
	{"strconv.AppendUint(20 digits)", `"strconv"`, `var buf [24]byte; b := strconv.AppendUint(buf[:0], ^uint64(in[1]), 10); r = uint64(len(b)) + uint64(b[0])`,
		func(in []byte) uint64 {
			b := strconv.AppendUint(nil, ^uint64(in[1]), 10)
			return uint64(len(b)) + uint64(b[0])
		}},
	{"strconv.Itoa(2 digits)", `"strconv"`, `r = uint64(len(strconv.Itoa(int(in[1]))))`,
		func(in []byte) uint64 { return uint64(len(strconv.Itoa(int(in[1])))) }},
	{"strconv.Quote(12 bytes)", `"strconv"`, `r = uint64(len(strconv.Quote(string(in[9:21]))))`,
		func(in []byte) uint64 { return uint64(len(strconv.Quote(string(in[9:21])))) }},
	{"strings.Index(32 bytes)", `"strings"`, `r = uint64(strings.Index(string(in[9:41]), "jkl"))`,
		func(in []byte) uint64 { return uint64(strings.Index(string(in[9:41]), "jkl")) }},
	{"strings.Split(4 fields)", `"strings"`, `r = uint64(len(strings.Split(string(in[23:38]), ",")))`,
		func(in []byte) uint64 { return uint64(len(strings.Split(string(in[23:38]), ","))) }},
	{"strings.ToUpper(12 ASCII bytes)", `"strings"`, `r = uint64(strings.ToUpper(string(in[9:21]))[0])`,
		func(in []byte) uint64 { return uint64(strings.ToUpper(string(in[9:21]))[0]) }},
	{"strings.EqualFold(non-ASCII)", `"strings"`, `if strings.EqualFold(string(in[41:46]), "kÉSS") { r = 1 }`,
		func(in []byte) uint64 {
			if strings.EqualFold(string(in[41:46]), "kÉSS") {
				return 1
			}
			return 0
		}},
	{"strings.Builder(4 writes)", `"strings"`, `var b strings.Builder; b.WriteString(string(in[1:9])); b.WriteByte(in[9]); b.WriteRune(rune(in[10])); b.WriteString("tail"); r = uint64(len(b.String()))`,
		func(in []byte) uint64 { return 14 }},
	{"bytes.Equal(32 bytes)", `"bytes"`, `if bytes.Equal(in[1:33], in[1:33]) { r = 1 }`, func(in []byte) uint64 { return 1 }},
	{"bytes.Index(32 bytes)", `"bytes"`, `r = uint64(bytes.Index(in[9:41], []byte("jkl")))`,
		func(in []byte) uint64 { return uint64(bytes.Index(in[9:41], []byte("jkl"))) }},
	{"binary.BigEndian.Uint64", `"encoding/binary"`, `r = binary.BigEndian.Uint64(in[1:])`, func(in []byte) uint64 { return binary.BigEndian.Uint64(in[1:]) }},
	{"binary.LittleEndian.PutUint64", `"encoding/binary"`, `var buf [8]byte; binary.LittleEndian.PutUint64(buf[:], uint64(in[1])<<40); r = uint64(buf[5])`,
		func(in []byte) uint64 { return uint64(in[1]) }},
	{"binary.Uvarint(3 bytes)", `"encoding/binary"`, `v, n := binary.Uvarint([]byte{0xff, 0xff, in[1]}); r = v + uint64(n)`,
		func(in []byte) uint64 { v, n := binary.Uvarint([]byte{0xff, 0xff, in[1]}); return v + uint64(n) }},
	{"errors.Is(depth 1)", `"errors"`, `if errors.Is(errA, errA) { r = 1 }`, func(in []byte) uint64 { return 1 }},
	{"errors.Is(depth 3)", `"errors"`, `var e error = &wrap{&wrap{errB}}; if errors.Is(e, errA) { r = 1 }; r += 2`, func(in []byte) uint64 { return 2 }},
	{"errors.As(depth 3)", `"errors"`, `var e error = &wrap{&wrap{errB}}; var w *wrap; if errors.As(e, &w) { r = 1 }`, func(in []byte) uint64 { return 1 }},
	{"utf8.DecodeRuneInString", `"unicode/utf8"`, `x, n := utf8.DecodeRuneInString(string(in[42:46])); r = uint64(x) + uint64(n)`,
		func(in []byte) uint64 {
			x, n := utf8.DecodeRuneInString(string(in[42:46]))
			return uint64(x) + uint64(n)
		}},
	{"utf8.RuneCount(64 bytes)", `"unicode/utf8"`, `r = uint64(utf8.RuneCount(in[1:65]))`, func(in []byte) uint64 { return uint64(utf8.RuneCount(in[1:65])) }},
	{"io.EOF.Error()", `"io"`, `r = uint64(len(io.EOF.Error()))`, func(in []byte) uint64 { return uint64(len(io.EOF.Error())) }},
	{"unicode.ToUpper(non-ASCII)", `"unicode"`, `r = uint64(unicode.ToUpper(rune(in[1]) + 0xb0))`, func(in []byte) uint64 { return uint64(unicode.ToUpper(rune(in[1]) + 0xb0)) }},
}

// stdlibProbeErrors declares the errors the errors probes use.
const stdlibProbeErrors = `
var errA = errors.New("a")
var errB = errors.New("b")

type wrap struct{ inner error }

func (w *wrap) Error() string { return "wrap" }
func (w *wrap) Unwrap() error { return w.inner }
`

func stdlibProbeSource(imports, body string) string {
	source := "package main\n\nimport (\n\t\"gosvm/solana\"\n"
	if imports != "" {
		source += "\t" + imports + "\n"
	}
	source += ")\n"
	if strings.Contains(imports, `"errors"`) {
		source += stdlibProbeErrors
	}
	return source + `
func put(out []byte, r uint64) {
	for j := 0; j < 8; j++ {
		out[j] = byte(r >> (8 * j))
	}
}

func Process(c solana.Context) uint64 {
	in := solana.Instruction(c)
	out := solana.Data(c, 0)
	r := uint64(0)
	if in[0] == 1 {
		` + body + `
	}
	put(out, r)
	return 0
}
`
}

// stdlibProbeCase is a probe's run (flag 1) or skipped (flag 0) case.
func stdlibProbeCase(name string, flag byte, want uint64) allocCase {
	in := append([]byte{flag}, stdlibPayload...)
	in = append(in, make([]byte, 128-len(in))...)
	out := make([]byte, 1024)
	if flag == 1 {
		binary.LittleEndian.PutUint64(out, want)
	}
	return allocCase{name: name, in: in, output: out}
}

// TestStdlibSBF builds examples/stdlib-values for fogo-v0 and solana-v3 and
// runs the SBF vectors through the pinned runners: each output account must
// match native Go's output, and every native panic must abort and roll back.
// It also measures the probes' CU. Skips without the v1.57 backend or a
// runner (make test). GOSVM_STDLIB_RESULTS=<dir> keeps fixtures, ELFs,
// reports and a summary.
func TestStdlibSBF(t *testing.T) {
	if testing.Short() {
		t.Skip("SBF runs are skipped in -short mode")
	}
	llvm := modernLLVM()
	if llvm == "" {
		t.Skip("pinned platform-tools v1.57 backend required")
	}
	vectors := StdlibVectorsForTest()
	var cases []allocCase
	aborts, panics, groups := 0, 0, map[byte]int{}
	for j, in := range vectors {
		c := allocCase{name: fmt.Sprintf("stdlib-%03d-g%d-op%d", j, in[0], in[1]), in: in, units: globalUnits, err: abortError, log: abortLog}
		result := nativeStdlibCall(in)
		groups[in[0]]++
		if line, ok := strings.CutPrefix(result, "L:"); ok {
			// A panic with a log line: the line must be logged once.
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
		} else {
			_, hexOut, _ := strings.Cut(strings.TrimSpace(result), ":")
			out, err := hex.DecodeString(hexOut)
			if err != nil || len(out) != 1024 {
				t.Fatalf("vector %d: native output %q: %v", j, result, err)
			}
			c.output = out
		}
		cases = append(cases, c)
	}
	program := stdlibSBFProgram(t)
	keep := os.Getenv("GOSVM_STDLIB_RESULTS")
	summaries := map[string]any{}
	for _, profile := range []string{target.Fogo, target.Solana} {
		t.Run(profile, func(t *testing.T) {
			runner, err := testvm.RunnerFor("", profile)
			if err != nil {
				t.Skipf("runner unavailable: %v", err)
			}
			result := runGlobalProgram(t, llvm, runner, profile, "stdlib-values", program, cases, keep)
			var cus []uint64
			for name, v := range result {
				if strings.HasPrefix(name, "stdlib-") {
					cus = append(cus, v.(uint64))
				}
			}
			sort.Slice(cus, func(i, j int) bool { return cus[i] < cus[j] })
			summary := map[string]any{"profile": profile, "elf_sha256": result["elf_sha256"], "cases": len(cases), "aborts": aborts, "panic_lines": panics,
				"cases_by_group": groups, "max_vector_cu": cus[len(cus)-1], "median_vector_cu": cus[len(cus)/2]}
			empty := &Program{Entry: "program", Packages: []PackageSources{{"program", []Source{{Name: "program.go", Data: []byte("package main\n\nimport \"gosvm/solana\"\n\nfunc Process(c solana.Context) uint64 { return 0 }\n")}}}}}
			idle := allocCase{name: "idle", in: make([]byte, 8), output: make([]byte, 1024)}
			baseline := runGlobalProgram(t, llvm, runner, profile, "probe-empty", empty, []allocCase{idle}, keep)["idle"].(uint64)
			probes := map[string]any{}
			for _, probe := range stdlibProbes {
				in := append([]byte{1}, stdlibPayload...)
				p := &Program{Entry: "program", Packages: []PackageSources{{"program", []Source{{Name: "program.go", Data: []byte(stdlibProbeSource(probe.imports, probe.body))}}}}}
				name := strings.NewReplacer(" ", "-", "(", "", ")", "", ".", "-").Replace(probe.name)
				r := runGlobalProgram(t, llvm, runner, profile, "probe-"+name, p,
					[]allocCase{stdlibProbeCase("skip", 0, 0), stdlibProbeCase("run", 1, probe.want(in))}, keep)
				skip, run := r["skip"].(uint64), r["run"].(uint64)
				probes[probe.name] = map[string]any{"imports": probe.imports, "call_cu": int64(run) - int64(skip),
					"init_cu": int64(skip) - int64(baseline), "elf_sha256": r["elf_sha256"]}
			}
			summary["empty_program_cu"] = baseline
			summary["probes"] = probes
			encoded, _ := json.MarshalIndent(summary, "", "  ")
			t.Logf("%s: %d cases (%d aborts with rollback) match native Go\n%s", profile, len(cases), aborts, encoded)
			summaries[profile] = summary
			if keep != "" {
				if err = os.WriteFile(filepath.Join(keep, profile, "summary.json"), append(encoded, '\n'), 0644); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
	_ = errors.New
	_ = bytes.Equal
	_ = stdlibvalues.Groups
}
