package sbftest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gosvm/internal/testvm"
)

type FixtureStepResult struct {
	CU        uint64          `json:"cu"`
	Error     json.RawMessage `json:"error"`
	Logs      []string        `json:"logs"`
	Committed bool            `json:"committed"`
	Sysvars   *testvm.Sysvars `json:"sysvars,omitempty"`
}
type FixtureCaseResult struct {
	Name  string              `json:"name"`
	Steps []FixtureStepResult `json:"steps"`
}

// RunGeneral runs an already-compiled ELF, independent of project generation.
// One process serves all cases. Reset/overrides isolate cases; steps share state.
func RunGeneral(ctx context.Context, elf, fixturePath, reportPath string, out io.Writer, opts FastOptions) error {
	started := time.Now()
	if e := os.MkdirAll(filepath.Dir(reportPath), 0755); e != nil {
		return e
	}
	suite, loadErr := LoadGeneral(fixturePath)
	inputs := []string{elf, fixturePath}
	for _, p := range suite.Programs {
		path := p.ELF
		if !filepath.IsAbs(path) {
			path = filepath.Join(filepath.Dir(fixturePath), path)
		}
		inputs = append(inputs, path)
	}
	if e := clearGeneralReport(reportPath, inputs...); e != nil {
		return e
	}
	if loadErr != nil {
		return loadErr
	}
	k, e := suite.validate()
	if e != nil {
		return e
	}
	re, e := regexp.Compile(opts.Pattern)
	if e != nil {
		return fmt.Errorf("SVM selection: %w", e)
	}
	var selected []FixtureCase
	traceSysvars := false
	for _, c := range suite.Cases {
		for _, step := range c.Steps {
			if len(step.Sysvars) > 0 {
				traceSysvars = true
			}
		}
		if re.MatchString(c.Name) {
			selected = append(selected, c)
		}
	}
	if len(selected) == 0 {
		return fmt.Errorf("SVM selection %q matched no fixtures", opts.Pattern)
	}
	image, e := os.ReadFile(elf)
	if e != nil {
		return e
	}
	if len(image) < 64 || !bytes.Equal(image[:4], []byte{127, 'E', 'L', 'F'}) || binary.LittleEndian.Uint32(image[48:52]) != 3 {
		return fmt.Errorf("fast SVM testing requires an SBF v3 main ELF")
	}
	programs := []any{map[string]any{"id": suite.ProgramID, "elf": base64.StdEncoding.EncodeToString(image)}}
	images := map[string]string{"program": fmt.Sprintf("%x", sha256.Sum256(image))}
	for _, program := range suite.Programs {
		path := program.ELF
		if !filepath.IsAbs(path) {
			path = filepath.Join(filepath.Dir(fixturePath), path)
		}
		data, e := os.ReadFile(path)
		if e != nil {
			return fmt.Errorf("%s: %w", program.Name, e)
		}
		hash := fmt.Sprintf("%x", sha256.Sum256(data))
		if !strings.EqualFold(hash, program.SHA256) {
			return fmt.Errorf("%s: ELF SHA-256 mismatch", program.Name)
		}
		images[program.Name] = hash
		programs = append(programs, map[string]any{"id": program.Address, "elf": base64.StdEncoding.EncodeToString(data)})
	}
	accounts := []any{}
	for _, a := range suite.Accounts {
		if a.Initial != nil {
			accounts = append(accounts, map[string]any{"pubkey": a.Address, "account": a.Initial.account()})
		}
	}
	features, e := testvm.Features()
	if e != nil {
		return e
	}
	config := map[string]any{"features": features, "accounts": accounts, "programs": programs, "payer": b58(k.addresses["payer"])}
	if len(suite.Sysvars) > 0 {
		config["sysvars"] = suite.Sysvars
	}
	if opts.Sysvars != "" {
		return fmt.Errorf("general fixtures declare sysvars in the fixture file; do not also use -svm-sysvars")
	}
	runner, e := testvm.RunnerPath(opts.Runner)
	if e != nil {
		return e
	}
	startup := time.Now()
	s, e := testvm.Start(ctx, runner)
	if e != nil {
		return e
	}
	defer s.Close()
	if e = s.Init(config); e != nil {
		return e
	}
	startupSeconds := time.Since(startup).Seconds()
	call := func(method string, params any, id int) testvm.Call {
		return testvm.Call{JSONRPC: "2.0", ID: id, Method: method, Params: params}
	}
	fmt.Fprintf(out, "Testing %d compiled SBF scenarios with LiteSVM...\n", len(selected))
	execution := time.Now()
	var results []FixtureCaseResult
	for _, c := range selected {
		overrides := []any{}
		for _, o := range c.Overrides {
			overrides = append(overrides, map[string]any{"pubkey": b58(k.addresses[o.Name]), "account": o.State.account()})
		}
		if e = s.ResetAccounts(overrides); e != nil {
			return fmt.Errorf("%s reset: %w", c.Name, e)
		}
		row := FixtureCaseResult{Name: c.Name}
		for stepIndex, step := range c.Steps {
			label := fmt.Sprintf("%s step %d", c.Name, stepIndex+1)
			if len(step.Sysvars) > 0 {
				controls, err := testvm.ParseSysvars(step.Sysvars)
				if err != nil {
					return fmt.Errorf("%s sysvars: %w", label, err)
				}
				if err = s.SetSysvars(controls); err != nil {
					return fmt.Errorf("%s sysvars: %w", label, err)
				}
			}
			var effectiveSysvars *testvm.Sysvars
			if traceSysvars {
				value, err := s.Sysvars()
				if err != nil {
					return fmt.Errorf("%s read sysvars: %w", label, err)
				}
				effectiveSysvars = &value
			}
			if step.FreshBlockhash {
				if e = s.ExpireBlockhash(); e != nil {
					return fmt.Errorf("%s fresh blockhash: %w", label, e)
				}
			}
			values, e := s.Batch([]testvm.Call{call("getLatestBlockhash", []any{}, 0)})
			if e != nil {
				return e
			}
			var latest struct{ Value struct{ Blockhash string } }
			if e = json.Unmarshal(values[0], &latest); e != nil {
				return e
			}
			hash, e := addressBytes(latest.Value.Blockhash)
			if e != nil {
				return fmt.Errorf("SVM returned invalid blockhash: %w", e)
			}
			tx, signature, e := fixtureTransaction(k, hash, step.Instructions)
			if e != nil {
				return fmt.Errorf("%s: %w", label, e)
			}
			addresses := []string{}
			for _, check := range step.Expect.Accounts {
				addresses = append(addresses, b58(k.addresses[check.Account]))
			}
			// Validate simulation before submission so a failing assertion does not
			// advance scenario state or obscure the offending transaction.
			values, e = s.Batch([]testvm.Call{call("simulateTransaction", []any{tx, map[string]any{"encoding": "base64", "sigVerify": true, "accounts": map[string]any{"addresses": addresses}}}, 0)})
			if e != nil {
				return e
			}
			var sim struct {
				Value struct {
					Err           json.RawMessage
					UnitsConsumed uint64
					Accounts      []*account
					Logs          []string
				}
			}
			if e = json.Unmarshal(values[0], &sim); e != nil {
				return e
			}
			if e = fixtureError(sim.Value.Err, step.Expect.Error); e != nil {
				return fmt.Errorf("%s simulation: %w\n%s", label, e, strings.Join(sim.Value.Logs, "\n"))
			}
			if step.Expect.CU != nil {
				if sim.Value.UnitsConsumed != *step.Expect.CU {
					return fmt.Errorf("%s: CU got %d, want %d", label, sim.Value.UnitsConsumed, *step.Expect.CU)
				}
			} else if sim.Value.UnitsConsumed == 0 {
				return fmt.Errorf("%s: no SBF compute recorded; set explicit cu: 0 for pre-execution errors", label)
			}
			for text, want := range step.Expect.LogCounts {
				got := 0
				for _, line := range sim.Value.Logs {
					if strings.Contains(line, text) {
						got++
					}
				}
				if got != want {
					return fmt.Errorf("%s: log %q count got %d, want %d\n%s", label, text, got, want, strings.Join(sim.Value.Logs, "\n"))
				}
			}
			if bytes.Equal(bytes.TrimSpace(step.Expect.Error), []byte("null")) {
				if len(sim.Value.Accounts) != len(step.Expect.Accounts) {
					return fmt.Errorf("%s: simulated account count mismatch", label)
				}
				for i, check := range step.Expect.Accounts {
					if e = fixtureAccount(label, "simulated", step.Expect.SimulationCheck(check), sim.Value.Accounts[i]); e != nil {
						return e
					}
				}
			}
			calls := []testvm.Call{call("sendTransaction", []any{tx, map[string]any{"encoding": "base64", "skipPreflight": true}}, 0), call("getSignatureStatuses", []any{[]string{signature}}, 1)}
			for i, address := range addresses {
				calls = append(calls, call("getAccountInfo", []any{address}, i+2))
			}
			values, e = s.Batch(calls)
			if e != nil {
				return e
			}
			var submitted string
			if e = json.Unmarshal(values[0], &submitted); e != nil || submitted != signature {
				return fmt.Errorf("%s: submitted signature mismatch", label)
			}
			var status struct {
				Value []*struct{ Err json.RawMessage }
			}
			if e = json.Unmarshal(values[1], &status); e != nil {
				return e
			}
			if len(status.Value) != 1 || status.Value[0] == nil {
				return fmt.Errorf("%s: missing submitted status", label)
			}
			if e = fixtureError(status.Value[0].Err, step.Expect.Error); e != nil {
				return fmt.Errorf("%s submission: %w", label, e)
			}
			for i, check := range step.Expect.Accounts {
				var persisted struct{ Value *account }
				if e = json.Unmarshal(values[i+2], &persisted); e != nil {
					return e
				}
				if e = fixtureAccount(label, "submitted/rollback", check, persisted.Value); e != nil {
					return e
				}
			}
			row.Steps = append(row.Steps, FixtureStepResult{CU: sim.Value.UnitsConsumed, Error: sim.Value.Err, Logs: sim.Value.Logs, Committed: true, Sysvars: effectiveSysvars})
			fmt.Fprintf(out, "PASS %-36s %d CU\n", label, sim.Value.UnitsConsumed)
		}
		results = append(results, row)
	}
	fixtureSeconds := time.Since(execution).Seconds()
	values, e := s.Batch([]testvm.Call{call("gosvmRuntimeInfo", []any{}, 0)})
	if e != nil {
		return e
	}
	fixtureBytes, e := os.ReadFile(fixturePath)
	if e != nil {
		return e
	}
	runnerBytes, e := os.ReadFile(runner)
	if e != nil {
		return e
	}
	report, e := json.MarshalIndent(map[string]any{"schema": 1, "fixture_format": FixtureFormat, "runtime_engine": "litesvm", "runtime_version": testvm.RunnerVersion,
		"isolation": "initial VM checkpoint plus declared overrides before each scenario; ordered steps share state", "selection": opts.Pattern, "cases": results,
		"elf_sha256": images["program"], "program_sha256": images, "fixtures_sha256": fmt.Sprintf("%x", sha256.Sum256(fixtureBytes)), "runner_sha256": fmt.Sprintf("%x", sha256.Sum256(runnerBytes)),
		"runner": values[0], "native_programs": suite.NativePrograms, "startup_seconds": startupSeconds, "fixture_seconds": fixtureSeconds, "total_seconds": time.Since(started).Seconds()}, "", "  ")
	if e != nil {
		return e
	}
	if e = s.Close(); e != nil {
		return fmt.Errorf("runner shutdown: %w", e)
	}
	return os.WriteFile(reportPath, append(report, '\n'), 0644)
}
func clearGeneralReport(path string, inputs ...string) error {
	absolute, e := filepath.Abs(path)
	if e != nil {
		return e
	}
	info, e := os.Stat(path)
	if e != nil && !os.IsNotExist(e) {
		return e
	}
	for _, input := range inputs {
		other, err := filepath.Abs(input)
		if err != nil {
			return err
		}
		inputInfo, _ := os.Stat(input)
		if absolute == other || (info != nil && inputInfo != nil && os.SameFile(info, inputInfo)) {
			return fmt.Errorf("report must not overwrite an input file")
		}
	}
	if info != nil {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var report struct {
			Schema        int
			RuntimeEngine string `json:"runtime_engine"`
		}
		if json.Unmarshal(data, &report) != nil || report.Schema != 1 || report.RuntimeEngine != "litesvm" {
			return fmt.Errorf("refusing to overwrite a file that is not an SVM results report: %s", path)
		}
	}
	if e = os.Remove(path); e != nil && !os.IsNotExist(e) {
		return e
	}
	return nil
}
func fixtureError(got, want json.RawMessage) error {
	g, e := canonicalJSON(got)
	if e != nil {
		return e
	}
	w, e := canonicalJSON(want)
	if e != nil {
		return e
	}
	if !bytes.Equal(g, w) {
		return fmt.Errorf("transaction error %s; expected %s", got, want)
	}
	return nil
}
func fixtureAccount(label, phase string, check FixtureCheck, got *account) error {
	name := label + " account " + check.Account
	if check.Absent {
		if got != nil {
			return fmt.Errorf("%s: %s expected absent account", name, phase)
		}
		return nil
	}
	if got == nil {
		return fmt.Errorf("%s: %s missing account", name, phase)
	}
	want := check.State
	data, e := stateBytes(got)
	if e != nil {
		return fmt.Errorf("%s: %w", name, e)
	}
	expected, _ := hex.DecodeString(want.Data)
	if !bytes.Equal(data, expected) {
		return stateMismatch(name, phase, data, expected)
	}
	if got.Owner != want.Owner || got.Lamports != want.Lamports || got.Executable != want.Executable || got.RentEpoch != want.RentEpoch {
		return fmt.Errorf("%s: %s metadata got owner=%s lamports=%d executable=%v rent_epoch=%d; want owner=%s lamports=%d executable=%v rent_epoch=%d", name, phase, got.Owner, got.Lamports, got.Executable, got.RentEpoch, want.Owner, want.Lamports, want.Executable, want.RentEpoch)
	}
	return nil
}
