package compiler

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"gosvm/sdk/system"
	"gosvm/solana"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSDK2LifecycleLoaderSelection(t *testing.T) {
	plain := `package app;import "gosvm/solana";func Process(c solana.Context)uint64{return solana.Count(c)}`
	for _, sdk := range []int{0, 1, 2} {
		p := &Program{SDK: sdk, Entry: "app", Packages: []PackageSources{unit("app", plain)}}
		c, err := CompileProgram(p)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(c, []byte("#define GOSVM_SDK2 1")) != (sdk == 2) {
			t.Fatal("changed legacy loader or omitted SDK2 original length", sdk)
		}
	}
	p := &Program{Entry: "app", Packages: []PackageSources{unit("app", `package app;import "gosvm/solana";func Process(c solana.Context)uint64{return solana.ResizeAccount(c,0,0)}`)}}
	c, err := CompileProgram(p)
	if err != nil || !bytes.Contains(c, []byte("#define GOSVM_SDK2 1")) {
		t.Fatal("unpinned current API must record original length when resizing", err)
	}
	p.SDK = 1
	if _, err := CompileProgram(p); err == nil {
		t.Fatal("SDK1 admitted ResizeAccount")
	}
}

func TestSDK2OwnedLifecycleDifferential(t *testing.T) {
	source := `package app
import "gosvm/solana"
func Process(c solana.Context)uint64{
 ix:=solana.Instruction(c);n:=uint64(0);for j:=uint64(0);j<8;j++{n=n|uint64(ix[1+j])<<(j*8)}
 switch ix[0]{
 case 0:return solana.ResizeAccount(c,0,n)
 case 1:return solana.CloseAccount(c,0,1)
 case 2:code:=solana.ResizeAccount(c,0,2);if code!=0{return code};return solana.ResizeAccount(c,0,n)
 case 3:return solana.CloseAccount(c,0,2)
 case 4:return solana.Rent(c,solana.Data(c,0)[:n])
 }
 return 999
}`
	driver := `
#include <stdio.h>
#include <string.h>
static u64 rent_code;
u64 sol_get_rent_sysvar(GosvmRent *r){r->rate=~0ULL;r->threshold_bits=0x3ff8000000000000ULL;r->burn=17;return rent_code;}
static u64 hash(u8 *b,u64 n){u64 h=14695981039346656037ULL;for(u64 j=0;j<n;j++)h=(h^b[j])*1099511628211ULL;return h;}
int main(void){u64 mode,n,original,w,x,owned,dw,dx,left,right,rc;
 while(scanf("%llu %llu %llu %llu %llu %llu %llu %llu %llu %llu %llu",&mode,&n,&original,&w,&x,&owned,&dw,&dx,&left,&right,&rc)==11){
  _Alignas(8) u8 key[40]={0},dest[32]={9},owner[32]={11},id[32]={11},buffer[10304]={0},ix[9]={0};
  key[8]=7;for(u64 j=0;j<4;j++)key[4+j]=(u8)(original>>(8*j));
  for(u64 j=0;j<8;j++){ix[1+j]=(u8)(n>>(8*j));buffer[j]=(u8)(32ULL>>(8*j));}
  for(u64 j=0;j<32;j++)buffer[8+j]=(u8)(j+1);
  buffer[10303]=91;ix[0]=(u8)mode;if(!owned)owner[0]=12;rent_code=rc;
  Context c={0};c.count=4;c.program=GOSVM_SLICE(id,32);c.instruction=GOSVM_SLICE(ix,9);
  c.accounts[0]=(AccountInfo){key+8,&left,32,buffer+8,owner,0,0,w,x};
  c.accounts[1]=(AccountInfo){dest,&right,0,0,owner,0,0,dw,dx};
  c.accounts[2]=c.accounts[0];c.accounts[3]=c.accounts[1];
  u64 code=go_Process(&c);if(buffer[10303]!=91)return 93;
  printf("%llu %llu %llu %llu %llu %u %llu %llu\n",code,c.accounts[0].data_len,c.accounts[2].data_len,left,right,owner[0],hash(c.accounts[0].data,c.accounts[0].data_len),sol_read64(buffer));
 }return 0;}`
	p := &Program{SDK: 2, Entry: "app", Packages: []PackageSources{unit("app", source)}}
	c, err := CompileProgram(p)
	if err != nil {
		t.Fatal(err)
	}
	cc, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("host Clang required")
	}
	dir := t.TempDir()
	file, bin := filepath.Join(dir, "life.c"), filepath.Join(dir, "life")
	if err := os.WriteFile(file, append(c, []byte(driver)...), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(cc, "-std=c11", "-O2", "-fsanitize=undefined", "-fno-sanitize-recover=all", file, "-o", bin).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	var input, want strings.Builder
	add := func(mode, n, original, w, x, owned, dw, dx, left, right, rc uint64) {
		fmt.Fprintf(&input, "%d %d %d %d %d %d %d %d %d %d %d\n", mode, n, original, w, x, owned, dw, dx, left, right, rc)
		id, key, dest, owner, data := make([]byte, 32), make([]byte, 32), make([]byte, 32), make([]byte, 32), make([]byte, 32)
		id[0], key[0], dest[0], owner[0] = 11, 7, 9, 11
		if owned == 0 {
			owner[0] = 12
		}
		for j := range data {
			data[j] = byte(j + 1)
		}
		src := solana.Account{Key: key, Owner: owner, Data: data, Lamports: left, Writable: w != 0, Executable: x != 0, OriginalDataLen: original}
		dst := solana.Account{Key: dest, Owner: make([]byte, 32), Lamports: right, Writable: dw != 0, Executable: dx != 0}
		ctx := solana.Context{ID: id, Accounts: []solana.Account{src, dst, src, dst}, ReadRent: func(b []byte) uint64 {
			binary.LittleEndian.PutUint64(b, math.MaxUint64)
			binary.LittleEndian.PutUint64(b[8:], math.Float64bits(1.5))
			b[16] = 17
			return rc
		}}
		code := uint64(999)
		switch mode {
		case 0:
			code = solana.ResizeAccount(ctx, 0, n)
		case 1:
			code = solana.CloseAccount(ctx, 0, 1)
		case 2:
			code = solana.ResizeAccount(ctx, 0, 2)
			if code == 0 {
				code = solana.ResizeAccount(ctx, 0, n)
			}
		case 3:
			code = solana.CloseAccount(ctx, 0, 2)
		case 4:
			code = solana.Rent(ctx, ctx.Accounts[0].Data[:n])
		}
		a := ctx.Accounts[0]
		h := uint64(14695981039346656037)
		for _, b := range a.Data {
			h = (h ^ uint64(b)) * 1099511628211
		}
		fmt.Fprintf(&want, "%d %d %d %d %d %d %d %d\n", code, len(a.Data), len(ctx.Accounts[2].Data), a.Lamports, ctx.Accounts[1].Lamports, a.Owner[0], h, len(a.Data))
	}
	rng := rand.New(rand.NewSource(2026100552))
	for j := 0; j < 1000; j++ {
		w := uint64(1)
		if j%7 == 0 {
			w = 0
		}
		add(uint64(j%4), rng.Uint64()%256, rng.Uint64()%256, w, 0, 1, 1, 0, rng.Uint64(), rng.Uint64(), 0)
	}
	for _, n := range []uint64{0, 1, 31, 32, 33, 10240, 10272, 10273, 10485761, math.MaxUint64} {
		add(0, n, 32, 1, 0, 1, 1, 0, 10, 20, 0)
		add(2, n, 32, 1, 0, 1, 1, 0, 10, 20, 0)
	}
	for _, n := range []uint64{0, 16, 17, 18, 32} {
		add(4, n, 32, 1, 0, 1, 1, 0, 10, 20, 0)
		add(4, n, 32, 1, 0, 1, 1, 0, 10, 20, 9999)
	}
	for _, edit := range [][5]uint64{{0, 0, 1, 1, 0}, {1, 1, 1, 1, 0}, {1, 0, 0, 1, 0}, {1, 0, 1, 0, 0}, {1, 0, 1, 1, 1}} {
		add(1, 0, 32, edit[0], edit[1], edit[2], edit[3], edit[4], 10, 20, 0)
	}
	cmd := exec.Command(bin)
	cmd.Stdin = strings.NewReader(input.String())
	out, err := cmd.CombinedOutput()
	if err != nil || string(out) != want.String() {
		t.Fatalf("native/C lifecycle mismatch or UB: %v\n%s", err, out)
	}
}

func TestSDK2RentCalculationC(t *testing.T) {
	p, err := ReadProgram("../../examples/lifecycle2")
	if err != nil {
		t.Fatal(err)
	}
	p.SDK = 2
	// A non-SDK arithmetic entrypoint keeps the oracle independent of syscalls.
	for j := range p.Packages {
		if p.Packages[j].Path != p.Entry {
			continue
		}
		p.Packages[j] = unit(p.Entry, `package lifecycle2;import "gosvm/sdk/system"
func read(b []byte,o uint64)uint64{n:=uint64(0);for j:=uint64(0);j<8;j++{n=n|uint64(b[o+j])<<(j*8)};return n}
func Process(s,i []byte)uint64{amount,code:=system.MinimumBalanceFor(read(i,0),read(i,8),read(i,16));for j:=uint64(0);j<8;j++{s[j]=byte(amount>>(j*8))};return code}`)
	}
	c, err := CompileProgram(p)
	if err != nil {
		t.Fatal(err)
	}
	bin := hostGenerated(t, c, `
#include <stdio.h>
int main(void){u64 rate,bits,size;while(scanf("%llu %llu %llu",&rate,&bits,&size)==3){u8 ix[24],out[8];for(u64 j=0;j<8;j++){ix[j]=(u8)(rate>>(8*j));ix[8+j]=(u8)(bits>>(8*j));ix[16+j]=(u8)(size>>(8*j));}u64 code=go_Process(GOSVM_SLICE(out,8),GOSVM_SLICE(ix,24));u64 amount=0;for(u64 j=0;j<8;j++)amount|=(u64)out[j]<<(8*j);printf("%llu %llu\n",amount,code);}return 0;}`)
	rng := rand.New(rand.NewSource(2026100553))
	var input, want strings.Builder
	for j := 0; j < 1000; j++ {
		size := rng.Uint64() % 10485761
		rate := rng.Uint64() % (math.MaxUint64/(128+size) + 1)
		bits := rng.Uint64()
		if j%3 == 0 {
			bits = uint64(1010+rng.Intn(60))<<52 | bits&0xfffffffffffff
		}
		fmt.Fprintf(&input, "%d %d %d\n", rate, bits, size)
		n, code := system.MinimumBalanceFor(rate, bits, size)
		fmt.Fprintf(&want, "%d %d\n", n, code)
	}
	cmd := exec.Command(bin)
	cmd.Stdin = strings.NewReader(input.String())
	if out, err := cmd.CombinedOutput(); err != nil || string(out) != want.String() {
		t.Fatalf("rent C mismatch: %v %s", err, out)
	}
}
