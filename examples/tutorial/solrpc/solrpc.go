// Package solrpc is a deliberately small Solana client: base58, program-derived
// addresses, legacy transactions and the handful of JSON-RPC calls the counter
// client needs. It keeps the tutorial free of third-party Solana SDKs; a real
// service can use any Solana Go SDK with the same generated instruction bytes.
package solrpc

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"time"

	"filippo.io/edwards25519"
)

type PublicKey [32]byte

// SystemProgram is the System program's address (all zeros).
var SystemProgram PublicKey

const alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

func (k PublicKey) String() string { return Base58(k[:]) }

// Base58 encodes bytes with the Bitcoin alphabet, as Solana addresses are.
func Base58(b []byte) string {
	n := new(big.Int).SetBytes(b)
	var out []byte
	for mod := new(big.Int); n.Sign() > 0; {
		n.DivMod(n, big.NewInt(58), mod)
		out = append(out, alphabet[mod.Int64()])
	}
	for _, c := range b {
		if c != 0 {
			break
		}
		out = append(out, '1')
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return string(out)
}

// ParseKey decodes a base58 address.
func ParseKey(s string) (PublicKey, error) {
	var k PublicKey
	n := new(big.Int)
	for _, c := range []byte(s) {
		i := bytes.IndexByte([]byte(alphabet), c)
		if i < 0 {
			return k, fmt.Errorf("invalid base58 address %q", s)
		}
		n.Mul(n, big.NewInt(58)).Add(n, big.NewInt(int64(i)))
	}
	zeros := 0
	for zeros < len(s) && s[zeros] == '1' {
		zeros++
	}
	b := append(make([]byte, zeros), n.Bytes()...)
	if len(b) != 32 {
		return k, fmt.Errorf("address %q is not 32 bytes", s)
	}
	copy(k[:], b)
	return k, nil
}

// LoadKeypair reads a Solana CLI keypair file (a JSON array of 64 bytes),
// such as the payer gosvm deploy creates.
func LoadKeypair(path string) (ed25519.PrivateKey, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var key []byte
	if err = json.Unmarshal(b, &key); err != nil || len(key) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("%s is not a 64-byte keypair file", path)
	}
	return ed25519.PrivateKey(key), nil
}

func KeyOf(key ed25519.PrivateKey) (k PublicKey) {
	copy(k[:], key.Public().(ed25519.PublicKey))
	return k
}

// FindProgramAddress returns the first off-curve address for seeds plus a bump
// byte, counting down from 255, as Solana's find_program_address does.
func FindProgramAddress(program PublicKey, seeds ...[]byte) (PublicKey, byte, error) {
	for bump := 255; bump >= 0; bump-- {
		h := sha256.New()
		for _, s := range seeds {
			h.Write(s)
		}
		h.Write([]byte{byte(bump)})
		h.Write(program[:])
		h.Write([]byte("ProgramDerivedAddress"))
		var k PublicKey
		copy(k[:], h.Sum(nil))
		if _, err := new(edwards25519.Point).SetBytes(k[:]); err != nil { // off the curve
			return k, byte(bump), nil
		}
	}
	return PublicKey{}, 0, errors.New("no program address found")
}

type AccountMeta struct {
	Key              PublicKey
	Signer, Writable bool
}

type Instruction struct {
	Program  PublicKey
	Accounts []AccountMeta
	Data     []byte
}

// Transaction builds and signs a legacy transaction. The first signer pays the
// fee; signers must be listed in the order their keys first appear.
func Transaction(blockhash PublicKey, ix Instruction, signers ...ed25519.PrivateKey) ([]byte, string) {
	// Accounts are ordered writable signers, readonly signers, writable
	// non-signers, readonly non-signers (the program last).
	metas := []AccountMeta{{Key: KeyOf(signers[0]), Signer: true, Writable: true}}
	metas = append(metas, ix.Accounts...)
	metas = append(metas, AccountMeta{Key: ix.Program})
	var keys []AccountMeta
	for _, m := range metas {
		found := false
		for i := range keys {
			if keys[i].Key == m.Key {
				keys[i].Signer, keys[i].Writable = keys[i].Signer || m.Signer, keys[i].Writable || m.Writable
				found = true
			}
		}
		if !found {
			keys = append(keys, m)
		}
	}
	var ordered []AccountMeta
	var header [3]byte
	for _, class := range []struct{ signer, writable bool }{{true, true}, {true, false}, {false, true}, {false, false}} {
		for _, k := range keys {
			if k.Signer == class.signer && k.Writable == class.writable {
				ordered = append(ordered, k)
				switch {
				case k.Signer:
					header[0]++
					if !k.Writable {
						header[1]++
					}
				case !k.Writable:
					header[2]++
				}
			}
		}
	}
	index := func(k PublicKey) byte {
		for i := range ordered {
			if ordered[i].Key == k {
				return byte(i)
			}
		}
		panic("missing key")
	}
	msg := append(header[:], shortvec(len(ordered))...)
	for _, k := range ordered {
		msg = append(msg, k.Key[:]...)
	}
	msg = append(msg, blockhash[:]...)
	msg = append(msg, 1, index(ix.Program))
	msg = append(msg, shortvec(len(ix.Accounts))...)
	for _, m := range ix.Accounts {
		msg = append(msg, index(m.Key))
	}
	msg = append(msg, shortvec(len(ix.Data))...)
	msg = append(msg, ix.Data...)
	tx := shortvec(len(signers))
	for _, s := range signers {
		tx = append(tx, ed25519.Sign(s, msg)...)
	}
	return append(tx, msg...), Base58(tx[1:65])
}

func shortvec(n int) []byte {
	var b []byte
	for {
		c := byte(n & 0x7f)
		if n >>= 7; n == 0 {
			return append(b, c)
		}
		b = append(b, c|0x80)
	}
}

// Client is a minimal JSON-RPC client.
type Client struct{ URL string }

func (c Client) Call(ctx context.Context, method string, params []any, result any) error {
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var out struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return fmt.Errorf("%s: HTTP %s: %w", method, resp.Status, err)
	}
	if out.Error != nil {
		return fmt.Errorf("%s: %s", method, out.Error.Message)
	}
	return json.Unmarshal(out.Result, result)
}

// AccountData returns an account's data, or nil when the account does not exist.
func (c Client) AccountData(ctx context.Context, key PublicKey) ([]byte, error) {
	var r struct {
		Value *struct {
			Data []string `json:"data"`
		} `json:"value"`
	}
	if err := c.Call(ctx, "getAccountInfo", []any{key.String(), map[string]any{"encoding": "base64", "commitment": "confirmed"}}, &r); err != nil || r.Value == nil {
		return nil, err
	}
	return base64.StdEncoding.DecodeString(r.Value.Data[0])
}

// Result is a confirmed transaction's outcome.
type Result struct {
	Signature    string
	Err          json.RawMessage
	ComputeUnits uint64
	Logs         []string
}

// Send signs ix with signers (the first pays), submits it and waits until the
// transaction is confirmed, returning its error (if any), compute units and logs.
func (c Client) Send(ctx context.Context, ix Instruction, signers ...ed25519.PrivateKey) (Result, error) {
	var latest struct {
		Value struct {
			Blockhash string `json:"blockhash"`
		} `json:"value"`
	}
	if err := c.Call(ctx, "getLatestBlockhash", []any{map[string]any{"commitment": "confirmed"}}, &latest); err != nil {
		return Result{}, err
	}
	blockhash, err := ParseKey(latest.Value.Blockhash)
	if err != nil {
		return Result{}, err
	}
	tx, signature := Transaction(blockhash, ix, signers...)
	var sent string
	if err = c.Call(ctx, "sendTransaction", []any{base64.StdEncoding.EncodeToString(tx), map[string]any{"encoding": "base64", "preflightCommitment": "confirmed"}}, &sent); err != nil {
		return Result{Signature: signature}, err
	}
	for {
		var got *struct {
			Meta struct {
				Err   json.RawMessage `json:"err"`
				Units uint64          `json:"computeUnitsConsumed"`
				Logs  []string        `json:"logMessages"`
			} `json:"meta"`
		}
		err = c.Call(ctx, "getTransaction", []any{signature, map[string]any{"encoding": "json", "commitment": "confirmed", "maxSupportedTransactionVersion": 0}}, &got)
		if err == nil && got != nil {
			return Result{signature, got.Meta.Err, got.Meta.Units, got.Meta.Logs}, nil
		}
		select {
		case <-ctx.Done():
			return Result{Signature: signature}, ctx.Err()
		case <-time.After(time.Second):
		}
	}
}
