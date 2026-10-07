package compiler

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const packedStoreSource = `package p
func Store64(b []byte,o,v uint64) {
 for j:=uint64(0);j<8;j++ { b[o+j]=byte(v>>(8*j)) }
}
func Store32(b []byte,o uint64,v uint32) {
 for j:=uint64(0);j<4;j++ { b[o+j]=byte(v>>(8*j)) }
}
func read(b []byte,o uint64)uint64 {
 v:=uint64(0);for j:=uint64(0);j<8;j++ {v=v|uint64(b[o+j])<<(8*j)};return v
}
func Process(s,i []byte)uint64 {
 o,v:=read(i,1),read(i,9)
 if i[0]==0 {Store64(s,o,v)} else {Store32(s,o,uint32(v))};return 0
}`

func compilePackedStore(t *testing.T, sdk int, source string) []byte {
	t.Helper()
	c, err := CompileProgram(&Program{SDK: sdk, Entry: "app", Packages: []PackageSources{{Path: "app", Sources: []Source{{Name: "store.go", Data: []byte(source)}}}}})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestPackedStoreRecognition(t *testing.T) {
	for _, sdk := range []int{0, 1, 2} {
		c := compilePackedStore(t, sdk, packedStoreSource)
		want := 0
		if sdk == 2 {
			want = 2
		}
		if n := bytes.Count(c, []byte("__builtin_memcpy(")); n != want {
			t.Fatalf("SDK %d fast stores: %d, want %d", sdk, n, want)
		}
	}
	// Similar-looking loops with different effects or widths must retain their
	// ordinary translation. Match variable identities, not spelling/function names.
	for name, body := range map[string]string{
		"short width":      `for j:=uint64(0);j<7;j++ {b[o+j]=byte(v>>(8*j))}`,
		"nonzero start":    `for j:=uint64(1);j<8;j++ {b[o+j]=byte(v>>(8*j))}`,
		"different stride": `for j:=uint64(0);j<8;j++ {b[o+j]=byte(v>>(4*j))}`,
		"mutation":         `for j:=uint64(0);j<8;j++ {b[o+j]=byte(v>>(8*j));v++}`,
		"side effect":      `for j:=uint64(0);j<8;j++ {b[o+j]=byte(next(v)>>(8*j))}`,
		"different offset": `for j:=uint64(0);j<8;j++ {b[j]=byte(v>>(8*j))}`,
		"extra store":      `b[0]=7;for j:=uint64(0);j<8;j++ {b[o+j]=byte(v>>(8*j))}`,
		"shadowed value":   `for j:=uint64(0);j<8;j++ {v:=j;b[o+j]=byte(v>>(8*j))}`,
	} {
		t.Run(name, func(t *testing.T) {
			s := `package p;func next(v uint64)uint64{return v+1};func f(b []byte,o,v uint64){` + body + `};func Process(s,i []byte)uint64{f(s,0,1);return 0}`
			if bytes.Contains(compilePackedStore(t, 2, s), []byte("__builtin_memcpy(")) {
				t.Fatal("different loop was optimized")
			}
		})
	}
	s := `package p;func f(b []byte,o,v uint64){for j:=uint64(0);j<8;j++{b[o+j]+=byte(v>>(8*j))}};func Process(s,i []byte)uint64{f(s,0,1);return 0}`
	_, err := CompileProgram(&Program{SDK: 2, Entry: "app", Packages: []PackageSources{{Path: "app", Sources: []Source{{Name: "store.go", Data: []byte(s)}}}}})
	if err == nil || !strings.Contains(err.Error(), "compound assignment unsupported") {
		t.Fatal("fast path bypassed unsupported syntax diagnostic", err)
	}
}

const packedStoreDriver = `
#include <stdio.h>
#include <stdlib.h>
#include <setjmp.h>
static jmp_buf driver_jump;static u8 driver_data[80];static int driver_panic;
void abort(void){driver_panic=1;longjmp(driver_jump,1);}
int main(void){unsigned op,alignment;u64 length,offset,value;char hex[161];u8 ix[17];
while(scanf("%u %u %llu %llu %llu %160s",&op,&alignment,&length,&offset,&value,hex)==6){
for(int j=0;j<80;j++){unsigned x;sscanf(hex+2*j,"%2x",&x);driver_data[j]=(u8)x;}
ix[0]=(u8)op;for(u64 j=0;j<8;j++){ix[1+j]=(u8)(offset>>(8*j));ix[9+j]=(u8)(value>>(8*j));}
driver_panic=0;
if(!setjmp(driver_jump))go_Process(GOSVM_SLICE(alignment==8?0:driver_data+alignment,length),GOSVM_SLICE(ix,17));
printf("%c:",driver_panic?'P':'W');for(int j=0;j<80;j++)printf("%02x",driver_data[j]);puts("");}
return 0;}`

func TestPackedStoreNativeCDifferential(t *testing.T) {
	cc, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("host Clang needed for packed stores")
	}
	rng := rand.New(rand.NewSource(20261006))
	var input, want strings.Builder
	for trial := 0; trial < 12000; trial++ {
		op, alignment, length := trial%2, (trial/2)%8, rng.Intn(65)
		offsets := []uint64{0, uint64(rng.Intn(80)), uint64(length), ^uint64(0), ^uint64(0) - 3, ^uint64(0) - 7, 1 << 63, uint64(length / 2)}
		offset, value := offsets[trial%len(offsets)], rng.Uint64()
		if trial%97 == 0 {
			alignment, length = 8, 0 // nil slice bounds failure
		}
		data := make([]byte, 80)
		rng.Read(data)
		fmt.Fprintf(&input, "%d %d %d %d %d %x\n", op, alignment, length, offset, value, data)
		// Independent encoding/binary oracle produces the complete wire word;
		// scatter it in Go index order to preserve partial writes before failure.
		var word [8]byte
		width := 8
		if op == 0 {
			binary.LittleEndian.PutUint64(word[:], value)
		} else {
			width = 4
			binary.LittleEndian.PutUint32(word[:], uint32(value))
		}
		panic := false
		for j := 0; j < width; j++ {
			index := offset + uint64(j)
			if index >= uint64(length) {
				panic = true
				break
			}
			data[alignment+int(index)] = word[j]
		}
		status := 'W'
		if panic {
			status = 'P'
		}
		fmt.Fprintf(&want, "%c:%x\n", status, data)
	}
	dir := t.TempDir()
	c := compilePackedStore(t, 2, packedStoreSource)
	for _, fallback := range []bool{false, true} {
		file, bin := filepath.Join(dir, fmt.Sprint(fallback)+".c"), filepath.Join(dir, fmt.Sprint(fallback))
		if err := os.WriteFile(file, append(c, []byte(packedStoreDriver)...), 0600); err != nil {
			t.Fatal(err)
		}
		args := []string{"-std=c11", "-O2", "-fsanitize=undefined", "-fno-sanitize-recover=all"}
		if fallback {
			args = append(args, "-U__BYTE_ORDER__") // unknown endianness must retain the original loop
		}
		args = append(args, file, "-o", bin)
		if out, err := exec.Command(cc, args...).CombinedOutput(); err != nil {
			t.Fatalf("compile: %v %s", err, out)
		}
		cmd := exec.Command(bin)
		cmd.Stdin = strings.NewReader(input.String())
		if out, err := cmd.CombinedOutput(); err != nil || string(out) != want.String() {
			t.Fatalf("packed store/fallback=%v partial writes or bounds differ: %v %s", fallback, err, out)
		}
	}
}
