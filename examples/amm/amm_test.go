package amm

import (
	"bytes"
	"encoding/binary"
	"math/big"
	"math/rand"
	"testing"
)

func TestSwapAgainstBigIntegerReference(t *testing.T) {
	rng := rand.New(rand.NewSource(99))
	for i := 0; i < 10000; i++ {
		x, y, a := uint64(rng.Intn(900000000)+1), uint64(rng.Intn(1000000000)+1), uint64(rng.Intn(1000000)+1)
		state, ix := make([]byte, 24), make([]byte, 16)
		binary.LittleEndian.PutUint64(state, x)
		binary.LittleEndian.PutUint64(state[8:], y)
		binary.LittleEndian.PutUint64(ix, a)
		effective := new(big.Int).SetUint64(a * 9970)
		num := new(big.Int).Mul(new(big.Int).SetUint64(y), effective)
		den := new(big.Int).Add(new(big.Int).SetUint64(x*10000), effective)
		out := new(big.Int).Div(num, den).Uint64()
		before := bytes.Clone(state)
		code := Process(state, ix)
		if out == 0 {
			if code != 5 || !bytes.Equal(before, state) {
				t.Fatal("zero output must fail without mutation")
			}
			continue
		}
		if code != 0 || binary.LittleEndian.Uint64(state) != x+a || binary.LittleEndian.Uint64(state[8:]) != y-out || binary.LittleEndian.Uint64(state[16:]) != 1 {
			t.Fatal("swap mismatch")
		}
		oldK := new(big.Int).Mul(new(big.Int).SetUint64(x), new(big.Int).SetUint64(y))
		newK := new(big.Int).Mul(new(big.Int).SetUint64(x+a), new(big.Int).SetUint64(y-out))
		if newK.Cmp(oldK) < 0 {
			t.Fatal("constant product decreased")
		}
	}
}

func TestInvalidLengths(t *testing.T) {
	for _, length := range []int{0, 1, 23, 25} {
		s := make([]byte, length)
		if Process(s, make([]byte, 16)) != 1 {
			t.Fatal("bad state length accepted")
		}
	}
	for _, length := range []int{0, 1, 15, 17} {
		if Process(make([]byte, 24), make([]byte, length)) != 1 {
			t.Fatal("bad instruction length accepted")
		}
	}
}
