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

func TestSDK2Secp256k1RecoverNativeCDifferential(t *testing.T) {
	source := strings.Replace(string(mustRead(t, filepath.Join("testdata", "secp256k1", "program.go"))), "package program", "package app", 1)
	p := &Program{SDK: 2, Entry: "app", Packages: []PackageSources{unit("app", source)}}
	c, err := CompileProgram(p)
	if err != nil {
		t.Fatal(err)
	}
	p.SDK = 1
	if _, err = CompileProgram(p); err == nil || !strings.Contains(err.Error(), "Secp256k1Recover") {
		t.Fatal("SDK1 admitted recovery", err)
	}
	cc, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("host Clang required")
	}
	// The stub returns the recovery ID's low byte as a status when it is
	// nonzero and below 4 (none written), else writes key[i]=hash[i%32]^sig[i].
	driver := `
#include <stdio.h>
#include <string.h>
static u64 calls;
u64 sol_secp256k1_recover(const u8 *hash,u64 id,const u8 *sig,u8 *out){calls++;if(id%256&&id%256<4)return id%256;
 for(u64 i=0;i<64;i++)out[i]=hash[i%32]^sig[i];return 0;}
int main(void){u8 ix[107];unsigned v;while(1){for(u64 i=0;i<107;i++){if(scanf("%2x",&v)!=1)return 0;ix[i]=(u8)v;}
 u8 account[72];u64 balance=0;memset(account,91,sizeof(account));calls=0;
 Context c={0};c.count=1;c.accounts[0].data=account;c.accounts[0].data_len=72;c.accounts[0].lamports=&balance;
 c.instruction=GOSVM_SLICE(ix,107);u64 code=go_Process(&c);printf("%llu %llu ",code,calls);
 for(u64 i=0;i<72;i++)printf("%02x",account[i]);puts("");}}`
	dir := t.TempDir()
	file, bin := filepath.Join(dir, "secp.c"), filepath.Join(dir, "secp")
	if err = os.WriteFile(file, append(c, []byte(driver)...), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(cc, "-std=c11", "-O2", "-fsanitize=undefined", "-fno-sanitize-recover=all", file, "-o", bin).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	var input, want strings.Builder
	vectors := 0
	for _, lengths := range [][3]byte{{32, 64, 64}, {31, 64, 64}, {32, 63, 64}, {32, 64, 63}, {0, 0, 0}} {
		for _, id := range []uint64{0, 1, 2, 3, 4, 256, 257, 1 << 40} {
			vectors++
			ix := make([]byte, 107)
			for i := range ix[:32] {
				ix[i] = byte(i*7 + int(id))
			}
			for i := 0; i < 8; i++ {
				ix[32+i] = byte(id >> (i * 8))
			}
			for i := range ix[40:104] {
				ix[40+i] = byte(i*11 + 1)
			}
			ix[104], ix[105], ix[106] = lengths[0], lengths[1], lengths[2]
			fmt.Fprintf(&input, "%x\n", ix)
			account := bytes.Repeat([]byte{91}, 72)
			calls := 0
			ctx := solana.Context{RecoverSecp256k1: func(hash []byte, id uint64, sig []byte) ([]byte, uint64) {
				calls++
				if id%256 != 0 && id%256 < 4 {
					return nil, id % 256
				}
				key := make([]byte, 64)
				for i := range key {
					key[i] = hash[i%32] ^ sig[i]
				}
				return key, 0
			}}
			code := solana.Secp256k1Recover(ctx, ix[:lengths[0]], id, ix[40:40+int(lengths[1])], account[8:8+int(lengths[2])])
			for j := 0; j < 8; j++ {
				account[j] = byte(code >> (j * 8))
			}
			fmt.Fprintf(&want, "0 %d %x\n", calls, account)
		}
	}
	cmd := exec.Command(bin)
	cmd.Stdin = strings.NewReader(input.String())
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if string(out) != want.String() {
		t.Fatalf("native/C secp256k1 bounds, statuses or key copy differ\n%s\nwant\n%s", out, want.String())
	}
	t.Logf("%d secp256k1 boundary vectors passed", vectors)
}
