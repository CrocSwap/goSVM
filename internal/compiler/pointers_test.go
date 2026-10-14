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
		"named pointer":    `type P *uint64;func Process(s,i []byte)uint64{var p P;return *p}`,
		"pointer pointer":  `func Process(s,i []byte)uint64{var p *uint64;pp:=&p;return **pp}`,
		"pointer slice":    `func Process(s,i []byte)uint64{p:=&s;return uint64(len(*p))}`,
		"context pointer":  `import "gosvm/solana";func Process(c solana.Context)uint64{p:=&c;return solana.Count(*p)}`,
		"method recursion": `type V uint64;func(v V)Read()uint64{return v.Read()};func Process(s,i []byte)uint64{return V(1).Read()}`,
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
	// The imported summary carries the local origin through, so the local
	// escapes and is moved to the heap (docs/GO_ALLOCATION.md).
	p.Packages[0] = unit("app", `package app;import "dep";func bad()*uint64{v:=dep.Value{N:4};_,p:=dep.Forward(&v);return p};func Process(s,i []byte)uint64{return *bad()}`)
	checked, err := Check(p)
	if err != nil {
		t.Fatal(err)
	}
	moved := false
	for _, fn := range checked.Functions {
		moved = moved || fn.Object.Name() == "bad" && len(fn.HeapVars) == 1 && fn.HeapVars[0].Name() == "v"
	}
	if !moved {
		t.Fatal("imported borrow summary lost local origin: v was not moved to the heap")
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
