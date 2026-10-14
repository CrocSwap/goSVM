package project

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoadSchemaFullSwap(t *testing.T) {
	s, err := LoadSchema(filepath.Join("..", "..", "examples", "full-swap"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	var counts []int
	for _, ix := range s.Instructions {
		names = append(names, ix.Name)
		counts = append(counts, len(ix.Accounts))
	}
	if !reflect.DeepEqual(names, []string{"Swap", "CreatePool", "ManagedSwap", "ClosePool"}) {
		t.Fatalf("instructions %v", names)
	}
	if !reflect.DeepEqual(counts, []int{8, 11, 8, 8}) {
		t.Fatalf("account counts %v", counts)
	}
	if len(s.Arguments) != len(s.Instructions) || s.Arguments[0].Name != "Swap" || s.Arguments[0].Kind != "instruction" {
		t.Fatalf("arguments %+v", s.Arguments)
	}
	pool, ok := s.State("Pool")
	if !ok || pool.Size != 145 || pool.Kind != "account" || len(pool.Discriminator) != 8 || pool.GoType == nil {
		t.Fatalf("Pool layout %+v", pool)
	}
	if pool.Discriminator[0] != 0xf1 || pool.Fields[0].Name != "VaultX" || pool.Fields[0].Offset != 8 || pool.Fields[0].Size != 32 {
		t.Fatalf("Pool fields %+v", pool.Fields[0])
	}
	managed, ok := s.State("ManagedPool")
	if !ok || managed.Size != 177 || managed.Fields[0].Name != "Core" || managed.Fields[0].Size != 137 {
		t.Fatalf("ManagedPool layout %+v", managed)
	}
	swap := s.Instructions[0]
	if swap.Accounts[0].Layout != "Pool" || swap.Accounts[0].Access != "write" || swap.Accounts[6].PDA == nil || len(swap.Accounts[6].PDA.Seeds) != 2 || len(swap.Relations) != 10 {
		t.Fatalf("Swap declaration %+v", swap)
	}
	create := s.Instructions[1]
	if create.Accounts[0].Init == nil || create.Accounts[0].Init.Payer != "Creator" {
		t.Fatalf("CreatePool init %+v", create.Accounts[0])
	}
	if closing := s.Instructions[3].Accounts[0].Close; closing == nil || closing.AuthorityField != "Pool.Creator" {
		t.Fatalf("ClosePool close %+v", closing)
	}
	if s.Errors["AccountCount"] != 6000 || s.Errors["AccountPDA"] != 6008 || len(s.ValidationOrder) == 0 || s.ValidationOrder[len(s.ValidationOrder)-1] != "writable_state_commit" {
		t.Fatalf("errors %v order %v", s.Errors, s.ValidationOrder)
	}
	if s.Types == nil || s.Types.Entry == nil || !strings.HasSuffix(pool.Type, "/model.Pool") {
		t.Fatalf("types %v %q", s.Types, pool.Type)
	}
}

func TestLoadSchemaRejectsSchema1(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "starter")
	if err := NewModule(dir, "example.org/starter"); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSchema(dir); err == nil || !strings.Contains(err.Error(), "schema-2") {
		t.Fatalf("schema 1 accepted: %v", err)
	}
}
