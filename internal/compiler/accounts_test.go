package compiler

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Exercise the serialized loader boundary, rather than constructing Context
// directly. Duplicate descriptors must retain data/flags/key aliasing across
// both the old boundary and the new final slot.
func TestSDKAccountDecoderCapacityAndAliases(t *testing.T) {
	source := `package app;import "gosvm/solana"
func Process(c solana.Context)uint64 {
 d:=solana.Instruction(c)
 if len(d)!=2 || solana.Count(c)!=uint64(d[0]) || solana.ProgramID(c)[0]!=77 {return 7300}
 if d[1]==1 && solana.Count(c)!=4 {return 7310}
 for j:=uint64(0);j<solana.Count(c);j++ {
  k:=uint64(solana.Key(c,j)[0])-1
  if len(solana.Data(c,j))!=1 || solana.Owner(c,j)[0]!=91 {return 7301}
  if solana.Signer(c,j)!=(k%3==0) || solana.Writable(c,j)!=(k%3!=1) || solana.Executable(c,j)!=(k%5==0) {return 7302}
  if solana.Writable(c,j) && !solana.Executable(c,j) {solana.Data(c,j)[0]=solana.Data(c,j)[0]+byte(j+1)}
 }
 return 0
}`
	driver := `
#include <stdio.h>
#include <string.h>
static u8 raw[700000];static u8 *data[64];
static void put(u8 *p,u64 n){for(u64 j=0;j<8;j++)p[j]=(u8)(n>>(8*j));}
int main(void){u64 n;unsigned pattern,bad,strict;
while(scanf("%llu %u %u %u",&n,&pattern,&bad,&strict)==4){
 memset(raw,0,sizeof(raw));memset(data,0,sizeof(data));put(raw,n);u8 *p=raw+8;
 if(n<=64){for(u64 i=0;i<n;i++){
  if(bad && i==n-1){p[0]=(u8)i;p+=8;}
  else if((pattern==1 && i%2==1)||(pattern==2 && i>0)||(pattern==3 && (i==31||i==63))){
   u64 ref=pattern==2?i-1:pattern==3?(i==31?16:40):0;p[0]=(u8)ref;data[i]=data[ref];p+=8;
  }else{
   p[0]=255;p[1]=i%3==0;p[2]=i%3!=1;p[3]=i%5==0;p[8]=(u8)(i+1);p[40]=91;
   put(p+72,100+i);put(p+80,1);data[i]=p+88;data[i][0]=(u8)(i+10);
   p=p+89+10240;p=(u8 *)(((u64)p+7)&~7ULL);put(p,800+i);p+=8;
  }
 }put(p,2);p[8]=(u8)n;p[9]=(u8)strict;p[10]=77;}
 u64 code=entrypoint(raw);printf("%llu:",code);
 if(n<=64)for(u64 i=0;i<n;i++)printf("%02x",data[i]?data[i][0]:0);
 puts("");
}return 0;}
`
	cc, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("host Clang required")
	}
	for _, tc := range []struct{ sdk, maxAccounts, limit int }{{1, 0, 16}, {2, 0, 32}, {2, WideAccounts, 64}} {
		sdk, limit := tc.sdk, tc.limit
		t.Run(fmt.Sprintf("SDK%d-%d", sdk, limit), func(t *testing.T) {
			c, err := CompileProgram(&Program{SDK: sdk, MaxAccounts: tc.maxAccounts, Entry: "app", Packages: []PackageSources{unit("app", source)}})
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			file, bin := filepath.Join(dir, "accounts.c"), filepath.Join(dir, "accounts")
			if err := os.WriteFile(file, append(c, []byte(driver)...), 0600); err != nil {
				t.Fatal(err)
			}
			if out, err := exec.Command(cc, "-std=c11", "-O2", "-fsanitize=undefined", "-fno-sanitize-recover=all", file, "-o", bin).CombinedOutput(); err != nil {
				t.Fatalf("%v: %s", err, out)
			}
			var input, want strings.Builder
			cases := 0
			for _, n := range []uint64{0, 1, 4, 5, 15, 16, 17, 31, 32, 33, 34, 40, 63, 64, 65, 1 << 63, ^uint64(0)} {
				for pattern := 0; pattern < 4; pattern++ {
					for bad := 0; bad < 2; bad++ {
						for strict := 0; strict < 2; strict++ {
							fmt.Fprintf(&input, "%d %d %d %d\n", n, pattern, bad, strict)
							code := uint64(0)
							if n > uint64(limit) {
								code = 1001
							} else if bad != 0 && n != 0 {
								code = 1002
							} else if strict != 0 && n != 4 {
								code = 7310
							}
							fmt.Fprintf(&want, "%d:", code)
							if n <= 64 {
								values, refs := make([]byte, n), make([]int, n)
								for i := range values {
									refs[i] = i
									values[i] = byte(i + 10)
									if bad != 0 && i == int(n)-1 {
										values[i] = 0
									} else if pattern == 1 && i%2 == 1 {
										refs[i] = 0
									} else if pattern == 2 && i > 0 {
										refs[i] = refs[i-1]
									} else if pattern == 3 && i == 31 {
										refs[i] = 16
									} else if pattern == 3 && i == 63 {
										refs[i] = 40
									}
								}
								if code == 0 {
									for i, ref := range refs {
										if ref%3 != 1 && ref%5 != 0 {
											values[ref] += byte(i + 1)
										}
									}
								}
								for _, ref := range refs {
									fmt.Fprintf(&want, "%02x", values[ref])
								}
							}
							want.WriteByte('\n')
							cases++
						}
					}
				}
			}
			cmd := exec.Command(bin)
			cmd.Stdin = strings.NewReader(input.String())
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("%v: %s", err, out)
			}
			if string(out) != want.String() {
				actual, expected := strings.Split(string(out), "\n"), strings.Split(want.String(), "\n")
				for i := range expected {
					if i >= len(actual) {
						t.Fatalf("missing output for case %d", i)
					}
					if actual[i] != expected[i] {
						t.Fatalf("case %d: got %q want %q", i, actual[i], expected[i])
					}
				}
			}
			t.Logf("%d serialized decoder/alias/handler-count cases under UBSan", cases)
		})
	}
}

// Only programs that opt in to 64 accounts get the wide table and Process's
// own frame; SDK 1 and other limits are rejected.
func TestMaxAccountsOptIn(t *testing.T) {
	source := `package app;import "gosvm/solana";func Process(c solana.Context)uint64{return solana.Count(c)}`
	for _, tc := range []struct {
		sdk, max   int
		wide, sdk2 bool
		err        string
	}{
		{2, 0, false, true, ""},
		{0, 0, false, false, ""},
		{2, WideAccounts, true, true, ""},
		{0, WideAccounts, true, true, ""},
		{1, WideAccounts, false, false, "requires SDK 2"},
		{2, 32, false, false, "use 0"},
		{2, 65, false, false, "use 0"},
	} {
		c, err := CompileProgram(&Program{SDK: tc.sdk, MaxAccounts: tc.max, Entry: "app", Packages: []PackageSources{unit("app", source)}})
		if tc.err != "" {
			if err == nil || !strings.Contains(err.Error(), tc.err) {
				t.Fatalf("SDK %d max %d: %v", tc.sdk, tc.max, err)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(c, []byte("#define GOSVM_WIDE_ACCOUNTS 1")) != tc.wide || bytes.Contains(c, []byte("#define GOSVM_SDK2 1")) != tc.sdk2 {
			t.Fatalf("SDK %d max %d: wrong loader selection", tc.sdk, tc.max)
		}
	}
}
