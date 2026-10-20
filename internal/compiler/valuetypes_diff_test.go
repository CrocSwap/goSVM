package compiler

import (
	"encoding/hex"
	"fmt"
	"math/rand"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	valuetypes "gosvm/examples/value-types"
)

// Struct and array comparison, type aliases, arrays of composite elements
// and panic against native Go: examples/value-types runs natively and as
// host-compiled C; output bytes, results, panics and each panic's log line
// must match for every vector. See docs/GO_TYPES.md.

func valueTypesSource(t *testing.T) []byte {
	return mustRead(t, "../../examples/value-types/program.go")
}

// valueTypesModes is the number of modes Run dispatches on.
const valueTypesModes = 5

// valueTypesDriver is allocDriver with sol_log_: a panic's log line is
// printed (hex, then a space) before the abort's "P:" output.
const valueTypesDriver = `
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
static u8 driver_out[1024];
static void print_out(void) {for (u64 j=0;j<1024;j++) printf("%02x",driver_out[j]); puts("");}
void sol_log_(const u8 *p,u64 n) {printf("L:");for (u64 j=0;j<n;j++) printf("%02x",p[j]);printf(" ");}
void abort(void) {printf("P:"); print_out(); fflush(stdout); _Exit(0);}
static u8 nibble(char c){return c<='9'?c-'0':c-'a'+10;}
int main(void){char ih[257];u8 in[128];
while(scanf("%256s",ih)==1){
 for(u64 j=0;j<128;j++)in[j]=(nibble(ih[j*2])<<4)|nibble(ih[j*2+1]);
 memset(driver_out,0,sizeof driver_out);
 memset(gosvm_host_heap,0,sizeof gosvm_host_heap);
 printf("%llu:",go_Process(GOSVM_SLICE(driver_out,1024),GOSVM_SLICE(in,128)));print_out();
}return 0;}`

// ExpectedPanicLine is the log line goSVM writes for a panic whose
// recovered native value is r ("" for runtime errors, which are traps
// without a line). The example panics error values only through
// error-typed operands, so they log "(error)". pkg is the package name the
// program is compiled as.
func ExpectedPanicLine(r any, pkg string) string {
	switch r.(type) {
	case *runtime.PanicNilError:
		return NilPanicLine
	case runtime.Error:
		return ""
	case error:
		return "panic: (error)"
	}
	v := reflect.ValueOf(r)
	t := v.Type()
	name := strings.Replace(t.String(), "valuetypes.", pkg+".", 1)
	prefix, suffix := "", ""
	if t.Name() != "" && t.PkgPath() != "" {
		prefix, suffix = name+"(", ")"
		if t.Kind() == reflect.String {
			prefix, suffix = name+`("`, `")`
		}
	}
	var s string
	switch t.Kind() {
	case reflect.String:
		s = v.String()
	case reflect.Bool:
		s = strconv.FormatBool(v.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		s = strconv.FormatInt(v.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		s = strconv.FormatUint(v.Uint(), 10)
	default:
		return "panic: (" + name + ")"
	}
	line := "panic: " + prefix + s + suffix
	if len(line) > PanicLineBytes {
		line = line[:PanicLineBytes]
	}
	// sol_log_ needs UTF-8: each byte that does not start a valid sequence
	// is logged as '?'.
	var b strings.Builder
	for len(line) > 0 {
		r, size := utf8.DecodeRuneInString(line)
		if r == utf8.RuneError && size == 1 {
			b.WriteByte('?')
		} else {
			b.WriteString(line[:size])
		}
		line = line[size:]
	}
	return b.String()
}

// nativeValueTypesCall runs Run natively: the result and output, or the
// panic's log line (hex, as the driver prints it) and the output.
func nativeValueTypesCall(in []byte) (result string) {
	out := make([]byte, 1024)
	defer func() {
		if r := recover(); r != nil {
			prefix := ""
			if line := ExpectedPanicLine(r, "valuetypes"); line != "" {
				prefix = "L:" + hex.EncodeToString([]byte(line)) + " "
			}
			result = fmt.Sprintf("%sP:%x\n", prefix, out)
		}
	}()
	code := valuetypes.Run(out, append([]byte(nil), in...))
	return fmt.Sprintf("%d:%x\n", code, out)
}

// valueTypesVectors covers every mode with random inputs and inputs of
// small bytes (so comparisons often succeed), and every panic selector.
func valueTypesVectors(seed int64, perMode int) [][]byte {
	rng := rand.New(rand.NewSource(seed))
	var vectors [][]byte
	for mode := byte(0); mode < valueTypesModes; mode++ {
		for j := 0; j < perMode; j++ {
			in := make([]byte, 128)
			rng.Read(in)
			in[0] = mode
			if j%3 == 1 {
				for k := 1; k < 128; k++ {
					in[k] = byte(rng.Intn(4))
				}
			}
			vectors = append(vectors, in)
		}
	}
	for sel := byte(0); sel < 20; sel++ {
		for _, x := range []byte{0, 1, 2, 3, 4, 5, 101, 130, 200, 255} {
			in := make([]byte, 128)
			rng.Read(in)
			in[0], in[1], in[3], in[4] = 3, sel, x, x
			vectors = append(vectors, in)
		}
	}
	return vectors
}

// ValueTypesVectorsForTest is the vector set the SBF test runs.
func ValueTypesVectorsForTest() [][]byte { return valueTypesVectors(20261024, 40) }

func compileValueTypes(t *testing.T) []byte {
	t.Helper()
	c, err := CompileProgram(&Program{Entry: "program", Packages: []PackageSources{{"program", []Source{{Name: "program.go", Data: valueTypesSource(t)}}}}})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestValueTypesDifferential(t *testing.T) {
	t.Parallel()
	bin := hostCompile(t, compileValueTypes(t), DefaultHeapBytes, valueTypesDriver, false)
	vectors := valueTypesVectors(20261014, 2000)
	var want []string
	var got strings.Builder
	var batch strings.Builder
	flush := func() {
		if batch.Len() > 0 {
			got.WriteString(runHost(t, bin, batch.String()))
			batch.Reset()
		}
	}
	panics, logged, runs := map[byte]int{}, map[byte]int{}, map[byte]int{}
	for _, in := range vectors {
		line := fmt.Sprintf("%x\n", in)
		result := nativeValueTypesCall(in)
		want = append(want, result)
		runs[in[0]]++
		if strings.Contains(result, "P:") {
			panics[in[0]]++
			if strings.HasPrefix(result, "L:") {
				logged[in[0]]++
			}
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
			t.Fatalf("vector %d (%x):\nC  %q\nGo %q", j, vectors[j], g, want[j])
		}
	}
	// Every mode except aliases has panic paths and must exercise both
	// outcomes; panics in modes 3 and 4 log a line.
	for mode := byte(0); mode < valueTypesModes; mode++ {
		if mode == 2 {
			if panics[mode] != 0 {
				t.Errorf("mode %d: %d of %d vectors panic", mode, panics[mode], runs[mode])
			}
			continue
		}
		if panics[mode] == 0 || panics[mode] == runs[mode] {
			t.Errorf("mode %d: %d of %d vectors panic", mode, panics[mode], runs[mode])
		}
	}
	if logged[3] == 0 || logged[4] == 0 {
		t.Errorf("logged panics: %v", logged)
	}
	t.Logf("%d vectors (panics %v, with a log line %v) match native Go", len(vectors), panics, logged)
}
