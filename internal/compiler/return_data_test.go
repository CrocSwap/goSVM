package compiler

import (
	"bytes"
	"fmt"
	"gosvm/solana"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSDK2ReturnDataNativeCDifferential(t *testing.T) {
	source := `package app;import "gosvm/solana";func Process(c solana.Context)uint64{return solana.SetReturnData(c,solana.Instruction(c))}`
	p := &Program{SDK: 2, Entry: "app", Packages: []PackageSources{unit("app", source)}}
	c, err := CompileProgram(p)
	if err != nil {
		t.Fatal(err)
	}
	p.SDK = 1
	if _, err = CompileProgram(p); err == nil || !strings.Contains(err.Error(), "SetReturnData") {
		t.Fatal("SDK1 admitted setter", err)
	}
	cc, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("host Clang required")
	}
	driver := `
#include <stdio.h>
#include <string.h>
static u8 stored[1024];static u64 size,calls;
void sol_set_return_data(const u8 *data,u64 n){calls++;size=n;for(u64 i=0;i<n;i++)stored[i]=data[i];}
int main(void){u64 n;while(scanf("%llu",&n)==1){
 u8 data[12288];for(u64 i=0;i<sizeof(data);i++)data[i]=(u8)(i*37+11);
 memset(stored,91,sizeof(stored));size=17;calls=0;Context c={0};c.instruction=GOSVM_SLICE(data,n);
 u64 code=go_Process(&c);memset(data,42,sizeof(data));printf("%llu %llu %llu ",code,calls,size);
 for(u64 i=0;i<size;i++)printf("%02x",stored[i]);puts("");}return 0;}`
	dir := t.TempDir()
	file, bin := filepath.Join(dir, "return.c"), filepath.Join(dir, "return")
	if err = os.WriteFile(file, append(c, []byte(driver)...), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(cc, "-std=c11", "-O2", "-fsanitize=undefined", "-fno-sanitize-recover=all", file, "-o", bin).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	var input, want strings.Builder
	sizes := []int{0, 1, 20, 1023, 1024, 1025, 10240}
	for n := 0; n <= 1100; n += 7 {
		sizes = append(sizes, n)
	}
	for _, n := range sizes {
		fmt.Fprintf(&input, "%d\n", n)
		data := make([]byte, n)
		for i := range data {
			data[i] = byte(i*37 + 11)
		}
		stored := bytes.Repeat([]byte{91}, 17)
		calls := 0
		code := solana.SetReturnData(solana.Context{WriteReturnData: func(b []byte) { calls++; stored = b }}, data)
		for i := range data {
			data[i] = 42
		}
		fmt.Fprintf(&want, "%d %d %d %x\n", code, calls, len(stored), stored)
	}
	cmd := exec.Command(bin)
	cmd.Stdin = strings.NewReader(input.String())
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if string(out) != want.String() {
		t.Fatal("native/C return-data guard, bytes or call count differs")
	}
	t.Logf("%d return-data vectors passed", len(sizes))
}
