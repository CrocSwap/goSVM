package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"gosvm/internal/sbftest"
)

// Export only after the independent benchmark assertions pass. Expected account
// bytes come from seed files plus wideReference math, never VM output snapshots.
func exportTokenFixtures(path, seedDir string, anchor bool, program []byte, fixtures []tokenFixture, vectors []tokenVector, rows []result, errors map[string]json.RawMessage, tokenImage []byte) error {
	if e := os.MkdirAll(filepath.Dir(path), 0755); e != nil {
		return e
	}
	tokenPath := filepath.Join(filepath.Dir(path), "spl-token.so")
	if e := exclusiveFixtureFile(tokenPath, tokenImage); e != nil {
		return e
	}
	suite := sbftest.FixtureSuite{Format: sbftest.FixtureFormat, ProgramID: b58(program), PayerSeed: hex.EncodeToString(key("token test fee payer - never fund")),
		Signers:  []sbftest.FixtureSigner{{Name: "authority", Seed: hex.EncodeToString(key("token test owner - never fund"))}},
		Programs: []sbftest.FixtureProgram{{Name: "token", Address: tokenAddress, ELF: "spl-token.so", SHA256: fmt.Sprintf("%x", sha256.Sum256(tokenImage))}}}
	names := map[string]string{tokenAddress: "token"}
	initial := map[string]sbftest.FixtureState{}
	cu := map[string]uint64{}
	for _, r := range rows {
		if r.Backend == "go" {
			cu[r.Name] = r.CU
		}
	}
	for i, f := range fixtures {
		for j, k := range f.keys {
			address := b58(k)
			if _, ok := names[address]; ok {
				continue
			}
			name := fmt.Sprintf("v%d_a%d", i, j)
			if j == 1 {
				name = "authority"
			}
			names[address] = name
			var seed struct{ Account account }
			data, e := os.ReadFile(filepath.Join(seedDir, address+".json"))
			if e != nil {
				return e
			}
			if e = json.Unmarshal(data, &seed); e != nil {
				return e
			}
			decoded, e := base64.StdEncoding.DecodeString(seed.Account.Data[0])
			if e != nil {
				return e
			}
			state := sbftest.FixtureState{Data: hex.EncodeToString(decoded), Owner: seed.Account.Owner, Lamports: seed.Account.Lamports, Executable: seed.Account.Executable, RentEpoch: seed.Account.RentEpoch}
			initial[name] = state
			suite.Accounts = append(suite.Accounts, sbftest.FixtureAccount{Name: name, Address: address, Initial: &state})
		}
	}
	instruction := func(f tokenFixture, data []byte) sbftest.FixtureInstruction {
		ix := sbftest.FixtureInstruction{Program: "program", Data: hex.EncodeToString(data), Accounts: []sbftest.FixtureMeta{}}
		for j := 0; j < f.count; j++ {
			index := j % len(f.keys)
			ix.Accounts = append(ix.Accounts, sbftest.FixtureMeta{Account: names[b58(f.keys[index])], Signer: index == 1 && f.userSigner, Writable: f.writable[index]})
		}
		return ix
	}
	checks := func(f tokenFixture, amounts []uint64) []sbftest.FixtureCheck {
		seen := map[string]bool{}
		var checks []sbftest.FixtureCheck
		for j, index := range []int{0, 2, 3, 4, 5} {
			name := names[b58(f.keys[index])]
			if seen[name] {
				continue
			}
			seen[name] = true
			state := initial[name]
			if amounts != nil {
				data, _ := hex.DecodeString(state.Data)
				offset := 64
				if index == 0 {
					offset = 129
					if anchor {
						offset += 8
					}
				}
				binary.LittleEndian.PutUint64(data[offset:], amounts[j])
				state.Data = hex.EncodeToString(data)
			}
			checks = append(checks, sbftest.FixtureCheck{Account: name, State: &state})
		}
		return checks
	}
	logCounts := func(calls, success int) map[string]int {
		return map[string]int{"Program " + tokenAddress + " invoke [2]": calls, "Program " + tokenAddress + " success": success}
	}
	for i, v := range vectors {
		var amounts []uint64
		calls, success := 0, 0
		if v.code == 0 {
			out := wideReference(v.x, v.y, v.amount)
			amounts = []uint64{v.counter + 1, v.userX - v.amount, v.x + v.amount, v.y - out, v.userY + out}
			calls, success = 2, 2
		} else if v.mutation == "input-frozen" {
			calls = 1
		} else if v.mutation == "output-frozen" {
			calls, success = 2, 1
		}
		compute := cu[v.name]
		suite.Cases = append(suite.Cases, sbftest.FixtureCase{Name: v.name, Steps: []sbftest.FixtureStep{{Instructions: []sbftest.FixtureInstruction{instruction(fixtures[i], fixtures[i].ix)}, Expect: sbftest.FixtureExpectation{Error: errors["go/"+v.name], CU: &compute, Accounts: checks(fixtures[i], amounts), LogCounts: logCounts(calls, success)}}}})
	}
	// Three transactions share a pool: two successful swaps, then a transaction
	// whose first swap succeeds and second instruction fails. All changes roll back.
	f := fixtures[0]
	base := vectors[0]
	amounts := []uint64{base.counter, base.userX, base.x, base.y, base.userY}
	scenario := sbftest.FixtureCase{Name: "stateful-swaps-and-atomic-rollback"}
	for step := 0; step < 3; step++ {
		data := words(base.amount+uint64(step), 0)
		if anchor {
			data = append(anchorTag("global:swap"), data...)
		}
		instructions := []sbftest.FixtureInstruction{instruction(f, data)}
		want := json.RawMessage("null")
		if step == 2 {
			bad := words(0, 0)
			if anchor {
				bad = append(anchorTag("global:swap"), bad...)
			}
			instructions = append(instructions, instruction(f, bad))
			want = json.RawMessage(`{"InstructionError":[1,{"Custom":112}]}`)
		} else {
			amount := base.amount + uint64(step)
			output := wideReference(amounts[2], amounts[3], amount)
			amounts[0]++
			amounts[1] -= amount
			amounts[2] += amount
			amounts[3] -= output
			amounts[4] += output
		}
		scenario.Steps = append(scenario.Steps, sbftest.FixtureStep{Instructions: instructions, Expect: sbftest.FixtureExpectation{Error: want, Accounts: checks(f, amounts), LogCounts: logCounts(2, 2)}})
	}
	suite.Cases = append(suite.Cases, scenario)
	// Override the shared pool's owner, then prove the following case sees genesis.
	badChecks := checks(f, nil)
	foreign := b58(key("foreign program"))
	state := *badChecks[0].State
	state.Owner = foreign
	badChecks[0].State = &state
	suite.Cases = append(suite.Cases, sbftest.FixtureCase{Name: "override-foreign-owner", Overrides: []sbftest.FixtureOverride{{Name: badChecks[0].Account, State: state}}, Steps: []sbftest.FixtureStep{{Instructions: []sbftest.FixtureInstruction{instruction(f, f.ix)}, Expect: sbftest.FixtureExpectation{Error: errors["go/pool-owner"], Accounts: badChecks, LogCounts: logCounts(0, 0)}}}})
	replay := suite.Cases[0]
	replay.Name = "after-override-isolated"
	suite.Cases = append(suite.Cases, replay)
	data, e := json.MarshalIndent(suite, "", "  ")
	if e != nil {
		return e
	}
	if e = exclusiveFixtureFile(path, append(data, '\n')); e != nil {
		return e
	}
	// Validate the actual exported file with the public fixture loader.
	if _, e = sbftest.LoadGeneral(path); e != nil {
		return e
	}
	fmt.Printf("Exported %d Go token scenarios to %s\n", len(suite.Cases), path)
	return nil
}
func exclusiveFixtureFile(path string, data []byte) error {
	f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if e != nil {
		return e
	}
	_, e = f.Write(bytes.Clone(data))
	closeErr := f.Close()
	if e != nil {
		return e
	}
	return closeErr
}
