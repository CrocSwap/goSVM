package compiler

import (
	"reflect"
	"strings"
	"testing"
)

func checkSource(t *testing.T, source string) *CheckedProgram {
	t.Helper()
	checked, err := Check(&Program{Entry: "program", Packages: []PackageSources{{"program", []Source{{Name: "program.go", Data: []byte(source)}}}}})
	if err != nil {
		t.Fatal(err)
	}
	return checked
}

func TestCheckedProgramDescribesContextProgram(t *testing.T) {
	checked := checkSource(t, `package main
import "gosvm/solana"
type S struct{ x uint64 }
func (s *S) Ptr() *uint64 { return &s.x }
func pick(a []byte, b []byte) []byte { return b }
func data(c solana.Context) []byte { return solana.Data(c, 0) }
func helper(n uint64) uint64 { return n + 1 }
func Process(c solana.Context) uint64 {
	var s S
	p := s.Ptr()
	*p = helper(1)
	d := data(c)
	_ = pick(d, d)
	return *p
}
`)
	if checked.EntryKind != EntryContext || checked.Process.Name() != "Process" || checked.SDKPackage == nil || checked.SDKPackage.Path() != "gosvm/solana" {
		t.Fatalf("entry %v %v %v", checked.EntryKind, checked.Process, checked.SDKPackage)
	}
	if checked.Entry == nil || checked.Entry.Path() != "program" || len(checked.Files) != 1 || checked.Info == nil || checked.Fset == nil {
		t.Fatal("typed program is incomplete")
	}
	names := func(functions []*Function) []string {
		var out []string
		for _, f := range functions {
			out = append(out, f.Object.Name())
		}
		return out
	}
	if got := names(checked.Functions); !reflect.DeepEqual(got, []string{"Ptr", "pick", "data", "helper", "Process"}) {
		t.Fatalf("functions %v", got)
	}
	if got := names(checked.CallOrder); got[len(got)-1] != "Process" || len(got) != 5 {
		t.Fatalf("call order %v", got)
	}
	process := checked.Functions[4]
	var callees []string
	for _, c := range process.Callees {
		callees = append(callees, c.Name())
	}
	if !reflect.DeepEqual(callees, []string{"Ptr", "helper", "data", "pick"}) {
		t.Fatalf("callees %v", callees)
	}
	want := map[string]ResultBorrow{
		"Ptr":    {Parameters: []int{0}},
		"pick":   {Parameters: []int{1}},
		"data":   {SDK: true},
		"helper": {},
	}
	for _, f := range checked.Functions[:4] {
		if len(f.Borrows.Results) != 1 || !reflect.DeepEqual(f.Borrows.Results[0], want[f.Object.Name()]) {
			t.Fatalf("%s borrows %+v", f.Object.Name(), f.Borrows)
		}
	}
}

func TestCheckLegacyEntryAndRejection(t *testing.T) {
	checked := checkSource(t, "package main\nfunc Process(input []byte, output []byte) uint64 { return 0 }\n")
	if checked.EntryKind != EntryBytes || checked.SDKPackage != nil || len(checked.CallOrder) != 1 {
		t.Fatalf("legacy entry %+v", checked)
	}
	source := "package main\nfunc Process(input []byte, output []byte) uint64 { f := float64(len(input)); return uint64(f) }\n"
	program := &Program{Entry: "program", Packages: []PackageSources{{"program", []Source{{Name: "p.go", Data: []byte(source)}}}}}
	checked, err := Check(program)
	if checked != nil || err == nil || !strings.Contains(err.Error(), "p.go:2:") || !strings.Contains(err.Error(), "unsupported type float64") {
		t.Fatalf("Check accepted float storage: %v", err)
	}
	if _, compileErr := CompileProgram(program); compileErr == nil || compileErr.Error() != err.Error() {
		t.Fatalf("CompileProgram error %v differs from Check %v", compileErr, err)
	}
}
