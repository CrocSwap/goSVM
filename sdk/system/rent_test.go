package system

import (
	"encoding/binary"
	"gosvm/solana"
	"math"
	"math/rand"
	"testing"
)

func TestRentMinimumDifferential(t *testing.T) {
	// Independent ordinary-Go IEEE arithmetic is the pinned Rust formula's oracle.
	// Explicitly saturate its final cast, since Go's out-of-range float cast differs.
	oracle := func(rate, bits, size uint64) (uint64, uint64) {
		f := math.Float64frombits(bits)
		if size > 10485760 || rate > math.MaxUint64/(128+size) || math.IsNaN(f) || math.IsInf(f, 0) || f < 0 {
			return 0, ErrRent
		}
		value := float64((128+size)*rate) * f
		if value >= 18446744073709551616.0 {
			return math.MaxUint64, 0
		}
		return uint64(value), 0
	}
	check := func(rate, bits, size uint64) {
		t.Helper()
		amount, code := MinimumBalanceFor(rate, bits, size)
		want, status := oracle(rate, bits, size)
		if amount != want || code != status {
			t.Fatalf("rate=%d threshold=%016x size=%d: (%d,%d), want (%d,%d)", rate, bits, size, amount, code, want, status)
		}
	}
	thresholds := []uint64{0, 1, 1 << 63, 0x000fffffffffffff, 0x0010000000000000,
		math.Float64bits(0.625), math.Float64bits(1), math.Float64bits(1.5), math.Float64bits(2), math.Float64bits(2.5),
		math.Float64bits(math.Nextafter(1, 0)), math.Float64bits(math.Nextafter(1, 2)),
		0x7fefffffffffffff, math.Float64bits(math.Inf(1)), math.Float64bits(math.Inf(-1)), math.Float64bits(math.NaN()), math.Float64bits(-1)}
	for _, size := range []uint64{0, 1, 145, 10240, 10485760, 10485761, math.MaxUint64} {
		for _, rate := range []uint64{0, 1, 3480, 1 << 46, (1 << 53) - 1, math.MaxUint64 / 128, math.MaxUint64} {
			for _, bits := range thresholds {
				check(rate, bits, size)
			}
		}
	}
	rng := rand.New(rand.NewSource(2026100551))
	for j := 0; j < 100000; j++ {
		size := rng.Uint64() % 10485761
		rate := rng.Uint64() % (math.MaxUint64/(128+size) + 1)
		bits := rng.Uint64()
		if j%3 == 0 {
			bits = uint64(1010+rng.Intn(60))<<52 | bits&0xfffffffffffff
		}
		check(rate, bits, size)
	}
	if amount, code := MinimumBalanceFor(3480, math.Float64bits(2), 145); code != 0 || amount != 1900080 {
		t.Fatal(amount, code)
	}
}

func TestRentCurrentSysvar(t *testing.T) {
	c := solana.Context{ReadRent: func(b []byte) uint64 {
		binary.LittleEndian.PutUint64(b, 777)
		binary.LittleEndian.PutUint64(b[8:], math.Float64bits(1.5))
		b[16] = 17
		return 0
	}}
	if n, code := MinimumBalance(c, 145); n != 318181 || code != 0 {
		t.Fatal(n, code)
	}
	c.ReadRent = func(b []byte) uint64 { b[0] = 99; return 987 }
	if n, code := MinimumBalance(c, 145); n != 0 || code != 987 {
		t.Fatal(n, code)
	}
}

// The 2.0 and 1.0 fast path must return exactly what the rounding emulation
// returns, errors included, at every boundary: n = (128+size)*rate on either
// side of 2^53 and of uint64 overflow, every size limit, and both thresholds'
// neighbours, which must take the exact path.
func TestRentFastPathMatchesExact(t *testing.T) {
	check := func(rate, bits, size uint64) {
		t.Helper()
		amount, code := MinimumBalanceFor(rate, bits, size)
		want, status := minimumBalanceExact(rate, bits, size)
		if amount != want || code != status {
			t.Fatalf("rate=%d threshold=%016x size=%d: (%d,%d), exact (%d,%d)", rate, bits, size, amount, code, want, status)
		}
	}
	thresholds := []uint64{thresholdTwo, thresholdOne, thresholdTwo - 1, thresholdTwo + 1, thresholdOne - 1, thresholdOne + 1,
		thresholdTwo | 1<<63, thresholdOne | 1<<63, 0, 1 << 63, math.Float64bits(2.5), math.Float64bits(0.5), math.Float64bits(math.NaN())}
	sizes := []uint64{0, 1, 82, 165, 10240, MaxDataLen - 1, MaxDataLen, MaxDataLen + 1, math.MaxUint64 - 128, math.MaxUint64}
	fast := 0
	for _, size := range sizes {
		rates := []uint64{0, 1, 3480, 6960, math.MaxUint64}
		if size <= MaxDataLen {
			exact := (uint64(1)<<53 - 1) / (128 + size) // largest rate with n < 2^53
			wrap := math.MaxUint64 / (128 + size)       // largest rate without overflow
			for _, r := range []uint64{exact, wrap} {
				for d := uint64(0); d < 3; d++ {
					rates = append(rates, r-d, r+d+1)
				}
			}
		}
		for _, rate := range rates {
			for _, bits := range thresholds {
				check(rate, bits, size)
				if size <= MaxDataLen && rate <= (uint64(1)<<53-1)/(128+size) && (bits == thresholdTwo || bits == thresholdOne) {
					fast++
				}
			}
		}
	}
	rng := rand.New(rand.NewSource(2026101901))
	for j := 0; j < 1000000; j++ {
		size := rng.Uint64() % (MaxDataLen + 2)
		limit := (uint64(1)<<53 - 1) / (128 + size)
		rate := limit - 64 + rng.Uint64()%128
		if j%2 == 0 {
			rate = rng.Uint64() % (2 * limit)
		}
		check(rate, []uint64{thresholdTwo, thresholdOne}[j%4/2], size)
	}
	if fast < 90 {
		t.Fatalf("only %d boundary inputs take the fast path", fast)
	}
}
