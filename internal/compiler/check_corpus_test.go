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
	// The array element rule now admits every integer type; the position and the
	// rejection are unchanged, only the message text describes the admitted
	// element types.
	16:  {Error: "s29.go:3:8: " + arrayElementRule},
	80:  {Error: "arrays-reject.go:1:40: " + arrayElementRule},
	138: {Error: "arrays-reject.go:1:44: " + arrayElementRule},
	148: {Error: "arrays-reject.go:1:18: " + arrayElementRule},
	224: {Error: "arrays-reject.go:1:18: " + arrayElementRule},
	310: {Error: "arrays-reject.go:1:18: " + arrayElementRule},
	// Signed integers and uint became supported (docs/GO_INTEGERS.md): int is
	// an ordinary type, and len/cap results are ordinary int values.
	33:  {CSHA256: "f76de6d7e64f37a30a48cf662a5ebd1eade24a7588c6ebc2ef610546ebc58fd0"}, // s71.go: `type N int` with a method was "int variables are unsupported"
	50:  {CSHA256: "8f183b463c0025adfa9655b9ca1ce1b5843414680dbb24afb4524b4c7e8ed2f1"}, // s06.go: `len(a)<<1 > 3` was "signed shifts unsupported"
	89:  {CSHA256: "d0b3ea2cf5f87023b7adeccc94e9617ef19dcf38bcbbddfee47196b07de99e59"}, // s24.go: `n := len(a); uint64(n + 1)` was "int variables are unsupported"
	104: {CSHA256: "837beeaeb9fddb87cba5be45e625f6dc0a591f230eb4341756eb7eea36eb27fc"}, // reject.go: `x:=1` was "int variables are unsupported"
	132: {CSHA256: "eb5b154b2488d1717566f84fda5e74f8211466374ba01720964bd7c292787520"}, // arrays-reject.go: `type K [32]int` was the array element rule
	141: {CSHA256: "4fa51348d02275dfe019f879b137e9cd33e237a04558d3d136b24ebabb3c79ae"}, // s54.go: `n := 0; n++` was "int variables are unsupported"
	187: {CSHA256: "9b0fd60c5afb735399ae14e48bb999229ee445058025ac422e6d6d6660eaca25"}, // s04.go: `len(a) > -1` was "constant is outside uint64"
	188: {CSHA256: "b88b939f4eee2d354fd3bbe8837074dcf7f22dee6d494ee104de08e11832654d"}, // s19.go: `var x int64` was "unsupported type int64"
	194: {CSHA256: "84bbbe1485125267306d0ff48370dd9b9202293ace5cfb13af41a944f2712b71"}, // s72.go: `func f() (int, uint64)` was "int variables are unsupported"
	203: {CSHA256: "d11ffb6f8370e942cae79e228118b6a613ceedae1b91249785c5a9a040480c23"}, // s08.go: `-len(a) < 0` was "signed negation unsupported"
	204: {CSHA256: "af6850345d59fc6a4de6986eacee6c4b1f1a4408b7f78711d0e8b2698a854ce3"}, // app/program.go: `n:=int(i[0])` was "int variables are unsupported"
	237: {CSHA256: "6746cf05fd8bd08cc51a418f63ebb3504f95f25eaf59504545d6f4f3e9b804cf"}, // s07.go: `len(a)+1 > 3` was "signed arithmetic unsupported"
	262: {CSHA256: "9d274bc667f92bf7517e45763e728acc79c8b1f6e9823ee09a6e556819cc35e8"}, // s09.go: `-len(a)+1 > 0` was "signed negation unsupported"
	276: {CSHA256: "d3edf0da3b809525b77d572db6685e8222d4b30ea92eae8bdd6db7f2ac30b93a"}, // s70.go: `func f() int` was "int variables are unsupported"
	292: {CSHA256: "c6449caba1bc423adea522b98a3a63141789c9afb7db67de907930acd77dd52a"}, // bad.go: `type Bad int; var b Bad` was "int variables are unsupported"
	305: {CSHA256: "a8fb60e2c3adc9557a164b313192efd5bdf764bde00532b39e3c5e22c9040e3c"}, // s25.go: `struct{ x int64 }`, `uint64(-len(a))` was "unsupported type int64"
	// The int parameter is accepted now; the next rejection is the package var.
	54: {Error: "s22.go:3:1: only functions and constants are supported at package scope"}, // was "int variables are unsupported"
	// Rule messages now say "integer" where they said "unsigned"; positions
	// and rejections are unchanged.
	3:   {Error: "s51.go:2:1: " + pointerElementRule},
	32:  {Error: "s28.go:2:1: " + pointerElementRule},
	190: {Error: "pointer-reject.go:1:60: " + pointerElementRule},
	233: {Error: "pointer-reject.go:1:45: " + pointerElementRule},
	42:  {Error: "bad.go:1:16: " + structFieldRule},
	257: {Error: "pointer-reject.go:1:16: " + structFieldRule},
}

const (
	arrayElementRule   = "array elements must be integer or bool scalars, or named versions of those scalars"
	pointerElementRule = "pointer elements must be integer/bool scalars, value structs or scalar arrays"
	structFieldRule    = "struct fields must be integer scalars, bools, scalar arrays, or value structs"
)

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
