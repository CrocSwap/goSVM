package token

import (
	"bytes"
	"encoding/binary"
	"gosvm/solana"
	"math/big"
	"math/rand"
	"testing"
)

func tlv(kind uint16, value []byte) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint16(b, kind)
	binary.LittleEndian.PutUint16(b[2:], uint16(len(value)))
	return append(b, value...)
}

func extendedData(base []byte, accountType byte, entries ...[]byte) []byte {
	b := append(append([]byte(nil), base...), make([]byte, 165-len(base))...)
	b = append(b, accountType)
	for _, e := range entries {
		b = append(b, e...)
	}
	return b
}

func baseMint(supply uint64, decimals byte) []byte {
	b := make([]byte, 82)
	b[0] = 1
	copy(b[4:], bytes.Repeat([]byte{7}, 32))
	binary.LittleEndian.PutUint64(b[36:], supply)
	b[44], b[45] = decimals, 1
	return b
}

func baseAccount(amount uint64) []byte {
	b := make([]byte, 165)
	copy(b, bytes.Repeat([]byte{6}, 32))
	copy(b[32:], bytes.Repeat([]byte{8}, 32))
	binary.LittleEndian.PutUint64(b[64:], amount)
	b[108] = 1
	return b
}

func feeConfig(olderEpoch, olderMax uint64, olderPoints uint16, newerEpoch, newerMax uint64, newerPoints uint16) []byte {
	b := make([]byte, 108)
	copy(b, bytes.Repeat([]byte{3}, 32))
	binary.LittleEndian.PutUint64(b[64:], 42)
	binary.LittleEndian.PutUint64(b[72:], olderEpoch)
	binary.LittleEndian.PutUint64(b[80:], olderMax)
	binary.LittleEndian.PutUint16(b[88:], olderPoints)
	binary.LittleEndian.PutUint64(b[90:], newerEpoch)
	binary.LittleEndian.PutUint64(b[98:], newerMax)
	binary.LittleEndian.PutUint16(b[106:], newerPoints)
	return b
}

func ownedContext(data []byte, program [32]byte) solana.Context {
	return solana.Context{Accounts: []solana.Account{{Key: bytes.Repeat([]byte{6}, 32), Owner: append([]byte(nil), program[:]...), Data: data}}}
}

func TestReadMintLayouts(t *testing.T) {
	pointer := tlv(ExtensionMetadataPointer, bytes.Repeat([]byte{9}, 64))
	for _, tc := range []struct {
		name    string
		data    []byte
		program [32]byte
		want    uint64
	}{
		{"classic", baseMint(5, 6), ProgramID(), 0},
		{"2022-base", baseMint(5, 6), Program2022ID(), 0},
		{"2022-metadata-pointer-234", extendedData(baseMint(5, 6), 1, pointer), Program2022ID(), 0},
		{"2022-two-extensions", extendedData(baseMint(5, 6), 1, tlv(1, feeConfig(0, 1, 2, 3, 4, 5)), pointer), Program2022ID(), 0},
		{"2022-trailing-zero-entry", append(extendedData(baseMint(5, 6), 1, pointer), 0, 0, 0, 0, 1), Program2022ID(), 0},
		{"2022-type-only", extendedData(baseMint(5, 6), 1), Program2022ID(), 0},
		{"classic-extended", extendedData(baseMint(5, 6), 1, pointer), ProgramID(), ErrMint},
		{"account-type", extendedData(baseMint(5, 6), 2, pointer), Program2022ID(), ErrMint},
		{"padding", func() []byte { b := extendedData(baseMint(5, 6), 1, pointer); b[100] = 1; return b }(), Program2022ID(), ErrMint},
		{"tlv-overrun", append(extendedData(baseMint(5, 6), 1), tlv(18, make([]byte, 64))[:40]...), Program2022ID(), ErrMint},
		{"tlv-partial-length", append(extendedData(baseMint(5, 6), 1, pointer), 18, 0, 0), Program2022ID(), ErrMint},
		{"tlv-trailing-byte", append(extendedData(baseMint(5, 6), 1, pointer), 18), Program2022ID(), 0},
		{"tlv-trailing-zero-type", append(extendedData(baseMint(5, 6), 1, pointer), 0, 0, 7), Program2022ID(), 0},
		{"multisig-length", append(extendedData(baseMint(5, 6), 1), make([]byte, 355-166)...), Program2022ID(), ErrMint},
		{"short", baseMint(5, 6)[:81], Program2022ID(), ErrMint},
		{"uninitialized", func() []byte { b := baseMint(5, 6); b[45] = 0; return b }(), ProgramID(), ErrMint},
		{"option-tag", func() []byte { b := baseMint(5, 6); b[1] = 1; return b }(), ProgramID(), ErrMint},
		{"freeze-tag", func() []byte { b := baseMint(5, 6); b[46] = 2; return b }(), ProgramID(), ErrMint},
		{"owner", baseMint(5, 6), [32]byte{1}, ErrMint},
		{"token-account", baseAccount(1), Program2022ID(), ErrMint},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state, code := ReadMint(ownedContext(tc.data, tc.program), 0)
			if code != tc.want {
				t.Fatal(code, tc.want)
			}
			if code == 0 && (state.Supply != 5 || state.Decimals != 6 || !state.HasAuthority || state.Authority[0] != 7 || state.HasFreeze) {
				t.Fatal(state)
			}
		})
	}
	c := ownedContext(baseMint(5, 6), ProgramID())
	c.Accounts[0].Executable = true
	if _, code := ReadMint(c, 0); code != ErrMint {
		t.Fatal("executable mint")
	}
	if _, code := ReadMint(c, 1); code != ErrMint {
		t.Fatal("index")
	}
}

func TestReadAccountLayouts(t *testing.T) {
	immutable := tlv(ExtensionImmutableOwner, nil)
	withheld := tlv(ExtensionTransferFeeAmount, make([]byte, 8))
	for _, tc := range []struct {
		name    string
		data    []byte
		program [32]byte
		want    uint64
	}{
		{"classic", baseAccount(9), ProgramID(), 0},
		{"2022-base", baseAccount(9), Program2022ID(), 0},
		{"2022-ata-170", extendedData(baseAccount(9), 2, immutable), Program2022ID(), 0},
		{"2022-fee-182", extendedData(baseAccount(9), 2, immutable, withheld), Program2022ID(), 0},
		{"classic-extended", extendedData(baseAccount(9), 2, immutable), ProgramID(), ErrAccount},
		{"mint-type", extendedData(baseAccount(9), 1, immutable), Program2022ID(), ErrAccount},
		{"tlv-overrun", extendedData(baseAccount(9), 2, tlv(2, make([]byte, 8))[:10]), Program2022ID(), ErrAccount},
		{"multisig-length", append(extendedData(baseAccount(9), 2), make([]byte, 355-166)...), Program2022ID(), ErrAccount},
		{"uninitialized", func() []byte { b := baseAccount(9); b[108] = 0; return b }(), Program2022ID(), ErrAccount},
		{"native", func() []byte { b := baseAccount(9); b[109] = 1; return b }(), Program2022ID(), ErrAccount},
		{"delegate-tag", func() []byte { b := baseAccount(9); b[72] = 2; return b }(), Program2022ID(), ErrAccount},
		{"close-tag", func() []byte { b := baseAccount(9); b[130] = 1; return b }(), Program2022ID(), ErrAccount},
		{"mint", baseMint(1, 1), Program2022ID(), ErrAccount},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state, code := ReadAccount(ownedContext(tc.data, tc.program), 0)
			if code != tc.want {
				t.Fatal(code, tc.want)
			}
			if code == 0 && (state.Amount != 9 || state.Mint[0] != 6 || state.Authority[0] != 8 || state.Frozen) {
				t.Fatal(state)
			}
		})
	}
	frozen := extendedData(baseAccount(9), 2, immutable)
	frozen[108] = 2
	if state, code := ReadAccount(ownedContext(frozen, Program2022ID()), 0); code != 0 || !state.Frozen {
		t.Fatal("frozen extended account", state, code)
	}
}

func TestFindExtensionAndTransferFeeConfig(t *testing.T) {
	pointer := tlv(ExtensionMetadataPointer, bytes.Repeat([]byte{9}, 64))
	data := extendedData(baseMint(5, 6), 1, pointer, tlv(ExtensionTransferFeeConfig, feeConfig(1, 100, 50, 7, 1<<40, 10000)))
	c := ownedContext(data, Program2022ID())
	if offset, length, code := FindExtension(c, 0, ExtensionMetadataPointer); code != 0 || offset != 170 || length != 64 {
		t.Fatal(offset, length, code)
	}
	if offset, length, code := FindExtension(c, 0, ExtensionTransferFeeConfig); code != 0 || offset != 238 || length != 108 {
		t.Fatal(offset, length, code)
	}
	if _, _, code := FindExtension(c, 0, ExtensionImmutableOwner); code != ErrExtension {
		t.Fatal(code)
	}
	if _, _, code := FindExtension(c, 0, 0); code != ErrExtension {
		t.Fatal("uninitialized type", code)
	}
	if _, _, code := FindExtension(ownedContext(baseMint(5, 6), ProgramID()), 0, ExtensionMetadataPointer); code != ErrAccount {
		t.Fatal("classic", code)
	}
	config, code := ReadTransferFeeConfig(c, 0)
	if code != 0 || config.ConfigAuthority[0] != 3 || config.WithdrawAuthority[0] != 0 || config.WithheldAmount != 42 || config.OlderEpoch != 1 || config.OlderMaximumFee != 100 || config.OlderBasisPoints != 50 || config.NewerEpoch != 7 || config.NewerMaximumFee != 1<<40 || config.NewerBasisPoints != 10000 {
		t.Fatal(config, code)
	}
	// Older fee before epoch 7: ceil(1001*50/10000) = 6; newer from 7: 100%.
	if fee, code := Fee(c, 0, 6, 1001); code != 0 || fee != 6 {
		t.Fatal(fee, code)
	}
	if fee, code := Fee(c, 0, 7, 1001); code != 0 || fee != 1001 {
		t.Fatal(fee, code)
	}
	if fee, code := Fee(c, 0, 0, 1_000_000); code != 0 || fee != 100 {
		t.Fatal("older maximum", fee, code)
	}
	for _, mint := range []solana.Context{ownedContext(baseMint(5, 6), ProgramID()), ownedContext(extendedData(baseMint(5, 6), 1, pointer), Program2022ID())} {
		if fee, code := Fee(mint, 0, 9, 1000); code != 0 || fee != 0 {
			t.Fatal("no fee extension", fee, code)
		}
	}
	if _, code := Fee(ownedContext(baseAccount(1), Program2022ID()), 0, 0, 1); code != ErrMint {
		t.Fatal("not a mint", code)
	}
	short := extendedData(baseMint(5, 6), 1, tlv(ExtensionTransferFeeConfig, make([]byte, 100)))
	if _, code := Fee(ownedContext(short, Program2022ID()), 0, 0, 1); code != ErrExtension {
		t.Fatal("short config", code)
	}
}

// TestTransferFeeArithmetic compares the two-limb division with big.Int for
// every basis-point boundary and random amounts, including overflow.
func TestTransferFeeArithmetic(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	amounts := []uint64{0, 1, 9999, 10000, 10001, 1 << 32, 1<<32 - 1, 1<<63 + 12345, ^uint64(0)}
	for j := 0; j < 20000; j++ {
		amounts = append(amounts, r.Uint64()>>(r.Intn(64)))
	}
	points := []uint16{0, 1, 2, 49, 50, 9999, 10000, 10001, 65535}
	for _, amount := range amounts {
		for _, p := range append(points, uint16(r.Intn(65536))) {
			maximum := ^uint64(0)
			if amount%3 == 0 {
				maximum = amount / 7
			}
			config := TransferFeeConfig{NewerBasisPoints: p, NewerMaximumFee: maximum}
			fee, code := config.Fee(0, amount)
			want := new(big.Int).Mul(new(big.Int).SetUint64(amount), big.NewInt(int64(p)))
			want.Add(want, big.NewInt(9999)).Div(want, big.NewInt(10000))
			switch {
			case p == 0 || amount == 0:
				if fee != 0 || code != 0 {
					t.Fatal(amount, p, fee, code)
				}
			case !want.IsUint64():
				if code != ErrExtension {
					t.Fatal("overflow", amount, p, fee, code)
				}
			default:
				expected := want.Uint64()
				if expected > maximum {
					expected = maximum
				}
				if code != 0 || fee != expected {
					t.Fatal(amount, p, maximum, fee, expected, code)
				}
			}
		}
	}
}
