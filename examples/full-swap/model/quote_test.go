package model

import (
	"math/big"
	"math/rand"
	"testing"
)

func refQuote(x, y, a uint64) uint64 {
	if x == 0 || y == 0 || a == 0 || a > ^uint64(0)-x {
		return 0
	}
	fee := new(big.Int).Mul(new(big.Int).SetUint64(a), big.NewInt(30))
	fee.Add(fee, big.NewInt(9999)).Div(fee, big.NewInt(10000))
	net := new(big.Int).Sub(new(big.Int).SetUint64(a), fee)
	num := new(big.Int).Mul(new(big.Int).SetUint64(y), net)
	den := new(big.Int).Add(new(big.Int).SetUint64(x), net)
	return num.Div(num, den).Uint64()
}

func TestWideQuote(t *testing.T) {
	rng := rand.New(rand.NewSource(71))
	for i := 0; i < 100000; i++ {
		x, y, a := rng.Uint64(), rng.Uint64(), rng.Uint64()
		if i%2 == 0 {
			a = a / (1 + uint64(i%100))
			x = x >> 1
		}
		got := Quote(x, y, a)
		want := refQuote(x, y, a)
		if got != want {
			t.Fatalf("x=%d y=%d amount=%d: got %d want %d", x, y, a, got, want)
		}
		if got > 0 {
			old := new(big.Int).Mul(new(big.Int).SetUint64(x), new(big.Int).SetUint64(y))
			next := new(big.Int).Mul(new(big.Int).SetUint64(x+a), new(big.Int).SetUint64(y-got))
			if next.Cmp(old) < 0 {
				t.Fatal("product decreased")
			}
		}
	}
	edges := []uint64{0, 1, 2, 3, 333, 334, 9999, 10000, 10001, 1 << 32, 1 << 63, ^uint64(0) - 1, ^uint64(0)}
	for _, x := range edges {
		for _, y := range edges {
			for _, a := range edges {
				if Quote(x, y, a) != refQuote(x, y, a) {
					t.Fatalf("edge mismatch %d %d %d", x, y, a)
				}
			}
		}
	}
}

func TestMulDiv(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	for i := 0; i < 100000; i++ {
		a, b, d := rng.Uint64(), rng.Uint64(), rng.Uint64()
		if i%100 == 0 {
			d = 1 << uint(i%64)
		}
		want := new(big.Int).Mul(new(big.Int).SetUint64(a), new(big.Int).SetUint64(b))
		want.Div(want, new(big.Int).SetUint64(d))
		v := uint64(0)
		if want.IsUint64() {
			v = want.Uint64()
		}
		if got := mulDiv(a, b, d); got != v {
			t.Fatalf("mulDiv(%d,%d,%d)=%d; expected %d", a, b, d, got, v)
		}
	}
	if mulDiv(1, 1, 0) != 0 {
		t.Fatal("invalid denominator")
	}
}
