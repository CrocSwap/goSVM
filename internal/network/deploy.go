package network

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"os"
	"time"
)

const (
	LoaderV3      = "BPFLoaderUpgradeab1e11111111111111111111111"
	SystemProgram = "11111111111111111111111111111111"
	SysvarRent    = "SysvarRent111111111111111111111111111111111"
	SysvarClock   = "SysvarC1ock11111111111111111111111111111111"
	// Bytes of program data per Write transaction, within the 1,232-byte packet.
	writeChunk = 900
)

// Meta is one instruction account reference.
type Meta struct {
	Key              []byte
	Signer, Writable bool
}

type Instruction struct {
	Program  []byte
	Accounts []Meta
	Data     []byte
}

func mustKey(s string) []byte {
	b, err := DecodeBase58(s)
	if err != nil || len(b) != 32 {
		panic("invalid built-in address " + s)
	}
	return b
}

func shortvec(n int) []byte {
	var b []byte
	for {
		c := byte(n & 0x7f)
		n >>= 7
		if n == 0 {
			return append(b, c)
		}
		b = append(b, c|0x80)
	}
}

// Sign compiles a legacy message (payer first; writable signers, readonly
// signers, writable then readonly non-signers) and signs it with every signer.
func Sign(payer ed25519.PrivateKey, blockhash []byte, ixs []Instruction, signers ...ed25519.PrivateKey) ([]byte, string, error) {
	type entry struct {
		key              []byte
		signer, writable bool
	}
	var keys []*entry
	find := func(k []byte) *entry {
		for _, e := range keys {
			if bytes.Equal(e.key, k) {
				return e
			}
		}
		e := &entry{key: k}
		keys = append(keys, e)
		return e
	}
	p := find(payer.Public().(ed25519.PublicKey))
	p.signer, p.writable = true, true
	for _, ix := range ixs {
		for _, m := range ix.Accounts {
			e := find(m.Key)
			e.signer = e.signer || m.Signer
			e.writable = e.writable || m.Writable
		}
		find(ix.Program)
	}
	var ordered []*entry
	for _, class := range [][2]bool{{true, true}, {true, false}, {false, true}, {false, false}} {
		for _, e := range keys {
			if e.signer == class[0] && e.writable == class[1] {
				ordered = append(ordered, e)
			}
		}
	}
	var nSigners, roSigned, roUnsigned int
	for _, e := range ordered {
		if e.signer {
			nSigners++
			if !e.writable {
				roSigned++
			}
		} else if !e.writable {
			roUnsigned++
		}
	}
	index := func(k []byte) byte {
		for i, e := range ordered {
			if bytes.Equal(e.key, k) {
				return byte(i)
			}
		}
		panic("unreachable")
	}
	msg := []byte{byte(nSigners), byte(roSigned), byte(roUnsigned)}
	msg = append(msg, shortvec(len(ordered))...)
	for _, e := range ordered {
		msg = append(msg, e.key...)
	}
	msg = append(msg, blockhash...)
	msg = append(msg, shortvec(len(ixs))...)
	for _, ix := range ixs {
		msg = append(msg, index(ix.Program))
		msg = append(msg, shortvec(len(ix.Accounts))...)
		for _, m := range ix.Accounts {
			msg = append(msg, index(m.Key))
		}
		msg = append(msg, shortvec(len(ix.Data))...)
		msg = append(msg, ix.Data...)
	}
	keyring := append([]ed25519.PrivateKey{payer}, signers...)
	tx := shortvec(nSigners)
	var first string
	for _, e := range ordered[:nSigners] {
		var key ed25519.PrivateKey
		for _, k := range keyring {
			if bytes.Equal(k.Public().(ed25519.PublicKey), e.key) {
				key = k
			}
		}
		if key == nil {
			return nil, "", fmt.Errorf("missing signer %s", Base58(e.key))
		}
		sig := ed25519.Sign(key, msg)
		if first == "" {
			first = Base58(sig)
		}
		tx = append(tx, sig...)
	}
	return append(tx, msg...), first, nil
}

func createAccount(payer, account []byte, lamports, space uint64, owner []byte) Instruction {
	data := make([]byte, 52)
	binary.LittleEndian.PutUint32(data, 0)
	binary.LittleEndian.PutUint64(data[4:], lamports)
	binary.LittleEndian.PutUint64(data[12:], space)
	copy(data[20:], owner)
	return Instruction{mustKey(SystemProgram), []Meta{{payer, true, true}, {account, true, true}}, data}
}

// ProgramDataAddress is the loader-v3 PDA [program] of the upgradeable loader.
func ProgramDataAddress(program []byte) []byte {
	loader := mustKey(LoaderV3)
	for bump := 255; bump >= 0; bump-- {
		data := append(append(append(bytes.Clone(program), byte(bump)), loader...), []byte("ProgramDerivedAddress")...)
		h := sha256.Sum256(data)
		if !onCurve(h[:]) {
			return h[:]
		}
	}
	panic("no PDA bump")
}

// onCurve reports whether 32 bytes decompress to an Edwards25519 point (math/big).
func onCurve(b []byte) bool {
	yBytes := bytes.Clone(b)
	yBytes[31] &= 127
	for i, j := 0, 31; i < j; i, j = i+1, j-1 {
		yBytes[i], yBytes[j] = yBytes[j], yBytes[i]
	}
	y := new(big.Int).SetBytes(yBytes)
	p := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 255), big.NewInt(19))
	d := new(big.Int).ModInverse(big.NewInt(121666), p)
	d.Mul(d, big.NewInt(-121665)).Mod(d, p)
	y2 := new(big.Int).Mul(y, y)
	y2.Mod(y2, p)
	num := new(big.Int).Sub(y2, big.NewInt(1))
	den := new(big.Int).Mul(d, y2)
	den.Add(den, big.NewInt(1))
	inv := new(big.Int).ModInverse(den, p)
	if inv == nil {
		return false
	}
	x2 := new(big.Int).Mul(num, inv)
	x2.Mod(x2, p)
	if x2.Sign() == 0 {
		return true
	}
	e := new(big.Int).Rsh(new(big.Int).Sub(p, big.NewInt(1)), 1)
	return new(big.Int).Exp(x2, e, p).Cmp(big.NewInt(1)) == 0
}

func (r RPC) blockhash(ctx context.Context) ([]byte, error) {
	var latest struct {
		Value struct {
			Blockhash string `json:"blockhash"`
		} `json:"value"`
	}
	if err := r.Call(ctx, "getLatestBlockhash", []any{map[string]any{"commitment": "confirmed"}}, &latest); err != nil {
		return nil, err
	}
	return DecodeBase58(latest.Value.Blockhash)
}

func (r RPC) rent(ctx context.Context, size int) (uint64, error) {
	var lamports uint64
	err := r.Call(ctx, "getMinimumBalanceForRentExemption", []any{size}, &lamports)
	return lamports, err
}

// send submits signed transactions and waits until each is confirmed or fails.
func (r RPC) send(ctx context.Context, txs [][]byte, sigs []string) error {
	for _, tx := range txs {
		var sig string
		if err := r.Call(ctx, "sendTransaction", []any{base64.StdEncoding.EncodeToString(tx), map[string]any{"encoding": "base64", "preflightCommitment": "confirmed"}}, &sig); err != nil {
			return err
		}
	}
	pending := map[string]bool{}
	for _, s := range sigs {
		pending[s] = true
	}
	for len(pending) > 0 {
		var list []string
		for s := range pending {
			list = append(list, s)
		}
		var st struct {
			Value []*struct {
				Err                json.RawMessage `json:"err"`
				ConfirmationStatus string          `json:"confirmationStatus"`
			} `json:"value"`
		}
		if err := r.Call(ctx, "getSignatureStatuses", []any{list}, &st); err != nil {
			return err
		}
		for i, v := range st.Value {
			if v == nil {
				continue
			}
			if len(v.Err) > 0 && string(v.Err) != "null" {
				return fmt.Errorf("transaction %s failed: %s", list[i], v.Err)
			}
			if v.ConfirmationStatus == "confirmed" || v.ConfirmationStatus == "finalized" {
				delete(pending, list[i])
			}
		}
		if len(pending) == 0 {
			break
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%d transactions unconfirmed: %w", len(pending), ctx.Err())
		case <-time.After(time.Second):
		}
	}
	return nil
}

// DeployV3 deploys an ELF through the upgradeable loader natively: a fresh
// buffer (its keypair saved to bufferPath until deployment finishes), chunked
// writes, then program creation and DeployWithMaxDataLen with the payer as
// upgrade authority. It returns the final deploy transaction signature.
func DeployV3(ctx context.Context, r RPC, payer, program ed25519.PrivateKey, elf []byte, bufferPath string, out io.Writer) (string, error) {
	loader := mustKey(LoaderV3)
	payerKey := []byte(payer.Public().(ed25519.PublicKey))
	programKey := []byte(program.Public().(ed25519.PublicKey))
	var existing struct {
		Value *json.RawMessage `json:"value"`
	}
	if err := r.Call(ctx, "getAccountInfo", []any{Base58(programKey), map[string]any{"encoding": "base64"}}, &existing); err != nil {
		return "", err
	}
	if existing.Value != nil {
		return "", fmt.Errorf("program %s already exists; upgrades are not implemented yet", Base58(programKey))
	}
	os.Remove(bufferPath)
	buffer, _, err := EnsureKeypair(bufferPath)
	if err != nil {
		return "", err
	}
	bufferKey := []byte(buffer.Public().(ed25519.PublicKey))
	bufferSize := 37 + len(elf)
	bufferRent, err := r.rent(ctx, bufferSize)
	if err != nil {
		return "", err
	}
	programRent, err := r.rent(ctx, 36)
	if err != nil {
		return "", err
	}
	hash, err := r.blockhash(ctx)
	if err != nil {
		return "", err
	}
	initialize := Instruction{loader, []Meta{{bufferKey, false, true}, {payerKey, false, false}}, []byte{0, 0, 0, 0}}
	tx, sig, err := Sign(payer, hash, []Instruction{createAccount(payerKey, bufferKey, bufferRent, uint64(bufferSize), loader), initialize}, buffer)
	if err != nil {
		return "", err
	}
	fmt.Fprintf(out, "Creating buffer %s (%d bytes)\n", Base58(bufferKey), bufferSize)
	if err = r.send(ctx, [][]byte{tx}, []string{sig}); err != nil {
		return "", err
	}
	var txs [][]byte
	var sigs []string
	for offset := 0; offset < len(elf); offset += writeChunk {
		end := min(offset+writeChunk, len(elf))
		data := make([]byte, 16, 16+end-offset)
		binary.LittleEndian.PutUint32(data, 1)
		binary.LittleEndian.PutUint32(data[4:], uint32(offset))
		binary.LittleEndian.PutUint64(data[8:], uint64(end-offset))
		data = append(data, elf[offset:end]...)
		write := Instruction{loader, []Meta{{bufferKey, false, true}, {payerKey, true, false}}, data}
		tx, sig, err := Sign(payer, hash, []Instruction{write})
		if err != nil {
			return "", err
		}
		txs, sigs = append(txs, tx), append(sigs, sig)
	}
	fmt.Fprintf(out, "Writing %d chunks\n", len(txs))
	if err = r.send(ctx, txs, sigs); err != nil {
		return "", err
	}
	if hash, err = r.blockhash(ctx); err != nil {
		return "", err
	}
	deployData := make([]byte, 12)
	binary.LittleEndian.PutUint32(deployData, 2)
	binary.LittleEndian.PutUint64(deployData[4:], uint64(len(elf)))
	deploy := Instruction{loader, []Meta{{payerKey, true, true}, {ProgramDataAddress(programKey), false, true}, {programKey, false, true}, {bufferKey, false, true},
		{mustKey(SysvarRent), false, false}, {mustKey(SysvarClock), false, false}, {mustKey(SystemProgram), false, false}, {payerKey, true, false}}, deployData}
	tx, sig, err = Sign(payer, hash, []Instruction{createAccount(payerKey, programKey, programRent, 36, loader), deploy}, program)
	if err != nil {
		return "", err
	}
	fmt.Fprintf(out, "Deploying program %s\n", Base58(programKey))
	if err = r.send(ctx, [][]byte{tx}, []string{sig}); err != nil {
		return "", err
	}
	os.Remove(bufferPath) // The loader consumed the buffer.
	return sig, nil
}
