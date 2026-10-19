package compiler

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"gosvm/solana"
	"math/bits"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// keccak256 is a test-only reference Keccak-256 (rate 136, 0x01 padding).
func keccak256(data []byte) []byte {
	rc := [24]uint64{0x0000000000000001, 0x0000000000008082, 0x800000000000808a, 0x8000000080008000, 0x000000000000808b,
		0x0000000080000001, 0x8000000080008081, 0x8000000000008009, 0x000000000000008a, 0x0000000000000088, 0x0000000080008009,
		0x000000008000000a, 0x000000008000808b, 0x800000000000008b, 0x8000000000008089, 0x8000000000008003, 0x8000000000008002,
		0x8000000000000080, 0x000000000000800a, 0x800000008000000a, 0x8000000080008081, 0x8000000000008080, 0x0000000080000001,
		0x8000000080008008}
	rot := [25]int{0, 1, 62, 28, 27, 36, 44, 6, 55, 20, 3, 10, 43, 25, 39, 41, 45, 15, 21, 8, 18, 2, 61, 56, 14}
	var a [25]uint64
	permute := func() {
		for round := 0; round < 24; round++ {
			var c, d [5]uint64
			for x := 0; x < 5; x++ {
				c[x] = a[x] ^ a[x+5] ^ a[x+10] ^ a[x+15] ^ a[x+20]
			}
			for x := 0; x < 5; x++ {
				d[x] = c[(x+4)%5] ^ bits.RotateLeft64(c[(x+1)%5], 1)
			}
			var b [25]uint64
			for x := 0; x < 5; x++ {
				for y := 0; y < 5; y++ {
					b[y+5*((2*x+3*y)%5)] = bits.RotateLeft64(a[x+5*y]^d[x], rot[x+5*y])
				}
			}
			for i := 0; i < 25; i++ {
				a[i] = b[i] ^ (^b[(i%5+1)%5+5*(i/5)] & b[(i%5+2)%5+5*(i/5)])
			}
			a[0] ^= rc[round]
		}
	}
	padded := append(append([]byte(nil), data...), 0x01)
	for len(padded)%136 != 0 {
		padded = append(padded, 0)
	}
	padded[len(padded)-1] |= 0x80
	for off := 0; off < len(padded); off += 136 {
		for i := 0; i < 17; i++ {
			for j := 0; j < 8; j++ {
				a[i] ^= uint64(padded[off+i*8+j]) << (8 * j)
			}
		}
		permute()
	}
	out := make([]byte, 32)
	for i := 0; i < 32; i++ {
		out[i] = byte(a[i/8] >> (8 * (i % 8)))
	}
	return out
}

func TestKeccakReference(t *testing.T) {
	t.Parallel()
	for input, want := range map[string]string{
		"":    "c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470",
		"abc": "4e03657aea45a94fc7d47ba826c8d667c0d1e6e33a64a036ec44f58fa12d6c45",
		"The quick brown fox jumps over the lazy dog": "4d741b6f1eb29cb2a9b9911c82f56fa8d73b04959d3d9d222895df6c0b28aa15",
	} {
		if got := hex.EncodeToString(keccak256([]byte(input))); got != want {
			t.Fatalf("%q: %s", input, got)
		}
	}
}

func TestSDK2Keccak256NativeCDifferential(t *testing.T) {
	t.Parallel()
	source := strings.Replace(string(mustRead(t, filepath.Join("testdata", "keccak", "program.go"))), "package program", "package app", 1)
	p := &Program{SDK: 2, Entry: "app", Packages: []PackageSources{unit("app", source)}}
	c, err := CompileProgram(p)
	if err != nil {
		t.Fatal(err)
	}
	p.SDK = 1
	if _, err = CompileProgram(p); err == nil || !strings.Contains(err.Error(), "Keccak256") {
		t.Fatal("SDK1 admitted Keccak256", err)
	}
	cc, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("host Clang required")
	}
	// The stub writes byte(i+len) and records the one segment it received.
	driver := `
#include <stdio.h>
#include <string.h>
static u64 calls,segments,seen;
u64 sol_keccak256(Seed *s,u64 n,u8 *out){calls++;segments=n;seen=s[0].len;for(u64 i=0;i<32;i++)out[i]=(u8)(i+s[0].len);return 0;}
int main(void){unsigned long long offset,length,out_offset;while(scanf("%llu %llu %llu",&offset,&length,&out_offset)==3){
 u8 input[300],account[80];for(u64 i=0;i<sizeof(input);i++)input[i]=(u8)(i*13+1);u64 balance=0;memset(account,91,sizeof(account));calls=0;
 u8 ix[24];for(u64 j=0;j<8;j++){ix[j]=(u8)(offset>>(j*8));ix[8+j]=(u8)(length>>(j*8));ix[16+j]=(u8)(out_offset>>(j*8));}
 Context c={0};c.count=2;c.accounts[0].data=input;c.accounts[0].data_len=sizeof(input);c.accounts[0].lamports=&balance;
 c.accounts[1].data=account;c.accounts[1].data_len=sizeof(account);c.accounts[1].lamports=&balance;
 c.instruction=GOSVM_SLICE(ix,24);u64 code=go_Process(&c);printf("%llu %llu %llu %llu ",code,calls,calls?segments:0,calls?seen:0);
 for(u64 i=0;i<sizeof(account);i++)printf("%02x",account[i]);puts("");}return 0;}`
	dir := t.TempDir()
	file, bin := filepath.Join(dir, "keccak.c"), filepath.Join(dir, "keccak")
	if err = os.WriteFile(file, append(c, []byte(driver)...), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(cc, "-std=c11", "-O2", "-fsanitize=undefined", "-fno-sanitize-recover=all", file, "-o", bin).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	var input, want strings.Builder
	vectors := 0
	huge := uint64(1) << 63
	for _, offset := range []uint64{0, 1, 299, 300, 301, huge} {
		for _, length := range []uint64{0, 1, 136, 299, 300, huge} {
			for _, outOffset := range []uint64{0, 40, 41, 72, huge} {
				vectors++
				fmt.Fprintf(&input, "%d %d %d\n", offset, length, outOffset)
				data := make([]byte, 300)
				for i := range data {
					data[i] = byte(i*13 + 1)
				}
				account := bytes.Repeat([]byte{91}, 80)
				calls, seen := 0, 0
				ctx := solana.Context{Accounts: []solana.Account{{Data: data}, {Data: account}}, Keccak: func(b []byte) []byte {
					calls++
					seen = len(b)
					out := make([]byte, 32)
					for i := range out {
						out[i] = byte(i + len(b))
					}
					return out
				}}
				var ix [24]byte
				for j := 0; j < 8; j++ {
					ix[j], ix[8+j], ix[16+j] = byte(offset>>(j*8)), byte(length>>(j*8)), byte(outOffset>>(j*8))
				}
				ctx.InstructionData = ix[:]
				code := runKeccakProbe(ctx)
				fmt.Fprintf(&want, "%d %d %d %d %x\n", code, calls, calls, seen, account)
			}
		}
	}
	cmd := exec.Command(bin)
	cmd.Stdin = strings.NewReader(input.String())
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if string(out) != want.String() {
		t.Fatalf("native/C Keccak-256 bounds, segment or output differ\n%s\nwant\n%s", out, want.String())
	}
	t.Logf("%d Keccak-256 boundary vectors passed", vectors)
}

// runKeccakProbe is testdata/keccak's Process in ordinary Go.
func runKeccakProbe(c solana.Context) uint64 {
	ix := solana.Instruction(c)
	if len(ix) != 24 || solana.Count(c) != 2 {
		return 1
	}
	read := func(off int) uint64 {
		v := uint64(0)
		for j := 0; j < 8; j++ {
			v |= uint64(ix[off+j]) << (j * 8)
		}
		return v
	}
	result := solana.Data(c, 1)
	code := solana.Keccak256(c, solana.Data(c, 0), read(0), read(8), result[8:], read(16))
	for j := 0; j < 8; j++ {
		result[j] = byte(code >> (j * 8))
	}
	return 0
}
