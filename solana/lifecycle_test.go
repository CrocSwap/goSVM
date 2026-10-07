package solana

import (
	"bytes"
	"encoding/binary"
	"math"
	"reflect"
	"testing"
)

func lifecycleContext() Context {
	id, left, right := make([]byte, 32), make([]byte, 32), make([]byte, 32)
	id[0], left[0], right[0] = 11, 7, 9
	source := Account{Key: left, Owner: append([]byte{}, id...), Data: []byte{1, 2, 3, 4}, Writable: true, Lamports: 100, OriginalDataLen: 4}
	destination := Account{Key: right, Owner: make([]byte, 32), Writable: true, Lamports: 50}
	return Context{ID: id, Accounts: []Account{source, destination, source, destination}}
}

func TestOwnedLifecycleAliases(t *testing.T) {
	c := lifecycleContext()
	if code := ResizeAccount(c, 0, 8); code != 0 || !bytes.Equal(c.Accounts[0].Data, []byte{1, 2, 3, 4, 0, 0, 0, 0}) {
		t.Fatal(code, c.Accounts)
	}
	c.Accounts[2].Data[7] = 99
	if c.Accounts[0].Data[7] != 99 || cap(c.Accounts[0].Data) != len(c.Accounts[0].Data) {
		t.Fatal("alias/capacity")
	}
	if ResizeAccount(c, 2, 2) != 0 || ResizeAccount(c, 0, 8) != 0 || !bytes.Equal(c.Accounts[2].Data, []byte{1, 2, 0, 0, 0, 0, 0, 0}) {
		t.Fatal("shrink/regrow must zero newly exposed bytes", c.Accounts)
	}
	if code := CloseAccount(c, 2, 3); code != 0 {
		t.Fatal(code)
	}
	for _, index := range []int{0, 2} {
		a := c.Accounts[index]
		if len(a.Data) != 0 || cap(a.Data) != 0 || a.Lamports != 0 || !bytes.Equal(a.Owner, make([]byte, 32)) {
			t.Fatal(a)
		}
	}
	if c.Accounts[1].Lamports != 150 || c.Accounts[3].Lamports != 150 {
		t.Fatal(c.Accounts)
	}
}

func TestOwnedLifecycleFailureAtomicity(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Context)
		call func(Context) uint64
		code uint64
	}{
		{"index", nil, func(c Context) uint64 { return ResizeAccount(c, 4, 1) }, 2005},
		{"readonly", func(c *Context) { c.Accounts[0].Writable = false }, func(c Context) uint64 { return ResizeAccount(c, 0, 1) }, 2011},
		{"executable", func(c *Context) { c.Accounts[0].Executable = true }, func(c Context) uint64 { return ResizeAccount(c, 0, 1) }, 2011},
		{"owner", func(c *Context) { c.Accounts[0].Owner[0]++ }, func(c Context) uint64 { return ResizeAccount(c, 0, 1) }, 2012},
		{"id", func(c *Context) { c.ID = nil }, func(c Context) uint64 { return ResizeAccount(c, 0, 1) }, 2012},
		{"key", func(c *Context) { c.Accounts[0].Key = nil }, func(c Context) uint64 { return ResizeAccount(c, 0, 1) }, 2013},
		{"growth", nil, func(c Context) uint64 { return ResizeAccount(c, 0, 10245) }, 2010},
		{"wide-length", nil, func(c Context) uint64 { return ResizeAccount(c, 0, math.MaxUint64) }, 2010},
		{"wide-original", func(c *Context) { c.Accounts[0].OriginalDataLen = math.MaxUint64 }, func(c Context) uint64 { return ResizeAccount(c, 0, 0) }, 2010},
		{"close-alias", nil, func(c Context) uint64 { return CloseAccount(c, 0, 2) }, 2013},
		{"close-destination", nil, func(c Context) uint64 { return CloseAccount(c, 0, 4) }, 2005},
		{"close-readonly", func(c *Context) { c.Accounts[1].Writable = false }, func(c Context) uint64 { return CloseAccount(c, 0, 1) }, 2011},
		{"close-executable", func(c *Context) { c.Accounts[1].Executable = true }, func(c Context) uint64 { return CloseAccount(c, 0, 1) }, 2011},
		{"refund-overflow", func(c *Context) { c.Accounts[1].Lamports = math.MaxUint64 - 99 }, func(c Context) uint64 { return CloseAccount(c, 0, 1) }, 2014},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c := lifecycleContext()
			if test.edit != nil {
				test.edit(&c)
			}
			before := cloneContext(c)
			if code := test.call(c); code != test.code {
				t.Fatal(code, test.code)
			}
			if !reflect.DeepEqual(c, before) {
				t.Fatal("failure mutated accounts")
			}
		})
	}
}

func cloneContext(c Context) Context {
	c.ID = append([]byte(nil), c.ID...)
	c.Accounts = append([]Account(nil), c.Accounts...)
	for j := range c.Accounts {
		a := &c.Accounts[j]
		a.Key = append([]byte(nil), a.Key...)
		a.Owner = append([]byte(nil), a.Owner...)
		a.Data = append([]byte(nil), a.Data...)
	}
	return c
}

func TestRentBoundaryPreservesFailedOutput(t *testing.T) {
	c := Context{ReadRent: func(b []byte) uint64 { b[0] = 99; return 1234 }}
	output := bytes.Repeat([]byte{7}, 17)
	if code := Rent(c, output); code != 1234 || !bytes.Equal(output, bytes.Repeat([]byte{7}, 17)) {
		t.Fatal(code, output)
	}
	if Rent(Context{}, output) != 2009 || Rent(c, output[:16]) != 2015 {
		t.Fatal("callback/length checks")
	}
	c.ReadRent = func(b []byte) uint64 {
		binary.LittleEndian.PutUint64(b, math.MaxUint64)
		binary.LittleEndian.PutUint64(b[8:], math.Float64bits(1.5))
		b[16] = 17
		return 0
	}
	if Rent(c, output) != 0 || binary.LittleEndian.Uint64(output) != math.MaxUint64 || binary.LittleEndian.Uint64(output[8:]) != math.Float64bits(1.5) || output[16] != 17 {
		t.Fatal(output)
	}
}
