package compiler

import (
	"fmt"
	"math/rand"
	"os/exec"
	"strings"
	"testing"

	pointervalues "gosvm/examples/pointer-values"
)

func TestPointerValuesDifferential(t *testing.T) {
	p, err := ReadProgram("../../examples/pointer-values")
	if err != nil {
		t.Fatal(err)
	}
	c, err := CompileProgram(p)
	if err != nil {
		t.Fatal(err)
	}
	host := hostGenerated(t, c, arrayHostDriver)
	rng := rand.New(rand.NewSource(20261007))
	var input, want strings.Builder
	for j := 0; j < 1000; j++ {
		s, i := make([]byte, 24), make([]byte, 16)
		rng.Read(s)
		rng.Read(i)
		i[0] = 0
		fmt.Fprintf(&input, "%x %x\n", i, s)
		result := nativeArrayCall(pointervalues.Process, s, i)
		if !strings.HasPrefix(result, "0:") {
			t.Fatalf("native fixture failed: %s", result)
		}
		want.WriteString(result)
	}
	cmd := exec.Command(host)
	cmd.Stdin = strings.NewReader(input.String())
	out, err := cmd.CombinedOutput()
	if err != nil || string(out) != want.String() {
		t.Fatalf("native-Go/C pointer mismatch: %v %s", err, out)
	}
	for mode := byte(1); mode <= 15; mode++ {
		for _, value := range []byte{0, 3, 4, 255} {
			s, i := make([]byte, 24), make([]byte, 16)
			i[0], i[1], i[2], i[8] = mode, value, value, value
			cmd := exec.Command(host)
			cmd.Stdin = strings.NewReader(fmt.Sprintf("%x %x\n", i, s))
			got, err := cmd.CombinedOutput()
			want := nativeArrayCall(pointervalues.Process, s, i)
			if err != nil || string(got) != want {
				t.Fatalf("nil/bounds/order mode %d value %d: got %q want %q (%v)", mode, value, got, want, err)
			}
		}
	}
}

func TestPointerLifetimeRejections(t *testing.T) {
	for name, source := range map[string]string{
		"loop back edge":          `func bad(in *uint64)*uint64{var n uint64;p:=in;q:=in;for j:=uint64(0);j<3;j++{q=p;p=&n};return q};func Process(s,i []byte)uint64{var n uint64;return *bad(&n)}`,
		"named pointer":           `type P *uint64;func Process(s,i []byte)uint64{var p P;return *p}`,
		"local return":            `func bad()*uint64{var n uint64;return &n};func Process(s,i []byte)uint64{return *bad()}`,
		"parameter copy":          `func bad(n uint64)*uint64{return &n};func Process(s,i []byte)uint64{return *bad(1)}`,
		"literal return":          `type V struct{N uint64};func bad()*V{return &V{N:1}};func Process(s,i []byte)uint64{return bad().N}`,
		"forwarded local":         `func keep(p *uint64)*uint64{return p};func bad()*uint64{var n uint64;return keep(&n)};func Process(s,i []byte)uint64{return *bad()}`,
		"tuple local":             `func keep(p *uint64)(uint64,*uint64){return 0,p};func bad()(uint64,*uint64){var n uint64;return keep(&n)};func Process(s,i []byte)uint64{_,p:=bad();return *p}`,
		"block escape":            `func Process(s,i []byte)uint64{var p *uint64;{var n uint64;p=&n};return *p}`,
		"loop escape":             `func Process(s,i []byte)uint64{var p *uint64;for j:=uint64(0);j<1;j++ {var n uint64;p=&n};return *p}`,
		"conditional escape":      `func Process(s,i []byte)uint64{var p *uint64;if len(i)>0{n:=uint64(i[0]);p=&n};return *p}`,
		"block tuple":             `func keep(p *uint64)(*uint64,uint64){return p,1};func Process(s,i []byte)uint64{var p *uint64;var n uint64;{local:=uint64(1);p,n=keep(&local)};return *p+n}`,
		"receiver copy":           `type V struct{N uint64};func(v V)Bad()*uint64{return &v.N};func Process(s,i []byte)uint64{v:=V{};return *v.Bad()}`,
		"callee local method":     `type V struct{N uint64};func(v *V)Field()*uint64{return &v.N};func bad()*uint64{v:=V{};return v.Field()};func Process(s,i []byte)uint64{return *bad()}`,
		"method expression local": `type V struct{N uint64};func(v *V)Field()*uint64{return &v.N};func bad()*uint64{v:=V{};return (*V).Field(&v)};func Process(s,i []byte)uint64{return *bad()}`,
		"pointer fields":          `type V struct{P *uint64};func Process(s,i []byte)uint64{return 0}`,
		"pointer pointer":         `func Process(s,i []byte)uint64{var p *uint64;pp:=&p;return **pp}`,
		"pointer slice":           `func Process(s,i []byte)uint64{p:=&s;return uint64(len(*p))}`,
		"context pointer":         `import "gosvm/solana";func Process(c solana.Context)uint64{p:=&c;return solana.Count(*p)}`,
		"method value":            `type V struct{N uint64};func(v V)Read()uint64{return v.N};func Process(s,i []byte)uint64{v:=V{};f:=v.Read;return f()}`,
		"method recursion":        `type V uint64;func(v V)Read()uint64{return v.Read()};func Process(s,i []byte)uint64{return V(1).Read()}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Compile("pointer-reject.go", []byte("package p;"+source))
			if err == nil || !strings.Contains(err.Error(), "pointer-reject.go:") {
				t.Fatalf("source-located rejection wanted: %v", err)
			}
		})
	}
}

func TestImportedMethodsAndBorrowedResults(t *testing.T) {
	p := &Program{Entry: "app", Packages: []PackageSources{
		unit("app", `package app;import "dep";func relay(v *dep.Value)(uint64,*uint64){return dep.Forward(v)};func Process(s,i []byte)uint64{v:=dep.Value{N:7};code,p:=relay(&v);*p=*p+5;old:=v.Read(2);v.Add(3);return code+old+v.N+(*dep.Value).Read(&v,1)}`),
		unit("dep", `package dep;type Value struct{N uint64};func(v Value)Read(n uint64)uint64{v.N=v.N+n;return v.N};func(v *Value)Add(n uint64){v.N=v.N+n};func(v *Value)Field()*uint64{return &v.N};func Forward(v *Value)(uint64,*uint64){return 0,v.Field()}`),
	}}
	c, err := CompileProgram(p)
	if err != nil {
		t.Fatal(err)
	}
	host := hostGenerated(t, c, `int main(void){return go_Process((slice){0,0},(slice){0,0})!=45;}`)
	if out, err := exec.Command(host).CombinedOutput(); err != nil {
		t.Fatalf("imported methods/borrowed results: %v %s", err, out)
	}
	p.Packages[0] = unit("app", `package app;import "dep";func bad()*uint64{v:=dep.Value{};_,p:=dep.Forward(&v);return p};func Process(s,i []byte)uint64{return *bad()}`)
	if _, err := CompileProgram(p); err == nil || !strings.Contains(err.Error(), "reference escapes") {
		t.Fatalf("imported borrow summary lost local origin: %v", err)
	}
}

func TestPointerArrayLengthAndReceiverEffects(t *testing.T) {
	source := `package p
 type K [4]byte
 type N uint64
 func(n *N)Inc() { *n++ }
 func array(s []byte)*K {s[0]++;return nil}
 func receiver(s []byte,p *N)*N {s[1]++;return p}
 func arg(s []byte)uint64{s[2]++;return 3}
 func(n *N)Add(x uint64){*n=N(uint64(*n)+x)}
 func Process(s,i []byte)uint64 {
  var p *K
  s[3]=byte(len(p)+cap(p))
  s[4]=byte(uint64(len(array(s)))+uint64(cap(array(s))))
  a:=[2]N{1,2}
  a[0].Inc()
  receiver(s,&a[1]).Add(arg(s))
  s[5],s[6]=byte(a[0]),byte(a[1])
  return 0
 }`
	host := hostProgram(t, source, `int main(void){u8 s[7]={0};u64 n=go_Process((slice){s,7},(slice){0,0});u8 want[7]={2,1,1,8,8,2,5};for(u64 j=0;j<7;j++)if(s[j]!=want[j])return 1;return n!=0;}`)
	if out, err := exec.Command(host).CombinedOutput(); err != nil {
		t.Fatalf("pointer-array len/cap and receiver effects: %v %s", err, out)
	}
}
