package testvm

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestSessionHelperProcess(t *testing.T) {
	mode := os.Getenv("GOSVM_SESSION_HELPER")
	if mode == "" {
		return
	}
	if mode == "blocked" {
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var msg struct {
			ID    uint64
			Op    string
			Calls []Call
		}
		json.Unmarshal(scanner.Bytes(), &msg)
		if mode == "malformed" {
			fmt.Println("not JSON")
			continue
		}
		if mode == "wrong-id" {
			msg.ID++
		}
		var result any = map[string]any{"ready": true}
		if msg.Op == "batch" {
			results := []any{}
			for _, call := range msg.Calls {
				id := call.ID
				if mode == "wrong-call-id" {
					id++
				}
				r := map[string]any{"jsonrpc": "2.0", "id": id, "result": true}
				if mode == "missing-call-id" {
					delete(r, "id")
				}
				if mode == "call-error" {
					delete(r, "result")
					r["error"] = map[string]any{"message": "deliberate rejection"}
				}
				results = append(results, r)
			}
			result = results
		}
		data, _ := json.Marshal(map[string]any{"schema": 1, "id": msg.ID, "result": result})
		fmt.Println(string(data))
	}
	os.Exit(0)
}
func helperSession(t *testing.T, mode string, ctx context.Context) *Session {
	t.Helper()
	childCtx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(childCtx, os.Args[0], "-test.run=^TestSessionHelperProcess$")
	cmd.Env = append(os.Environ(), "GOSVM_SESSION_HELPER="+mode)
	s, e := startSession(cmd, cancel)
	if e != nil {
		cancel()
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
func TestSessionRejectsCorruptResponses(t *testing.T) {
	for _, mode := range []string{"wrong-id", "malformed"} {
		t.Run(mode, func(t *testing.T) {
			s := helperSession(t, mode, context.Background())
			if e := s.Init(map[string]any{}); e == nil {
				t.Fatal("corrupt response accepted")
			}
		})
	}
}
func TestSessionChecksEveryCallIdentityAndError(t *testing.T) {
	for _, mode := range []string{"wrong-call-id", "missing-call-id", "call-error"} {
		t.Run(mode, func(t *testing.T) {
			s := helperSession(t, mode, context.Background())
			if e := s.Init(map[string]any{}); e != nil {
				t.Fatal(e)
			}
			if _, e := s.Batch([]Call{{JSONRPC: "2.0", ID: 0, Method: "unknown"}}); e == nil {
				t.Fatal("bad call response accepted")
			}
		})
	}
}
func TestSessionCancellationUnblocksRead(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	s := helperSession(t, "blocked", ctx)
	started := time.Now()
	e := s.Init(map[string]any{})
	if e == nil || time.Since(started) > 3*time.Second {
		t.Fatalf("cancellation did not unblock: %v", e)
	}
}
func TestRunnerProfileIsValidAndOverrideIsExplicit(t *testing.T) {
	t.Setenv("GOSVM_TEST_FEATURES", "")
	data, e := Features()
	if e != nil {
		t.Fatal(e)
	}
	var ids []string
	if e = json.Unmarshal(data, &ids); e != nil || len(ids) != 237 {
		t.Fatalf("bad profile: %v", e)
	}
	t.Setenv("GOSVM_TEST_FEATURES", "/does/not/exist")
	if _, e = Features(); e == nil || !strings.Contains(e.Error(), "does/not/exist") {
		t.Fatal("override silently fell back")
	}
}
