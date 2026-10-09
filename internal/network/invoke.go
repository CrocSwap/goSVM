package network

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"time"
)

// Outcome is what a cluster reports for one invocation.
type Outcome struct {
	Signature    string          `json:"signature,omitempty"`
	Err          json.RawMessage `json:"err"`
	ReturnData   []byte          `json:"return_data"`
	ReturnSource string          `json:"return_program,omitempty"`
	ComputeUnits uint64          `json:"compute_units"`
	Logs         []string        `json:"logs"`
	Committed    bool            `json:"committed"`
}

func DecodeBase58(s string) ([]byte, error) {
	const alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
	n := new(big.Int)
	for _, c := range []byte(s) {
		i := -1
		for j := range alphabet {
			if alphabet[j] == c {
				i = j
				break
			}
		}
		if i < 0 {
			return nil, fmt.Errorf("invalid base58 %q", s)
		}
		n.Mul(n, big.NewInt(58)).Add(n, big.NewInt(int64(i)))
	}
	out := n.Bytes()
	for _, c := range []byte(s) {
		if c != '1' {
			break
		}
		out = append([]byte{0}, out...)
	}
	return out, nil
}

// Transaction builds a signed legacy transaction: the payer invokes program
// with data and no accounts.
func Transaction(payer ed25519.PrivateKey, program, blockhash []byte, data []byte) ([]byte, string) {
	shortvec := func(n int) []byte {
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
	msg := []byte{1, 0, 1}
	msg = append(msg, shortvec(2)...)
	msg = append(msg, payer.Public().(ed25519.PublicKey)...)
	msg = append(msg, program...)
	msg = append(msg, blockhash...)
	msg = append(msg, shortvec(1)...)
	msg = append(msg, 1)
	msg = append(msg, shortvec(0)...)
	msg = append(msg, shortvec(len(data))...)
	msg = append(msg, data...)
	sig := ed25519.Sign(payer, msg)
	tx := append(append(shortvec(1), sig...), msg...)
	return tx, Base58(sig)
}

// Invoke simulates the instruction, or when send is true submits it and waits
// for confirmation, returning the runtime's error, return data, CU and logs.
func Invoke(ctx context.Context, r RPC, payer ed25519.PrivateKey, programID string, data []byte, send bool) (Outcome, error) {
	var out Outcome
	program, err := DecodeBase58(programID)
	if err != nil || len(program) != 32 {
		return out, fmt.Errorf("invalid program ID %q", programID)
	}
	var latest struct {
		Value struct {
			Blockhash string `json:"blockhash"`
		} `json:"value"`
	}
	if err = r.Call(ctx, "getLatestBlockhash", []any{map[string]any{"commitment": "confirmed"}}, &latest); err != nil {
		return out, err
	}
	blockhash, err := DecodeBase58(latest.Value.Blockhash)
	if err != nil {
		return out, err
	}
	tx, signature := Transaction(payer, program, blockhash, data)
	encoded := base64.StdEncoding.EncodeToString(tx)
	type meta struct {
		Err        json.RawMessage `json:"err"`
		Logs       []string        `json:"logs"`
		LogMsgs    []string        `json:"logMessages"`
		Units      uint64          `json:"unitsConsumed"`
		CU         uint64          `json:"computeUnitsConsumed"`
		ReturnData *struct {
			ProgramID string   `json:"programId"`
			Data      []string `json:"data"`
		} `json:"returnData"`
	}
	fill := func(m meta) error {
		out.Err, out.ComputeUnits = m.Err, m.Units+m.CU
		out.Logs = append(m.Logs, m.LogMsgs...)
		if m.ReturnData != nil && len(m.ReturnData.Data) > 0 {
			out.ReturnSource = m.ReturnData.ProgramID
			b, err := base64.StdEncoding.DecodeString(m.ReturnData.Data[0])
			if err != nil {
				return err
			}
			out.ReturnData = b
		}
		return nil
	}
	if !send {
		var sim struct {
			Value meta `json:"value"`
		}
		if err = r.Call(ctx, "simulateTransaction", []any{encoded, map[string]any{"encoding": "base64", "sigVerify": true, "commitment": "confirmed"}}, &sim); err != nil {
			return out, err
		}
		return out, fill(sim.Value)
	}
	out.Signature = signature
	var sent string
	if err = r.Call(ctx, "sendTransaction", []any{encoded, map[string]any{"encoding": "base64", "skipPreflight": true}}, &sent); err != nil {
		return out, err
	}
	for {
		var tx *struct {
			Meta meta `json:"meta"`
		}
		err = r.Call(ctx, "getTransaction", []any{signature, map[string]any{"encoding": "json", "commitment": "confirmed", "maxSupportedTransactionVersion": 0}}, &tx)
		if err == nil && tx != nil {
			out.Committed = true
			return out, fill(tx.Meta)
		}
		select {
		case <-ctx.Done():
			return out, fmt.Errorf("transaction %s not confirmed: %w", signature, ctx.Err())
		case <-time.After(time.Second):
		}
	}
}
