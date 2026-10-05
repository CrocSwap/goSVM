package tokenswap

import (
	"bytes"
	"encoding/binary"
	"math/big"
	"math/rand"
	"testing"

	"gosvm/solana"
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

func fixture() (solana.Context, *[]uint64) {
	keys := make([][]byte, 8)
	for i := range keys {
		keys[i] = bytes.Repeat([]byte{byte(i + 1)}, 32)
	}
	keys[7] = []byte{6, 221, 246, 225, 215, 101, 161, 147, 217, 203, 225, 70, 206, 235, 121, 172, 28, 180, 133, 237, 95, 91, 55, 145, 58, 140, 245, 133, 126, 255, 0, 169}
	c := solana.Context{Accounts: make([]solana.Account, 8), ID: bytes.Repeat([]byte{9}, 32), InstructionData: make([]byte, 16)}
	for i := range c.Accounts {
		c.Accounts[i] = solana.Account{Key: keys[i], Owner: make([]byte, 32)}
	}
	c.Accounts[0].Data = make([]byte, 137)
	c.Accounts[0].Owner = c.ID
	c.Accounts[0].Writable = true
	s := c.Accounts[0].Data
	copy(s, keys[3])
	copy(s[32:], keys[4])
	copy(s[64:], bytes.Repeat([]byte{10}, 32))
	copy(s[96:], bytes.Repeat([]byte{11}, 32))
	s[128] = 255
	c.Accounts[1].Signer = true
	c.Accounts[7].Executable = true
	for i := 2; i <= 5; i++ {
		a := &c.Accounts[i]
		a.Writable = true
		a.Owner = keys[7]
		a.Data = make([]byte, 165)
		a.Data[108] = 1
		mint := s[64:96]
		if i >= 4 {
			mint = s[96:128]
		}
		copy(a.Data, mint)
		authority := keys[6]
		if i == 2 || i == 5 {
			authority = keys[1]
		}
		copy(a.Data[32:], authority)
		binary.LittleEndian.PutUint64(a.Data[64:], 1_000_000_000_000_000_000)
	}
	binary.LittleEndian.PutUint64(c.InstructionData, 1_000_000_000_000_000)
	c.CheckPDA = func(i uint64, seed []byte, bump byte) bool {
		return i == 6 && bump == 255 && bytes.Equal(seed, keys[0]) && bytes.Equal(c.Accounts[6].Key, keys[6])
	}
	calls := []uint64{}
	c.Invoke = func(program, src, dst, authority, amount uint64, seed []byte, bump byte) uint64 {
		calls = append(calls, src)
		if program != 7 || c.Accounts[src].Data[108] != 1 || c.Accounts[dst].Data[108] != 1 {
			return 900
		}
		if src == 2 && (authority != 1 || seed != nil) {
			return 901
		}
		if src == 4 && (authority != 6 || !bytes.Equal(seed, keys[0]) || bump != 255) {
			return 902
		}
		s, d := c.Accounts[src].Data, c.Accounts[dst].Data
		balance := binary.LittleEndian.Uint64(s[64:])
		other := binary.LittleEndian.Uint64(d[64:])
		if balance < amount || other > ^uint64(0)-amount {
			return 903
		}
		binary.LittleEndian.PutUint64(s[64:], balance-amount)
		binary.LittleEndian.PutUint64(d[64:], other+amount)
		return 0
	}
	return c, &calls
}

func TestNativeSwap(t *testing.T) {
	c, calls := fixture()
	amount := load64(c.InstructionData, 0)
	out := refQuote(load64(c.Accounts[3].Data, 64), load64(c.Accounts[4].Data, 64), amount)
	if code := Process(c); code != 0 {
		t.Fatalf("swap: %d", code)
	}
	if len(*calls) != 2 || (*calls)[0] != 2 || (*calls)[1] != 4 || load64(c.Accounts[0].Data, 129) != 1 {
		t.Fatal("wrong CPI order or counter")
	}
	if load64(c.Accounts[3].Data, 64) != 1e18+amount || load64(c.Accounts[4].Data, 64) != 1e18-out {
		t.Fatal("wrong vault balances")
	}
}

func TestNativeValidation(t *testing.T) {
	tests := []struct {
		name   string
		code   uint64
		change func(*solana.Context)
	}{
		{"count", 101, func(c *solana.Context) { c.Accounts = c.Accounts[:7] }},
		{"short-instruction", 102, func(c *solana.Context) { c.InstructionData = c.InstructionData[:15] }},
		{"short-pool", 102, func(c *solana.Context) { c.Accounts[0].Data = c.Accounts[0].Data[:136] }},
		{"pool-owner", 103, func(c *solana.Context) { c.Accounts[0].Owner = make([]byte, 32) }},
		{"pool-readonly", 103, func(c *solana.Context) { c.Accounts[0].Writable = false }},
		{"signer", 104, func(c *solana.Context) { c.Accounts[1].Signer = false }},
		{"token-program", 105, func(c *solana.Context) { c.Accounts[7].Key = make([]byte, 32) }},
		{"alias", 106, func(c *solana.Context) { c.Accounts[5].Key = c.Accounts[2].Key }},
		{"vault-substitute", 107, func(c *solana.Context) { c.Accounts[3].Key = bytes.Repeat([]byte{66}, 32) }},
		{"token-owner", 108, func(c *solana.Context) { c.Accounts[2].Owner = make([]byte, 32) }},
		{"token-length", 108, func(c *solana.Context) { c.Accounts[5].Data = c.Accounts[5].Data[:164] }},
		{"token-readonly", 108, func(c *solana.Context) { c.Accounts[4].Writable = false }},
		{"native-token", 108, func(c *solana.Context) { c.Accounts[2].Data[109] = 1 }},
		{"mint", 109, func(c *solana.Context) { c.Accounts[5].Data[0] ^= 1 }},
		{"authority", 110, func(c *solana.Context) { c.Accounts[2].Data[32] ^= 1 }},
		{"bump", 111, func(c *solana.Context) { c.Accounts[0].Data[128]-- }},
		{"input-overflow", 112, func(c *solana.Context) { store64(c.InstructionData, 0, ^uint64(0)) }},
		{"insufficient-input", 112, func(c *solana.Context) { store64(c.Accounts[2].Data, 64, 0) }},
		{"slippage", 113, func(c *solana.Context) { store64(c.InstructionData, 8, ^uint64(0)) }},
		{"output-overflow", 113, func(c *solana.Context) { store64(c.Accounts[5].Data, 64, ^uint64(0)) }},
		{"counter-overflow", 114, func(c *solana.Context) { store64(c.Accounts[0].Data, 129, ^uint64(0)) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, calls := fixture()
			tc.change(&c)
			before := make([][]byte, len(c.Accounts))
			for i := range before {
				before[i] = bytes.Clone(c.Accounts[i].Data)
			}
			if got := Process(c); got != tc.code {
				t.Fatalf("got %d want %d", got, tc.code)
			}
			if len(*calls) != 0 {
				t.Fatal("CPI before validation")
			}
			for i, a := range c.Accounts {
				if !bytes.Equal(before[i], a.Data) {
					t.Fatal("mutation before validation")
				}
			}
		})
	}
}

func TestFailedSecondCPI(t *testing.T) {
	c, calls := fixture()
	c.Accounts[5].Data[108] = 2
	if code := Process(c); code != 900 {
		t.Fatalf("expected second transfer failure: %d", code)
	}
	if len(*calls) != 2 || load64(c.Accounts[0].Data, 129) != 0 {
		t.Fatal("wrong failure handling")
	}
	// Rollback belongs to the VM; the end-to-end suite must assert its atomicity.
}
