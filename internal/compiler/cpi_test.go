package compiler

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"gosvm/solana"
	"math/rand"
	"os/exec"
	"strings"
	"testing"
)

func TestSDK2InvokeBoundaryDifferential(t *testing.T) {
	source := `package p;import "gosvm/solana";func Process(c solana.Context)uint64{return solana.Invoke(c,uint64(solana.Instruction(c)[0]),solana.Data(c,1),solana.Data(c,2),solana.Data(c,3))}`
	driver := `
#include <stdio.h>
#include <string.h>
static Context *observed;
u64 sol_create_program_address(Seed *s,u64 n,u8 *p,u8 *o){return 1;}
u64 sol_invoke_signed_c(Instruction *ix,AccountInfo *infos,u64 count,Seeds *groups,u64 ng){
 if(infos!=observed->accounts || count!=4 || ix->count!=(u64)infos[1].data_len/9 || ix->data!=infos[2].data || ix->len!=infos[2].data_len) return 999;
 for(u64 j=0;j<ix->count;j++){u64 index=sol_read64(infos[1].data+j*9);u8 flags=infos[1].data[j*9+8];if(ix->metas[j].key!=infos[index].key || ix->metas[j].writable!=((flags&1)!=0) || ix->metas[j].signer!=((flags&2)!=0))return 998;}
 u8 *encoded=infos[3].data;u64 off=1;if(ng!=(infos[3].data_len?encoded[0]:0))return 997;
 for(u64 j=0;j<ng;j++){u64 seeds=encoded[off++];if(groups[j].len!=seeds)return 996;for(u64 k=0;k<seeds;k++){u64 n=encoded[off++];if(groups[j].ptr[k].len!=n || memcmp(groups[j].ptr[k].ptr,encoded+off,n))return 995;off+=n;}}
 return 0x1200000000ULL;
}

static int unhex(char *s,u8 *b){if(!strcmp(s,"-"))return 0;int n=0;for(int j=0;s[j];j+=2){unsigned x;if(sscanf(s+j,"%2x",&x)!=1)return -1;b[n++]=(u8)x;}return n;}
int main(void){unsigned p,exec;char m[1000],d[20500],s[2200];while(scanf("%u %u %999s %20499s %2199s",&p,&exec,m,d,s)==5){u8 mb[500],db[10250],sb[1100],ix[1]={(u8)p};u8 keys[4][32]={{0},{1},{2},{3}};Context c={0};c.count=4;c.accounts[0].executable=exec;c.accounts[1].data=mb;c.accounts[1].data_len=unhex(m,mb);c.accounts[2].data=db;c.accounts[2].data_len=unhex(d,db);c.accounts[3].data=sb;c.accounts[3].data_len=unhex(s,sb);for(int j=0;j<4;j++)c.accounts[j].key=keys[j];c.instruction=GOSVM_SLICE(ix,1);observed=&c;printf("%llu\n",go_Process(&c));}return 0;}
`
	bin := hostProgram(t, source, driver)
	rng := rand.New(rand.NewSource(221051))
	var input, want strings.Builder
	add := func(program byte, executable bool, metas, data, seeds []byte) {
		execFlag := 0
		if executable {
			execFlag = 1
		}
		encode := func(b []byte) string {
			if len(b) == 0 {
				return "-"
			}
			return hex.EncodeToString(b)
		}
		fmt.Fprintf(&input, "%d %d %s %s %s\n", program, execFlag, encode(metas), encode(data), encode(seeds))
		c := solana.Context{Accounts: []solana.Account{{Executable: executable}, {}, {}, {}}}
		c.InvokeInstruction = func(_ uint64, _, _, _ []byte) uint64 { return 0x1200000000 }
		fmt.Fprintf(&want, "%d\n", solana.Invoke(c, uint64(program), metas, data, seeds))
	}
	for i := 0; i < 1000; i++ {
		metas := make([]byte, rng.Intn(160))
		seeds := make([]byte, rng.Intn(64))
		data := make([]byte, rng.Intn(64))
		rng.Read(metas)
		rng.Read(seeds)
		rng.Read(data)
		if i%3 == 0 {
			metas = make([]byte, rng.Intn(17)*9)
			for j := 0; j < len(metas); j += 9 {
				binary.LittleEndian.PutUint64(metas[j:], uint64(rng.Intn(4)))
				metas[j+8] = byte(rng.Intn(4))
			}
			seeds = []byte{1, 2, 1, 7, 0}
		}
		add(byte(i%5), i%7 != 0, metas, data, seeds)
	}
	for _, s := range [][]byte{nil, {0}, {1, 0}, {2, 1, 0, 1, 1, 8}, {1, 17}, {1, 1, 33}, {1, 1, 2, 7}, {0, 0}, bytes.Repeat([]byte{0}, 1025)} {
		add(0, true, nil, nil, s)
	}
	add(0, true, nil, make([]byte, 10240), nil)
	add(0, true, nil, make([]byte, 10241), nil)
	cmd := exec.Command(bin)
	cmd.Stdin = strings.NewReader(input.String())
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if string(out) != want.String() {
		t.Fatal("native/C bounded CPI status or marshaling mismatch")
	}
}

func TestSDKVersionsAndNativeOnlyTypes(t *testing.T) {
	if _, err := CompileProgram(&Program{SDK: 3}); err == nil || !strings.Contains(err.Error(), "unsupported SDK") {
		t.Fatal(err)
	}
	if _, err := TypeCheckProgram(&Program{SDK: 3}); err == nil || !strings.Contains(err.Error(), "unsupported SDK") {
		t.Fatal(err)
	}
	if _, err := Compile("account.go", []byte(`package p;import "gosvm/solana";func Process(c solana.Context)uint64 {var a solana.Account;return a.Lamports}`)); err == nil || !strings.Contains(err.Error(), "only Context") || !strings.Contains(err.Error(), "account.go:") {
		t.Fatalf("native-only Account accepted: %v", err)
	}
}
