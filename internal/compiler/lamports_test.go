package compiler

import (
	"bytes"
	"fmt"
	"gosvm/solana"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// MoveLamports' runtime is emitted only for programs that call it, so the C
// of every other program is unchanged; SDK 1 does not have it.
func TestSDK2MoveLamportsEmittedOnlyWhenCalled(t *testing.T) {
	t.Parallel()
	plain := `package app;import "gosvm/solana";func Process(c solana.Context)uint64{return solana.CloseAccount(c,0,1)}`
	moving := `package app;import "gosvm/solana";func Process(c solana.Context)uint64{return solana.MoveLamports(c,0,1,2)}`
	for _, tc := range []struct {
		source string
		want   bool
	}{{plain, false}, {moving, true}} {
		c, err := CompileProgram(&Program{SDK: 2, Entry: "app", Packages: []PackageSources{unit("app", tc.source)}})
		if err != nil {
			t.Fatal(err)
		}
		if got := bytes.Contains(c, []byte("sol_MoveLamports")); got != tc.want {
			t.Fatalf("MoveLamports runtime emitted %v, want %v", got, tc.want)
		}
	}
	if _, err := CompileProgram(&Program{SDK: 1, Entry: "app", Packages: []PackageSources{unit("app", moving)}}); err == nil || !strings.Contains(err.Error(), "MoveLamports") {
		t.Fatal("SDK1 admitted MoveLamports", err)
	}
}

// The generated C and the native SDK agree on every status and balance,
// through duplicate descriptors, under undefined-behaviour sanitization.
// Accounts 2 and 3 duplicate 0 and 1, so moves between them alias.
func TestSDK2MoveLamportsNativeCDifferential(t *testing.T) {
	t.Parallel()
	source := `package app
import "gosvm/solana"
func Process(c solana.Context)uint64{
 ix:=solana.Instruction(c);n:=uint64(0);for j:=uint64(0);j<8;j++{n=n|uint64(ix[2+j])<<(j*8)}
 return solana.MoveLamports(c,uint64(ix[0]),uint64(ix[1]),n)
}`
	driver := `
#include <stdio.h>
int main(void){u64 from,to,amount,left,right,owned,w,x,dw,dx;
 while(scanf("%llu %llu %llu %llu %llu %llu %llu %llu %llu %llu",&from,&to,&amount,&left,&right,&owned,&w,&x,&dw,&dx)==10){
  u8 key[32]={7},dest[32]={9},owner[32]={11},other[32]={12},id[32]={11},ix[10]={0};
  ix[0]=(u8)from;ix[1]=(u8)to;for(u64 j=0;j<8;j++)ix[2+j]=(u8)(amount>>(8*j));
  Context c={0};c.count=4;c.program=GOSVM_SLICE(id,32);c.instruction=GOSVM_SLICE(ix,10);
  c.accounts[0]=(AccountInfo){key,&left,0,0,owned?owner:other,0,0,w,x};
  c.accounts[1]=(AccountInfo){dest,&right,0,0,other,0,0,dw,dx};
  c.accounts[2]=c.accounts[0];c.accounts[3]=c.accounts[1];
  u64 code=go_Process(&c);
  printf("%llu %llu %llu %llu %llu\n",code,*c.accounts[0].lamports,*c.accounts[1].lamports,*c.accounts[2].lamports,*c.accounts[3].lamports);
 }return 0;}`
	c, err := CompileProgram(&Program{SDK: 2, Entry: "app", Packages: []PackageSources{unit("app", source)}})
	if err != nil {
		t.Fatal(err)
	}
	cc, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("host Clang required")
	}
	dir := t.TempDir()
	file, bin := filepath.Join(dir, "move.c"), filepath.Join(dir, "move")
	if err := os.WriteFile(file, append(c, []byte(driver)...), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(cc, "-std=c11", "-O2", "-fsanitize=undefined", "-fno-sanitize-recover=all", file, "-o", bin).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	var input, want strings.Builder
	cases := 0
	add := func(from, to, amount, left, right, owned, w, x, dw, dx uint64) {
		cases++
		fmt.Fprintf(&input, "%d %d %d %d %d %d %d %d %d %d\n", from, to, amount, left, right, owned, w, x, dw, dx)
		id, key, dest, owner, other := make([]byte, 32), make([]byte, 32), make([]byte, 32), make([]byte, 32), make([]byte, 32)
		id[0], key[0], dest[0], owner[0], other[0] = 11, 7, 9, 11, 12
		if owned == 0 {
			owner = other
		}
		src := solana.Account{Key: key, Owner: owner, Lamports: left, Writable: w != 0, Executable: x != 0}
		dst := solana.Account{Key: dest, Owner: other, Lamports: right, Writable: dw != 0, Executable: dx != 0}
		ctx := solana.Context{ID: id, Accounts: []solana.Account{src, dst, src, dst}}
		code := solana.MoveLamports(ctx, from, to, amount)
		a := ctx.Accounts
		fmt.Fprintf(&want, "%d %d %d %d %d\n", code, a[0].Lamports, a[1].Lamports, a[2].Lamports, a[3].Lamports)
	}
	max := uint64(math.MaxUint64)
	// Every pair of the four descriptors (including self and alias moves),
	// with edge balances and amounts, owned and not.
	for from := uint64(0); from < 5; from++ {
		for to := uint64(0); to < 5; to++ {
			for _, b := range [][3]uint64{{0, 0, 0}, {10, 20, 0}, {10, 20, 10}, {10, 20, 11}, {max, 0, max}, {max, 1, max}, {max, 1, 0}, {5, max, 1}, {5, max, 0}, {max, max, max}, {max, max, 1}} {
				for owned := uint64(0); owned < 2; owned++ {
					add(from, to, b[2], b[0], b[1], owned, 1, 0, 1, 0)
				}
			}
		}
	}
	// Privileges: each flag of each side, with a move that would succeed.
	for _, f := range [][4]uint64{{1, 0, 1, 0}, {0, 0, 1, 0}, {1, 1, 1, 0}, {1, 0, 0, 0}, {1, 0, 1, 1}, {0, 1, 0, 1}} {
		for _, pair := range [][2]uint64{{0, 1}, {1, 0}, {0, 2}, {0, 3}, {2, 1}} {
			add(pair[0], pair[1], 3, 10, 20, 1, f[0], f[1], f[2], f[3])
		}
	}
	rng := rand.New(rand.NewSource(2026101901))
	for j := 0; j < 1000; j++ {
		value := func() uint64 {
			switch rng.Intn(4) {
			case 0:
				return rng.Uint64()
			case 1:
				return max - rng.Uint64()%1000
			default:
				return rng.Uint64() % 2000
			}
		}
		add(rng.Uint64()%4, rng.Uint64()%4, value(), value(), value(), uint64(rng.Intn(4)/3^1), uint64(rng.Intn(8)/7^1), uint64(rng.Intn(8)/7), uint64(rng.Intn(8)/7^1), uint64(rng.Intn(8)/7))
	}
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
	t.Logf("%d native/C MoveLamports vectors passed under UBSan", cases)
}
