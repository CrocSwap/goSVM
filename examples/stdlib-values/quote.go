package stdlibvalues

import "strconv"

// QuoteOps is the number of operations of the quote group.
const QuoteOps = 4

func quoteGroup(w *W, v Input) {
	a := v.a
	r := rune(v.h0) | rune(v.h1&0x1f)<<16
	if v.h2&1 == 1 {
		r = -r
	}
	switch v.op % QuoteOps {
	case 0: // quoting strings
		w.str(strconv.Quote(a))
		w.str(strconv.QuoteToASCII(a))
		w.str(strconv.QuoteToGraphic(a))
		w.bs(strconv.AppendQuote(fresh(v.b), a))
		w.bs(strconv.AppendQuoteToASCII(nil, a))
		w.bs(strconv.AppendQuoteToGraphic(fresh(v.b), a))
		w.flag(strconv.CanBackquote(a))
	case 1: // quoting runes and classifying them
		w.str(strconv.QuoteRune(r))
		w.str(strconv.QuoteRuneToASCII(r))
		w.str(strconv.QuoteRuneToGraphic(r))
		w.bs(strconv.AppendQuoteRune(nil, r))
		w.bs(strconv.AppendQuoteRuneToASCII(fresh(v.b), r))
		w.bs(strconv.AppendQuoteRuneToGraphic(nil, r))
		w.flag(strconv.IsPrint(r))
		w.flag(strconv.IsGraphic(r))
		for _, x := range a {
			w.flag(strconv.IsPrint(x))
			w.flag(strconv.IsGraphic(x))
		}
	case 2: // unquoting arbitrary text
		s, err := strconv.Unquote(a)
		w.str(s)
		w.err(err)
		p, err := strconv.QuotedPrefix(a)
		w.str(p)
		w.err(err)
		quote := byte(v.h3)
		switch v.h3 % 3 {
		case 0:
			quote = '"'
		case 1:
			quote = '\''
		}
		value, multibyte, tail, err := strconv.UnquoteChar(a, quote)
		w.u64(uint64(value))
		w.flag(multibyte)
		w.str(tail)
		w.err(err)
	case 3: // unquoting what was quoted, and the quoted form of b
		for _, q := range []string{strconv.Quote(a), strconv.QuoteToASCII(a), strconv.QuoteRune(r), "`" + a + "`", "'" + v.b + "'"} {
			s, err := strconv.Unquote(q)
			w.str(s)
			w.err(err)
		}
	}
}
