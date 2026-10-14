package compiler

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"go/parser"
	"go/token"
	"math/rand"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	genericvalues "gosvm/examples/generic-values"
)

// genericProgram builds a one-file program "g.go" in package path "program".
func genericSource(source string) *Program {
	return &Program{Entry: "program", Packages: []PackageSources{{"program", []Source{{Name: "g.go", Data: []byte("package p\n" + source)}}}}}
}

// genericCheck returns Check's full diagnostic, or "" when accepted, and
// requires CompileProgram to agree.
func genericCheck(t *testing.T, source string) string {
	t.Helper()
	_, err := Check(genericSource(source))
	_, compileErr := CompileProgram(genericSource(source))
	if (err == nil) != (compileErr == nil) || err != nil && err.Error() != compileErr.Error() {
		t.Fatalf("Check %v, CompileProgram %v", err, compileErr)
	}
	if err == nil {
		return ""
	}
	return err.Error()
}

const unsigned = "type Unsigned interface{ ~uint8 | ~uint16 | ~uint32 | ~uint64 }\n"

func TestGenericAcceptedForms(t *testing.T) {
	for name, source := range map[string]string{
		"inferred and explicit": unsigned + `func Min[T Unsigned](a, b T) T { if a < b { return a }; return b }
func Process(s, i []byte) uint64 { return Min(uint64(s[0]), 2) + uint64(Min[uint8](s[0], s[1])) + uint64(Min[uint16](3, 4)) }`,
		"type set operations": unsigned + `func Ops[T Unsigned](a, b T, n uint8) T { return (a+b-a*b)/(b|1)%(b|1) ^ a&^b | a<<n>>n ^ ^a ^ -b ^ T(7) }
func Process(s, i []byte) uint64 { return uint64(Ops(s[0], s[1], 3)) + uint64(Ops(uint32(1), 2, 3)) }`,
		"multiple type parameters": unsigned + `func Conv[T, U Unsigned](x T) U { return U(x) }
func Process(s, i []byte) uint64 { return Conv[uint8, uint64](s[0]) + uint64(Conv[uint64, uint16](9)) }`,
		"struct methods and pointers": unsigned + `type Stack[T Unsigned] struct { items [4]T; n uint64 }
func (s *Stack[T]) Push(v T) { s.items[s.n] = v; s.n++ }
func (s Stack[T]) Len() uint64 { return s.n }
func Fill[T Unsigned](s *Stack[T], v T) { s.Push(v); s.Push(v) }
func Process(s, i []byte) uint64 { var a Stack[uint8]; Fill(&a, s[0]); p := &Stack[uint64]{}; p.Push(1); return a.Len() + p.Len() }`,
		"generic named array": unsigned + `type V[T Unsigned] [4]T
func (v V[T]) First() T { return v[0] }
func Process(s, i []byte) uint64 { v := V[uint32]{1, 2}; return uint64(v.First()) + uint64(V[uint8]{s[0]}.First()) }`,
		"comparable": `type K [4]byte
func Eq[T comparable](a, b T) bool { return a == b }
func Process(s, i []byte) uint64 { if Eq(K{}, K{1}) || Eq(true, s[0] == 1) || Eq(s[0], 7) { return 1 }; return 0 }`,
		"any with pointers and byte slices": `type P struct{ a, b uint64 }
func Id[T any](x T) T { return x }
func Swap[T any](a, b *T) { *a, *b = *b, *a }
func Process(s, i []byte) uint64 { x, y := P{1, 2}, P{3, 4}; Swap(&x, &y); v := Id(s); return x.a + uint64(v[0]) + *Id(&y.b) }`,
		"method constraint": `type Sizer interface{ Size() uint64 }
type W uint16
func (w W) Size() uint64 { return uint64(w) }
type S struct{ n uint64 }
func (s *S) Size() uint64 { return s.n }
func Total[T Sizer](a, b T) uint64 { return a.Size() + b.Size() }
func Process(s, i []byte) uint64 { return Total(W(1), W(2)) + Total(&S{3}, &S{4}) }`,
		"nested instances": `type Box[T any] struct{ V T }
type Pair[A, B any] struct { A A; B B }
func (b Box[T]) Get() T { return b.V }
func Process(s, i []byte) uint64 { var b Box[Pair[uint8, Box[uint64]]]; b.V.B.V = 5; return b.Get().B.Get() }`,
		"generic struct field": `type Box[T any] struct{ V T }
type Holder struct { A Box[uint32]; B Box[[2]bool] }
func Process(s, i []byte) uint64 { var h Holder; h.B.V[1] = true; if h.B.V[1] { return uint64(h.A.V) + 1 }; return 0 }`,
		"constraint declarations alone": unsigned + "type Sizer interface{ Size() uint64 }\nfunc Process(s, i []byte) uint64 { return 0 }",
		// Uninstantiated generic code emits nothing and is only Go-type-checked.
		"uninstantiated body": `func G[T any](x T) string { return "unchecked" }
type Unused[T any] struct{ m map[uint64]T }
func Process(s, i []byte) uint64 { return 0 }`,
		"byte and uint8 instances": `type Box[T any] struct{ V T }
func (b Box[T]) Get() T { return b.V }
func Process(s, i []byte) uint64 { var a Box[byte]; var c Box[uint8]; c = a; return uint64(c.Get() + a.Get()) }`,
		"multiple results and views": unsigned + `func Split[T Unsigned](x T) (T, T) { return x >> 4, x & 15 }
func Head[T Unsigned](s []byte, n T) []byte { return s[:n] }
func Process(s, i []byte) uint64 { a, b := Split(s[0]); h := Head(s, uint32(2)); return uint64(a) + uint64(b) + uint64(len(h)) }`,
	} {
		t.Run(name, func(t *testing.T) {
			if got := genericCheck(t, source); got != "" {
				t.Fatal(got)
			}
		})
	}
}

func TestGenericRejections(t *testing.T) {
	limit := func(format string) string {
		var b strings.Builder
		b.WriteString("func Process(s, i []byte) uint64 {\n")
		for n := 1; n <= MaxInstantiations+1; n++ {
			fmt.Fprintf(&b, format+"\n", n)
		}
		b.WriteString("return 0 }\n")
		return b.String()
	}
	for name, c := range map[string]struct{ source, want string }{
		"string argument": {"func Id[T any](x T) T { return x }\nfunc Process(s, i []byte) uint64 { _ = Id(\"x\"); return 0 }",
			"g.go:3:40: instantiation Id[string]: type argument string for T: unsupported type string"},
		"inferred int argument": {"func Id[T any](x T) T { return x }\nfunc Process(s, i []byte) uint64 { _ = Id(1); return 0 }",
			"g.go:3:40: instantiation Id[int]: type argument int for T: use explicitly typed unsigned integers; int variables are unsupported"},
		"interface argument": {"func Id[T any](x T) T { return x }\nfunc Process(s, i []byte) uint64 { _ = Id[any](nil); return 0 }",
			"g.go:3:40: instantiation Id[any]: type argument any for T: interface values unsupported (any); interfaces are only usable as type parameter constraints"},
		"array argument limit": {"func Id[T any](x T) T { return x }\nfunc Process(s, i []byte) uint64 { _ = Id([600]uint16{}); return 0 }",
			"g.go:3:40: instantiation Id[[600]uint16]: type argument [600]uint16 for T: array storage exceeds experimental 1024-byte per-value limit"},
		"second argument": {"func Pick[A, B any](a A, b B) A { return a }\nfunc Process(s, i []byte) uint64 { return Pick(uint64(1), [1]int64{}) }",
			"g.go:3:43: instantiation Pick[uint64, [1]int64]: type argument [1]int64 for B: array elements must be byte, uint16, uint32, uint64, bool, or named versions of those scalars"},
		"type argument of a type": {"type Box[T any] struct{ V T }\nfunc Process(s, i []byte) uint64 { var b Box[string]; _ = b; return 0 }",
			"g.go:3:40: instantiation Box[string]: type argument string for T: unsupported type string"},
		"instance field rule": {"type Box[T any] struct{ V T }\nfunc Process(s, i []byte) uint64 { var b Box[[]byte]; _ = b; return 0 }",
			"g.go:3:40: instantiation Box[[]uint8]: g.go:3:40: struct fields must be unsigned scalars, bools, scalar arrays, or value structs"},
		"instance body rule": {"type P struct{ a uint64 }\nfunc Eq[T comparable](a, b T) bool { return a == b }\nfunc Process(s, i []byte) uint64 { if Eq(P{}, P{}) { return 1 }; return 0 }",
			"g.go:4:39: instantiation Eq[P]: g.go:3:45: struct comparisons unsupported; compare fields explicitly"},
		"instance borrow rule": {"func Ref[T any](x T) *T { return &x }\nfunc Process(s, i []byte) uint64 { return *Ref(uint64(1)) }",
			"g.go:3:44: instantiation Ref[uint64]: g.go:2:27: reference escapes local storage; heap allocation unsupported"},
		"instance variadic": {"func V[T any](xs ...T) {}\nfunc Process(s, i []byte) uint64 { V[uint64](); return 0 }",
			"g.go:3:36: instantiation V[uint64]: g.go:2:1: variadics are unsupported"},
		"direct recursion": {"func F[T any](x T) T { return F(x) }\nfunc Process(s, i []byte) uint64 { return F(uint64(1)) }",
			"g.go:2:1: recursion is not supported"},
		"recursion with other arguments": {unsigned + "func F[T Unsigned](x T) T { if x == 0 { return x }; return T(F[uint8](uint8(x - 1))) }\nfunc Process(s, i []byte) uint64 { return F(uint64(1)) }",
			"g.go:3:1: recursion is not supported"},
		"method recursion": {"type S[T any] struct{ v T }\nfunc (s S[T]) M() T { return s.M() }\nfunc Process(s, i []byte) uint64 { return S[uint64]{}.M() }",
			"g.go:3:1: recursion is not supported"},
		// Only the instance Total[S] calls S.Size: the cycle exists after
		// instantiation, through the method constraint.
		"recursion through a method constraint": {"type Sizer interface{ Size() uint64 }\ntype S struct{ n uint64 }\nfunc (s S) Size() uint64 { return Total(s) }\nfunc Total[T Sizer](x T) uint64 { return x.Size() }\nfunc Process(s, i []byte) uint64 { return Total(S{}) }",
			"g.go:4:1: recursion is not supported"},
		"mutual recursion": {"func F[T any](x T) T { return G(x) }\nfunc G[T any](x T) T { return F[T](x) }\nfunc Process(s, i []byte) uint64 { return F(uint64(1)) }",
			"g.go:2:1: recursion is not supported"},
		"function instantiation limit": {"func Id[T any](x T) T { return x }\n" + limit("_ = Id([%d]byte{})"),
			"g.go:68:5: too many instantiations of Id (limit 64)"},
		"method instantiation limit": {"type B[T any] struct{ v T }\nfunc (b B[T]) M() {}\n" + limit("B[[%d]byte]{}.M()"),
			"g.go:69:15: too many instantiations of B.M (limit 64)"},
		"type instantiation limit": {"type B[T any] struct{ v T }\n" + limit("_ = B[[%d]byte]{}"),
			"g.go:68:5: too many instantiations of B (limit 64)"},
		"interface variable": {"func Process(s, i []byte) uint64 { var x any; _ = x; return 0 }",
			"g.go:2:40: interface values unsupported (any); interfaces are only usable as type parameter constraints"},
		"interface parameter": {"type Sizer interface{ Size() uint64 }\nfunc f(x Sizer) uint64 { return 0 }\nfunc Process(s, i []byte) uint64 { return 0 }",
			"g.go:3:1: interface values unsupported (program.Sizer); interfaces are only usable as type parameter constraints"},
		"constraint violation": {unsigned + "func Min[T Unsigned](a, b T) T { return a }\nfunc Process(s, i []byte) uint64 { _ = Min(\"a\", \"b\"); return 0 }",
			"g.go:4:40: string does not satisfy Unsigned (string missing in ~uint8 | ~uint16 | ~uint32 | ~uint64)"},
		"generic function value": {"func Id[T any](x T) T { return x }\nfunc Process(s, i []byte) uint64 { f := Id[uint64]; return f(1) }",
			"g.go:3:41: unsupported identifier"},
		"generic method value": {"type B[T any] struct{ v T }\nfunc (b B[T]) Get() T { return b.v }\nfunc Process(s, i []byte) uint64 { f := B[uint64]{}.Get; return f() }",
			"g.go:4:41: method values unsupported; call the method directly"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := genericCheck(t, c.source); got != c.want {
				t.Fatalf("got  %q\nwant %q", got, c.want)
			}
		})
	}
}

// A library package's generic types and functions are instantiated with the
// importing package's types; the instance belongs to the generic's package.
func TestGenericCrossPackage(t *testing.T) {
	lib := `package lib
type Unsigned interface{ ~uint32 | ~uint64 }
type Stack[T any] struct { top, prev T; n uint64 }
func (s *Stack[T]) Push(v T) { s.prev, s.top = s.top, v; s.n++ }
func (s *Stack[T]) Top() T { return s.top }
func Max[T Unsigned](a, b T) T { if a > b { return a }; return b }
`
	app := `package app
import "lib"
type Order struct{ Price, Size uint64 }
func Process(s, i []byte) uint64 {
	var st lib.Stack[Order]
	st.Push(Order{Price: 3, Size: uint64(s[0])})
	return lib.Max(st.Top().Price, st.Top().Size) + uint64(lib.Max[uint32](1, 2))
}
`
	program := &Program{Entry: "app", Packages: []PackageSources{{"lib", []Source{{Name: "lib.go", Data: []byte(lib)}}}, {"app", []Source{{Name: "app.go", Data: []byte(app)}}}}}
	checked, err := Check(program)
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, inst := range checked.Instances {
		keys = append(keys, inst.Key)
	}
	want := []string{"lib.Stack[app.Order].Push", "lib.Stack[app.Order].Top", "lib.Max[uint32]", "lib.Max[uint64]"}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("instances %q", keys)
	}
	if len(checked.TypeInstances) != 1 || checked.TypeInstances[0].Key != "lib.Stack[app.Order]" || checked.TypeInstances[0].Name != "Stack[app.Order]" {
		t.Fatalf("type instances %+v", checked.TypeInstances)
	}
	out, err := CompileProgram(program)
	if err != nil {
		t.Fatal(err)
	}
	for _, symbol := range []string{"gosvm_ty_lib_DStack_Lapp_DOrder_R", "gosvm_fn_lib_DStack_Lapp_DOrder_R_DPush", "gosvm_fn_lib_DMax_Luint32_R"} {
		if !bytes.Contains(out, []byte(symbol)) {
			t.Errorf("C lacks %s", symbol)
		}
	}
}

// CheckedProgram lists each instantiation as an ordinary function, in place
// of its generic declaration, with its own callees and borrow summary.
func TestCheckedProgramInstances(t *testing.T) {
	checked, err := Check(genericSource(`type Box[T any] struct{ V T }
func (b *Box[T]) Ref() *T { return &b.V }
func Pick[T any](a T, s []byte) T { return a }
func Sum[T ~uint8 | ~uint64](a, b T) T { return a + b }
func Process(s, i []byte) uint64 {
	var b Box[uint64]
	*b.Ref() = Sum[uint64](1, 2)
	v := Pick(s, i)
	return *b.Ref() + uint64(Sum(v[0], Pick(s[1], v)))
}`))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	byName := map[string]*Function{}
	for _, fn := range checked.Functions {
		names = append(names, fn.Object.Name())
		byName[fn.Object.Name()] = fn
		if (fn.Instance != nil) != (fn.Object.Name() != "Process") {
			t.Errorf("%s: instance %v", fn.Object.Name(), fn.Instance)
		}
	}
	want := []string{"Box[uint64].Ref", "Pick[[]uint8]", "Pick[uint8]", "Sum[uint64]", "Sum[uint8]", "Process"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("functions %q", names)
	}
	if len(checked.Instances) != 5 || checked.Instances[1] != byName["Pick[[]uint8]"].Instance {
		t.Fatalf("instances %+v", checked.Instances)
	}
	pickBytes, pickByte := byName["Pick[[]uint8]"], byName["Pick[uint8]"]
	if pickBytes.Instance.Origin != pickByte.Instance.Origin || pickBytes.Instance.Origin.Name() != "Pick" || pickBytes.Decl == pickByte.Decl {
		t.Fatal("instances must share the origin but not the declaration")
	}
	if pickBytes.Decl.Type.TypeParams != nil || checked.Info.Defs[pickBytes.Decl.Name] != pickBytes.Object {
		t.Fatal("an instance declaration is non-generic and defines its object")
	}
	// Per-instance borrow summaries: a []byte result aliases its argument,
	// a byte result aliases nothing; the method result aliases its receiver.
	if got := pickBytes.Borrows.Results[0].Parameters; !reflect.DeepEqual(got, []int{0}) {
		t.Errorf("Pick[[]uint8] borrows %v", got)
	}
	if got := pickByte.Borrows.Results[0].Parameters; got != nil {
		t.Errorf("Pick[uint8] borrows %v", got)
	}
	if got := byName["Box[uint64].Ref"].Borrows.Results[0].Parameters; !reflect.DeepEqual(got, []int{0}) {
		t.Errorf("Box[uint64].Ref borrows %v", got)
	}
	process := byName["Process"]
	var callees []string
	for _, callee := range process.Callees {
		callees = append(callees, callee.Name())
	}
	if !reflect.DeepEqual(callees, []string{"Box[uint64].Ref", "Sum[uint64]", "Pick[[]uint8]", "Sum[uint8]", "Pick[uint8]"}) {
		t.Errorf("callees %q", callees)
	}
	if checked.CallOrder[len(checked.CallOrder)-1] != process {
		t.Error("Process must be last in call order")
	}
	// Explicit instantiation resolves through the index expression's name.
	explicit := 0
	for id, inst := range checked.Instantiated {
		if inst.Key == "program.Sum[uint64]" && id.Name == "Sum" {
			explicit++
		}
	}
	if explicit != 1 || len(checked.TypeInstances) != 1 || checked.TypeInstances[0].Symbol != "gosvm_ty_program_DBox_Luint64_R" {
		t.Errorf("explicit %d, type instances %+v", explicit, checked.TypeInstances)
	}
}

// byte and uint8 (rune and int32) are one type: Go keeps Box[byte] and
// Box[uint8] as distinct, identical instances, which share one key, one C
// struct, one field layout and one instance per method.
func TestGenericAliasSpellingsShareInstances(t *testing.T) {
	source := `type Box[T any] struct{ V T; N uint64 }
func (b *Box[T]) Set(v T) { b.V = v; b.N++ }
func Id[T any](x T) T { return x }
type Holder struct{ A Box[byte]; B Box[uint8] }
func conv(b Box[byte]) Box[uint8] { return b }
func Process(s, i []byte) uint64 {
	var h Holder
	h.A.Set(s[0])
	h.B.Set(uint8(1))
	h.B = conv(h.A)
	return uint64(Id(h.B.V)) + uint64(Id(byte(2))) + h.A.N + h.B.N
}`
	checked, err := Check(genericSource(source))
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, inst := range checked.Instances {
		keys = append(keys, inst.Key)
	}
	if !reflect.DeepEqual(keys, []string{"program.Box[uint8].Set", "program.Id[uint8]"}) || len(checked.TypeInstances) != 1 {
		t.Fatalf("instances %q, type instances %d", keys, len(checked.TypeInstances))
	}
	out, err := CompileProgram(genericSource(source))
	if err != nil {
		t.Fatal(err)
	}
	if n := bytes.Count(out, []byte("} gosvm_ty_program_DBox_Luint8_R;")); n != 1 {
		t.Fatalf("Box[uint8] typedefs: %d", n)
	}
	bin := hostProgram(t, "package p\n"+source, arrayHostDriver)
	state := make([]byte, 24)
	state[0] = 7
	if got := runHost(t, bin, fmt.Sprintf("%x %x\n", make([]byte, 16), state)); !strings.HasPrefix(got, "11:") {
		t.Fatalf("host result %q, want 7+2+1+1", got)
	}
}

var genericSymbol = regexp.MustCompile(`gosvm_(fn|ty)_\w+`)

func symbols(c []byte) []string {
	set := map[string]bool{}
	for _, s := range genericSymbol.FindAll(c, -1) {
		set[string(s)] = true
	}
	var out []string
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// Instance names derive only from the generic declaration and the type
// arguments: rebuilding is byte-identical, and reordering declarations keeps
// every instance and type-instance symbol (local temporaries are numbered in
// emission order, for generic and ordinary code alike).
func TestGenericNamesAreDeterministic(t *testing.T) {
	source, err := os.ReadFile("../../examples/generic-values/program.go")
	if err != nil {
		t.Fatal(err)
	}
	first, err := Compile("program.go", source)
	if err != nil {
		t.Fatal(err)
	}
	for j := 0; j < 3; j++ {
		again, err := Compile("program.go", source)
		if err != nil || !bytes.Equal(first, again) {
			t.Fatalf("rebuild differs: %v", err)
		}
	}
	// Reverse the order of the top-level declarations.
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "program.go", source, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	var reordered bytes.Buffer
	reordered.WriteString("package genericvalues\n")
	for j := len(file.Decls) - 1; j >= 0; j-- {
		d := file.Decls[j]
		reordered.Write(source[fset.Position(d.Pos()).Offset:fset.Position(d.End()).Offset])
		reordered.WriteString("\n")
	}
	second, err := Compile("program.go", reordered.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(first, second) {
		t.Fatal("reordering should change emission order")
	}
	a, b := symbols(first), symbols(second)
	if len(a) < 40 || !reflect.DeepEqual(a, b) {
		t.Fatalf("symbols changed with declaration order:\n%q\n%q", a, b)
	}
}

func TestMangleIsInjective(t *testing.T) {
	keys := []string{"a.F[uint8]", "a.F[uint8]_", "a.F_5buint8]", "a_DF", "a.DF", "a._DF", "a/b.F[x, y]", "a/b.F[x,y]", "a.S[*a.T].M", "a.S[a.T].M", "a.F[[2]uint8]", "é.F", "_xc3_xa9.F"}
	seen := map[string]string{}
	for _, k := range keys {
		m := mangle(k)
		if !regexp.MustCompile(`^[A-Za-z0-9_]+$`).MatchString(m) {
			t.Errorf("%q: %q is not a C identifier tail", k, m)
		}
		if other, ok := seen[m]; ok {
			t.Errorf("%q and %q both mangle to %q", k, other, m)
		}
		seen[m] = k
	}
}

// Programs without generics lower to their own files: no instance or type
// instance, and the lowered view is the original syntax.
func TestNoGenericsLowersToItself(t *testing.T) {
	checked, err := Check(genericSource("type P struct{ a uint64 }\nfunc (p P) A() uint64 { return p.a }\nfunc Process(s, i []byte) uint64 { return P{1}.A() }"))
	if err != nil {
		t.Fatal(err)
	}
	if len(checked.Instances) != 0 || len(checked.TypeInstances) != 0 || len(checked.Instantiated) != 0 {
		t.Fatal("unexpected instances")
	}
	for i, file := range checked.lowered {
		if file != checked.Files[i] {
			t.Fatal("lowering changed a file without generics")
		}
	}
}

func genericVectors() []oracleVector {
	rng := rand.New(rand.NewSource(1022))
	var vectors []oracleVector
	add := func(mode byte, a, b uint64, c byte) {
		state, instruction := make([]byte, 24), make([]byte, 16)
		rng.Read(state)
		instruction[0] = mode
		binary.LittleEndian.PutUint64(instruction[1:], a)
		// b overlaps a's top byte; the program reads both as written here.
		binary.LittleEndian.PutUint64(instruction[8:], b)
		instruction[15] = c
		vectors = append(vectors, oracleVector{state, instruction})
	}
	for j := 0; j < 800; j++ {
		add(byte(j%8), rng.Uint64()>>uint(rng.Intn(64)), rng.Uint64()>>uint(rng.Intn(64)), byte(rng.Intn(40)))
	}
	edges := []uint64{0, 1, 2, 7, 255, 256, 65535, 65536, 1 << 32, 1<<63 + 5, ^uint64(0)}
	for mode := byte(0); mode < 8; mode++ {
		for _, a := range edges {
			for _, b := range edges {
				for _, c := range []byte{0, 5, 16, 200} {
					add(mode, a, b, c)
				}
			}
		}
	}
	return vectors
}

// The generic oracle runs natively and as host-compiled generated C: every
// instance (four unsigned widths, structs, arrays, pointers, nested calls)
// must match Go's results, panics and state.
func TestGenericDifferential(t *testing.T) {
	differential(t, "../../examples/generic-values/program.go", genericvalues.Process, genericVectors())
}

func GenericVectorsForTest() [][2][]byte { return exportVectors(genericVectors()) }
