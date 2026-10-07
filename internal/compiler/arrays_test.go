package compiler

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"strings"
	"testing"

	arraycapacity "gosvm/examples/array-capacity"
	arrayvalues "gosvm/examples/array-values"
)

func nativeArrayCall(call func([]byte, []byte) uint64, state, instruction []byte) (result string) {
	defer func() {
		if recover() != nil {
			result = fmt.Sprintf("P:%x\n", state)
		}
	}()
	code := call(state, instruction)
	return fmt.Sprintf("%d:%x\n", code, state)
}

func nativeArrayResult(state, instruction []byte) string {
	return nativeArrayCall(arrayvalues.Process, state, instruction)
}

const arrayHostDriver = `
#include <stdio.h>
#include <stdlib.h>
static u8 driver_state[24];
static void print_state(void) {for (u64 j=0;j<24;j++) printf("%02x",driver_state[j]); puts("");}
void abort(void) {printf("P:"); print_state(); fflush(stdout); _Exit(0);}
static u8 nibble(char c){return c<='9'?c-'0':c-'a'+10;}
int main(void){char ih[33],sh[49];u8 i[16];
while(scanf("%32s %48s",ih,sh)==2){
 for(u64 j=0;j<16;j++)i[j]=(nibble(ih[j*2])<<4)|nibble(ih[j*2+1]);
 for(u64 j=0;j<24;j++)driver_state[j]=(nibble(sh[j*2])<<4)|nibble(sh[j*2+1]);
 printf("%llu:",go_Process(GOSVM_SLICE(driver_state,24),GOSVM_SLICE(i,16)));print_state();
}return 0;}`

func TestScalarArraysDifferential(t *testing.T) {
	source, err := os.ReadFile("../../examples/array-values/program.go")
	if err != nil {
		t.Fatal(err)
	}
	bin := hostProgram(t, string(source), arrayHostDriver)
	rng := rand.New(rand.NewSource(51432))
	var input, expected strings.Builder
	for j := 0; j < 1000; j++ {
		state, instruction := make([]byte, 24), make([]byte, 16)
		rng.Read(state)
		rng.Read(instruction)
		instruction[0], instruction[1] = 0, byte(j%32)
		fmt.Fprintf(&input, "%x %x\n", instruction, state)
		want := nativeArrayResult(state, instruction)
		if !strings.HasPrefix(want, "0:") {
			t.Fatalf("native array fixture failed case %d: %s", j, want)
		}
		expected.WriteString(want)
	}
	cmd := exec.Command(bin)
	cmd.Stdin = strings.NewReader(input.String())
	got, err := cmd.CombinedOutput()
	if err != nil || string(got) != expected.String() {
		t.Fatalf("native-Go/C array mismatch: %v\n%s", err, got)
	}
	// Include observable RHS effects before a failed assignment bounds check,
	// zero-length arrays, byte wraparound, valid edge indexes and custom errors.
	for _, row := range [][3]byte{{1, 32, 0}, {1, 255, 255}, {2, 32, 0}, {2, 255, 255}, {3, 0, 0}, {3, 255, 255}, {1, 31, 0}, {2, 31, 255}, {4, 31, 255}} {
		state, instruction := make([]byte, 24), make([]byte, 16)
		instruction[0], instruction[1], instruction[2] = row[0], row[1], row[2]
		cmd := exec.Command(bin)
		cmd.Stdin = strings.NewReader(fmt.Sprintf("%x %x\n", instruction, state))
		got, err := cmd.CombinedOutput()
		want := nativeArrayResult(state, instruction)
		if err != nil || string(got) != want {
			t.Fatalf("bounds/order row %v: got %q want %q (%v)", row, got, want, err)
		}
	}
	for _, index := range []uint64{0, 31, 32, 1 << 32, 1 << 63, ^uint64(0)} {
		state, instruction := make([]byte, 24), make([]byte, 16)
		instruction[0] = 5
		binary.LittleEndian.PutUint64(instruction[8:], index)
		cmd := exec.Command(bin)
		cmd.Stdin = strings.NewReader(fmt.Sprintf("%x %x\n", instruction, state))
		got, err := cmd.CombinedOutput()
		want := nativeArrayResult(state, instruction)
		if err != nil || string(got) != want {
			t.Fatalf("full-width bounds %d: got %q want %q (%v)", index, got, want, err)
		}
	}
}

func TestArrayDeclarationOrder(t *testing.T) {
	sources := []Source{
		{"a.go", []byte(`package p; type Box struct { K Key };func Process(s,i []byte)uint64 {b:=Box{K:Key{1}};return uint64(b.K[0])}`)},
		{"b.go", []byte(`package p;type Key [32]byte;func echo(k [32]byte)[32]byte{return k}`)},
	}
	a, err := CompileSources(sources)
	if err != nil {
		t.Fatal(err)
	}
	b, err := CompileSources([]Source{sources[1], sources[0]})
	if err != nil || !bytes.Equal(a, b) {
		t.Fatalf("array output depends on file enumeration: %v", err)
	}
	bin := hostSources(t, sources, `int main(void){return go_Process((slice){0,0},(slice){0,0})!=1;}`)
	if out, err := exec.Command(bin).CombinedOutput(); err != nil {
		t.Fatalf("cross-file array declarations: %v %s", err, out)
	}
}

func TestArrayRejections(t *testing.T) {
	for name, source := range map[string]string{
		"oversize byte":    `type K [1025]byte;func Process(s,i []byte)uint64{return 0}`,
		"oversize word":    `type K [129]uint64;func Process(s,i []byte)uint64{return 0}`,
		"huge length":      `type K [9223372036854775807]byte;func Process(s,i []byte)uint64{return 0}`,
		"signed element":   `type K [32]int;func Process(s,i []byte)uint64{return 0}`,
		"nested array":     `type K [2][32]byte;func Process(s,i []byte)uint64{return 0}`,
		"struct element":   `type S struct{ N uint64 };type K [2]S;func Process(s,i []byte)uint64{return 0}`,
		"context element":  `import "gosvm/solana";type K [2]solana.Context;func Process(c solana.Context)uint64{return 0}`,
		"slice element":    `type K [2][]byte;func Process(s,i []byte)uint64{return 0}`,
		"slice conversion": `func Process(s,i []byte)uint64{a:=[24]byte(s);return uint64(a[0])}`,
		"word slice":       `func Process(s,i []byte)uint64{a:=[32]uint64{};b:=a[:];return b[0]}`,
		"array range":      `func Process(s,i []byte)uint64{a:=[32]byte{};for _,v:=range a {s[0]=v};return 0}`,
		"pointer element":  `type K [2]*uint64;func Process(s,i []byte)uint64{return 0}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Compile("arrays-reject.go", []byte("package p;"+source))
			if err == nil || !strings.Contains(err.Error(), "arrays-reject.go:") {
				t.Fatalf("expected source-located array rejection, got %v", err)
			}
		})
	}
}

func TestArrayStorageLimit(t *testing.T) {
	for _, typ := range []string{"[1024]byte", "[128]uint64", "[256]uint32", "[1024]bool", "[0]byte"} {
		_, err := Compile("limit.go", []byte("package p;type K "+typ+`;func Process(s,i []byte)uint64{var a K;return uint64(len(a))}`))
		if err != nil {
			t.Fatalf("boundary %s: %v", typ, err)
		}
	}
}

func TestArrayCapacityMemory(t *testing.T) {
	source, err := os.ReadFile("../../examples/array-capacity/program.go")
	if err != nil {
		t.Fatal(err)
	}
	for mode, variant := range []struct {
		handler string
		count   uint16
	}{
		{"bytes", 1024}, {"words", 128}, {"smallWords", 256}, {"flags", 1024},
	} {
		t.Run(variant.handler, func(t *testing.T) {
			entry := fmt.Sprintf("\nfunc Process(s,i []byte)uint64{s[3]=37;%s(s,i);return 0}\n", variant.handler)
			bin := hostProgram(t, string(source)+entry, arrayHostDriver)
			n := variant.count
			for _, pair := range [][2]uint16{{0, 0}, {n - 1, n - 1}, {0, n - 1}, {n - 1, 0}, {n / 2, n / 2}, {n, 0}, {0, n}} {
				for _, value := range []byte{0, 255} {
					state, instruction := bytes.Repeat([]byte{91}, 24), make([]byte, 16)
					instruction[0], instruction[3], instruction[4] = byte(mode), value, 17
					binary.LittleEndian.PutUint16(instruction[1:], pair[0])
					binary.LittleEndian.PutUint16(instruction[5:], pair[1])
					if value != 0 {
						binary.LittleEndian.PutUint64(instruction[8:], ^uint64(0))
					}
					cmd := exec.Command(bin)
					cmd.Stdin = strings.NewReader(fmt.Sprintf("%x %x\n", instruction, state))
					got, err := cmd.CombinedOutput()
					want := nativeArrayCall(arraycapacity.Check, state, instruction)
					if err != nil || string(got) != want {
						t.Fatalf("capacity indexes %v value %d: got %q want %q (%v)", pair, value, got, want, err)
					}
				}
			}
		})
	}
}
