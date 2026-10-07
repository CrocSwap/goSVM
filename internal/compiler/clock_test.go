package compiler

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"gosvm/solana"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSDK2ClockNativeCDifferential(t *testing.T) {
	source := `package app;import "gosvm/solana"
func Process(c solana.Context)uint64{return solana.Clock(c,solana.Data(c,0)[1:uint64(solana.Instruction(c)[0])+1])}`
	p := &Program{SDK: 2, Entry: "app", Packages: []PackageSources{unit("app", source)}}
	c, err := CompileProgram(p)
	if err != nil {
		t.Fatal(err)
	}
	p.SDK = 1
	if _, err := CompileProgram(p); err == nil || !strings.Contains(err.Error(), "Clock") {
		t.Fatal("SDK1 admitted Clock", err)
	}
	cc, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("host Clang required")
	}
	driver := `
#include <stdio.h>
#include <string.h>
static u64 words[5],status,calls;
u64 sol_get_clock_sysvar(GosvmClock *c){calls++;for(u64 i=0;i<5;i++)c->words[i]=words[i];return status;}
int main(void){u64 n;while(scanf("%llu %llu %llu %llu %llu %llu %llu",&n,&status,&words[0],&words[1],&words[2],&words[3],&words[4])==7){
 u8 data[82],ix[1]={(u8)n};memset(data,91,sizeof(data));Context c={0};calls=0;
 c.count=1;c.accounts[0].data=data;c.accounts[0].data_len=sizeof(data);c.instruction=GOSVM_SLICE(ix,1);
 u64 code=go_Process(&c);printf("%llu %llu ",code,calls);for(u64 j=0;j<82;j++)printf("%02x",data[j]);printf("\n");}return 0;}`
	dir := t.TempDir()
	file, bin := filepath.Join(dir, "clock.c"), filepath.Join(dir, "clock")
	if err := os.WriteFile(file, append(c, []byte(driver)...), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(cc, "-std=c11", "-O2", "-fsanitize=undefined", "-fno-sanitize-recover=all", file, "-o", bin).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	rng := rand.New(rand.NewSource(20261005))
	var input, want strings.Builder
	for i := 0; i < 1010; i++ {
		n, status := 40, uint64(0)
		if i%4 == 1 {
			n = []int{0, 1, 39, 41, 80}[i%5]
		}
		if i%4 == 2 {
			status = 9876
		}
		var words [5]uint64
		for j := range words {
			words[j] = rng.Uint64()
		}
		if i == 0 {
			words = [5]uint64{9007199254740993, ^uint64(22), 7, 9, ^uint64(123455)}
		}
		fmt.Fprintf(&input, "%d %d %d %d %d %d %d\n", n, status, words[0], words[1], words[2], words[3], words[4])
		data := bytes.Repeat([]byte{91}, 82)
		calls := 0
		ctx := solana.Context{ReadClock: func(out []byte) uint64 {
			calls++
			for j, v := range words {
				binary.LittleEndian.PutUint64(out[8*j:], v)
			}
			return status
		}}
		code := solana.Clock(ctx, data[1:n+1])
		fmt.Fprintf(&want, "%d %d %x\n", code, calls, data)
	}
	cmd := exec.Command(bin)
	cmd.Stdin = strings.NewReader(input.String())
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if string(out) != want.String() {
		t.Fatal("native/C Clock outputs, guards or syscall counts differ")
	}
}
