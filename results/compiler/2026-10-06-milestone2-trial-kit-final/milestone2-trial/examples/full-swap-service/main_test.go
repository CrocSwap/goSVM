package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"math/big"
	"math/rand"
	"testing"

	"example.org/gosvm/full-swap/client"
	"example.org/gosvm/full-swap/model"
)

func TestManagedPoolCanonicalWireAndLifecycleArguments(t *testing.T) {
	var pool model.ManagedPool
	for j := 0; j < 32; j++ {
		pool.Core.VaultX[j] = byte(j)
		pool.Core.VaultY[j] = byte(j + 32)
		pool.Core.MintX[j] = byte(j + 64)
		pool.Core.MintY[j] = byte(j + 96)
		pool.Creator[j] = byte(j + 128)
	}
	pool.Core.Bump = 251
	pool.Core.Swaps = 0x1122334455667788
	disc := func(kind, name string) []byte {
		sum := sha256.Sum256([]byte("gosvm:" + kind + ":" + name + ":v1"))
		return sum[:8]
	}
	want := append(disc("account", "ManagedPool"), make([]byte, 169)...)
	for j := 0; j < 128; j++ {
		want[8+j] = byte(j)
	}
	want[136] = 251
	binary.LittleEndian.PutUint64(want[137:], pool.Core.Swaps)
	for j := 0; j < 32; j++ {
		want[145+j] = byte(j + 128)
	}
	if got := client.EncodeAccountManagedPool(pool); !bytes.Equal(got, want) {
		t.Fatal("managed pool wire differs")
	}
	decoded, err := client.DecodeAccountManagedPool(want)
	if err != nil || decoded != pool {
		t.Fatal(decoded, err)
	}
	args := model.CreatePoolArgs{ReserveX: 0x1122334455667788, ReserveY: ^uint64(0), Bump: 251, XBump: 253, YBump: 249}
	init := append(disc("instruction", "CreatePool"), make([]byte, 19)...)
	binary.LittleEndian.PutUint64(init[8:], args.ReserveX)
	binary.LittleEndian.PutUint64(init[16:], args.ReserveY)
	copy(init[24:], []byte{251, 253, 249})
	if !bytes.Equal(client.EncodeInstructionCreatePool(args), init) {
		t.Fatal("create arguments differ")
	}
	back, err := client.DecodeInstructionCreatePool(init)
	if err != nil || back != args {
		t.Fatal(back, err)
	}
	close := client.EncodeInstructionClosePool(model.ClosePoolArgs{})
	if !bytes.Equal(close, disc("instruction", "ClosePool")) {
		t.Fatal("empty close arguments differ")
	}
	if _, err := client.DecodeInstructionClosePool(close); err != nil {
		t.Fatal(err)
	}
	swap := model.SwapArgs{AmountIn: 10000, MinOut: 1}
	swapWire := append(disc("instruction", "ManagedSwap"), make([]byte, 16)...)
	binary.LittleEndian.PutUint64(swapWire[8:], swap.AmountIn)
	binary.LittleEndian.PutUint64(swapWire[16:], swap.MinOut)
	if !bytes.Equal(client.EncodeInstructionManagedSwap(swap), swapWire) {
		t.Fatal("managed swap arguments differ")
	}
}

func TestCanonicalWireAndQuote(t *testing.T) {
	// Independent constants/layout, not an import of generator offsets.
	var pool model.Pool
	for j := 0; j < 32; j++ {
		pool.VaultX[j] = byte(j)
		pool.VaultY[j] = byte(j + 32)
		pool.MintX[j] = byte(j + 64)
		pool.MintY[j] = byte(j + 96)
	}
	pool.Bump = 253
	pool.Swaps = 0x1122334455667788
	want := append([]byte{0xf1, 0x9a, 0x6d, 0x04, 0x11, 0xb1, 0x6d, 0xbc}, make([]byte, 137)...)
	for j := 0; j < 128; j++ {
		want[8+j] = byte(j)
	}
	want[136] = 253
	binary.LittleEndian.PutUint64(want[137:], pool.Swaps)
	b := client.EncodeAccountPool(pool)
	if !bytes.Equal(b, want) {
		t.Fatal("pool wire changed")
	}
	decoded, err := client.DecodeAccountPool(want)
	if err != nil || decoded != pool {
		t.Fatal(decoded, err)
	}
	a := model.SwapArgs{AmountIn: 0x1122334455667788, MinOut: 0x8877665544332211}
	ix := append([]byte{0xf8, 0xc6, 0x9e, 0x91, 0xe1, 0x75, 0x87, 0xc8}, make([]byte, 16)...)
	binary.LittleEndian.PutUint64(ix[8:], a.AmountIn)
	binary.LittleEndian.PutUint64(ix[16:], a.MinOut)
	if !bytes.Equal(client.EncodeInstructionSwap(a), ix) {
		t.Fatal("instruction wire changed")
	}
	got, err := client.DecodeInstructionSwap(ix)
	if err != nil || got != a {
		t.Fatal(got, err)
	}
	rng := rand.New(rand.NewSource(252026))
	for j := 0; j < 1000; j++ {
		x, y, amount := rng.Uint64(), rng.Uint64(), rng.Uint64()
		want := uint64(0)
		if x != 0 && y != 0 && amount != 0 && amount <= ^uint64(0)-x {
			fee := new(big.Int).Mul(new(big.Int).SetUint64(amount), big.NewInt(30))
			fee.Add(fee, big.NewInt(9999)).Div(fee, big.NewInt(10000))
			net := new(big.Int).Sub(new(big.Int).SetUint64(amount), fee)
			n := new(big.Int).Mul(new(big.Int).SetUint64(y), net)
			d := new(big.Int).Add(new(big.Int).SetUint64(x), net)
			want = n.Div(n, d).Uint64()
		}
		if got := model.Quote(x, y, amount); got != want {
			t.Fatalf("quote %d/%d/%d: %d != %d", x, y, amount, got, want)
		}
	}
}
