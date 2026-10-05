package compiler

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gosvm/examples/amm"
	"gosvm/examples/tokenswap"
)

func hostProgram(t *testing.T, source, driver string) string {
	return hostSources(t, []Source{{"test.go", []byte(source)}}, driver)
}
func hostSources(t *testing.T, sources []Source, driver string) string {
	t.Helper()
	cc, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("host Clang is needed for differential compiler tests")
	}
	c, err := CompileSources(sources)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "test.c")
	binary := filepath.Join(dir, "test")
	if err = os.WriteFile(file, append(c, []byte(driver)...), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(cc, "-std=c11", "-O2", file, "-o", binary).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	return binary
}

func TestWideQuoteDifferential(t *testing.T) {
	source, err := os.ReadFile("../../examples/tokenswap/swap.go")
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(string(source), "func Quote(")
	if start < 0 {
		t.Fatal("Quote not found")
	}
	program := "package p\nfunc Process(s,i []byte) uint64 {return Quote(load64(s,0),load64(s,8),load64(i,0))}\n" + string(source[start:])
	bin := hostProgram(t, program, `
#include <stdio.h>
static void put(u8 *p,u64 n){for(int i=0;i<8;i++)p[i]=(u8)(n>>(i*8));}
int main(void){u64 x,y,a;while(scanf("%llu %llu %llu",&x,&y,&a)==3){u8 s[16],i[8];put(s,x);put(s+8,y);put(i,a);printf("%llu\n",go_Process((slice){s,16},(slice){i,8}));}return 0;}`)
	rng := rand.New(rand.NewSource(123))
	var input, expected strings.Builder
	for j := 0; j < 10000; j++ {
		x, y, a := rng.Uint64()>>1, rng.Uint64(), rng.Uint64()>>2
		fmt.Fprintf(&input, "%d %d %d\n", x, y, a)
		fmt.Fprintf(&expected, "%d\n", tokenswap.Quote(x, y, a))
	}
	cmd := exec.Command(bin)
	cmd.Stdin = strings.NewReader(input.String())
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if string(out) != expected.String() {
		t.Fatal("C/native wide arithmetic mismatch")
	}
}

func TestRejectUnsupported(t *testing.T) {
	for name, source := range map[string]string{
		"imports":            `import "fmt";func Process(s,i []byte) uint64 {fmt.Println("x");return 0}`,
		"heap":               `func Process(s,i []byte) uint64 {x:=make([]byte,1);return uint64(x[0])}`,
		"signed":             `func Process(s,i []byte) uint64 {x:=1;return uint64(x)}`,
		"goroutine":          `func f(){};func Process(s,i []byte) uint64 {go f();return 0}`,
		"defer":              `func f(){};func Process(s,i []byte) uint64 {defer f();return 0}`,
		"global":             `var x uint64;func Process(s,i []byte) uint64 {return x}`,
		"recursion":          `func f(x uint64)uint64{return f(x)};func Process(s,i []byte) uint64{return f(0)}`,
		"indirect-recursion": `func f()uint64{return g()};func g()uint64{return f()};func Process(s,i []byte)uint64{return f()}`,
		"init":               `func init(){};func Process(s,i []byte) uint64{return 0}`,
		"bodyless":           `func f();func Process(s,i []byte) uint64{return 0}`,
		"bad-entry":          `func Process(s []byte) uint64{return 0}`,
		"continue":           `func Process(s,i []byte) uint64 {for j:=uint64(0);j<3;j++ {continue};return 0}`,
		"multi-assign":       `func Process(s,i []byte) uint64 {a,b:=uint64(1),uint64(2);return a+b}`,
		"function-value":     `func f()uint64{return 1};func Process(s,i []byte) uint64 {f2:=f;return f2()}`,
		"context-zero":       `import "gosvm/solana";func Process(c solana.Context)uint64 {var d solana.Context;return solana.Count(d)}`,
		"context-literal":    `import "gosvm/solana";func Process(c solana.Context)uint64 {d:=solana.Context{};return solana.Count(d)}`,
		"context-fields":     `import "gosvm/solana";func Process(c solana.Context)uint64 {return uint64(len(c.Accounts))}`,
		"context-field-call": `import "gosvm/solana";func Process(c solana.Context)uint64 {return c.Invoke(0,1,2,3,4,solana.Key(c,0),0)}`,
		"sdk-function-value": `import "gosvm/solana";func Process(c solana.Context)uint64 {f:=solana.Count;return f(c)}`,
		"sdk-dot-import":     `import . "gosvm/solana";func Process(c Context)uint64 {return Count(c)}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Compile("reject.go", []byte("package p;"+source)); err == nil {
				t.Fatal("accepted unsupported Go")
			}
		})
	}
}

func TestSDKContext(t *testing.T) {
	// Host execution exercises the emitted account decoder, alias import, opaque
	// context copies, mutable account views, and boundary getters. CPI/PDA calls
	// are covered in the actual validator suite rather than mocked here.
	source := `package p
import chain "gosvm/solana"
func pass(c chain.Context) chain.Context {return c}
func Process(c chain.Context) uint64 {
    copy:=pass(c)
    if chain.Count(copy)!=1 || !chain.Writable(copy,0) || chain.Signer(copy,0) || chain.Executable(copy,0) {return 1}
    s:=chain.Data(copy,0);ix:=chain.Instruction(copy);key:=chain.Key(copy,0);owner:=chain.Owner(copy,0);id:=chain.ProgramID(copy)
    if len(s)!=24 || len(ix)!=16 || key[0]!=3 || owner[0]!=9 || id[0]!=9 {return 2}
    s[0]=ix[0]
    return 0
}`
	bin := hostProgram(t, source, `
int main(void){u8 raw[10424]={0};raw[0]=1;raw[8]=255;raw[10]=1;raw[16]=3;raw[48]=9;raw[88]=24;raw[10368]=16;raw[10376]=42;raw[10392]=9;
u64 r=entrypoint(raw);return r?r:(raw[96]==42?0:3);}`)
	if out, err := exec.Command(bin).CombinedOutput(); err != nil {
		t.Fatalf("SDK account ABI: %v: %s", err, out)
	}
}

func TestSDKHashBounds(t *testing.T) {
	source := `package p
import "gosvm/solana"
func Process(c solana.Context)uint64 {
 s:=solana.Data(c,0)
 if solana.SHA256(c,s,18446744073709551615,1,s,0)!=2003 {return 1}
 if solana.SHA256(c,s,0,18446744073709551615,s,0)!=2003 {return 2}
 if solana.SHA256(c,s,0,1,s,18446744073709551615)!=2003 {return 3}
 return 0
}`
	bin := hostProgram(t, source, `u64 sol_sha256(Seed*s,u64 n,u8*out){return 999;}
int main(void){Context c={0};u8 b[64]={0};c.count=1;c.accounts[0].data=b;c.accounts[0].data_len=64;return go_Process(&c);}`)
	if out, err := exec.Command(bin).CombinedOutput(); err != nil {
		t.Fatalf("hash bounds %v: %s", err, out)
	}
}

func TestGoSemantics(t *testing.T) {
	source := `package p
func bump(s []byte) uint64 { s[0] = s[0]+1; return uint64(s[0]) }
func Process(s,i []byte) uint64 {
    var zero uint64
    x:=byte(255); x=x+1
    y:=uint32(4294967295); y=y+1
    z:=uint64(18446744073709551615); z=z+1
    wide:=uint64(64); shifted:=uint64(5)<<wide
    a:=bump(s)*10+bump(s)
    if false && bump(s)>0 {return 90}
    if true || bump(s)>0 {zero=zero+1}
    {a:=uint64(99);zero=zero+a}
    for j:=uint64(0);j<4;j++ {if j==2 {break}; zero=zero+1}
    var empty []byte
    if len(empty)!=0 {return 91}
    if s[0]!=2 || a!=12 || zero!=102 || x!=0 || y!=0 || z!=0 || shifted!=0 {return 92}
    return 0
}`
	binary := hostProgram(t, source, `int main(void) {u8 s[1]={0};return (int)go_Process((slice){s,1},(slice){s,0});}`)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, binary).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
}

func TestRuntimeGuards(t *testing.T) {
	for name, expr := range map[string]string{"bounds": "uint64(s[uint64(i[0])])", "division": "uint64(s[0])/uint64(i[0])"} {
		t.Run(name, func(t *testing.T) {
			index := 0
			if name == "bounds" {
				index = 1
			}
			binary := hostProgram(t, "package p;func Process(s,i []byte) uint64{return "+expr+"}", fmt.Sprintf(`int main(void){u8 s[1]={1}, i[1]={%d};return (int)go_Process((slice){s,1},(slice){i,1});}`, index))
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := exec.CommandContext(ctx, binary).Run(); err == nil {
				t.Fatal("expected runtime trap")
			}
		})
	}
}

func TestAMMDifferential(t *testing.T) {
	source, err := os.ReadFile("../../examples/amm/amm.go")
	if err != nil {
		t.Fatal(err)
	}
	binaryPath := hostProgram(t, string(source), `
#include <stdio.h>
static void put(u8 *p,u64 n) {for(int i=0;i<8;i++) p[i]=(u8)(n>>(i*8));}
static u64 get(u8 *p) {u64 n=0;for(int i=0;i<8;i++) n|=(u64)p[i]<<(i*8);return n;}
int main(void) {
    u64 x,y,n,a,m;
    while(scanf("%llu %llu %llu %llu %llu",&x,&y,&n,&a,&m)==5) {
        u8 s[24], ix[16];put(s,x);put(s+8,y);put(s+16,n);put(ix,a);put(ix+8,m);
        u64 code=go_Process((slice){s,24},(slice){ix,16});
        printf("%llu %llu %llu %llu\n",code,get(s),get(s+8),get(s+16));
    }
    return 0;
}`)
	rng := rand.New(rand.NewSource(7))
	var input, expected strings.Builder
	for j := 0; j < 2000; j++ {
		x, y, n, a, m := rng.Uint64()%1000000001, rng.Uint64()%1000000001, rng.Uint64(), rng.Uint64()%1000002, rng.Uint64()%100000
		if j%4 == 0 {
			x, y, n, a, m = rng.Uint64(), rng.Uint64(), rng.Uint64(), rng.Uint64(), rng.Uint64()
		}
		fmt.Fprintf(&input, "%d %d %d %d %d\n", x, y, n, a, m)
		s, ix := make([]byte, 24), make([]byte, 16)
		for k, v := range []uint64{x, y, n} {
			binary.LittleEndian.PutUint64(s[k*8:], v)
		}
		binary.LittleEndian.PutUint64(ix, a)
		binary.LittleEndian.PutUint64(ix[8:], m)
		code := amm.Process(s, ix)
		fmt.Fprintf(&expected, "%d %d %d %d\n", code, binary.LittleEndian.Uint64(s), binary.LittleEndian.Uint64(s[8:]), binary.LittleEndian.Uint64(s[16:]))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binaryPath)
	cmd.Stdin = strings.NewReader(input.String())
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if !bytes.Equal(out, []byte(expected.String())) {
		t.Fatal("generated C differs from native Go")
	}
}
