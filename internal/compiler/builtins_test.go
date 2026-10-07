package compiler

import (
	"fmt"
	"math/big"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMultiplyBuiltinNativeDifferential(t *testing.T) {
	cc, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("host Clang required")
	}
	dir := t.TempDir()
	file, bin := filepath.Join(dir, "multi3.c"), filepath.Join(dir, "multi3")
	driver := `
#include <stdio.h>
int main(void){gosvm_builtin_u64 al,ah,bl,bh;
while(scanf("%llu %llu %llu %llu",&al,&ah,&bl,&bh)==4){
 gosvm_builtin_u128 a=((gosvm_builtin_u128)ah<<64)|al,b=((gosvm_builtin_u128)bh<<64)|bl;
 gosvm_builtin_u128 v=__multi3(a,b);
 printf("%llu %llu\n",(gosvm_builtin_u64)v,(gosvm_builtin_u64)(v>>64));}return 0;}`
	if err = os.WriteFile(file, append(append([]byte(nil), compilerBuiltins...), []byte(driver)...), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(cc, "-std=c11", "-O2", "-fno-builtin", "-fsanitize=undefined", "-fno-sanitize-recover=all", file, "-o", bin).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	var input, want strings.Builder
	rng := rand.New(rand.NewSource(20261006))
	edges := []uint64{0, 1, 2, (1 << 32) - 1, 1 << 32, (1 << 63) - 1, 1 << 63, ^uint64(0)}
	count := 0
	check := func(al, ah, bl, bh uint64) {
		fmt.Fprintf(&input, "%d %d %d %d\n", al, ah, bl, bh)
		a := new(big.Int).Lsh(new(big.Int).SetUint64(ah), 64)
		a.Or(a, new(big.Int).SetUint64(al))
		b := new(big.Int).Lsh(new(big.Int).SetUint64(bh), 64)
		b.Or(b, new(big.Int).SetUint64(bl))
		v := new(big.Int).Mul(a, b)
		lo := v.Uint64()
		v.Rsh(v, 64)
		fmt.Fprintf(&want, "%d %d\n", lo, v.Uint64())
		count++
	}
	for _, al := range edges {
		for _, ah := range edges {
			for _, bl := range edges {
				for _, bh := range edges {
					check(al, ah, bl, bh)
				}
			}
		}
	}
	for i := 0; i < 2000; i++ {
		check(rng.Uint64(), rng.Uint64(), rng.Uint64(), rng.Uint64())
	}
	cmd := exec.Command(bin)
	cmd.Stdin = strings.NewReader(input.String())
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if string(out) != want.String() {
		t.Fatal("freestanding __multi3 differs from math/big modulo 2^128")
	}
	t.Logf("%d multiplication vectors passed", count)
}

func TestSBFUnresolvedSymbolGuard(t *testing.T) {
	llvm := filepath.Join(os.Getenv("HOME"), ".cache/solana/v1.51/platform-tools/llvm/bin")
	if _, err := os.Stat(filepath.Join(llvm, "clang")); err != nil {
		t.Skip("pinned SBF LLVM required")
	}
	dir := t.TempDir()
	for _, name := range []string{"abort", "sol_set_return_data", "__multi3", "__unrecognized_helper"} {
		c, obj, so := filepath.Join(dir, name+".c"), filepath.Join(dir, name+".o"), filepath.Join(dir, name+".so")
		source := fmt.Sprintf("extern void %s(void); unsigned long long entrypoint(void){%s();return 0;}\n", name, name)
		if err := os.WriteFile(c, []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
		for _, args := range [][]string{{"clang", "-target", "sbf", "-mcpu=generic", "-O2", "-fPIC", "-c", c, "-o", obj}, {"ld.lld", "-z", "notext", "-shared", "--Bdynamic", "--entry", "entrypoint", "-o", so, obj}} {
			if out, err := exec.Command(filepath.Join(llvm, args[0]), args[1:]...).CombinedOutput(); err != nil {
				t.Fatalf("%v %s", err, out)
			}
		}
		names, err := undefinedSymbols(so, true)
		if err != nil || len(names) != 1 || names[0] != name {
			t.Fatal(names, err)
		}
		err = validateSBFImports(so, "v0")
		if legacySyscalls[name] {
			if err != nil {
				t.Fatal(err)
			}
		} else if err == nil || !strings.Contains(err.Error(), name) {
			t.Fatal("unresolved non-syscall accepted", name, err)
		}
		if err = validateSBFImports(so, "v3"); err == nil {
			t.Fatal("v3 accepted dynamic import", name)
		}
	}
}
