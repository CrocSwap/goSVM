package compiler

import (
	"encoding/binary"
	"fmt"
	"math/rand"
	"os/exec"
	"strings"
	"testing"

	sliceviews "gosvm/examples/slice-views"
)

func TestSliceViewsDifferential(t *testing.T) {
	p, err := ReadProgram("../../examples/slice-views")
	if err != nil {
		t.Fatal(err)
	}
	c, err := CompileProgram(p)
	if err != nil {
		t.Fatal(err)
	}
	host := hostGenerated(t, c, arrayHostDriver)
	rng := rand.New(rand.NewSource(20261008))
	var input, want strings.Builder
	for j := 0; j < 1000; j++ {
		s, i := make([]byte, 24), make([]byte, 16)
		rng.Read(s)
		rng.Read(i)
		i[0] = 0
		fmt.Fprintf(&input, "%x %x\n", i, s)
		result := nativeArrayCall(sliceviews.Process, s, i)
		if !strings.HasPrefix(result, "0:") {
			t.Fatalf("native fixture failed: %s", result)
		}
		want.WriteString(result)
	}
	cmd := exec.Command(host)
	cmd.Stdin = strings.NewReader(input.String())
	out, err := cmd.CombinedOutput()
	if err != nil || string(out) != want.String() {
		t.Fatalf("native-Go/C views mismatch: %v %s", err, out)
	}
	for mode := byte(1); mode <= 11; mode++ {
		for _, edge := range [][3]uint64{{0, 0, 0}, {0, 4, 4}, {3, 4, 6}, {4, 3, 4}, {0, 24, 24}, {24, 24, 24}, {24, 25, 25}, {0, 4, 3}, {0, 4, 25}, {4, 6, 6}, {0, 3, 1 << 32}, {0, 3, 1 << 63}, {0, 3, ^uint64(0)}} {
			s, i := make([]byte, 24), make([]byte, 16)
			i[0], i[1], i[2], i[3] = mode, byte(edge[0]), 255, byte(edge[1])
			binary.LittleEndian.PutUint64(i[8:], edge[2])
			cmd := exec.Command(host)
			cmd.Stdin = strings.NewReader(fmt.Sprintf("%x %x\n", i, s))
			got, err := cmd.CombinedOutput()
			want := nativeArrayCall(sliceviews.Process, s, i)
			if err != nil || string(got) != want {
				t.Fatalf("view mode %d edge %v: got %q want %q (%v)", mode, edge, got, want, err)
			}
		}
	}
}
func TestSliceViewArrayBoundary(t *testing.T) {
	p, err := ReadProgram("../../examples/slice-views")
	if err != nil {
		t.Fatal(err)
	}
	c, err := CompileProgram(p)
	if err != nil {
		t.Fatal(err)
	}
	host := hostGenerated(t, c, arrayHostDriver)
	for _, high := range []uint64{1023, 1024, 1025} {
		for _, mode := range []byte{10, 11} {
			s, i := make([]byte, 24), make([]byte, 16)
			i[0], i[2], i[4] = mode, 255, 91
			binary.LittleEndian.PutUint64(i[8:], high)
			cmd := exec.Command(host)
			cmd.Stdin = strings.NewReader(fmt.Sprintf("%x %x\n", i, s))
			got, err := cmd.CombinedOutput()
			want := nativeArrayCall(sliceviews.Process, s, i)
			if err != nil || string(got) != want {
				t.Fatalf("array boundary %d mode %d: got %q want %q (%v)", high, mode, got, want, err)
			}
		}
	}
}
func TestSliceViewRejections(t *testing.T) {
	for name, source := range map[string]string{
		"local array":          `func bad()[]byte{var a [32]byte;return a[:]};func Process(s,i []byte)uint64{return uint64(len(bad()))}`,
		"value parameter":      `func bad(a [32]byte)[]byte{return a[:]};func Process(s,i []byte)uint64{return uint64(len(bad([32]byte{})))}`,
		"value receiver":       `type K [32]byte;func(k K)Bad()[]byte{return k[:]};func Process(s,i []byte)uint64{var k K;return uint64(len(k.Bad()))}`,
		"block view":           `func Process(s,i []byte)uint64{var v []byte;{var k [32]byte;v=k[:]};return uint64(len(v))}`,
		"loop view":            `func Process(s,i []byte)uint64{var v []byte;for j:=uint64(0);j<1;j++{var k [32]byte;v=k[:]};return uint64(len(v))}`,
		"converted local":      `func bad()[]byte{var k [32]byte;v:=k[:];return []byte(v)};func Process(s,i []byte)uint64{return uint64(len(bad()))}`,
		"forwarded tuple":      `func keep(v []byte)(uint64,[]byte){return 0,v};func bad()(uint64,[]byte){var k [32]byte;return keep(k[:])};func Process(s,i []byte)uint64{_,v:=bad();return uint64(len(v))}`,
		"view element pointer": `func bad()*byte{var k [32]byte;v:=k[:];return &v[0]};func Process(s,i []byte)uint64{return uint64(*bad())}`,
		"word views":           `func Process(s,i []byte)uint64{var k [4]uint64;v:=k[:];return v[0]}`,
		"unaddressable array":  `func k()[4]byte{return [4]byte{}};func Process(s,i []byte)uint64{return uint64(len(k()[:]))}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Compile("views-reject.go", []byte("package p;"+source))
			if err == nil || !strings.Contains(err.Error(), "views-reject.go:") {
				t.Fatalf("source-located rejection wanted: %v", err)
			}
		})
	}
}
func TestSDKViewCapacities(t *testing.T) {
	source := `package p;import "gosvm/solana";func Process(c solana.Context)uint64 {s:=solana.Data(c,0);k:=solana.Key(c,0);o:=solana.Owner(c,0);i:=solana.Instruction(c);p:=solana.ProgramID(c);if cap(s)!=24||cap(k)!=32||cap(o)!=32||cap(i)!=16||cap(p)!=32{return 1};v:=s[4:8:12];w:=v[:8];w[7]=41;return 0}`
	c, err := Compile("sdk-views.go", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	host := hostGenerated(t, c, `int main(void){u8 s[24]={0},key[32]={0},i[16]={0};Context c={0};c.count=1;c.accounts[0].data=s;c.accounts[0].data_len=24;c.accounts[0].key=key;c.accounts[0].owner=key;c.instruction=GOSVM_SLICE(i,16);c.program=GOSVM_SLICE(key,32);return go_Process(&c)!=0||s[11]!=41;}`)
	if out, err := exec.Command(host).CombinedOutput(); err != nil {
		t.Fatalf("SDK byte-view capacities: %v %s", err, out)
	}
}

func TestNilSliceContexts(t *testing.T) {
	source := `package p
 type V uint64
 func accept(s []byte)uint64{if s!=nil{return 1};return uint64(cap(s))}
 func empty()[]byte{return nil}
 func pair()([]byte,uint64){return nil,0}
 func(v V)Empty(s []byte)[]byte{return s}
 func Process(s,i []byte)uint64 {
  var a []byte=nil
  var b,c []byte
  b,c=nil,nil
  a=nil
  a,n:=pair()
  v:=V(1)
  var d []byte=v.Empty(nil)
  e:=V.Empty(v,nil)
  if a!=nil||b!=nil||c!=nil||d!=nil||e!=nil||empty()!=nil{return 1}
  return n+accept(nil)
 }`
	host := hostProgram(t, source, `int main(void){return go_Process(GOSVM_SLICE(0,0),GOSVM_SLICE(0,0))!=0;}`)
	if out, err := exec.Command(host).CombinedOutput(); err != nil {
		t.Fatalf("nil slice contexts: %v %s", err, out)
	}
}
func TestViewCapacityBoundary(t *testing.T) {
	source := `package p
 func Process(s,i []byte)uint64 {
  var k [1024]byte
  k[1023]=255
  v:=k[0:0:1024]
  w:=v[:uint64(cap(v))]
  w[0]=37
  p:=&w[1023]
  *p++
  if len(v)!=0||cap(v)!=1024||k[0]!=37||k[1023]!=0{return 1}
  return 0
 }`
	host := hostProgram(t, source, `int main(void){return go_Process(GOSVM_SLICE(0,0),GOSVM_SLICE(0,0))!=0;}`)
	if out, err := exec.Command(host).CombinedOutput(); err != nil {
		t.Fatalf("view capacity boundary: %v %s", err, out)
	}
}

func TestSDKNilSliceArguments(t *testing.T) {
	source := `package p;import "gosvm/solana";func Process(c solana.Context)uint64{if solana.SHA256(c,nil,0,0,nil,0)!=2003{return 1};return 0}`
	c, err := Compile("sdk-nil.go", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	host := hostGenerated(t, c, `int main(void){Context c={0};return go_Process(&c)!=0;}`)
	if out, err := exec.Command(host).CombinedOutput(); err != nil {
		t.Fatalf("SDK nil byte-slice arguments: %v %s", err, out)
	}
}

func TestViewAggregateReturn(t *testing.T) {
	// Multiple borrowed descriptors form an aggregate even without array types.
	// Preserve their individual pointer/length/capacity fields across forwarding.
	var resultTypes, returns, names, terms []string
	for j := 0; j < 24; j++ {
		resultTypes = append(resultTypes, "[]byte")
		returns = append(returns, fmt.Sprintf("s[%d:24:24]", j))
		names = append(names, fmt.Sprintf("v%d", j))
		terms = append(terms, fmt.Sprintf("uint64(len(v%d))+uint64(cap(v%d))", j, j))
	}
	source := fmt.Sprintf(`package p;func many(s []byte)(%s){return %s};func forward(s []byte)(%s){return many(s)};func Process(s,i []byte)uint64{%s:=forward(s);return %s}`, strings.Join(resultTypes, ","), strings.Join(returns, ","), strings.Join(resultTypes, ","), strings.Join(names, ","), strings.Join(terms, "+"))
	host := hostProgram(t, source, `int main(void){u8 s[24]={0};return go_Process(GOSVM_SLICE(s,24),GOSVM_SLICE(0,0))!=600;}`)
	if out, err := exec.Command(host).CombinedOutput(); err != nil {
		t.Fatalf("borrowed descriptor aggregate: %v %s", err, out)
	}
}
