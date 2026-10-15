package stdlibvalues

import "strings"

// BuilderOps is the number of operations of the builder group.
const BuilderOps = 3

// builderScript runs the operations p encodes on b and writes the Builder's
// length, capacity and contents after each.
func builderScript(w *W, b *strings.Builder, p []byte, v Input) {
	for j := 0; j+1 < len(p); j += 2 {
		arg := p[j+1]
		switch p[j] % 8 {
		case 0:
			n, err := b.WriteString(v.a[:int(arg)%(len(v.a)+1)])
			w.int(n)
			w.err(err)
		case 1:
			w.err(b.WriteByte(arg))
		case 2:
			n, err := b.WriteRune(runeFrom(int(arg)<<4, int(p[j])))
			w.int(n)
			w.err(err)
		case 3:
			n, err := b.Write([]byte(v.b[:int(arg)%(len(v.b)+1)]))
			w.int(n)
			w.err(err)
		case 4:
			b.Grow(int(arg % 70))
		case 5:
			if arg%4 == 0 {
				b.Reset()
			}
		case 6:
			s := b.String()
			w.str(s)
		case 7:
			w.int(b.Len())
		}
		w.int(b.Len())
		w.int(b.Cap())
	}
	w.str(b.String())
}

func builderGroup(w *W, v Input) {
	p := v.raw[8 : 8+2*(v.h0%24)]
	switch v.op % BuilderOps {
	case 0: // a script on a local Builder
		var b strings.Builder
		builderScript(w, &b, p, v)
	case 1: // a negative Grow panics
		var b strings.Builder
		b.WriteString(v.a)
		w.int(b.Cap())
		b.Grow(-1 - v.h1%3)
	case 2: // using a copy of a non-zero Builder panics; a copy of a zero
		// Builder is usable, and Reset makes a copy usable again
		var b strings.Builder
		if v.h1%3 != 0 {
			b.WriteString(v.a)
		}
		c := b
		if v.h1%3 == 2 {
			c.Reset()
		}
		w.u8(0x43)
		c.WriteString(v.b)
		w.str(c.String())
		w.str(b.String())
	}
}
