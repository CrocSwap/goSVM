// Command fixtures writes testdata/svm.json, the local SVM scenarios for
// gosvm test --svm, using the generated client's encodings.
//
//	go run ./cmd/fixtures > testdata/svm.json
package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"os"

	"example.com/counter/client"
	"example.com/counter/model"
	"example.com/counter/solrpc"
)

// Local test identities only: derived from fixed strings, never funded anywhere.
func seed(name string) []byte {
	s := sha256.Sum256([]byte("counter tutorial " + name + "; local tests only"))
	return s[:]
}

func keyFor(name string) solrpc.PublicKey {
	return solrpc.KeyOf(ed25519.NewKeyFromSeed(seed(name)))
}

const system = "11111111111111111111111111111111"
const start = 1_000_000_000 // each signer's initial lamports
const rent = 1_287_600      // (128 + 57 bytes) * 3480 lamports * 2 years

type obj = map[string]any

func wallet(lamports uint64) obj {
	return obj{"data": "", "owner": system, "lamports": lamports, "executable": false, "rent_epoch": 0}
}

func main() {
	b, err := suiteJSON()
	if err != nil {
		panic(err)
	}
	os.Stdout.Write(b)
}

// suiteJSON returns the fixture file's contents.
func suiteJSON() ([]byte, error) {
	program := keyFor("program")
	owner, intruder := keyFor("owner"), keyFor("intruder")
	counter, bump, err := solrpc.FindProgramAddress(program, []byte("counter"), owner[:])
	if err != nil {
		return nil, err
	}
	state := func(count uint64) obj {
		data := client.EncodeAccountCounter(model.Counter{Owner: model.PublicKey(owner), Count: count, Limit: 10, Bump: bump})
		return obj{"data": hex.EncodeToString(data), "owner": program.String(), "lamports": rent, "executable": false, "rent_epoch": 0}
	}
	meta := func(name string, signer, writable bool) obj {
		return obj{"account": name, "signer": signer, "writable": writable}
	}
	create := func(limit uint64) obj {
		return obj{"program": "program", "data": hex.EncodeToString(client.EncodeInstructionCreate(model.CreateArgs{Limit: limit, Bump: bump})),
			"accounts": []obj{meta("counter", false, true), meta("owner", true, true), meta("system", false, false)}}
	}
	increment := func(signer string, amount uint64) obj {
		return obj{"program": "program", "data": hex.EncodeToString(client.EncodeInstructionIncrement(model.IncrementArgs{Amount: amount})),
			"accounts": []obj{meta("counter", false, true), meta(signer, true, false)}}
	}
	// expect lists the error (nil for success) and every watched account's state.
	expect := func(e any, counterState obj, logs obj) obj {
		accounts := []obj{
			{"account": "owner", "state": wallet(start - rent)},
			{"account": "intruder", "state": wallet(start)},
			{"account": "counter", "state": counterState},
		}
		if counterState == nil {
			accounts[0]["state"] = wallet(start)
			accounts[2] = obj{"account": "counter", "absent": true}
		}
		out := obj{"error": e, "accounts": accounts}
		if logs != nil {
			out["log_counts"] = logs
		}
		return out
	}
	custom := func(code uint64) obj { return obj{"InstructionError": []any{0, obj{"Custom": code}}} }
	step := func(ix obj, want obj) obj { return obj{"instructions": []obj{ix}, "expect": want} }
	created := step(create(10), expect(nil, state(0), obj{system + " invoke": 1}))

	var returned [8]byte
	binary.LittleEndian.PutUint64(returned[:], 8)
	preview := obj{"program": "program", "accounts": []obj{meta("owner", true, false)},
		"data": hex.EncodeToString(client.EncodeInstructionPreview(model.PreviewArgs{Count: 5, Limit: 10, Amount: 3}))}

	suite := struct {
		Format         string `json:"format"`
		ProgramID      string `json:"program_id"`
		PayerSeed      string `json:"payer_seed"`
		Sysvars        obj    `json:"sysvars"`
		Signers        []obj  `json:"signers"`
		NativePrograms []obj  `json:"native_programs"`
		Accounts       []obj  `json:"accounts"`
		Cases          []obj  `json:"cases"`
	}{
		"gosvm-svm-fixtures-v1",
		program.String(),
		hex.EncodeToString(seed("fee payer")),
		obj{"rent": obj{"lamports_per_byte_year": 3480, "exemption_threshold": 2.0, "burn_percent": 50}},
		[]obj{{"name": "owner", "seed": hex.EncodeToString(seed("owner"))}, {"name": "intruder", "seed": hex.EncodeToString(seed("intruder"))}},
		[]obj{{"name": "system", "address": system}},
		[]obj{
			{"name": "owner", "address": owner.String(), "initial": wallet(start)},
			{"name": "intruder", "address": intruder.String(), "initial": wallet(start)},
			{"name": "counter", "address": counter.String()},
		},
		[]obj{
			{"name": "create-and-increment", "steps": []obj{
				created,
				step(increment("owner", 3), expect(nil, state(3), nil)),
				step(increment("owner", 7), expect(nil, state(10), nil)),
			}},
			{"name": "increment-past-limit", "steps": []obj{
				created,
				step(increment("owner", 11), expect(custom(model.ErrLimit), state(0), nil)),
			}},
			{"name": "only-the-owner-increments", "steps": []obj{
				created,
				step(increment("intruder", 1), expect(custom(6007), state(0), nil)),
			}},
			{"name": "failed-create-rolls-back", "steps": []obj{
				step(create(0), expect(custom(model.ErrLimit), nil, nil)),
			}},
			{"name": "preview", "steps": []obj{
				step(preview, expect(nil, nil, obj{"Program return: " + program.String() + " " + base64.StdEncoding.EncodeToString(returned[:]): 1})),
			}},
		},
	}
	b, err := json.MarshalIndent(suite, "", "  ")
	return append(b, '\n'), err
}
