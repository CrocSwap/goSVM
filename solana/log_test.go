package solana_test

import (
	"fmt"
	"math"
	"math/rand"
	"testing"
	"unicode/utf8"

	"gosvm/sdk/fixture"
	"gosvm/solana"
)

// Native messages are the text the runtime writes after "Program log: ":
// sol_log_64's "{:#x}, ..." words and Pubkey::to_string's base58.
func TestLogMessages(t *testing.T) {
	var lines []string
	c := solana.Context{WriteLog: func(m string) { lines = append(lines, m) }}
	rng := rand.New(rand.NewSource(2026101904))
	var want []string
	for j := 0; j < 500; j++ {
		var w [5]uint64
		for k := range w {
			switch rng.Intn(3) {
			case 0:
				w[k] = rng.Uint64()
			case 1:
				w[k] = uint64(rng.Intn(17))
			default:
				w[k] = math.MaxUint64 >> uint(rng.Intn(64))
			}
		}
		if solana.Log64(c, w[0], w[1], w[2], w[3], w[4]) != 0 {
			t.Fatal("Log64 status")
		}
		want = append(want, fmt.Sprintf("%#x, %#x, %#x, %#x, %#x", w[0], w[1], w[2], w[3], w[4]))
		key := make([]byte, 32)
		rng.Read(key)
		for k := 0; k < rng.Intn(4); k++ {
			key[k] = 0 // leading zero bytes are base58 '1's
		}
		if j == 0 {
			key = make([]byte, 32)
		}
		if solana.LogPubkey(c, key) != 0 {
			t.Fatal("LogPubkey status")
		}
		want = append(want, fixture.Base58(key))
	}
	if solana.Log(c, "héllo 世界") != 0 || solana.Log(c, "") != 0 {
		t.Fatal("Log status")
	}
	want = append(want, "héllo 世界", "")
	if solana.LogPubkey(c, make([]byte, 31)) != 2019 || solana.LogPubkey(c, nil) != 2019 {
		t.Fatal("LogPubkey length")
	}
	if fmt.Sprint(lines) != fmt.Sprint(want) || len(lines) != len(want) {
		t.Fatal("log messages differ")
	}
	if solana.Log(solana.Context{}, "no callback") != 0 || solana.Log64(solana.Context{}, 1, 2, 3, 4, 5) != 0 || solana.LogPubkey(solana.Context{}, make([]byte, 32)) != 0 {
		t.Fatal("a nil WriteLog discards lines")
	}
}

// Log accepts exactly the valid UTF-8 strings (Rust's str::from_utf8), and
// panics natively where the runtime fails the instruction.
func TestLogRejectsInvalidUTF8(t *testing.T) {
	rng := rand.New(rand.NewSource(2026101905))
	inputs := []string{"\xff", "\xc0\x80", "\xed\xa0\x80", "\xf4\x90\x80\x80", "a\xe2\x82", "\xe2\x82\xac", "\xf0\x9f\x98\x80", "\x7f"}
	for j := 0; j < 20000; j++ {
		b := make([]byte, rng.Intn(6))
		for k := range b {
			b[k] = byte(0x7c + rng.Intn(0x84))
		}
		inputs = append(inputs, string(b))
	}
	invalid := 0
	for _, s := range inputs {
		panicked := func() (p bool) {
			defer func() { p = recover() != nil }()
			solana.Log(solana.Context{}, s)
			return false
		}()
		if panicked == utf8.ValidString(s) {
			t.Fatalf("%q: panicked %v", s, panicked)
		}
		if panicked {
			invalid++
		}
	}
	if invalid < 1000 {
		t.Fatal("too few invalid inputs", invalid)
	}
}
