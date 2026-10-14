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
	// Generics became supported (docs/GO_GENERICS.md): the instantiation
	// f[uint64] is emitted as its own function, and an unused generic type
	// emits nothing.
	27:  {CSHA256: "b4b469cbd78f41a300064e2a6d229a2e0788dd4542820fcae073fd414f7651a0"}, // s03.go: was "generics and bodyless functions are unsupported"
	106: {CSHA256: "d80e4236ff975aa36ae8a3ecf70cc2d1a0ca458449015d327c86e5413a0814d6"}, // s00.go: was "generic types unsupported"
	// Interface values are still rejected at the same position; the message
	// now says interfaces are only usable as constraints.
	140: {Error: "s42.go:2:40: interface values unsupported (interface{}); " + interfaceRule},
	198: {Error: "s43.go:2:1: interface values unsupported (interface{}); " + interfaceRule},
	272: {Error: "control-reject.go:1:11: interface values unsupported (error); " + interfaceRule},
	// Invocation-heap allocation (docs/GO_ALLOCATION.md): escaping locals move
	// to the heap instead of being rejected, and new/make/append/copy, slice
	// literals, slices of any element type, non-byte views and pointer/slice
	// struct fields became supported. Each was a rejection; it now compiles.
	4:   {CSHA256: "a0804eb54eb184e3c464dac41ca9beaeae743e44f0ddee4ec491c24bc1c98afa"}, // return &n: was "reference escapes local storage; heap allocation unsupported"
	9:   {CSHA256: "7ea53a480f5b9f9956ba4472613af2f6b4dd5b8c18955a37cdd64a2685165272"}, // block escape: was "reference escapes its lexical storage scope; ..."
	14:  {CSHA256: "94b71334a777db16e7835135a49b3f76e105c50bb12842ad6ef0b1d4889637c1"}, // s13.go copy(a, b): was "only len and cap builtins supported"
	17:  {CSHA256: "a4c63e1758390004459b92b377913ada81754ab1f4cd11d9caca19a77192286d"}, // tuple local: was "reference escapes local storage; ..."
	21:  {CSHA256: "3ac15f702a89c7a23c6dd65ea44d99a740010ee1960a148e871e6f3be7dbf9c9"}, // view element pointer: was "reference escapes local storage; ..."
	36:  {CSHA256: "5ed691e0702c4de0540f9d07452f3732d80c8078fae0f2bc6d0a65b81231458a"}, // parameter copy: was "reference escapes local storage; ..."
	38:  {CSHA256: "cf7e0f120fb4afee8205675f13e2e7ddcf7b2b52b5602ee1c2dc4e92bb86a35d"}, // value receiver view: was "reference escapes local storage; ..."
	42:  {CSHA256: "91982e45a08892a3267928a7c5008dea4a31f5e8205d13977f57a399a82c99f0"}, // struct { B []byte }: was "struct fields must be unsigned scalars, bools, scalar arrays, or value structs" (signed integers alone only reworded it; allocation accepts it, and its C uses no signed or generic code)
	44:  {CSHA256: "baf9602282157acef4614b69fff652a7d5594fbf2be63b88b6b5152188d0ab49"}, // forwarded tuple view: was "reference escapes local storage; ..."
	67:  {CSHA256: "a0804eb54eb184e3c464dac41ca9beaeae743e44f0ddee4ec491c24bc1c98afa"}, // s74.go return &x: was "reference escapes local storage; ..."
	71:  {CSHA256: "56fd7966e85aa0b3cd84a797a18cef28b1d3f17818eb7afd871688d30b12536a"}, // imported borrow summary: was "reference escapes local storage; ..."
	90:  {CSHA256: "69921d5f82d65ac62ba2d425f7939b5b3055b78e40d2a654252d70ee8fe0c909"}, // local array view: was "reference escapes local storage; ..."
	126: {CSHA256: "855c07853e9d93c43c40ac5c84b9c189b63716b518af28df4a243ac432e37fdb"}, // forwarded local: was "reference escapes local storage; ..."
	137: {CSHA256: "9872902e190e3a7ec51baea1a728be04ddfcabec720a3f857356e079d79b6c58"}, // loop back edge: was "reference escapes local storage; ..."
	149: {CSHA256: "bd34b300f230e6b2f2241b6952570f642ca56ffbd5a1f38a89f407da6a1bb0d5"}, // s05.go []byte{1, 2}: was "only value struct literals supported"
	166: {CSHA256: "e0f04cb67fc1cc10020053b9f6dcef025c7f012e969e249098320b5e4efaef6c"}, // literal return: was "reference escapes local storage; ..."
	175: {CSHA256: "c03b8c8712aac7bf56a36161fca4cb7b1e903bd4caed25ce875a715f2002d526"}, // block view: was "reference escapes its lexical storage scope; ..."
	214: {CSHA256: "faf01e2de5ee2a0899cc3320e60fcc09462c09adbc01c0f64186f183b708fb31"}, // loop escape: was "reference escapes its lexical storage scope; ..."
	234: {CSHA256: "9e255aca30469be4521f512f5fa9c3848f375778922446f7358f2559cb819494"}, // conditional escape: was "reference escapes its lexical storage scope; ..."
	235: {CSHA256: "eb69930c1524863b52b3fbf359f711d63dee9a0708903931dc2c15caad80cf61"}, // [32]uint64 view: was "slice views require byte/uint8 elements"
	238: {CSHA256: "ee9954b9f27a2b0430a127e82ae56c7b4bf2398bf9582e14b987817d74f086b2"}, // callee local method: was "reference escapes local storage; ..."
	241: {CSHA256: "e5fe97047625ade873ae72af734655875223a4845ebcd85fc4e67529ffa86daf"}, // value parameter view: was "reference escapes local storage; ..."
	253: {CSHA256: "ee9954b9f27a2b0430a127e82ae56c7b4bf2398bf9582e14b987817d74f086b2"}, // method expression local: was "reference escapes local storage; ..."
	254: {CSHA256: "3222b8dc2283dad7e26dfc1c249a2b68033f991018cc8d7dce8e817db2dc16bd"}, // make([]byte, 1): was "unsupported expression *ast.ArrayType"
	256: {CSHA256: "1d02328635096474df62c3e9fcf2cfec2a261230fc13d348dbaea644ef4215c8"}, // loop view: was "reference escapes its lexical storage scope; ..."
	257: {CSHA256: "906563a2ca3839c3e25b87a9b8cf9731087be1b6c6e839c9b50d7a71ef197f6e"}, // struct{P *uint64}: was "struct fields must be unsigned scalars, bools, scalar arrays, or value structs" (as entry 42)
	261: {CSHA256: "00b525a5d039ddb7ac7f1c5a911905d00cd8a437ef98eced10c7e7309083b6e7"}, // [4]uint64 view: was "slice views require byte/uint8 elements"
	263: {CSHA256: "6ac5bfb41ddf10e4e2c95b132e5cf94e841f9fb8ea67a77f7cf1fb11cb7a7c13"}, // block tuple: was "reference escapes its lexical storage scope; ..."
	277: {CSHA256: "39a431aafe16ce94a2ff8e5a2014e373e795c5e1ee7641198b5e3b6b58927be0"}, // value receiver copy: was "reference escapes local storage; ..."
	288: {CSHA256: "da0fd08a96472bf2a18d478b819ecd80e02e8de101e88dcafda861c8f88da98a"}, // s30.go []uint64 parameter: was "unsupported type []uint64"
	304: {CSHA256: "1867a3a4f4cfba2e88fe7f58882361492a7aeb580f4c9f83e145b3a47c430657"}, // converted local view: was "reference escapes local storage; ..."
	307: {CSHA256: "73616bde6d18a3643f5c8b6880b50b27cfdd16b0eb0994184fb05c3ef0b2d2bb"}, // s73.go [4]uint64 view: was "slice views require byte/uint8 elements"
}

const (
	arrayElementRule   = "array elements must be integer or bool scalars, or named versions of those scalars"
	pointerElementRule = "pointer elements must be integer/bool scalars, value structs or scalar arrays"
)

const interfaceRule = "interfaces are only usable as type parameter constraints"

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
	// Signed integers, generics and allocation each turned recorded rejections
	// into accepted programs (corpusChanges); the rest still exercise Check.
	if rejected < 95 {
		t.Fatalf("corpus exercises only %d rejections", rejected)
	}
	if changed != len(corpusChanges) {
		t.Fatalf("applied %d of %d corpus changes", changed, len(corpusChanges))
	}
}
