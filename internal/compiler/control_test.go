package compiler

import (
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"strings"
	"testing"

	controlvalues "gosvm/examples/control-values"
)

func TestControlValuesDifferential(t *testing.T) {
	source, err := os.ReadFile("../../examples/control-values/program.go")
	if err != nil {
		t.Fatal(err)
	}
	host := hostProgram(t, string(source), arrayHostDriver)
	rng := rand.New(rand.NewSource(20261006))
	var input, want strings.Builder
	for j := 0; j < 1000; j++ {
		s, i := make([]byte, 24), make([]byte, 16)
		rng.Read(s)
		rng.Read(i)
		i[0] = 0
		fmt.Fprintf(&input, "%x %x\n", i, s)
		result := nativeArrayCall(controlvalues.Process, s, i)
		if !strings.HasPrefix(result, "0:") {
			t.Fatalf("native fixture failed: %s", result)
		}
		want.WriteString(result)
	}
	cmd := exec.Command(host)
	cmd.Stdin = strings.NewReader(input.String())
	out, err := cmd.CombinedOutput()
	if err != nil || string(out) != want.String() {
		t.Fatalf("native-Go/C controls mismatch: %v %s", err, out)
	}
	for _, edge := range [][3]byte{{1, 0, 0}, {1, 23, 255}, {1, 24, 0}, {1, 255, 255}, {2, 0, 0}, {2, 3, 255}, {2, 4, 0}, {2, 255, 255}, {3, 0, 0}, {3, 0, 255}} {
		s, i := make([]byte, 24), make([]byte, 16)
		i[0], i[1], i[2] = edge[0], edge[1], edge[2]
		cmd := exec.Command(host)
		cmd.Stdin = strings.NewReader(fmt.Sprintf("%x %x\n", i, s))
		got, err := cmd.CombinedOutput()
		want := nativeArrayCall(controlvalues.Process, s, i)
		if err != nil || string(got) != want {
			t.Fatalf("assignment effects before failure %v: got %q want %q (%v)", edge, got, want, err)
		}
	}
}
func TestControlRejections(t *testing.T) {
	for name, source := range map[string]string{
		"named results":    `func pair()(a,b uint64){return 1,2};func Process(s,i []byte)uint64{a,b:=pair();return a+b}`,
		"interface error":  `func pair()(uint64,error){return 0,nil};func Process(s,i []byte)uint64{a,_:=pair();return a}`,
		"fallthrough":      `func Process(s,i []byte)uint64{switch i[0]{case 1:fallthrough;default:return 0}}`,
		"labeled continue": `func Process(s,i []byte)uint64{L:for{continue L};return 0}`,
		"struct switch":    `type V struct{N uint64};func Process(s,i []byte)uint64{switch (V{}){case V{}:return 0};return 1}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Compile("control-reject.go", []byte("package p;"+source))
			if err == nil || !strings.Contains(err.Error(), "control-reject.go:") {
				t.Fatalf("source-located rejection wanted: %v", err)
			}
		})
	}
}
func TestImportedMultipleResults(t *testing.T) {
	p := &Program{Entry: "app", Packages: []PackageSources{
		unit("app", `package app;import "dep";func forward()(dep.Value,uint64){return dep.Pair()};func Process(s,i []byte)uint64{v,code:=forward();v.N,code=code,v.N;return dep.Add(dep.Pair())+v.N+code}`),
		unit("dep", `package dep;type Value struct{N uint64};func Pair()(Value,uint64){return Value{N:7},3};func Add(v Value,n uint64)uint64{return v.N+n}`),
	}}
	c, err := CompileProgram(p)
	if err != nil {
		t.Fatal(err)
	}
	host := hostGenerated(t, c, `int main(void){return go_Process((slice){0,0},(slice){0,0})!=20;}`)
	if out, err := exec.Command(host).CombinedOutput(); err != nil {
		t.Fatalf("imported multiple results: %v %s", err, out)
	}
}
