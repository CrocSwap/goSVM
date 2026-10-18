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
	// SIMD-0431: extensions must add at least 10 KiB unless reaching the maximum.
	minimumExtend     = 10 * 1024
	maxAccountData    = 10 * 1024 * 1024
	programDataHeader = 45
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
	address, _, ok := FindProgramAddress([][]byte{program}, mustKey(LoaderV3))
	if !ok {
		panic("no PDA bump")
	}
	return address
}

// CreateProgramAddress derives the address of seeds (each at most 32 bytes,
// at most 16 including any bump) under program, reporting false when the hash
// lies on the curve or the seeds are out of bounds.
func CreateProgramAddress(seeds [][]byte, program []byte) ([]byte, bool) {
	if len(seeds) > 16 {
		return nil, false
	}
	h := sha256.New()
	for _, s := range seeds {
		if len(s) > 32 {
			return nil, false
		}
		h.Write(s)
	}
	h.Write(program)
	h.Write([]byte("ProgramDerivedAddress"))
	sum := h.Sum(nil)
	if onCurve(sum) {
		return nil, false
	}
	return sum, true
}

// FindProgramAddress appends the canonical bump: the highest of 255..0 whose
// address is off the curve, as Solana's find_program_address does.
func FindProgramAddress(seeds [][]byte, program []byte) ([]byte, byte, bool) {
	for bump := 255; bump >= 0; bump-- {
		if address, ok := CreateProgramAddress(append(append([][]byte(nil), seeds...), []byte{byte(bump)}), program); ok {
			return address, byte(bump), true
		}
	}
	return nil, 0, false
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

// sendRaw submits one signed transaction, retrying transient RPC failures.
// Resubmitting identical bytes is safe: the signature deduplicates it.
func (r RPC) sendRaw(ctx context.Context, tx []byte) error {
	var err error
	for attempt := 0; attempt < 5; attempt++ {
		var sig string
		if err = r.Call(ctx, "sendTransaction", []any{base64.StdEncoding.EncodeToString(tx), map[string]any{"encoding": "base64", "preflightCommitment": "confirmed"}}, &sig); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(attempt+1) * time.Second):
		}
	}
	return err
}

// await polls until every signature is confirmed (true) or the timeout passes
// (returned as still pending). A failed transaction is an error.
func (r RPC) await(ctx context.Context, sigs []string, timeout time.Duration) (map[string]bool, error) {
	pending := map[string]bool{}
	for _, s := range sigs {
		pending[s] = true
	}
	deadline := time.Now().Add(timeout)
	for len(pending) > 0 && time.Now().Before(deadline) {
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
		if err := r.Call(ctx, "getSignatureStatuses", []any{list}, &st); err == nil {
			for i, v := range st.Value {
				if v == nil {
					continue
				}
				if len(v.Err) > 0 && string(v.Err) != "null" {
					return pending, fmt.Errorf("transaction %s failed: %s", list[i], v.Err)
				}
				if v.ConfirmationStatus == "confirmed" || v.ConfirmationStatus == "finalized" {
					delete(pending, list[i])
				}
			}
		}
		if len(pending) == 0 {
			break
		}
		select {
		case <-ctx.Done():
			return pending, ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return pending, nil
}

// send submits signed transactions and waits until each is confirmed or fails.
// Unconfirmed transactions are resubmitted unchanged until their blockhash
// expires; they are never re-signed, so none can execute twice.
func (r RPC) send(ctx context.Context, txs [][]byte, sigs []string) error {
	pending := map[string]bool{}
	for _, s := range sigs {
		pending[s] = true
	}
	for round := 0; round < 6 && len(pending) > 0; round++ {
		for i, tx := range txs {
			if pending[sigs[i]] {
				if err := r.sendRaw(ctx, tx); err != nil && round == 0 {
					return err
				}
			}
		}
		var list []string
		for s := range pending {
			list = append(list, s)
		}
		left, err := r.await(ctx, list, 15*time.Second)
		if err != nil {
			return err
		}
		pending = left
	}
	if len(pending) > 0 {
		return fmt.Errorf("%d transactions unconfirmed after their blockhash expired; rerun the command", len(pending))
	}
	return nil
}

// DeployV3 deploys an ELF through the upgradeable loader natively: a fresh
// buffer (its keypair saved to bufferPath until deployment finishes), chunked
// writes, then program creation and DeployWithMaxDataLen with the payer as
// upgrade authority. It returns the final deploy transaction signature.
func DeployV3(ctx context.Context, r RPC, payer, authority, program ed25519.PrivateKey, elf []byte, bufferPath string, out io.Writer) (string, error) {
	loader := mustKey(LoaderV3)
	payerKey := []byte(payer.Public().(ed25519.PublicKey))
	authorityKey := []byte(authority.Public().(ed25519.PublicKey))
	programKey := []byte(program.Public().(ed25519.PublicKey))
	var existing struct {
		Value *json.RawMessage `json:"value"`
	}
	if err := r.Call(ctx, "getAccountInfo", []any{Base58(programKey), map[string]any{"encoding": "base64"}}, &existing); err != nil {
		return "", err
	}
	if existing.Value != nil {
		return "", fmt.Errorf("program %s already exists; use UpgradeV3", Base58(programKey))
	}
	bufferKey, err := r.writeBuffer(ctx, payer, authority, elf, bufferPath, out)
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
	deployData := make([]byte, 12)
	binary.LittleEndian.PutUint32(deployData, 2)
	binary.LittleEndian.PutUint64(deployData[4:], uint64(len(elf)))
	deploy := Instruction{loader, []Meta{{payerKey, true, true}, {ProgramDataAddress(programKey), false, true}, {programKey, false, true}, {bufferKey, false, true},
		{mustKey(SysvarRent), false, false}, {mustKey(SysvarClock), false, false}, {mustKey(SystemProgram), false, false}, {authorityKey, true, false}}, deployData}
	tx, sig, err := Sign(payer, hash, []Instruction{createAccount(payerKey, programKey, programRent, 36, loader), deploy}, program, authority)
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

// writeBuffer creates a loader-v3 buffer under the upgrade authority, paid by
// the payer (its keypair kept at bufferPath until consumed), and writes the ELF.
func (r RPC) writeBuffer(ctx context.Context, payer, authority ed25519.PrivateKey, elf []byte, bufferPath string, out io.Writer) ([]byte, error) {
	loader := mustKey(LoaderV3)
	payerKey := []byte(payer.Public().(ed25519.PublicKey))
	authorityKey := []byte(authority.Public().(ed25519.PublicKey))
	os.Remove(bufferPath)
	buffer, _, err := EnsureKeypair(bufferPath)
	if err != nil {
		return nil, err
	}
	bufferKey := []byte(buffer.Public().(ed25519.PublicKey))
	bufferSize := 37 + len(elf)
	bufferRent, err := r.rent(ctx, bufferSize)
	if err != nil {
		return nil, err
	}
	hash, err := r.blockhash(ctx)
	if err != nil {
		return nil, err
	}
	initialize := Instruction{loader, []Meta{{bufferKey, false, true}, {authorityKey, false, false}}, []byte{0, 0, 0, 0}}
	tx, sig, err := Sign(payer, hash, []Instruction{createAccount(payerKey, bufferKey, bufferRent, uint64(bufferSize), loader), initialize}, buffer)
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(out, "Creating buffer %s (%d bytes)\n", Base58(bufferKey), bufferSize)
	if err = r.send(ctx, [][]byte{tx}, []string{sig}); err != nil {
		return nil, err
	}
	// Writes are idempotent (same offset, same bytes), so a chunk that is still
	// unconfirmed after a round is re-signed with a fresh blockhash and resent.
	var offsets []int
	for offset := 0; offset < len(elf); offset += writeChunk {
		offsets = append(offsets, offset)
	}
	fmt.Fprintf(out, "Writing %d chunks\n", len(offsets))
	for round := 0; len(offsets) > 0; round++ {
		if round == 6 {
			return nil, fmt.Errorf("%d buffer chunks unconfirmed; rerun the command (the buffer is reclaimed automatically)", len(offsets))
		}
		if round > 0 {
			if hash, err = r.blockhash(ctx); err != nil {
				return nil, err
			}
		}
		bySig := map[string]int{}
		var sigs []string
		for _, offset := range offsets {
			end := min(offset+writeChunk, len(elf))
			data := make([]byte, 16, 16+end-offset)
			binary.LittleEndian.PutUint32(data, 1)
			binary.LittleEndian.PutUint32(data[4:], uint32(offset))
			binary.LittleEndian.PutUint64(data[8:], uint64(end-offset))
			data = append(data, elf[offset:end]...)
			write := Instruction{loader, []Meta{{bufferKey, false, true}, {authorityKey, true, false}}, data}
			tx, sig, err := Sign(payer, hash, []Instruction{write}, authority)
			if err != nil {
				return nil, err
			}
			r.sendRaw(ctx, tx) // A chunk that fails to send is retried next round.
			bySig[sig] = offset
			sigs = append(sigs, sig)
		}
		left, err := r.await(ctx, sigs, 30*time.Second)
		if err != nil {
			return nil, err
		}
		offsets = offsets[:0]
		for sig := range left {
			offsets = append(offsets, bySig[sig])
		}
	}
	return bufferKey, nil
}

// Account returns an account's owner and data, or ok=false if it does not exist.
func (r RPC) Account(ctx context.Context, address []byte) (owner string, data []byte, ok bool, err error) {
	var res struct {
		Value *struct {
			Owner string   `json:"owner"`
			Data  []string `json:"data"`
		} `json:"value"`
	}
	if err = r.Call(ctx, "getAccountInfo", []any{Base58(address), map[string]any{"encoding": "base64", "commitment": "confirmed"}}, &res); err != nil || res.Value == nil {
		return "", nil, false, err
	}
	data, err = base64.StdEncoding.DecodeString(res.Value.Data[0])
	return res.Value.Owner, data, true, err
}

// Deployed reports whether a loader-v3 program exists at programKey and, if so,
// its upgrade authority (nil when immutable) and deployed program-data bytes.
func (r RPC) Deployed(ctx context.Context, programKey []byte) (exists bool, authority, programData []byte, err error) {
	owner, data, ok, err := r.Account(ctx, programKey)
	if err != nil || !ok {
		return false, nil, nil, err
	}
	if owner != LoaderV3 || len(data) < 36 || binary.LittleEndian.Uint32(data) != 2 {
		return true, nil, nil, fmt.Errorf("%s is not an upgradeable (loader-v3) program", Base58(programKey))
	}
	address := ProgramDataAddress(programKey)
	if !bytes.Equal(data[4:36], address) {
		return true, nil, nil, fmt.Errorf("%s points at unexpected program data %s", Base58(programKey), Base58(data[4:36]))
	}
	owner, data, ok, err = r.Account(ctx, address)
	if err != nil {
		return true, nil, nil, err
	}
	if !ok || owner != LoaderV3 || len(data) < programDataHeader || binary.LittleEndian.Uint32(data) != 3 {
		return true, nil, nil, fmt.Errorf("invalid program data account %s", Base58(address))
	}
	if data[12] == 1 {
		authority = data[13:45]
	}
	return true, authority, data[programDataHeader:], nil
}

// Unchanged reports whether deployed program data already holds exactly elf
// (followed only by zero padding).
func Unchanged(deployed, elf []byte) bool {
	if len(deployed) < len(elf) || !bytes.Equal(deployed[:len(elf)], elf) {
		return false
	}
	for _, b := range deployed[len(elf):] {
		if b != 0 {
			return false
		}
	}
	return true
}

// ExtendBytes returns how many bytes to add so capacity holds size, respecting
// the 10 KiB minimum extension and the maximum account size (0: no extension).
func ExtendBytes(capacity, size int) (int, error) {
	if size <= capacity {
		return 0, nil
	}
	headroom := maxAccountData - (programDataHeader + capacity)
	need := size - capacity
	if need > headroom {
		return 0, fmt.Errorf("program of %d bytes exceeds the maximum program data size", size)
	}
	return min(max(need, minimumExtend), headroom), nil
}

func (r RPC) slot(ctx context.Context) (uint64, error) {
	var slot uint64
	err := r.Call(ctx, "getSlot", []any{map[string]any{"commitment": "confirmed"}}, &slot)
	return slot, err
}

// UpgradeV3 replaces a deployed program's code with elf, signed by its upgrade
// authority (fees and any extension paid by the payer). It extends program data first when elf is larger, in
// a separate transaction, then waits for a later slot before upgrading. The
// returned signature is empty when the deployed code is already identical.
func UpgradeV3(ctx context.Context, r RPC, payer, authority ed25519.PrivateKey, programKey, elf []byte, bufferPath string, out io.Writer) (string, error) {
	loader := mustKey(LoaderV3)
	payerKey := []byte(payer.Public().(ed25519.PublicKey))
	authorityKey := []byte(authority.Public().(ed25519.PublicKey))
	exists, current, deployed, err := r.Deployed(ctx, programKey)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", fmt.Errorf("program %s does not exist; deploy it first", Base58(programKey))
	}
	if current == nil {
		return "", fmt.Errorf("program %s is immutable (no upgrade authority)", Base58(programKey))
	}
	if !bytes.Equal(current, authorityKey) {
		return "", fmt.Errorf("program %s upgrade authority is %s, not %s; pass that authority's keypair with -authority", Base58(programKey), Base58(current), Base58(authorityKey))
	}
	if Unchanged(deployed, elf) {
		fmt.Fprintf(out, "Program %s already runs this exact build; nothing to upgrade\n", Base58(programKey))
		return "", nil
	}
	programData := ProgramDataAddress(programKey)
	extend, err := ExtendBytes(len(deployed), len(elf))
	if err != nil {
		return "", err
	}
	if extend > 0 {
		data := make([]byte, 8)
		binary.LittleEndian.PutUint32(data, 6)
		binary.LittleEndian.PutUint32(data[4:], uint32(extend))
		ix := Instruction{loader, []Meta{{programData, false, true}, {programKey, false, true}, {mustKey(SystemProgram), false, false}, {payerKey, true, true}}, data}
		hash, err := r.blockhash(ctx)
		if err != nil {
			return "", err
		}
		tx, sig, err := Sign(payer, hash, []Instruction{ix})
		if err != nil {
			return "", err
		}
		fmt.Fprintf(out, "Extending program data by %d bytes\n", extend)
		if err = r.send(ctx, [][]byte{tx}, []string{sig}); err != nil {
			return "", err
		}
		// The loader refuses further changes in the slot that extended it.
		start, err := r.slot(ctx)
		if err != nil {
			return "", err
		}
		for {
			now, err := r.slot(ctx)
			if err != nil {
				return "", err
			}
			if now > start {
				break
			}
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(400 * time.Millisecond):
			}
		}
	}
	bufferKey, err := r.writeBuffer(ctx, payer, authority, elf, bufferPath, out)
	if err != nil {
		return "", err
	}
	hash, err := r.blockhash(ctx)
	if err != nil {
		return "", err
	}
	upgrade := Instruction{loader, []Meta{{programData, false, true}, {programKey, false, true}, {bufferKey, false, true}, {payerKey, false, true},
		{mustKey(SysvarRent), false, false}, {mustKey(SysvarClock), false, false}, {authorityKey, true, false}}, []byte{3, 0, 0, 0}}
	tx, sig, err := Sign(payer, hash, []Instruction{upgrade}, authority)
	if err != nil {
		return "", err
	}
	fmt.Fprintf(out, "Upgrading program %s\n", Base58(programKey))
	if err = r.send(ctx, [][]byte{tx}, []string{sig}); err != nil {
		return "", err
	}
	os.Remove(bufferPath) // The loader consumed the buffer; its lamports went to the payer.
	return sig, nil
}

// ReclaimBuffers closes leftover loader-v3 buffers under the given authority
// (from interrupted deployments), returning their lamports to the payer.
// Closing needs only the authority's signature. RPCs that refuse the account
// scan are skipped silently.
func (r RPC) ReclaimBuffers(ctx context.Context, payer, authority ed25519.PrivateKey, out io.Writer) error {
	payerKey := []byte(payer.Public().(ed25519.PublicKey))
	authorityKey := []byte(authority.Public().(ed25519.PublicKey))
	filter := base64.StdEncoding.EncodeToString(append([]byte{1, 0, 0, 0, 1}, authorityKey...))
	var accounts []struct {
		Pubkey  string `json:"pubkey"`
		Account struct {
			Lamports uint64 `json:"lamports"`
		} `json:"account"`
	}
	if err := r.Call(ctx, "getProgramAccounts", []any{LoaderV3, map[string]any{"encoding": "base64", "commitment": "confirmed",
		"dataSlice": map[string]any{"offset": 0, "length": 0}, "filters": []any{map[string]any{"memcmp": map[string]any{"offset": 0, "bytes": filter, "encoding": "base64"}}}}}, &accounts); err != nil {
		return nil
	}
	for _, a := range accounts {
		buffer, err := DecodeBase58(a.Pubkey)
		if err != nil {
			return err
		}
		hash, err := r.blockhash(ctx)
		if err != nil {
			return err
		}
		closeIx := Instruction{mustKey(LoaderV3), []Meta{{buffer, false, true}, {payerKey, false, true}, {authorityKey, true, false}}, []byte{5, 0, 0, 0}}
		tx, sig, err := Sign(payer, hash, []Instruction{closeIx}, authority)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "Reclaiming %.6f from leftover buffer %s\n", float64(a.Account.Lamports)/1e9, a.Pubkey)
		if err = r.send(ctx, [][]byte{tx}, []string{sig}); err != nil {
			return err
		}
	}
	return nil
}

// AuthorityChange selects a SetAuthority form. Exactly one of NewKey (checked:
// the new authority signs), NewAddress (unchecked) or Immutable is set.
type AuthorityChange struct {
	NewKey     ed25519.PrivateKey
	NewAddress []byte
	Immutable  bool
}

// SetAuthority changes a program's upgrade authority, signed by the current one.
func SetAuthority(ctx context.Context, r RPC, payer, current ed25519.PrivateKey, programKey []byte, change AuthorityChange) (string, error) {
	currentKey := []byte(current.Public().(ed25519.PublicKey))
	exists, authority, _, err := r.Deployed(ctx, programKey)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", fmt.Errorf("program %s does not exist", Base58(programKey))
	}
	if authority == nil {
		return "", fmt.Errorf("program %s is already immutable", Base58(programKey))
	}
	if !bytes.Equal(authority, currentKey) {
		return "", fmt.Errorf("program %s upgrade authority is %s, not %s; pass it with -authority", Base58(programKey), Base58(authority), Base58(currentKey))
	}
	metas := []Meta{{ProgramDataAddress(programKey), false, true}, {currentKey, true, false}}
	signers := []ed25519.PrivateKey{current}
	data := []byte{4, 0, 0, 0}
	switch {
	case change.NewKey != nil && change.NewAddress == nil && !change.Immutable:
		metas = append(metas, Meta{[]byte(change.NewKey.Public().(ed25519.PublicKey)), true, false})
		signers = append(signers, change.NewKey)
		data[0] = 7 // SetAuthorityChecked
	case change.NewAddress != nil && change.NewKey == nil && !change.Immutable:
		if len(change.NewAddress) != 32 {
			return "", fmt.Errorf("invalid new authority address")
		}
		metas = append(metas, Meta{change.NewAddress, false, false})
	case change.Immutable && change.NewKey == nil && change.NewAddress == nil:
	default:
		return "", fmt.Errorf("choose exactly one authority change")
	}
	hash, err := r.blockhash(ctx)
	if err != nil {
		return "", err
	}
	tx, sig, err := Sign(payer, hash, []Instruction{{mustKey(LoaderV3), metas, data}}, signers...)
	if err != nil {
		return "", err
	}
	return sig, r.send(ctx, [][]byte{tx}, []string{sig})
}
