package protocol

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"gosvm/solana"
	"os"
	"testing"
)

func TestIndependentReference(t *testing.T) {
	b, err := os.ReadFile("../../build/protocol/fixtures.json")
	if os.IsNotExist(err) {
		t.Skip("run python3 scripts/protocol_generate.py")
	}
	if err != nil {
		t.Fatal(err)
	}
	var fixtures struct {
		Authority []byte
		Cases     []struct {
			Name, Mutation                                string
			State, Config, Payload, Instruction, Expected []byte
			Code                                          uint64
			Accounts                                      int
		}
	}
	// JSON arrays decode into []byte as well as base64 strings.
	if err = json.Unmarshal(b, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, v := range fixtures.Cases {
		t.Run(v.Name, func(t *testing.T) {
			id := bytes.Repeat([]byte{99}, 32)
			state := bytes.Clone(v.State)
			c := solana.Context{ID: id, InstructionData: v.Instruction, Hash: func(b []byte) []byte { h := sha256.Sum256(b); return h[:] }}
			for i := 0; i < 4; i++ {
				c.Accounts = append(c.Accounts, solana.Account{Key: bytes.Repeat([]byte{byte(i + 1)}, 32), Owner: id, Writable: i == 0})
			}
			c.Accounts[0].Data = state
			c.Accounts[1].Key = fixtures.Authority
			c.Accounts[1].Signer = true
			c.Accounts[2].Data = v.Config
			c.Accounts[3].Data = v.Payload
			switch v.Mutation {
			case "wrong-owner":
				c.Accounts[0].Owner = make([]byte, 32)
			case "config-owner":
				c.Accounts[2].Owner = make([]byte, 32)
			case "payload-owner":
				c.Accounts[3].Owner = make([]byte, 32)
			case "readonly":
				c.Accounts[0].Writable = false
			case "no-signer":
				c.Accounts[1].Signer = false
			case "alias":
				c.Accounts[3] = c.Accounts[2]
			}
			if v.Accounts <= 4 {
				c.Accounts = c.Accounts[:v.Accounts]
			} else {
				c.Accounts = append(c.Accounts, c.Accounts[0])
			}
			if got := Process(c); got != v.Code {
				t.Fatalf("code %d want %d", got, v.Code)
			}
			if v.Code == 0 && !bytes.Equal(state, v.Expected) {
				t.Fatal("state mismatch against Python reference")
			}
		})
	}
}
