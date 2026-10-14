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

// corpusChanges lists the recorded programs whose verdict changed on purpose
// after the corpus was frozen, with the outcome the current compiler must
// produce instead. The .gz is never edited; every other entry must still match
// its recording exactly.
var corpusChanges = map[int]corpusEntry{
	// range loops and uint16 became supported (docs/GO_CONTROL.md, GO_ARRAYS.md).
	60:  {CSHA256: "b24001c7cfe06b63cfa8a1787a5009f2b59bb1667f3e9cfd6b356feeeb7b97f3"}, // s26.go: `type U uint16` was "unsupported type program.U"
	88:  {CSHA256: "67eba2cd0da9026e35ac7315ef8fd16bccf9b680b70e55e7cd355c55bff5bd29"}, // arrays-reject.go: `for _,v:=range a` over [32]byte
	260: {CSHA256: "8a7e3e83cd5a13e9acfa665da71172b34b20137583cdda1761869b349a41eb62"}, // s36.go: `for i := range a` over a byte slice
	// The array element rule now lists uint16; the position and the rejection are
	// unchanged, only the message text names the added width.
	16:  {Error: "s29.go:3:8: " + arrayElementRule},
	80:  {Error: "arrays-reject.go:1:40: " + arrayElementRule},
	132: {Error: "arrays-reject.go:1:18: " + arrayElementRule},
	138: {Error: "arrays-reject.go:1:44: " + arrayElementRule},
	148: {Error: "arrays-reject.go:1:18: " + arrayElementRule},
	224: {Error: "arrays-reject.go:1:18: " + arrayElementRule},
	310: {Error: "arrays-reject.go:1:18: " + arrayElementRule},
}

const arrayElementRule = "array elements must be byte, uint16, uint32, uint64, bool, or named versions of those scalars"

// Check must accept exactly the programs the pre-Check compiler accepted and
// reject the rest with the identical first error; CompileProgram must still
// produce byte-identical C. The emitter applies no rules, so an emission
// failure after a successful Check would surface here as a mismatch. Entries
// in corpusChanges are verified against their new expected outcome instead.
func TestCorpusMatchesPreCheckCompiler(t *testing.T) {
	rejected, changed := 0, 0
	for i, e := range loadCorpus(t) {
		e := e
		if change, ok := corpusChanges[i]; ok {
			e.Error, e.CSHA256 = change.Error, change.CSHA256
			changed++
		}
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
				t.Errorf("entry %d: emitted C changed (sha256 %x)", i, sum)
			}
		}
	}
	if rejected < 100 {
		t.Fatalf("corpus exercises only %d rejections", rejected)
	}
	if changed != len(corpusChanges) {
		t.Fatalf("applied %d of %d corpus changes", changed, len(corpusChanges))
	}
}
