package sbftest

import (
	"bytes"
	"context"
	"crypto/ed25519"
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

type FastOptions struct{ Runner, Pattern, Sysvars, Fixtures string }

// RunFast executes schema-1 fixtures through one versioned stdio session. Cases
// have independent seeded state; instructions within each transaction stay ordered.
func RunFast(ctx context.Context, dir, elf string, out io.Writer, opts FastOptions) error {
	if opts.Fixtures != "" {
		return RunGeneral(ctx, elf, opts.Fixtures, filepath.Join(dir, "build/svm-results.json"), out, opts)
	}
	started := time.Now()
	build := filepath.Join(dir, "build")
	if e := os.MkdirAll(build, 0755); e != nil {
		return e
	}
	reportPath := filepath.Join(build, "svm-results.json")
	if e := os.Remove(reportPath); e != nil && !os.IsNotExist(e) {
		return e
	}
	suite, e := Load(filepath.Join(dir, "testdata/sbf.json"))
	if e != nil {
		return e
	}
	selected, e := selectCases(suite, opts.Pattern)
	if e != nil {
		return e
	}
	elfBytes, e := os.ReadFile(elf)
	if e != nil {
		return e
	}
	if len(elfBytes) < 64 || !bytes.Equal(elfBytes[:4], []byte{127, 'E', 'L', 'F'}) || binary.LittleEndian.Uint32(elfBytes[48:52]) != 3 {
		return fmt.Errorf("fast SVM testing requires an SBF v3 ELF")
	}
	path, e := testvm.RunnerPath(opts.Runner)
	if e != nil {
		return e
	}
	features, e := testvm.Features()
	if e != nil {
		return e
	}
	var controls json.RawMessage
	if opts.Sysvars != "" {
		controls, e = os.ReadFile(opts.Sysvars)
		if e != nil {
			return e
		}
		if !json.Valid(controls) {
			return fmt.Errorf("SVM sysvars file must contain one JSON object")
		}
	}
	program := key("gosvm starter program; local tests only")
	payer := ed25519.NewKeyFromSeed(key("gosvm starter fee payer; never fund"))
	accounts := []any{}
	for i, c := range selected {
		data, _ := hex.DecodeString(c.Initial)
		owner := b58(program)
		if c.WrongOwner {
			owner = b58(make([]byte, 32))
		}
		a := account{Data: []string{base64.StdEncoding.EncodeToString(data), "base64"}, Owner: owner, Lamports: 10000000}
		accounts = append(accounts, map[string]any{"pubkey": b58(key(fmt.Sprintf("gosvm state %d", i))), "account": a})
	}
	startup := time.Now()
	fmt.Fprintf(out, "Testing %d compiled SBF fixtures with LiteSVM...\n", len(selected))
	s, e := testvm.Start(ctx, path)
	if e != nil {
		return e
	}
	defer s.Close()
	config := map[string]any{"features": features, "accounts": accounts, "payer": b58(payer.Public().(ed25519.PublicKey)),
		"programs": []any{map[string]any{"id": b58(program), "elf": base64.StdEncoding.EncodeToString(elfBytes)}}}
	if controls != nil {
		config["sysvars"] = controls
	}
	if e = s.Init(config); e != nil {
		return e
	}
	call := func(method string, params any, id int) testvm.Call {
		return testvm.Call{JSONRPC: "2.0", ID: id, Method: method, Params: params}
	}
	values, e := s.Batch([]testvm.Call{call("getLatestBlockhash", []any{}, 0)})
	if e != nil {
		return e
	}
	var latest struct{ Value struct{ Blockhash string } }
	if e = json.Unmarshal(values[0], &latest); e != nil {
		return e
	}
	if len(un58(latest.Value.Blockhash)) != 32 {
		return fmt.Errorf("SVM returned invalid blockhash")
	}
	startupSeconds := time.Since(startup).Seconds()
	execution := time.Now()
	calls := []testvm.Call{}
	for i, c := range selected {
		pool := key(fmt.Sprintf("gosvm state %d", i))
		tx := transaction(payer, pool, program, un58(latest.Value.Blockhash), c)
		wire, _ := base64.StdEncoding.DecodeString(tx)
		signature := b58(wire[1:65])
		id := len(calls)
		calls = append(calls,
			call("simulateTransaction", []any{tx, map[string]any{"encoding": "base64", "sigVerify": true, "accounts": map[string]any{"addresses": []string{b58(pool)}}}}, id),
			call("sendTransaction", []any{tx, map[string]any{"encoding": "base64", "skipPreflight": true}}, id+1),
			call("getSignatureStatuses", []any{[]string{signature}}, id+2),
			call("getAccountInfo", []any{b58(pool)}, id+3))
	}
	// Restore the initial VM before each case, including fees/history/sysvars.
	// A case's instructions remain ordered within its transaction.
	var responses []json.RawMessage
	for offset := 0; offset < len(calls); offset += 4 {
		if e := s.Reset(); e != nil {
			return e
		}
		end := offset + 4
		batch, e := s.Batch(calls[offset:end])
		if e != nil {
			return e
		}
		responses = append(responses, batch...)
	}
	results := []Result{}
	for i, c := range selected {
		var sim struct {
			Value struct {
				Err           json.RawMessage
				UnitsConsumed uint64
				Accounts      []*account
				Logs          []string
			}
		}
		if e = json.Unmarshal(responses[i*4], &sim); e != nil {
			return e
		}
		if e = checkError(sim.Value.Err, c); e != nil {
			return fmt.Errorf("%s simulation: %w\n%s", c.Name, e, strings.Join(sim.Value.Logs, "\n"))
		}
		if sim.Value.UnitsConsumed == 0 {
			return fmt.Errorf("%s: no SBF compute recorded", c.Name)
		}
		expected, _ := hex.DecodeString(c.Expected)
		if c.Error == 0 {
			if len(sim.Value.Accounts) != 1 {
				return fmt.Errorf("%s: missing simulated state", c.Name)
			}
			data, e := stateBytes(sim.Value.Accounts[0])
			if e != nil {
				return fmt.Errorf("%s: simulated state: %w", c.Name, e)
			}
			if !bytes.Equal(data, expected) {
				return stateMismatch(c.Name, "simulated", data, expected)
			}
		}
		var status struct {
			Value []struct{ Err json.RawMessage }
		}
		if e = json.Unmarshal(responses[i*4+2], &status); e != nil {
			return e
		}
		if len(status.Value) != 1 {
			return fmt.Errorf("%s: missing submitted status", c.Name)
		}
		if e = checkError(status.Value[0].Err, c); e != nil {
			return fmt.Errorf("%s commit: %w", c.Name, e)
		}
		var persisted struct{ Value *account }
		if e = json.Unmarshal(responses[i*4+3], &persisted); e != nil {
			return e
		}
		data, e := stateBytes(persisted.Value)
		if e != nil {
			return fmt.Errorf("%s: submitted state: %w", c.Name, e)
		}
		if !bytes.Equal(data, expected) {
			return stateMismatch(c.Name, "submitted/rollback", data, expected)
		}
		results = append(results, Result{c.Name, sim.Value.UnitsConsumed, c.Error, true})
		fmt.Fprintf(out, "PASS %-28s %d CU\n", c.Name, sim.Value.UnitsConsumed)
	}
	fixtureSeconds := time.Since(execution).Seconds()
	values, e = s.Batch([]testvm.Call{call("gosvmRuntimeInfo", []any{}, 0)})
	if e != nil {
		return e
	}
	// Keep uint64 sysvar fields exact; decoding into any would round values
	// above 2^53 through float64 before writing the report.
	info := values[0]
	fixtures, e := os.ReadFile(filepath.Join(dir, "testdata/sbf.json"))
	if e != nil {
		return e
	}
	runnerBytes, e := os.ReadFile(path)
	if e != nil {
		return e
	}
	report, e := json.MarshalIndent(map[string]any{"schema": 1, "runtime_engine": "litesvm", "runtime_version": testvm.RunnerVersion, "transport": "stdio schema 1; ordered batches of 4 calls; reset between fixtures", "isolation": "initial VM checkpoint restored before every fixture",
		"elf_sha256": fmt.Sprintf("%x", sha256.Sum256(elfBytes)), "fixtures_sha256": fmt.Sprintf("%x", sha256.Sum256(fixtures)), "runner_sha256": fmt.Sprintf("%x", sha256.Sum256(runnerBytes)),
		"selection": opts.Pattern, "cases": results, "runner": info, "startup_seconds": startupSeconds, "fixture_seconds": fixtureSeconds, "total_seconds": time.Since(started).Seconds()}, "", "  ")
	if e != nil {
		return e
	}
	if e = s.Close(); e != nil {
		return fmt.Errorf("runner shutdown: %w", e)
	}
	return os.WriteFile(reportPath, append(report, '\n'), 0644)
}

func stateMismatch(name, phase string, got, want []byte) error {
	if len(got) != len(want) {
		return fmt.Errorf("%s: %s state length got %d, want %d", name, phase, len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			return fmt.Errorf("%s: %s state mismatch at byte %d: got 0x%02x, want 0x%02x", name, phase, i, got[i], want[i])
		}
	}
	return fmt.Errorf("%s: %s state mismatch", name, phase)
}

func selectCases(s Suite, pattern string) ([]Case, error) {
	re, e := regexp.Compile(pattern)
	if e != nil {
		return nil, fmt.Errorf("SVM selection: %w", e)
	}
	var selected []Case
	for _, c := range s.Cases {
		if re.MatchString(c.Name) {
			selected = append(selected, c)
		}
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("SVM selection %q matched no fixtures", pattern)
	}
	return selected, nil
}
