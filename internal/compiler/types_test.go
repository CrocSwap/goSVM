package compiler

import (
	"go/types"
	"strings"
	"testing"
)

func TestTypeCheckBeforeEntryGeneration(t *testing.T) {
	p := &Program{Entry: "app", Packages: []PackageSources{
		unit("app", `package app;import "gosvm/solana";import "app/model";type Accounts struct{Pool model.Pool};func Swap(c solana.Context,a Accounts,n model.Amount)(model.Pool,uint64){a.Pool.N=a.Pool.N+n;return a.Pool,0}`),
		unit("app/model", `package model;type Key [32]byte;type Amount uint64;type Pool struct{Authority Key;N Amount}`),
	}}
	graph, err := TypeCheckProgram(p)
	if err != nil {
		t.Fatal(err)
	}
	pool := graph.Packages["app/model"].Scope().Lookup("Pool").Type()
	bundle := graph.Entry.Scope().Lookup("Accounts").Type().Underlying().(*types.Struct)
	handler := graph.Entry.Scope().Lookup("Swap").Type().(*types.Signature)
	if !types.Identical(bundle.Field(0).Type(), pool) || !types.Identical(handler.Results().At(0).Type(), pool) {
		t.Fatal("canonical imported type identity lost")
	}
	if len(graph.Files) != 2 || len(graph.Info.Types) == 0 || graph.Fset.Position(pool.(*types.Named).Obj().Pos()).Filename != "app/model/program.go" {
		t.Fatal("missing source/type evidence")
	}
	if _, err := CompileProgram(p); err == nil || !strings.Contains(err.Error(), "require Process") {
		t.Fatalf("type inspection bypassed entry validation: %v", err)
	}
}
func TestTypeInspectionIsNotSubsetCertification(t *testing.T) {
	p := &Program{Entry: "app", Packages: []PackageSources{unit("app", `package app;func Process(s,i []byte)uint64{n:=int(i[0]);return uint64(n)}`)}}
	if _, err := TypeCheckProgram(p); err != nil {
		t.Fatal(err)
	}
	if _, err := CompileProgram(p); err == nil || !strings.Contains(err.Error(), "int variables") {
		t.Fatalf("final subset check missing: %v", err)
	}
	p.Packages[0] = unit("app", `package app;type V struct{N missing}`)
	if _, err := TypeCheckProgram(p); err == nil || !strings.Contains(err.Error(), "app/program.go:") {
		t.Fatalf("source-located type error wanted: %v", err)
	}
}
