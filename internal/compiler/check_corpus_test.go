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
	// The array element rule was reworded when it admitted every integer type
	// (entries 16, 80, 138, 148, 224 and 310); arrays of composite elements
	// later made five of them compile and gave arrays of Context their own
	// message (listed with value types below).
	//
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
	// Entry 54 (s22.go): the int parameter became accepted, then the package
	// var; it is listed with package-level variables below.
	// Rule messages now say "integer" where they said "unsigned"; positions
	// and rejections are unchanged.
	32:  {Error: "s28.go:2:1: " + pointerElementRule},
	233: {Error: "pointer-reject.go:1:45: " + pointerElementRule},
	// Generics became supported (docs/GO_GENERICS.md): the instantiation
	// f[uint64] is emitted as its own function, and an unused generic type
	// emits nothing.
	27:  {CSHA256: "b4b469cbd78f41a300064e2a6d229a2e0788dd4542820fcae073fd414f7651a0"}, // s03.go: was "generics and bodyless functions are unsupported"
	106: {CSHA256: "d80e4236ff975aa36ae8a3ecf70cc2d1a0ca458449015d327c86e5413a0814d6"}, // s00.go: was "generic types unsupported"
	// Interface values became supported (docs/GO_INTERFACES.md): an empty
	// type switch on a nil interface{} and an unused interface{} parameter
	// compile. error stayed rejected (its Error() method returns a string)
	// until strings were added; entry 272 is listed with them below.
	140: {CSHA256: "63c89e0040819ca7a82cba771a537716c55385192f5d21028afc82d3cd010217"}, // s42.go `var x interface{}; switch x.(type) {}`: was "unsupported type interface{}" (generics reworded it to name the constraint rule)
	198: {CSHA256: "bac1ea585f5ef40028da5cf56b79789e4e8cb2f7d53b939a4dd55463af60254a"}, // s43.go `func f(x interface{})`: likewise
	// Invocation-heap allocation (docs/GO_ALLOCATION.md): escaping locals move
	// to the heap instead of being rejected, and new/make/append/copy, slice
	// literals, slices of any element type, non-byte views and pointer/slice
	// struct fields became supported. Each was a rejection; it now compiles.
	4:   {CSHA256: "dfa460fc47456032668252c4e7b9a17f467f0beebaa14e6e38447bee4d46e068"}, // return &n: was "reference escapes local storage; heap allocation unsupported"
	9:   {CSHA256: "cd30ff1c4341e1e96c12188dc8cf3a77d2aa51a2781245a300ffc3d2d271550f"}, // block escape: was "reference escapes its lexical storage scope; ..."
	14:  {CSHA256: "d855888bd991a84f6ac4e18bd88a79a058226f6a0464cec77d30a4090c3015cd"}, // s13.go copy(a, b): was "only len and cap builtins supported"
	17:  {CSHA256: "4a46a782c03b1601edbc3aa2e2e9317df8853e9ad956cafe4a6abf791aae96ba"}, // tuple local: was "reference escapes local storage; ..."
	21:  {CSHA256: "b2112887a4d2cf9cf27b5e8da1f44e6754493b08240853b97b9c17754687e45e"}, // view element pointer: was "reference escapes local storage; ..."
	36:  {CSHA256: "fef9553323ecdae42995b9a0a8dc8b932083dc2a2ffe4c39749fa485af764e39"}, // parameter copy: was "reference escapes local storage; ..."
	38:  {CSHA256: "4345d89e03aea37b054b526f3566d39cc2c49163b4bf79fcaf95a1278545b1b5"}, // value receiver view: was "reference escapes local storage; ..."
	42:  {CSHA256: "b47d6c94272812e6e0a79c1f2f916f0625730d13c851627d6dd1d3ece1dddb4a"}, // struct { B []byte }: was "struct fields must be unsigned scalars, bools, scalar arrays, or value structs" (signed integers alone only reworded it; allocation accepts it, and its C uses no signed or generic code)
	44:  {CSHA256: "c654a7cd45597df66f7571a7c260d9b7adfc829734f5a14e7689eed5fea75566"}, // forwarded tuple view: was "reference escapes local storage; ..."
	67:  {CSHA256: "dfa460fc47456032668252c4e7b9a17f467f0beebaa14e6e38447bee4d46e068"}, // s74.go return &x: was "reference escapes local storage; ..."
	71:  {CSHA256: "ba3d4e971f0979937f092d0ebdc4ca81df8444192430d12d5f1508ed205a974b"}, // imported borrow summary: was "reference escapes local storage; ..."
	90:  {CSHA256: "376fce874742f7dd4c4e561dbb3ffd702db45c1b271f9d3c4405bf55846cf580"}, // local array view: was "reference escapes local storage; ..."
	126: {CSHA256: "1c269501ee630ccddc8ddc2e77e9c28279f77e9d6290a97c8711090dbf48af20"}, // forwarded local: was "reference escapes local storage; ..."
	137: {CSHA256: "afe6de4b06c08c4491d34ec8cec0ee71f24ea8fdc1f5d9c09837eed7ce8b2a02"}, // loop back edge: was "reference escapes local storage; ..."
	149: {CSHA256: "d363eaee57022025ba1b452e57484e1e8450329c7e4c7962652c65b4d66f09c9"}, // s05.go []byte{1, 2}: was "only value struct literals supported"
	166: {CSHA256: "699b8ccc4ee9d494f6d202876a594156a260a7166508c4b295fcb4bf421f8bc7"}, // literal return: was "reference escapes local storage; ..."
	175: {CSHA256: "8da4cd08107151497f43f2a2295858f39abcb93eec6ec2ec8aab849d1f244497"}, // block view: was "reference escapes its lexical storage scope; ..."
	214: {CSHA256: "fe156115a9dde3cffdefabf1121aab31cca80717ed824189436792860c9c21f6"}, // loop escape: was "reference escapes its lexical storage scope; ..."
	234: {CSHA256: "6be7c548b10efd83cc0601ce3b13f02e9191b23c1f0752fa8933e819752b0915"}, // conditional escape: was "reference escapes its lexical storage scope; ..."
	235: {CSHA256: "b74f1a61815ebd86bf8241ccf7db1b21c56ae0bbbf4d946ec2e472e784e26d7f"}, // [32]uint64 view: was "slice views require byte/uint8 elements"
	238: {CSHA256: "de1b861bd7f6e99f982fe64b318c2511de1c72b10114181152dc45bf9cce0bdb"}, // callee local method: was "reference escapes local storage; ..."
	241: {CSHA256: "140ae14c62ae6576e24acd99ab28f2c2defc3405f3bbe13e724c4fef4d90cd4b"}, // value parameter view: was "reference escapes local storage; ..."
	253: {CSHA256: "de1b861bd7f6e99f982fe64b318c2511de1c72b10114181152dc45bf9cce0bdb"}, // method expression local: was "reference escapes local storage; ..."
	254: {CSHA256: "45d7603a0b44e96193e60107e6815ff3375a737372e471fa440a71f6caead340"}, // make([]byte, 1): was "unsupported expression *ast.ArrayType"
	256: {CSHA256: "d7c3b4a654d6252319b369452f8e9a4ca16b5ad627a89c6445d92c497ab7114a"}, // loop view: was "reference escapes its lexical storage scope; ..."
	257: {CSHA256: "4a87e7c45a994c9046795b681dd000bcb52750c5eb54906d29d002932d339c5f"}, // struct{P *uint64}: was "struct fields must be unsigned scalars, bools, scalar arrays, or value structs" (as entry 42)
	261: {CSHA256: "8c82ebf0935759a4fbe12f0fca03949d78295be9f2965c86909ae2f3685b1c0d"}, // [4]uint64 view: was "slice views require byte/uint8 elements"
	263: {CSHA256: "0fcceb5571a63b09d6d6ef933a424a6f2766a6bfa247cc92dfcc3d4af6861ab8"}, // block tuple: was "reference escapes its lexical storage scope; ..."
	277: {CSHA256: "6284f362b4aaed53ecf4e27372dd9844706bca124be22de1e62cc9eb45e963fe"}, // value receiver copy: was "reference escapes local storage; ..."
	288: {CSHA256: "6303c066f5933e82e3f5ecb8c0db1ab28d0066658c1af1a6c09fcb0d60347f59"}, // s30.go []uint64 parameter: was "unsupported type []uint64"
	304: {CSHA256: "8eff277e24f9b0faab2915560880f75a37e6e2378e978d95d2912b75c627c2b3"}, // converted local view: was "reference escapes local storage; ..."
	307: {CSHA256: "31b62ae4fd10ce4425234960c5dbfb5fa7c4cae7f84a664c47be870818bd4127"}, // s73.go [4]uint64 view: was "slice views require byte/uint8 elements"
	// Strings and error became supported (docs/GO_STRINGS.md): the error
	// result and the []byte("abc") conversion compile, and a standard-library
	// import names math/bits as the only one available.
	272: {CSHA256: "01aed7d5a8b1c0ad1d9da6cbfad2ccc30365abb378ac8024d4e421d041442c68"}, // control-reject.go `func pair() (uint64, error)`: was "unsupported type error", then the error-values message
	308: {CSHA256: "bc34f4f2f213a97d82503cbedb6d13bd1e59174e4ffe033f76f7cd380bdfe8d1"}, // s10.go `x := []byte("abc")`: was "slice conversion requires identical byte slices or nil"
	// (Entry 249's message changed again with the standard-library
	// overlay; it is listed there.)
	// No entry is affected by both strings and function values: merging them
	// changed no verdict or C hash recorded by either side.
	//
	// Function values, closures, defer and named results became supported
	// (docs/GO_FUNCTIONS.md): each was a rejection and now compiles. Two
	// remaining rejections are reached by the new rules first: a call
	// through a Context field (Context fields stay opaque) and an SDK
	// function used as a value (gosvm/solana has no Go bodies).
	34:  {CSHA256: "8595544262d26a763411c7c8bb44880c6d03ef0098ca4d04fe4568289960101e"},                               // f2:=f; f2(): was "unsupported identifier"
	65:  {CSHA256: "ef1906da10e87cf29e30eda8b6f0a2a282a822ced60e07f452c71264c962fc3c"},                               // func f()(a,b uint64): was "named results are unsupported"
	81:  {CSHA256: "d8fdbfe5a5991e3521b1d9159b8c65f32cf81aeb3295d8991d5195069e806898"},                               // defer f(): was "unsupported statement *ast.DeferStmt"
	91:  {CSHA256: "d5fa4c049fd9b3c5a0950d8f82a285779965847f7a57d41c88b0a315e79de9e4"},                               // s14.go f := func() {}: was "unsupported expression *ast.FuncLit"
	116: {Error: "reject.go:1:78: Context fields are opaque"},                                                        // c.Invoke(...) through a Context field: was "methods and function values unsupported"
	122: {Error: "reject.go:1:74: gosvm/solana functions cannot be used as values; call them in a function literal"}, // f:=solana.Count: was "only value struct fields supported"
	134: {CSHA256: "2f25d7579d7e34f590edfc32ee596dcf6789b5e6e869b59f0d87bc942c80de08"},                               // s27.go type F func(): was "unsupported type program.F"
	136: {CSHA256: "ef1906da10e87cf29e30eda8b6f0a2a282a822ced60e07f452c71264c962fc3c"},                               // func pair()(a,b uint64): was "named results are unsupported" (same C as 65)
	153: {CSHA256: "e49ae32ab980d9b8c81e7e69ac6d02d83500f13dba1873e0247eca4099cb9876"},                               // imported f:=s.F: was "method values unsupported; call the method directly"
	183: {CSHA256: "d8fdbfe5a5991e3521b1d9159b8c65f32cf81aeb3295d8991d5195069e806898"},                               // s63.go defer f(): was "unsupported statement *ast.DeferStmt" (same C as 81)
	202: {CSHA256: "42fecb8761b7e175e49263b339f58e2e9a6150bb69074218952e49cc2687a411"},                               // s12.go func() uint64 {...}(): was "only direct function calls supported"
	229: {CSHA256: "663f0499684a21b55fef529a5deb467c2aaf653baa61a27575b330452beaccc0"},                               // f:=v.Read: was "method values unsupported; call the method directly"
	248: {CSHA256: "b43fe9401e9f9d4486dbb0d3c0cade269fe342a20b523b0067f2ead1217f8b40"},                               // s37.go f := s.M: was "method values unsupported; call the method directly"
	291: {CSHA256: "97bda7425bf54a6627a8ce226e89ccf3ab3e56925058168bd57dfbf7e9cc4c8a"},                               // imported f:=G: was "unsupported identifier"
	// Maps became supported (docs/GO_MAPS.md): the unused nil map compiles.
	// Compound assignment on integers became supported (docs/GO_INTEGERS.md):
	// both recorded compound assignments compile. Neither is affected by
	// another merged feature.
	161: {CSHA256: "877afcf937ed4e30ca8acf824296db0d8c04520c3e9328a0b1bd145eabf41a58"}, // s44.go `x <<= 2` on a uint64 local: was "compound assignment unsupported"
	232: {CSHA256: "37b80f76ea948cb1e3ebde14eb6e86b6081f6b722394d51775b073884cae2f7c"}, // store.go `b[o+j] += byte(v>>(8*j))` on a byte-slice element in a loop: was "compound assignment unsupported"
	// No entry is affected by both maps and function values: entry 258 has no
	// function value, and none of the function-value entries uses a map, so
	// merging them changed no verdict or C hash recorded by either side.
	258: {CSHA256: "e445bfeed82dbc1fa96f86a6b3828095fcbd7193fba2d17229cd5f2395871ee5"}, // s34.go `var m map[uint64]uint64; _ = m`: was "unsupported type map[uint64]uint64"
	// Package-level variables and init functions became supported
	// (docs/GO_GLOBALS.md): every recorded package-level var and init
	// function now compiles. Variables never written are read-only data
	// (zero cost); entry 54 was listed above with its earlier change.
	54:  {CSHA256: "4eabe761928394e3e9621d26e72c3f41718db099c544148366a078100dab2f1b"}, // s22.go `var G uint64`: was "only functions and constants are supported at package scope"
	155: {CSHA256: "ab2dd66c3ec98d4a07e30fb136d3886823c84dd04e3b504f85282717567ddda9"}, // reject.go `var x uint64; return x`: likewise
	177: {CSHA256: "a645437863b63515b86d3ca14de938f134d2c4735fc08715a4cad9e913facf66"}, // dep/program.go `var N uint64=1` read from another package: likewise
	178: {CSHA256: "abe2bef8566cb6c04e310fec04cdc44011119dc8723956fed488e93b99e0a0fb"}, // swap/bad.go `var UnsupportedGlobal uint64` (the CLI test now uses a float64): likewise
	186: {CSHA256: "f891973d3fb7766cb0399984d671bd8ca5669a669b6e35f28589200bd33523ec"}, // multi-state/bad.go `var Global uint64` (the schema-2 test now uses a float64): likewise
	240: {CSHA256: "af2eefe98753ad6c2e738b6365d621a62f5a710ea7c2e11338c146ff39be8f77"}, // reject.go `func init(){}`: was "init functions are unsupported" (then b5d064c9..., see compile-time evaluation below)
	245: {CSHA256: "4eabe761928394e3e9621d26e72c3f41718db099c544148366a078100dab2f1b"}, // s23.go `var G uint64` (same C as entry 54): was "only functions and constants are supported at package scope"
	259: {CSHA256: "32b02994941902507eb277dea22a917549fa3714e14f50f5fd027d991072f9ab"}, // dep/program.go `func init(){}`: was "init functions are unsupported" (then 18dc882b..., see compile-time evaluation below)
	// Compile-time evaluation of package initialization
	// (docs/GO_GLOBALS.md#compile-time-evaluation): the two empty init
	// functions above are evaluated at compile time, so gosvm_init no
	// longer calls them (their C changed); no verdict changed and no other
	// recorded program has an initialization step.
	273: {CSHA256: "fb9a7312d4f614e48dccc2d2a4a407c4f867d4852338c4f119b39d7f834039c0"}, // swap/bad.go `var Mutable uint64` (the generation test now uses a float64): was "only functions ... at package scope"
	// Struct comparison, type aliases and arrays of composite elements became
	// supported (docs/GO_TYPES.md, docs/GO_ARRAYS.md): each was a rejection
	// and now compiles. Arrays of Context stay rejected, with their own
	// message at the same position. No other entry's C changed.
	16:  {CSHA256: "2a1ce67ca1a068273f98f61b88dc81e25628ba3321f3ff1b2535718d9328a34f"}, // s29.go `type A [2]S`: was the array element rule
	138: {CSHA256: "2a1ce67ca1a068273f98f61b88dc81e25628ba3321f3ff1b2535718d9328a34f"}, // `type K [2]S` (same C as 16): was the array element rule
	148: {CSHA256: "4f3a9fd70eb54ef3cf5c03afba67be79b501e47c55b3ebcc9b52d7100caa07bc"}, // `type K [2][32]byte`: was the array element rule
	224: {CSHA256: "af0540f769e7e0d7a21a5fee2b40c338e268470bbe88a98187e3583d8336767c"}, // `type K [2][]byte`: was the array element rule
	310: {CSHA256: "dbfbeb58ec6cb358ccde4e1369a0336ae2696c328704bdabd8c14d14be193599"}, // `type K [2]*uint64`: was the array element rule
	98:  {CSHA256: "9ab28d50e96367ed1bb8d27295320337acb3d1fa2c672bada91a88ea0d7d014c"}, // s58.go `switch s { case S{}: }`: was "struct switch comparisons unsupported; ..."
	193: {CSHA256: "a2e9a8b29dc0b3db21768d789a28e7f97f63c9fd8db202ba75d134e76d83147c"}, // `switch (V{}) {case V{}: ...}`: was "struct switch comparisons unsupported; ..."
	151: {CSHA256: "56496b4bd1ca69f1da1265cf375e95b1bdcc8bada64869be69a9244263f11516"}, // `if a==a`: was "struct comparisons unsupported; compare fields explicitly"
	168: {CSHA256: "d80e4236ff975aa36ae8a3ecf70cc2d1a0ca458449015d327c86e5413a0814d6"}, // `type V = uint64`, unused (same C as 106): was "type aliases unsupported"
	80:  {Error: "arrays-reject.go:1:40: arrays of Context unsupported"},               // `type K [2]solana.Context`: was the array element rule
	// Evaluation order became gc's (docs/ERROR_CONTRACT.md#evaluation-order):
	// no verdict changed; these programs read an operand that a later call
	// in the same expression may write (Check cannot prove the call does
	// not), so the operand is now read after the call. Their results are
	// unchanged.
	47:  {CSHA256: "8431eac63614236f107cd7a8e40f66b89738186cda1782b8a99fde9337905bd9"}, // pointer-values: b.Value(next(s)) copies *b after next(s), which writes through a slice
	164: {CSHA256: "8431eac63614236f107cd7a8e40f66b89738186cda1782b8a99fde9337905bd9"}, // the same program from the examples sweep
	130: {CSHA256: "acf26e408a167542466ed2d1aa654a627d998ee1870ad1d87d668c3aca8dd921"}, // control-values: s[uint64(i[1])] reads i[1] after next(s), next(s)
	139: {CSHA256: "acf26e408a167542466ed2d1aa654a627d998ee1870ad1d87d668c3aca8dd921"}, // the same program from the examples sweep
	// Package-level variables and value types were developed in parallel;
	// no package-variable entry uses a value-type feature and vice versa,
	// so merging them changed no verdict or C hash recorded by either side
	// (62 rejections remain).
	// Standard-library packages (docs/GO_STDLIB.md): an unsupported
	// standard-library import lists the packages available on chain, and
	// pointers to pointers (errors.As(err, &p) for a pointer-typed p needs
	// a **T) and variadic functions (errors.Join) became supported. No
	// other entry is affected.
	120: {CSHA256: "ab554322d245840ef1e11f3e7a52210520fcfe53075ed9cf1abc4ed3c44f2750"}, // s02.go `func f(xs ...uint64)` called as f(): was "variadics are unsupported"
	// (Entry 249's list gained io with the io overlay; see below.)
	3:   {CSHA256: "35e30e3a3b6c5b64a7bf16ab58665fc2616eeb85e7362cc64e837a805105a3b6"}, // s51.go `func f(p **uint64)`: was pointerElementRule
	190: {CSHA256: "877ada41476ba1837ae0f9276ed3222dcd48d27e644e436dfdcc06deb8acceeb"}, // pointer-reject.go `pp:=&p;return **pp`: was pointerElementRule (it now compiles; the nil dereference traps at run time)
	// The standard library and value types were developed in parallel: no
	// entry is affected by both, so merging them changed no verdict or C
	// hash recorded by either side.
	// Aggregate parameters on SBF (docs/GO_STDLIB.md#aggregate-parameters-on-sbf):
	// no verdict changed. These programs copy an aggregate parameter that a
	// function writes at entry (64 programs, 58 of them listed here; the
	// same 64 the written-parameter rule alone changes); append's growth
	// helper no longer writes its slice parameter, which changes the C of
	// every program that appends. Entries above whose C changed for either
	// reason carry their new hashes in place (53 entries).
	// Real libraries (docs/GO_STDLIB.md#io, results/compiler/2026-10-16-real-libraries):
	// package io joined the overlay, so the list of available packages in
	// the unsupported-import message names it. No other entry changed:
	// no recorded program has an elided &T{...} of array type, and the
	// read-only pointer parameters of package-level variables change no
	// recorded program's placement.
	249: {Error: `reject.go:1:18: could not import fmt (unsupported import "fmt"; standard-library packages on chain: bytes, encoding/binary, errors, io, math, strconv, strings, unicode, unicode/utf8 and math/bits (docs/GO_STDLIB.md))`}, // was "...; use a module-backed program for user packages", then "...; the only standard-library package on-chain is math/bits", then without io
	6:   {CSHA256: "01f287d402774053e6ca44fdcaa3157cd7f6c348df854f5098092cd87ab06724"},                                                                                                                                                       // model.go
	8:   {CSHA256: "01f287d402774053e6ca44fdcaa3157cd7f6c348df854f5098092cd87ab06724"},                                                                                                                                                       // model.go
	10:  {CSHA256: "d0af7c7bf21c2bc15e6dfebe12bbb259bad502ab5678ea74668aeb0525631a34"},                                                                                                                                                       // program.go
	11:  {CSHA256: "513db2a5c364047e3a630a5f2e38599aa5e1eb41c8b4658c3c4067b139aeff66"},                                                                                                                                                       // entry.go
	20:  {CSHA256: "9eea3c26d783a4a139269f4ee7ce08370c2c4c348e7215292f513a2ce1538d03"},                                                                                                                                                       // model.go; then the CPI bounds (below)
	22:  {CSHA256: "8b36bc705cf986395c265e047fb7aa133b7e2561d4e259172930b6af11ee7358"},                                                                                                                                                       // pda.go; then the CPI bounds (below)
	40:  {CSHA256: "8ea5fabbd6de885686956be6f6e294059b5115153b3866006e3d8a1deb7373ca"},                                                                                                                                                       // program.go
	43:  {CSHA256: "9eea3c26d783a4a139269f4ee7ce08370c2c4c348e7215292f513a2ce1538d03"},                                                                                                                                                       // model.go; then the CPI bounds (below)
	48:  {CSHA256: "01f287d402774053e6ca44fdcaa3157cd7f6c348df854f5098092cd87ab06724"},                                                                                                                                                       // model.go
	51:  {CSHA256: "513db2a5c364047e3a630a5f2e38599aa5e1eb41c8b4658c3c4067b139aeff66"},                                                                                                                                                       // types.go
	57:  {CSHA256: "01f287d402774053e6ca44fdcaa3157cd7f6c348df854f5098092cd87ab06724"},                                                                                                                                                       // model.go
	58:  {CSHA256: "7e0975a8e457446af4c53308487b583a1fd28d90c586064e235a2c578bb9c6e7"},                                                                                                                                                       // model.go
	59:  {CSHA256: "01f287d402774053e6ca44fdcaa3157cd7f6c348df854f5098092cd87ab06724"},                                                                                                                                                       // model.go
	68:  {CSHA256: "b243ca6e867114fc883d29532f1d76a85183bb8e349e676715807bd4e3373b6d"},                                                                                                                                                       // model.go; then the CPI bounds (below)
	77:  {CSHA256: "01f287d402774053e6ca44fdcaa3157cd7f6c348df854f5098092cd87ab06724"},                                                                                                                                                       // model.go
	83:  {CSHA256: "01f287d402774053e6ca44fdcaa3157cd7f6c348df854f5098092cd87ab06724"},                                                                                                                                                       // model.go
	85:  {CSHA256: "a033ec1c6d2eabaac330c3c60ee55775aa603965f81066ca83904ee185e83438"},                                                                                                                                                       // model.go; then the CPI bounds (below)
	86:  {CSHA256: "b243ca6e867114fc883d29532f1d76a85183bb8e349e676715807bd4e3373b6d"},                                                                                                                                                       // model.go; then the CPI bounds (below)
	92:  {CSHA256: "01f287d402774053e6ca44fdcaa3157cd7f6c348df854f5098092cd87ab06724"},                                                                                                                                                       // model.go
	93:  {CSHA256: "d0af7c7bf21c2bc15e6dfebe12bbb259bad502ab5678ea74668aeb0525631a34"},                                                                                                                                                       // test.go
	99:  {CSHA256: "01f287d402774053e6ca44fdcaa3157cd7f6c348df854f5098092cd87ab06724"},                                                                                                                                                       // model.go
	105: {CSHA256: "01f287d402774053e6ca44fdcaa3157cd7f6c348df854f5098092cd87ab06724"},                                                                                                                                                       // model.go
	108: {CSHA256: "01f287d402774053e6ca44fdcaa3157cd7f6c348df854f5098092cd87ab06724"},                                                                                                                                                       // model.go
	112: {CSHA256: "e4a852c6db22eefec5f520cbcd4b7f609ab4b9cbf718c1000a155366fd8d7984"},                                                                                                                                                       // model.go; then the CPI bounds (below)
	114: {CSHA256: "d508df22ee50a02bdf37a08cf366b6c97ba3d310baca4ce84795dd6a75957a60"},                                                                                                                                                       // pda.go; then the CPI bounds (below)
	115: {CSHA256: "ff9d74d9cd831d0c92b087ea98d60ea3d16b969ddd582013014146d070930048"},                                                                                                                                                       // model.go
	117: {CSHA256: "01f287d402774053e6ca44fdcaa3157cd7f6c348df854f5098092cd87ab06724"},                                                                                                                                                       // model.go
	118: {CSHA256: "01f287d402774053e6ca44fdcaa3157cd7f6c348df854f5098092cd87ab06724"},                                                                                                                                                       // model.go
	121: {CSHA256: "01f287d402774053e6ca44fdcaa3157cd7f6c348df854f5098092cd87ab06724"},                                                                                                                                                       // model.go
	123: {CSHA256: "01f287d402774053e6ca44fdcaa3157cd7f6c348df854f5098092cd87ab06724"},                                                                                                                                                       // model.go
	142: {CSHA256: "01f287d402774053e6ca44fdcaa3157cd7f6c348df854f5098092cd87ab06724"},                                                                                                                                                       // model.go
	143: {CSHA256: "01f287d402774053e6ca44fdcaa3157cd7f6c348df854f5098092cd87ab06724"},                                                                                                                                                       // model.go
	147: {CSHA256: "01f287d402774053e6ca44fdcaa3157cd7f6c348df854f5098092cd87ab06724"},                                                                                                                                                       // model.go
	160: {CSHA256: "01f287d402774053e6ca44fdcaa3157cd7f6c348df854f5098092cd87ab06724"},                                                                                                                                                       // model.go
	167: {CSHA256: "e4a852c6db22eefec5f520cbcd4b7f609ab4b9cbf718c1000a155366fd8d7984"},                                                                                                                                                       // model.go; then the CPI bounds (below)
	169: {CSHA256: "01f287d402774053e6ca44fdcaa3157cd7f6c348df854f5098092cd87ab06724"},                                                                                                                                                       // model.go
	172: {CSHA256: "01f287d402774053e6ca44fdcaa3157cd7f6c348df854f5098092cd87ab06724"},                                                                                                                                                       // model.go
	176: {CSHA256: "4d5e4c37f3960bd864eec5d57db4feec57401f779a0922b8d8ef644504964dc9"},                                                                                                                                                       // model.go; then the CPI bounds (below)
	181: {CSHA256: "f3ec7d095818e1b1d8339cad5a0a05a40a47c8d14991736b41c7eb6094046e77"},                                                                                                                                                       // model.go
	192: {CSHA256: "01f287d402774053e6ca44fdcaa3157cd7f6c348df854f5098092cd87ab06724"},                                                                                                                                                       // model.go
	206: {CSHA256: "01f287d402774053e6ca44fdcaa3157cd7f6c348df854f5098092cd87ab06724"},                                                                                                                                                       // model.go
	207: {CSHA256: "01f287d402774053e6ca44fdcaa3157cd7f6c348df854f5098092cd87ab06724"},                                                                                                                                                       // model.go
	208: {CSHA256: "01f287d402774053e6ca44fdcaa3157cd7f6c348df854f5098092cd87ab06724"},                                                                                                                                                       // model.go
	210: {CSHA256: "9972da2424b26332db0ffefc83d0c1206dd02f978c7fa2e8802cf1acf0b3d15a"},                                                                                                                                                       // pda.go; then the CPI bounds (below)
	211: {CSHA256: "01f287d402774053e6ca44fdcaa3157cd7f6c348df854f5098092cd87ab06724"},                                                                                                                                                       // model.go
	217: {CSHA256: "53dd42168007cd967b671f8d2809218c04114f41e6d2946682d78c434a5e0adb"},                                                                                                                                                       // model.go; then the CPI bounds (below)
	219: {CSHA256: "03a66c44f073b48586edb8f45ff3f74cf3d91eceee4f3cf3480ed5e3e17e0950"},                                                                                                                                                       // arrays.go
	225: {CSHA256: "d0af7c7bf21c2bc15e6dfebe12bbb259bad502ab5678ea74668aeb0525631a34"},                                                                                                                                                       // arrays.go
	227: {CSHA256: "01f287d402774053e6ca44fdcaa3157cd7f6c348df854f5098092cd87ab06724"},                                                                                                                                                       // model.go
	242: {CSHA256: "2951b8daf4fbbcd53df58a0715fd040b930ee9ecfae912ef3a3c987fc16b2653"},                                                                                                                                                       // model.go; then the CPI bounds (below)
	247: {CSHA256: "01f287d402774053e6ca44fdcaa3157cd7f6c348df854f5098092cd87ab06724"},                                                                                                                                                       // model.go
	270: {CSHA256: "01f287d402774053e6ca44fdcaa3157cd7f6c348df854f5098092cd87ab06724"},                                                                                                                                                       // model.go
	278: {CSHA256: "d0af7c7bf21c2bc15e6dfebe12bbb259bad502ab5678ea74668aeb0525631a34"},                                                                                                                                                       // arrays.go
	281: {CSHA256: "16142e17f43688e613ff873a0dfde14930f11661560529abb6c7a104d25ac748"},                                                                                                                                                       // pda.go; then the CPI bounds (below)
	283: {CSHA256: "01f287d402774053e6ca44fdcaa3157cd7f6c348df854f5098092cd87ab06724"},                                                                                                                                                       // model.go
	300: {CSHA256: "01f287d402774053e6ca44fdcaa3157cd7f6c348df854f5098092cd87ab06724"},                                                                                                                                                       // model.go
	302: {CSHA256: "20a027a31cf7ecee4e78344692d5e2a2ec2c30a6972827bf9a1ba00bb900f8d0"},                                                                                                                                                       // model.go; then the CPI bounds (below)
	309: {CSHA256: "01f287d402774053e6ca44fdcaa3157cd7f6c348df854f5098092cd87ab06724"},                                                                                                                                                       // model.go

	// Slice-to-array conversions (docs/GO_ARRAYS.md#slice-to-array-conversions,
	// results/compiler/2026-10-19-slice-array-conversions) compile.
	282: {CSHA256: "dba1a9b2bc23a517d41657a18b7dfd0b5bdcc531cdcb2594cb008c93aebb0acc"}, // arrays-reject.go `a:=[24]byte(s)`: was "array conversions require identical underlying arrays; slice conversions unsupported"
	// CPI bounds (docs/SDK2.md#cpi-bounds): no verdict changed. The SDK-2 C
	// runtime's sol_Invoke now allows 64 metas and 4 signer groups and is
	// never inlined, which changes the C of every recorded program that
	// includes it: the 15 entries above marked "the CPI bounds" and these.
	0:   {CSHA256: "1727cec46995666a495a458c870dc09a39999d3568eb2c56ec1ae3eb32268990"},
	69:  {CSHA256: "20ac5f3f22f5ced2dc9f15ba7a044960acf5dcc82512344172bb988820c61ab2"},
	75:  {CSHA256: "40ca71a5341383b89015c6eb3abda6be0168266436bf380a6d43dc7eb2b911ac"},
	95:  {CSHA256: "483aeb8d977f92fde8df69df3d7e6fc899bb9f33c7f5ded03066d47a3f4f80bc"},
	97:  {CSHA256: "da6e475c71a3bc3e043491d437671a37fc36a2648c537c9972c89d901fbb6809"},
	109: {CSHA256: "3c8db21e9def028d47a45fba621349190ff89bab48467e121decd649390d85f7"},
	129: {CSHA256: "769cf777dbd533eac1091ef4764276fd7e04a3c3b5707f0132090d54845b86fc"},
	144: {CSHA256: "50e50e516938ed118c6f2c8e06c1ed64bf0219d9cebaef75f94630e67419ef37"},
	152: {CSHA256: "4363f365bfe841379440ad9cb37fcecac61985311379d87c25c0b41075f8c23b"},
	158: {CSHA256: "b7a06bf33a1bf33cf8d147223c92257bb4e0ac9ea911300a87df452ef6fe8b09"},
	170: {CSHA256: "8f88245bddaccf43cf3a3915e643aa04fa9fddf0e71c95f7b5edbc5b7b813baf"},
	174: {CSHA256: "8ab3c4f648c4dd65996dd8e7b58e4ed2498d8e60857a7996f54724acf3966b06"},
	243: {CSHA256: "d6155fcce27285820eadc2b9b0fcee8738fee33d6e597a355581ca2c8e2dfb90"},
	252: {CSHA256: "b554c9adaec403339df872c13c09658fb3c39a2320294d4ec66f9ddd996b4b1a"},
	264: {CSHA256: "6fbb02e34c6b86c049d80e250e9aedea215ab1638d34a4cade1f6b70b856992c"},
	279: {CSHA256: "a490ff6230552064bb25a323f268729ea71e6d59fcf0b454724b2ceaa47f75d3"},
	294: {CSHA256: "07cb8f81bccffd7a03986d9b11e2cfd010d8033526cb02ad6228cba10e2c8cff"},
	306: {CSHA256: "593b0f353b68b5b19a2362135a4b2686534656e44984331b2f3a2af7156dc9a1"},
}

const (
	pointerElementRule = "pointer elements must be integer/bool scalars, value structs or scalar arrays"
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
	// Signed integers, generics, allocation, strings, function values, maps,
	// compound assignment, package-level variables, value types (struct
	// comparison, aliases, composite arrays), pointers to pointers and
	// variadic functions each turned recorded rejections into accepted
	// programs (corpusChanges); the rest (58) still exercise Check.
	if rejected < 57 {
		t.Fatalf("corpus exercises only %d rejections", rejected)
	}
	if changed != len(corpusChanges) {
		t.Fatalf("applied %d of %d corpus changes", changed, len(corpusChanges))
	}
}
