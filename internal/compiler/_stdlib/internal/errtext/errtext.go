// Copyright 2026 The goSVM Authors. Derived from the Go standard library;
// see ../../LICENSE.

// Package errtext builds the Error text of the standard library's wrapping
// errors without recursion. It is part of goSVM's on-chain overlay and not a
// Go package (docs/GO_STDLIB.md#intrinsics).
//
// In Go, (*errors.joinError).Error and (*strconv.NumError).Error call the
// Error method of the errors they wrap, which may be joinErrors or
// NumErrors themselves: recursion through the error interface, which the
// on-chain subset rejects. Here such errors are Nodes: they describe their
// text as literal parts and wrapped errors, and Error expands every Node it
// meets with an explicit stack, in the order Go's recursive calls would run.
// The Error methods of all other errors are called through a dispatch that
// excludes Nodes, so the call graph stays acyclic.
package errtext

// A Node is an error whose text consists of literal strings and the texts
// of other errors. Only the overlay can implement it: Parts is internal.
type Node interface {
	error
	ErrorParts(p *Parts)
}

// Parts collects a Node's text in order.
type Parts struct {
	items []part
}

// part is a literal (err == nil) or the text of err.
type part struct {
	lit    string
	err    error
	hasErr bool
}

// Lit appends a literal string.
func (p *Parts) Lit(s string) { p.items = append(p.items, part{lit: s}) }

// Err appends the Error text of err (a nil err aborts when its text is
// needed, as calling Error on a nil error panics in Go).
func (p *Parts) Err(err error) { p.items = append(p.items, part{err: err, hasErr: true}) }

// Error returns n's text: what n.Error() returns in Go.
func Error(n Node) string {
	var text []byte
	// pending holds, innermost last, the parts not yet written of each
	// Node being expanded.
	var root Parts
	n.ErrorParts(&root)
	pending := [][]part{root.items}
	for len(pending) > 0 {
		level := pending[len(pending)-1]
		if len(level) == 0 {
			pending = pending[:len(pending)-1]
			continue
		}
		item := level[0]
		pending[len(pending)-1] = level[1:]
		if !item.hasErr {
			text = append(text, item.lit...)
			continue
		}
		if nested, ok := item.err.(Node); ok {
			var p Parts
			nested.ErrorParts(&p)
			pending = append(pending, p.items)
			continue
		}
		text = append(text, leafError(item.err)...)
	}
	return unsafeString(text)
}

// leafError returns err.Error() for an err that is not a Node.
//
// goSVM: the directive makes this call dispatch to every Error method but
// those of the Node implementations, which cannot reach it.
//
//gosvm:dispatch-except internal/errtext.Node
func leafError(err error) string {
	return err.Error()
}

// unsafeString returns a string sharing b's bytes, as
// unsafe.String(unsafe.SliceData(b), len(b)) does: b must not change later.
//
// goSVM: the compiler replaces the body (a copy, which has the same value)
// with a conversion that shares the bytes.
//
//gosvm:intrinsic unsafeString
func unsafeString(b []byte) string {
	return string(b)
}
