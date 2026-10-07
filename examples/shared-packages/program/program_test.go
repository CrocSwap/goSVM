package program

import (
	"example.com/shared-app/model"
	"example.com/shared-app/quote"
	"example.com/shared-app/wire"
	"testing"
)

func TestSharedSwap(t *testing.T) {
	p := model.Pool{ReserveX: 1000000, ReserveY: 2000000, Sequence: 4}
	a := model.SwapArgs{AmountIn: 1000, MinOut: 1998}
	s, i := make([]byte, wire.PoolBytes), make([]byte, wire.SwapBytes)
	wire.EncodePool(s, p)
	wire.EncodeSwap(i, a)
	if out := quote.Quote(p, a); out != 1998 {
		t.Fatal(out)
	}
	if code := Process(s, i); code != 0 {
		t.Fatal(code)
	}
	want := model.Pool{ReserveX: 1001000, ReserveY: 1998002, Sequence: 5}
	if got := wire.DecodePool(s); got != want {
		t.Fatalf("got %+v want %+v", got, want)
	}
	a.MinOut = 100000
	wire.EncodeSwap(i, a)
	if code := Process(s, i); code != model.ErrSlippage {
		t.Fatal(code)
	}
	if wire.DecodePool(s) != want {
		t.Fatal("failed instruction changed state")
	}
}
