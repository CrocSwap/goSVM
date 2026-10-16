package testvm

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"gosvm/internal/runner"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

// Profile is the observed local validator 3.0.15 feature set, not mainnet.
// Activation slots are normalized by the experimental runner.
//
//go:embed profiles/validator-3.0.15.json
var profile []byte

func Features() (json.RawMessage, error) {
	if path := os.Getenv("GOSVM_TEST_FEATURES"); path != "" {
		return os.ReadFile(path)
	}
	return json.RawMessage(profile), nil
}

// RunnerPath resolves a runner for historical (target-less) tests.
func RunnerPath(explicit string) (string, error) { return RunnerFor(explicit, "") }

// ManagedRunnerVersions lists, in preference order, the managed runner packages
// that may be selected automatically for a profile. fogo-v0 and legacy are
// qualified on the LiteSVM 0.8.2 engine: 0.5.1, or an existing 0.5.0
// installation (same engine, without returnData in simulation results). The
// modern engine is eligible for fogo-v0 only with an explicit
// GOSVM_TEST_FEATURES file, because the default v0 feature list targets 0.8.2.
func ManagedRunnerVersions(profileName string) []string {
	switch profileName {
	case "solana-v3":
		return []string{runner.ModernVersion}
	case "fogo-v0":
		if os.Getenv("GOSVM_TEST_FEATURES") != "" {
			return []string{runner.FogoVersion, runner.Version, runner.ModernVersion}
		}
		return []string{runner.FogoVersion, runner.Version}
	default:
		return []string{runner.FogoVersion, runner.Version}
	}
}

// RunnerInstallHint is the exact command installing the profile's preferred runner.
func RunnerInstallHint(profileName string) string {
	return runner.InstallHint(ManagedRunnerVersions(profileName)[0])
}

// RunnerFor selects the runner for a profile: explicit path, GOSVM_TEST_RUNNER,
// a verified compatible managed package, then gosvm-svm-runner on PATH. A damaged
// managed install fails closed. Identity is rechecked when the session starts.
func RunnerFor(explicit, profileName string) (string, error) {
	if explicit == "" {
		explicit = os.Getenv("GOSVM_TEST_RUNNER")
	}
	if explicit == "" {
		for _, version := range ManagedRunnerVersions(profileName) {
			managed, exists, err := runner.ManagedPathFor(version)
			if err != nil {
				return "", err
			}
			if exists {
				return managed, nil
			}
		}
		explicit = "gosvm-svm-runner"
	}
	path, e := exec.LookPath(explicit)
	if e != nil {
		return "", fmt.Errorf("SVM runner %s unavailable%s; install it with: %s (or supply -svm-runner/GOSVM_TEST_RUNNER): %w", requiredRunner(profileName), forProfile(profileName), RunnerInstallHint(profileName), e)
	}
	return path, nil
}

func forProfile(name string) string {
	if name == "" {
		return ""
	}
	return " for " + name
}

func requiredRunner(profileName string) string {
	if profileName == "solana-v3" {
		return ModernRunnerVersion
	}
	return RunnerVersion
}

const ModernRunnerVersion = runner.ModernIdentity

func FeaturesFor(profileName string) (json.RawMessage, error) {
	if profileName == "solana-v3" && os.Getenv("GOSVM_TEST_FEATURES") == "" {
		return json.RawMessage("null"), nil
	}
	return Features()
}
func RunnerCompatible(version, profileName string) bool {
	switch profileName {
	case "solana-v3":
		return version == ModernRunnerVersion
	case "fogo-v0":
		return version == RunnerVersion || version == HistoricalRunnerVersion || version == ModernRunnerVersion
	default:
		return version == RunnerVersion || version == HistoricalRunnerVersion
	}
}

const maxFrame = 16 * 1024 * 1024

type Call struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params"`
}
type Session struct {
	Identity  string
	cmd       *exec.Cmd
	input     io.WriteCloser
	output    *bufio.Reader
	cancel    context.CancelFunc
	done      chan struct{}
	waitError error
	id        uint64
	mu        sync.Mutex
	closing   sync.Once
}

func Start(ctx context.Context, path string) (*Session, error) { return StartTarget(ctx, path, "") }

func StartTarget(ctx context.Context, path, profileName string) (*Session, error) {
	versionCtx, stop := context.WithTimeout(ctx, 3*time.Second)
	version, e := exec.CommandContext(versionCtx, path, "--version").CombinedOutput()
	stop()
	if e != nil {
		return nil, fmt.Errorf("runner version: %w: %s", e, version)
	}
	identity := string(bytes.TrimSpace(version))
	if !RunnerCompatible(identity, profileName) {
		return nil, fmt.Errorf("require %s%s; found %s at %s; install it with: %s", requiredRunner(profileName), forProfile(profileName), identity, path, RunnerInstallHint(profileName))
	}
	childCtx, cancel := context.WithCancel(ctx)
	s, e := startSession(exec.CommandContext(childCtx, path, "--stdio"), cancel)
	if s != nil {
		s.Identity = identity
	}
	if e != nil {
		cancel()
	}
	return s, e
}
func startSession(cmd *exec.Cmd, cancel context.CancelFunc) (*Session, error) {
	s := &Session{cmd: cmd, cancel: cancel, done: make(chan struct{})}
	var e error
	s.input, e = cmd.StdinPipe()
	if e != nil {
		return nil, e
	}
	output, e := cmd.StdoutPipe()
	if e != nil {
		s.input.Close()
		return nil, e
	}
	s.output = bufio.NewReader(output)
	cmd.Stderr = os.Stderr
	if e = cmd.Start(); e != nil {
		s.input.Close()
		output.Close()
		return nil, e
	}
	go func() { s.waitError = cmd.Wait(); close(s.done) }()
	return s, nil
}
func (s *Session) Close() error {
	s.closing.Do(func() {
		s.input.Close()
		select {
		case <-s.done:
		case <-time.After(500 * time.Millisecond):
			s.cancel()
			<-s.done
		}
		s.cancel()
	})
	return s.waitError
}
func (s *Session) request(op string, body any, out any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.id
	s.id++
	message := map[string]any{"schema": 1, "id": id, "op": op}
	if op == "init" {
		message["config"] = body
	} else if op == "batch" {
		message["calls"] = body
	} else if op == "set_sysvars" {
		message["sysvars"] = body
	} else if op == "reset" && body != nil {
		message["accounts"] = body
	}
	data, e := json.Marshal(message)
	if e != nil {
		return e
	}
	if len(data)+1 > maxFrame {
		return fmt.Errorf("SVM request exceeds 16 MiB")
	}
	if _, e = s.input.Write(append(data, '\n')); e != nil {
		return fmt.Errorf("SVM request: %w", e)
	}
	var line []byte
	for {
		part, err := s.output.ReadSlice('\n')
		line = append(line, part...)
		if len(line) > maxFrame {
			return fmt.Errorf("SVM response exceeds 16 MiB")
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		if err != nil {
			return fmt.Errorf("SVM response: %w", err)
		}
		break
	}
	var reply struct {
		Schema int
		ID     *uint64
		Result json.RawMessage
		Error  json.RawMessage
	}
	if e = json.Unmarshal(line, &reply); e != nil {
		return fmt.Errorf("SVM protocol JSON: %w", e)
	}
	if reply.Schema != 1 || reply.ID == nil || *reply.ID != id {
		return fmt.Errorf("SVM protocol identity mismatch")
	}
	if len(reply.Error) > 0 && string(reply.Error) != "null" {
		return fmt.Errorf("SVM protocol: %s", reply.Error)
	}
	return json.Unmarshal(reply.Result, out)
}

// Clock fields are explicit: setting a slot does not advance timestamps or epochs.
type Clock struct {
	Slot                uint64 `json:"slot"`
	EpochStartTimestamp int64  `json:"epoch_start_timestamp"`
	Epoch               uint64 `json:"epoch"`
	LeaderScheduleEpoch uint64 `json:"leader_schedule_epoch"`
	UnixTimestamp       int64  `json:"unix_timestamp"`
}
type Rent struct {
	LamportsPerByteYear uint64  `json:"lamports_per_byte_year"`
	ExemptionThreshold  float64 `json:"exemption_threshold"`
	BurnPercent         uint8   `json:"burn_percent"`
}
type Sysvars struct {
	Clock *Clock `json:"clock,omitempty"`
	Rent  *Rent  `json:"rent,omitempty"`
}

func (s *Session) Sysvars() (Sysvars, error) {
	var value Sysvars
	err := s.request("get_sysvars", nil, &value)
	return value, err
}
func (s *Session) control(op string, body any) error {
	var ack struct{ Done bool }
	if err := s.request(op, body, &ack); err != nil {
		return err
	}
	if !ack.Done {
		return fmt.Errorf("SVM %s did not complete", op)
	}
	return nil
}
func (s *Session) SetSysvars(value Sysvars) error { return s.control("set_sysvars", value) }

// Snapshot replaces the single checkpoint. Init creates the initial checkpoint.
func (s *Session) Snapshot() error { return s.control("snapshot", nil) }

// Reset restores VM state and statuses, but preserves cumulative diagnostics.
func (s *Session) Reset() error { return s.control("reset", nil) }

// ResetAccounts atomically restores the checkpoint and applies ordinary account
// overrides. Programs and sysvars must use their dedicated initialization/control paths.
func (s *Session) ResetAccounts(accounts any) error { return s.control("reset", accounts) }
func (s *Session) ExpireBlockhash() error           { return s.control("expire_blockhash", nil) }
func (s *Session) Init(config any) error {
	var ready struct{ Ready bool }
	if e := s.request("init", config, &ready); e != nil {
		return e
	}
	if !ready.Ready {
		return fmt.Errorf("SVM session did not become ready")
	}
	return nil
}
func (s *Session) Batch(calls []Call) ([]json.RawMessage, error) {
	if len(calls) > 4096 {
		return nil, fmt.Errorf("SVM batch exceeds 4096 calls")
	}
	var results []struct {
		Result  json.RawMessage
		Error   json.RawMessage
		ID      *int
		JSONRPC string
	}
	if e := s.request("batch", calls, &results); e != nil {
		return nil, e
	}
	if len(results) != len(calls) {
		return nil, fmt.Errorf("SVM batch response count mismatch")
	}
	values := make([]json.RawMessage, len(results))
	for i, r := range results {
		if r.JSONRPC != "2.0" || r.ID == nil || *r.ID != calls[i].ID {
			return nil, fmt.Errorf("SVM call %d response identity mismatch", i)
		}
		if len(r.Error) > 0 && string(r.Error) != "null" {
			return nil, fmt.Errorf("SVM call %d: %s", i, r.Error)
		}
		values[i] = r.Result
	}
	return values, nil
}
