package compiler

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gosvm/internal/sbftest"
	"gosvm/internal/targets"
	"gosvm/internal/testvm"
)

// A small affine secp256k1 oracle over math/big: signing for the vectors,
// and recovery for the expected key or status, independent of the runtime.
var (
	secpP  = secpHex("fffffffffffffffffffffffffffffffffffffffffffffffffffffffefffffc2f")
	secpN  = secpHex("fffffffffffffffffffffffffffffffebaaedce6af48a03bbfd25e8cd0364141")
	secpGx = secpHex("79be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798")
	secpGy = secpHex("483ada7726a3c4655da4fbfc0e1108a8fd17b448a68554199c47d08ffb10d4b8")
)

func secpHex(s string) *big.Int { n, _ := new(big.Int).SetString(s, 16); return n }

type secpPoint struct{ x, y *big.Int } // nil x is the point at infinity

func secpAdd(a, b secpPoint) secpPoint {
	if a.x == nil {
		return b
	}
	if b.x == nil {
		return a
	}
	p := secpP
	var l *big.Int
	if a.x.Cmp(b.x) == 0 {
		if new(big.Int).Add(a.y, b.y).Mod(new(big.Int).Add(a.y, b.y), p).Sign() == 0 {
			return secpPoint{}
		}
		num := new(big.Int).Mul(big.NewInt(3), new(big.Int).Mul(a.x, a.x))
		l = num.Mul(num, new(big.Int).ModInverse(new(big.Int).Lsh(a.y, 1), p))
	} else {
		num := new(big.Int).Sub(b.y, a.y)
		l = num.Mul(num, new(big.Int).ModInverse(new(big.Int).Mod(new(big.Int).Sub(b.x, a.x), p), p))
	}
	l.Mod(l, p)
	x := new(big.Int).Sub(new(big.Int).Mul(l, l), new(big.Int).Add(a.x, b.x))
	x.Mod(x, p)
	y := new(big.Int).Sub(new(big.Int).Mul(l, new(big.Int).Sub(a.x, x)), a.y)
	return secpPoint{x, y.Mod(y, p)}
}

func secpMul(k *big.Int, a secpPoint) secpPoint {
	r := secpPoint{}
	for i := k.BitLen() - 1; i >= 0; i-- {
		r = secpAdd(r, r)
		if k.Bit(i) == 1 {
			r = secpAdd(r, a)
		}
	}
	return r
}

func secpBytes(n *big.Int) []byte { return n.FillBytes(make([]byte, 32)) }

func secpKey(q secpPoint) []byte { return append(secpBytes(q.x), secpBytes(q.y)...) }

// secpSign returns r||s and the recovery ID for a deterministic nonce.
func secpSign(hash []byte, d, k *big.Int) ([]byte, uint64) {
	g := secpPoint{secpGx, secpGy}
	R := secpMul(k, g)
	r := new(big.Int).Mod(R.x, secpN)
	e := new(big.Int).Mod(new(big.Int).SetBytes(hash), secpN)
	s := new(big.Int).Mul(new(big.Int).ModInverse(k, secpN), new(big.Int).Add(e, new(big.Int).Mul(r, d)))
	s.Mod(s, secpN)
	id := uint64(R.y.Bit(0))
	if R.x.Cmp(secpN) >= 0 {
		id |= 2
	}
	return append(secpBytes(r), secpBytes(s)...), id
}

// secpRecover is the expected key, or the runtime status 2 or 3.
func secpRecover(hash []byte, id uint64, signature []byte) ([]byte, uint64) {
	if id > 3 {
		return nil, 2
	}
	r, s := new(big.Int).SetBytes(signature[:32]), new(big.Int).SetBytes(signature[32:])
	if r.Sign() == 0 || s.Sign() == 0 || r.Cmp(secpN) >= 0 || s.Cmp(secpN) >= 0 {
		return nil, 3
	}
	x := new(big.Int).Set(r)
	if id&2 != 0 {
		x.Add(x, secpN)
	}
	if x.Cmp(secpP) >= 0 {
		return nil, 3
	}
	rhs := new(big.Int).Add(new(big.Int).Exp(x, big.NewInt(3), secpP), big.NewInt(7))
	rhs.Mod(rhs, secpP)
	y := new(big.Int).Exp(rhs, new(big.Int).Rsh(new(big.Int).Add(secpP, big.NewInt(1)), 2), secpP)
	if new(big.Int).Exp(y, big.NewInt(2), secpP).Cmp(rhs) != 0 {
		return nil, 3
	}
	if y.Bit(0) != uint(id&1) {
		y.Sub(secpP, y)
	}
	e := new(big.Int).Mod(new(big.Int).SetBytes(hash), secpN)
	ri := new(big.Int).ModInverse(r, secpN)
	u1 := new(big.Int).Mod(new(big.Int).Mul(new(big.Int).Neg(e), ri), secpN)
	u2 := new(big.Int).Mod(new(big.Int).Mul(s, ri), secpN)
	q := secpAdd(secpMul(u1, secpPoint{secpGx, secpGy}), secpMul(u2, secpPoint{x, y}))
	if q.x == nil {
		return nil, 3
	}
	return secpKey(q), 0
}

type secpCase struct {
	name      string
	hash      []byte
	id        uint64
	signature []byte
	key       []byte // expected key, nil for a failure status
	status    uint64
}

func secpCases(t *testing.T) []secpCase {
	scalar := func(label string, i int) *big.Int {
		h := sha256.Sum256([]byte(fmt.Sprintf("gosvm secp256k1 %s %d", label, i)))
		return new(big.Int).Add(new(big.Int).Mod(new(big.Int).SetBytes(h[:]), new(big.Int).Sub(secpN, big.NewInt(1))), big.NewInt(1))
	}
	var cases []secpCase
	add := func(name string, hash []byte, id uint64, signature []byte) {
		key, status := secpRecover(hash, id, signature)
		cases = append(cases, secpCase{name, hash, id, signature, key, status})
	}
	for i := 0; i < 8; i++ {
		d := scalar("key", i)
		hash := sha256.Sum256([]byte(fmt.Sprintf("gosvm secp256k1 message %d", i)))
		signature, id := secpSign(hash[:], d, scalar("nonce", i))
		want := secpKey(secpMul(d, secpPoint{secpGx, secpGy}))
		if key, status := secpRecover(hash[:], id, signature); status != 0 || hex.EncodeToString(key) != hex.EncodeToString(want) {
			t.Fatalf("oracle recovery %d", i)
		}
		add(fmt.Sprintf("valid-%d", i), hash[:], id, signature)
		add(fmt.Sprintf("other-parity-%d", i), hash[:], id^1, signature)
		high := append(append([]byte(nil), signature[:32]...), secpBytes(new(big.Int).Sub(secpN, new(big.Int).SetBytes(signature[32:])))...)
		add(fmt.Sprintf("high-s-%d", i), hash[:], id^1, high)
	}
	hash := sha256.Sum256([]byte("gosvm secp256k1 message 0"))
	signature, id := secpSign(hash[:], scalar("key", 0), scalar("nonce", 0))
	for _, bad := range []uint64{4, 255, 256, 1 << 40} {
		add(fmt.Sprintf("recovery-id-%d", bad), hash[:], bad, signature)
	}
	zeroR := append(make([]byte, 32), signature[32:]...)
	add("zero-r", hash[:], id, zeroR)
	zeroS := append(append([]byte(nil), signature[:32]...), make([]byte, 32)...)
	add("zero-s", hash[:], id, zeroS)
	overR := append(secpBytes(secpN), signature[32:]...)
	add("r-at-order", hash[:], id, overR)
	add("high-x-recovery-id", hash[:], id|2, signature)
	return cases
}

func secpIx(c secpCase, lengths [3]byte) []byte {
	ix := make([]byte, 107)
	copy(ix, c.hash)
	binary.LittleEndian.PutUint64(ix[32:], c.id)
	copy(ix[40:], c.signature)
	ix[104], ix[105], ix[106] = lengths[0], lengths[1], lengths[2]
	return ix
}

// TestSecp256k1RecoverSBF builds testdata/secp256k1 for fogo-v0 and
// solana-v3 and checks the runtime's recovered keys and statuses against a
// math/big oracle: valid signatures, the other parity, high-s, bad recovery
// IDs and malformed scalars, plus a wrong-length call that never reaches the
// syscall. It skips without the v1.57 backend or a runner.
// GOSVM_SECP256K1_RESULTS=<dir> keeps the fixtures, ELFs and reports.
func TestSecp256k1RecoverSBF(t *testing.T) {
	t.Parallel()
	llvm := modernLLVM()
	if llvm == "" {
		t.Skip("pinned platform-tools v1.57 backend required")
	}
	keep := os.Getenv("GOSVM_SECP256K1_RESULTS")
	cases := secpCases(t)
	for _, profile := range []string{target.Fogo, target.Solana} {
		t.Run(profile, func(t *testing.T) {
			runner, err := testvm.RunnerFor("", profile)
			if err != nil {
				t.Skipf("runner unavailable: %v", err)
			}
			dir := t.TempDir()
			if keep != "" {
				dir = filepath.Join(keep, profile)
				if err = os.MkdirAll(dir, 0755); err != nil {
					t.Fatal(err)
				}
			}
			if err = os.WriteFile(filepath.Join(dir, "program.go"), mustRead(t, filepath.Join("testdata", "secp256k1", "program.go")), 0644); err != nil {
				t.Fatal(err)
			}
			elf := filepath.Join(dir, "program.so")
			if err = Build(dir, elf, llvm, profile); err != nil {
				t.Fatal(err)
			}
			program := base58(probeKey("gosvm secp256k1 probe program; local tests only"))
			preset := strings.Repeat("5b", 72)
			suite := sbftest.FixtureSuite{Format: sbftest.FixtureFormat, ProgramID: program,
				PayerSeed: hex.EncodeToString(probeKey("gosvm secp256k1 probe payer; never fund")),
				Accounts: []sbftest.FixtureAccount{{Name: "result", Address: base58(probeKey("gosvm secp256k1 result")),
					Initial: &sbftest.FixtureState{Data: preset, Owner: program, Lamports: 1_000_000_000}}}}
			step := func(ix []byte, status uint64, key []byte) sbftest.FixtureStep {
				after := make([]byte, 72)
				binary.LittleEndian.PutUint64(after, status)
				for i := 8; i < 72; i++ {
					after[i] = 0x5b
				}
				copy(after[8:], key)
				return sbftest.FixtureStep{Instructions: []sbftest.FixtureInstruction{{Program: "program", Data: hex.EncodeToString(ix),
					Accounts: []sbftest.FixtureMeta{{Account: "result", Writable: true}}}},
					Expect: sbftest.FixtureExpectation{Error: json.RawMessage("null"), Accounts: []sbftest.FixtureCheck{{Account: "result",
						State: &sbftest.FixtureState{Data: hex.EncodeToString(after), Owner: program, Lamports: 1_000_000_000}}}}}
			}
			for _, c := range cases {
				suite.Cases = append(suite.Cases, sbftest.FixtureCase{Name: c.name, Steps: []sbftest.FixtureStep{step(secpIx(c, [3]byte{32, 64, 64}), c.status, c.key)}})
			}
			suite.Cases = append(suite.Cases, sbftest.FixtureCase{Name: "short-output", Steps: []sbftest.FixtureStep{step(secpIx(cases[0], [3]byte{32, 64, 63}), 2018, nil)}})
			fixtures, err := json.MarshalIndent(suite, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			fixturePath := filepath.Join(dir, "fixtures.json")
			if err = os.WriteFile(fixturePath, fixtures, 0644); err != nil {
				t.Fatal(err)
			}
			p, err := target.Resolve(profile)
			if err != nil {
				t.Fatal(err)
			}
			report := filepath.Join(dir, "svm-results.json")
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			var log strings.Builder
			if err = sbftest.RunGeneral(ctx, elf, fixturePath, report, io.Writer(&log), sbftest.FastOptions{Runner: runner, Profile: p.Name, PlatformTools: p.Tools}); err != nil {
				t.Fatalf("%v\n%s", err, log.String())
			}
			var parsed struct {
				Cases []sbftest.FixtureCaseResult `json:"cases"`
			}
			if err = json.Unmarshal(mustRead(t, report), &parsed); err != nil {
				t.Fatal(err)
			}
			if len(parsed.Cases) != len(suite.Cases) {
				t.Fatalf("ran %d of %d cases", len(parsed.Cases), len(suite.Cases))
			}
			var summary strings.Builder
			for _, c := range parsed.Cases {
				fmt.Fprintf(&summary, "%s %d CU\n", c.Name, c.Steps[0].CU)
			}
			t.Logf("%d secp256k1 scenarios passed\n%s", len(parsed.Cases), summary.String())
		})
	}
}
