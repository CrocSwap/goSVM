package compiler

import (
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
)

// corpusEntry is one program recorded from the pre-Check compiler (commit
// dfcb8fc): every program the root test suite, the repository examples and
// the supplementary rejection table passed to CompileProgram, with either the
// exact error text or the SHA-256 of the emitted C. Host paths in source names
// and errors were rewritten consistently to /repo and /scratch.
type corpusEntry struct {
	Program Program `json:"program"`
	Error   string  `json:"error"`
	CSHA256 string  `json:"c_sha256"`
}

func loadCorpus(t *testing.T) []corpusEntry {
	t.Helper()
	f, err := os.Open("testdata/check_corpus.jsonl.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	z, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	var entries []corpusEntry
	s := bufio.NewScanner(z)
	s.Buffer(nil, 64<<20)
	for s.Scan() {
		var e corpusEntry
		if err := json.Unmarshal(s.Bytes(), &e); err != nil {
			t.Fatal(err)
		}
		entries = append(entries, e)
	}
	if err := s.Err(); err != nil {
		t.Fatal(err)
	}
	if len(entries) < 300 {
		t.Fatalf("corpus has only %d programs", len(entries))
	}
	return entries
}

// Check must accept exactly the programs the pre-Check compiler accepted and
// reject the rest with the identical first error; CompileProgram must still
// produce byte-identical C. The emitter applies no rules, so an emission
// failure after a successful Check would surface here as a mismatch.
func TestCorpusMatchesPreCheckCompiler(t *testing.T) {
	rejected := 0
	for i, e := range loadCorpus(t) {
		e := e
		checked, checkErr := Check(&e.Program)
		gotCheck := ""
		if checkErr != nil {
			gotCheck = checkErr.Error()
			rejected++
			if checked != nil {
				t.Errorf("entry %d: Check returned a program with error %q", i, gotCheck)
			}
		}
		if gotCheck != e.Error {
			t.Errorf("entry %d: Check error %q, recorded CompileProgram error %q", i, gotCheck, e.Error)
			continue
		}
		out, err := CompileProgram(&e.Program)
		got := ""
		if err != nil {
			got = err.Error()
		}
		if got != e.Error {
			t.Errorf("entry %d: CompileProgram error %q, recorded %q", i, got, e.Error)
			continue
		}
		if err == nil {
			sum := sha256.Sum256(out)
			if hex.EncodeToString(sum[:]) != e.CSHA256 {
				t.Errorf("entry %d: emitted C changed", i)
			}
		}
	}
	if rejected < 100 {
		t.Fatalf("corpus exercises only %d rejections", rejected)
	}
}
