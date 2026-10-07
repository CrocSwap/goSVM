package compiler

import (
	"bytes"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	arrayvalues "gosvm/examples/array-values"
)

func TestSDK2AggregateMemory(t *testing.T) {
	cc, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("host Clang is needed for aggregate memory validation")
	}
	// Check alignment, short/large lengths, unsigned byte conversion, unchanged
	// source and destination canaries. Undefined/alignment behavior must also pass
	// UBSan, independently of the application's actual-SBF checks.
	dir := t.TempDir()
	file := filepath.Join(dir, "memory.c")
	bin := filepath.Join(dir, "memory")
	source := `#include <stddef.h>
#include <stdio.h>
typedef unsigned char u8;typedef unsigned long long u64;
` + arrayMemoryWords + `
int main(void) {
 _Alignas(8) u8 src[2200],dst[2200];
 __SIZE_TYPE__ counts[]={0,1,7,8,9,31,32,33,255,256,511,512,1023,1024,1100};
 int values[]={0,1,127,255,256,-1,0x12345678};
 for(int sa=0;sa<8;sa++)for(int da=0;da<8;da++)for(int n=0;n<15;n++) {
  __SIZE_TYPE__ count=counts[n];
  for(int j=0;j<2200;j++){src[j]=(u8)(j*37+11);dst[j]=91;}
  if(memcpy(dst+da,src+sa,count)!=dst+da)return 1;
  for(int j=0;j<2200;j++) {
   if(src[j]!=(u8)(j*37+11))return 2;
   u8 want=j>=da&&(__SIZE_TYPE__)(j-da)<count?src[sa+j-da]:91;
   if(dst[j]!=want)return 3;
  }
  for(int v=0;v<7;v++) {
   for(int j=0;j<2200;j++)dst[j]=91;
   if(memset(dst+da,values[v],count)!=dst+da)return 4;
   for(int j=0;j<2200;j++){u8 want=j>=da&&(__SIZE_TYPE__)(j-da)<count?(u8)values[v]:91;if(dst[j]!=want)return 5;}
  }
 }
 puts("7680 alignment/count/value vectors passed");return 0;
}`
	if err := os.WriteFile(file, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(cc, "-std=c11", "-O2", "-fno-builtin", "-fsanitize=undefined", "-fno-sanitize-recover=all", file, "-o", bin)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("compile memory: %v %s", err, out)
	}
	if out, err := exec.Command(bin).CombinedOutput(); err != nil {
		t.Fatalf("memory mismatch/UB: %v %s", err, out)
	}
}

func TestSDK2ArrayValueSemantics(t *testing.T) {
	source, err := os.ReadFile("../../examples/array-values/program.go")
	if err != nil {
		t.Fatal(err)
	}
	p := &Program{SDK: 2, Entry: "app", Packages: []PackageSources{{Path: "app", Sources: []Source{{Name: "arrays.go", Data: source}}}}}
	c, err := CompileProgram(p)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(c, []byte("gosvm_memory_word")) {
		t.Fatal("explicit SDK 2 did not select word memory")
	}
	host := hostGenerated(t, c, arrayHostDriver)
	rng := rand.New(rand.NewSource(52626))
	var input, want strings.Builder
	for j := 0; j < 1000; j++ {
		state, ix := make([]byte, 24), make([]byte, 16)
		rng.Read(state)
		rng.Read(ix)
		ix[0], ix[1] = 0, byte(j%32)
		fmt.Fprintf(&input, "%x %x\n", ix, state)
		want.WriteString(nativeArrayCall(arrayvalues.Process, state, ix))
	}
	cmd := exec.Command(host)
	cmd.Stdin = strings.NewReader(input.String())
	if out, err := cmd.CombinedOutput(); err != nil || string(out) != want.String() {
		t.Fatalf("SDK 2 value copy mismatch: %v %s", err, out)
	}
	for _, sdk := range []int{0, 1} {
		p.SDK = sdk
		c, err := CompileProgram(p)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(c, []byte("gosvm_memory_word")) {
			t.Fatal("legacy/unpinned output changed", sdk)
		}
	}
}
