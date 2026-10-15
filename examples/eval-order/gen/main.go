// Command gen writes examples/eval-order/program.go: generated cases that
// combine every operand location a call can change (package variables,
// slice, array and map elements, fields and indirections through pointers,
// closure-captured and address-taken locals) with every expression and
// statement form, followed by the hand-written cases in special.go.txt.
//
//	go run ./examples/eval-order/gen
package main

import (
	_ "embed"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

//go:embed header.go.txt
var header string

//go:embed special.go.txt
var special string

// location is an operand read R and the writer calls that change it.
type location struct {
	read    string
	writers []string
	setup   string
	post    string
}

var locations = []location{
	{read: "g", writers: []string{"wG()"}},
	{read: "gs[0]", writers: []string{"wGs()", "wGs0()"}},
	{read: "gs[gi]", writers: []string{"wGi()", "wGs()"}},
	{read: "ga[gi]", writers: []string{"wGa()", "wGi()"}},
	{read: "gp.a", writers: []string{"wGp()", "wGpa()"}},
	{read: "(*gp).b", writers: []string{"wGp()"}},
	{read: "*gq", writers: []string{"wGq()", "wGqv()"}},
	{read: "gm[1]", writers: []string{"wGm()", "wGme()"}},
	{read: "gm[g%3]", writers: []string{"wG()", "wGm()"}},
	{read: "gt.a", writers: []string{"wGt()"}},
	{read: "uint64(gstr[1])", writers: []string{"wGstr()"}},
	{read: "uint64(len(gs))", writers: []string{"wApp()"}},
	{read: "c", writers: []string{"setC()"}, setup: "c := v1\nsetC := func() uint64 {\nstep()\nc = v2\nreturn v3\n}", post: "w.u64(c)"},
	{read: "x", writers: []string{"setPtr(px)"}, setup: "x := v1\npx := &x", post: "w.u64(x)"},
	{read: "la[1]", writers: []string{"setSl(ls)"}, setup: "la := [3]uint64{v1, v1, v1}\nls := la[:]", post: "w.u64(la[1])"},
	{read: "y", writers: []string{"wG()"}, setup: "y := v1"},
	{read: "gt.Val()", writers: []string{"wGt()"}},
}

// expressions are value forms of R and F.
var expressions = []string{
	"R + F", "F + R", "R*3 - F", "R/(F|1) + R", "R%(F|1) + R*7", "-R + F", "R << (F & 7)", "R/dz + F",
	"two(R, F)", "two(F, R)", "two(R, two(R, F))", "two(R+F, R)",
	"lit2(T{R, F})", "lit2(T{b: F, a: R})", "sl2([]uint64{R, F})", "ar2([2]uint64{R, F})",
	"mp2(map[uint64]uint64{1: R, 2: F})", "mp2(map[uint64]uint64{2: F, 1: R})",
	"b2u(R == v1 && F != 99)", "two(b2u(R == v1 || F == 99), R)", "b2u(R == v1) + F", "two(b2u(R != v1), F)",
	"uint64(uint8(R) + uint8(F))", "anyU(R, F)", "any8(uint8(R+0), F)", "any8(uint8(R), F)",
}

// statements are statement forms of R and F.
var statements = []string{
	"a, b := func() (uint64, uint64) {\nreturn R, F\n}()\nw.u64(a)\nw.u64(b)",
	"a, b := R, F\nw.u64(a)\nw.u64(b)",
	"var a, b = R, F\nw.u64(a)\nw.u64(b)",
	"func() {\ndefer rec(w, R, F)\n}()",
	"switch R {\ncase F:\nw.u64(1)\ncase v2:\nw.u64(2)\ndefault:\nw.u64(3)\n}",
	"switch R + F {\ncase v1 + v3:\nw.u64(1)\ncase v2 + v3:\nw.u64(2)\ndefault:\nw.u64(3)\n}",
	"if R+F == v2+v3 {\nw.u64(1)\n} else {\nw.u64(2)\n}",
	"n := uint64(0)\nfor R+F+n < v2+v3+2 && n < 4 {\nn++\n}\nw.u64(n)",
}

// targets are assignment storage L and the writers that change its operands.
var targets = []location{
	{read: "gs[gi]", writers: []string{"wGi()", "wGs()"}},
	{read: "gs[0]", writers: []string{"wGs()", "wGs0()"}},
	{read: "ga[gi]", writers: []string{"wGi()", "wGa()"}},
	{read: "gm[g%3]", writers: []string{"wG()", "wGm()"}},
	{read: "gm[uint64(gi)]", writers: []string{"wGi()", "wGm()"}},
	{read: "gp.a", writers: []string{"wGp()"}},
	{read: "(*gp).b", writers: []string{"wGp()"}},
	{read: "*gq", writers: []string{"wGq()"}},
	{read: "gt.a", writers: []string{"wGt()"}},
	{read: "la[gi]", writers: []string{"wGi()"}, setup: "la := [3]uint64{v1, v1, v1}", post: "w.u64(la[0])\nw.u64(la[1])\nw.u64(la[2])"},
	{read: "lp.a", writers: []string{"setLp()"}, setup: "lp := &T{v1, v1}\nold := lp\nsetLp := func() uint64 {\nstep()\nlp = &T{v2, v2}\nreturn v3\n}", post: "w.u64(old.a)\nw.u64(lp.a)"},
}

// assignments are forms of storage L and writer F.
var assignments = []string{
	"L = F", "L += F", "L = F + L", "L, aux = F, v1", "aux, L = v1, F", "L, aux = fpair(F)",
}

func main() {
	var cases []string
	add := func(setup, body, post string) {
		var b strings.Builder
		fmt.Fprintf(&b, "func c%d(w *W) {\n", len(cases))
		for _, part := range []string{setup, body, post} {
			if part != "" {
				b.WriteString(part + "\n")
			}
		}
		b.WriteString("}\n")
		cases = append(cases, b.String())
	}
	for _, l := range locations {
		for _, f := range l.writers {
			for _, e := range expressions {
				add(l.setup, "w.u64("+strings.NewReplacer("R", l.read, "F", f).Replace(e)+")", l.post)
			}
			for _, s := range statements {
				add(l.setup, strings.NewReplacer("R", l.read, "F", f).Replace(s), l.post)
			}
		}
	}
	for _, l := range targets {
		for _, f := range l.writers {
			for _, a := range assignments {
				body := strings.NewReplacer("L", l.read, "F", f).Replace(a)
				if strings.Contains(a, "aux") {
					body = "var aux uint64\n" + body + "\nw.u64(aux)"
				}
				add(l.setup, body, l.post)
			}
		}
	}
	var out strings.Builder
	out.WriteString(header)
	fmt.Fprintf(&out, "\n// generatedCases is the number of generated cases.\nconst generatedCases = %d\n\n", len(cases))
	out.WriteString("func generated(w *W, k int) {\nswitch k {\n")
	for i := range cases {
		fmt.Fprintf(&out, "case %d:\nc%d(w)\n", i, i)
	}
	out.WriteString("}\n}\n\n")
	for _, c := range cases {
		out.WriteString(c + "\n")
	}
	out.WriteString(special)
	_, file, _, _ := runtime.Caller(0)
	path := filepath.Join(filepath.Dir(file), "..", "program.go")
	src, err := format.Source([]byte(out.String()))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.WriteFile(path, src, 0644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
