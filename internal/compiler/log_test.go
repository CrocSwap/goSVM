package compiler

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"gosvm/solana"
	"math/rand"
	"os/exec"
	"strings"
	"testing"
)

// The logging runtime is emitted only for programs that call Log, Log64 or
// LogPubkey; SDK 1 does not have them.
func TestSDK2LogRuntimeEmittedOnlyWhenCalled(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		source string
		want   bool
	}{
		{`package app;import "gosvm/solana";func Process(c solana.Context)uint64{return solana.Count(c)}`, false},
		{`package app;import "gosvm/solana";func Process(c solana.Context)uint64{return solana.Log64(c,1,2,3,4,5)}`, true},
		{`package app;import "gosvm/solana";func Process(c solana.Context)uint64{solana.Log(c,"hi");return 0}`, true},
	} {
		c, err := CompileProgram(&Program{SDK: 2, Entry: "app", Packages: []PackageSources{unit("app", tc.source)}})
		if err != nil {
			t.Fatal(err)
		}
		if got := bytes.Contains(c, []byte("gosvm_sol_log_64")); got != tc.want {
			t.Fatalf("log runtime emitted %v, want %v:\n%s", got, tc.want, tc.source)
		}
	}
	for _, call := range []string{`solana.Log(c,"x")`, `solana.Log64(c,1,2,3,4,5)`, `solana.LogPubkey(c,nil)`} {
		source := `package app;import "gosvm/solana";func Process(c solana.Context)uint64{return ` + call + `}`
		if _, err := CompileProgram(&Program{SDK: 1, Entry: "app", Packages: []PackageSources{unit("app", source)}}); err == nil {
			t.Fatal("SDK1 admitted", call)
		}
	}
}

// The generated C passes the native SDK's messages to the log syscalls: the
// same bytes for Log, the same words for Log64 and key for LogPubkey, and
// the same statuses.
func TestSDK2LogNativeCDifferential(t *testing.T) {
	t.Parallel()
	source := `package app
import "gosvm/solana"
func word(b []byte,o uint64)uint64{n:=uint64(0);for j:=uint64(0);j<8;j++{n=n|uint64(b[o+j])<<(j*8)};return n}
func Process(c solana.Context)uint64{
 ix:=solana.Instruction(c)
 switch ix[0]{
 case 0:return solana.Log(c,"hello, world")
 case 1:return solana.Log(c,string(ix[1:]))
 case 2:return solana.Log64(c,word(ix,1),word(ix,9),word(ix,17),word(ix,25),word(ix,33))
 case 3:return solana.LogPubkey(c,ix[1:])
 case 4:solana.Log(c,"");solana.Log(c,"two");return solana.Log64(c,0,0,0,0,0)
 case 5:panic("never sent: panic's log runtime coexists with Log's")
 }
 return 99
}`
	driver := `
#include <stdio.h>
void sol_log_(const u8 *p,u64 n){printf("log ");for(u64 i=0;i<n;i++)printf("%02x",p[i]);printf(";");}
void sol_log_64_(u64 a,u64 b,u64 c,u64 d,u64 e){printf("log64 %llu %llu %llu %llu %llu;",a,b,c,d,e);}
void sol_log_pubkey(const u8 *p){printf("key ");for(u64 i=0;i<32;i++)printf("%02x",p[i]);printf(";");}
int main(void){char line[4096];
 while(fgets(line,sizeof line,stdin)){
  u8 ix[2048];u64 n=0;for(char *q=line;q[0]&&q[1]&&q[0]!='\n';q+=2){unsigned v;sscanf(q,"%2x",&v);ix[n++]=(u8)v;}
  for(u64 i=0;i<sizeof gosvm_host_heap/8;i++)gosvm_host_heap[i]=0; // a fresh invocation heap
  Context c={0};c.instruction=GOSVM_SLICE(ix,n);
  u64 code=go_Process(&c);printf(" %llu\n",code);
 }return 0;}`
	c, err := CompileProgram(&Program{SDK: 2, Entry: "app", Packages: []PackageSources{unit("app", source)}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("host Clang required")
	}
	bin := hostGenerated(t, append([]byte(hostHeapHeader(DefaultHeapBytes)), c...), driver)
	var input, want strings.Builder
	cases := 0
	add := func(ix []byte) {
		cases++
		fmt.Fprintf(&input, "%x\n", ix)
		var lines []string
		ctx := solana.Context{InstructionData: ix, WriteLog: func(m string) { lines = append(lines, m) }}
		// Record what each native call passes on, in the C driver's terms.
		var got strings.Builder
		var code uint64
		switch ix[0] {
		case 0, 1, 4:
			code = map[byte]func() uint64{
				0: func() uint64 { return solana.Log(ctx, "hello, world") },
				1: func() uint64 { return solana.Log(ctx, string(ix[1:])) },
				4: func() uint64 { solana.Log(ctx, ""); solana.Log(ctx, "two"); return solana.Log64(ctx, 0, 0, 0, 0, 0) },
			}[ix[0]]()
			for i, l := range lines {
				if ix[0] == 4 && i == 2 {
					got.WriteString("log64 0 0 0 0 0;")
					continue
				}
				fmt.Fprintf(&got, "log %s;", hex.EncodeToString([]byte(l)))
			}
		case 2:
			w := func(o int) uint64 {
				v := uint64(0)
				for j := 0; j < 8; j++ {
					v |= uint64(ix[o+j]) << (8 * j)
				}
				return v
			}
			code = solana.Log64(ctx, w(1), w(9), w(17), w(25), w(33))
			if len(lines) == 1 {
				fmt.Fprintf(&got, "log64 %d %d %d %d %d;", w(1), w(9), w(17), w(25), w(33))
			}
		case 3:
			code = solana.LogPubkey(ctx, ix[1:])
			if len(lines) == 1 {
				fmt.Fprintf(&got, "key %x;", ix[1:])
			}
		default:
			code = 99
		}
		fmt.Fprintf(&want, "%s %d\n", got.String(), code)
	}
	rng := rand.New(rand.NewSource(2026101903))
	add([]byte{0})
	add([]byte{4})
	add([]byte{9})
	for _, n := range []int{0, 1, 31, 32, 33, 100} {
		key := make([]byte, n+1)
		key[0] = 3
		rng.Read(key[1:])
		add(key)
	}
	for j := 0; j < 300; j++ {
		words := make([]byte, 41)
		words[0] = 2
		rng.Read(words[1:])
		add(words)
		text := []byte{1}
		for k := rng.Intn(200); k > 0; k-- {
			text = append(text, byte(32+rng.Intn(95)))
		}
		add(text)
	}
	add(append([]byte{1}, "héllo, 世界 ✓ 😀"...))
	cmd := exec.Command(bin)
	cmd.Stdin = strings.NewReader(input.String())
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if string(out) != want.String() {
		actual, expected := strings.Split(string(out), "\n"), strings.Split(want.String(), "\n")
		for i := range expected {
			if i >= len(actual) || actual[i] != expected[i] {
				t.Fatalf("case %d: C %q, native %q", i, actual[i], expected[i])
			}
		}
	}
	t.Logf("%d native/C logging vectors passed", cases)
}
