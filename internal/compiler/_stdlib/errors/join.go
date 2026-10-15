// Copyright 2022 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// goSVM on-chain overlay of Go 1.22.0 errors/join.go. Change (marked
// "goSVM:"): joinError.Error builds its text with internal/errtext instead
// of calling the Error methods of the joined errors itself, which would be
// recursion through the error interface when one is a *joinError (goSVM
// rejects recursion); the text is the same.

package errors

import "internal/errtext"

// Join returns an error that wraps the given errors.
// Any nil error values are discarded.
// Join returns nil if every value in errs is nil.
// The error formats as the concatenation of the strings obtained
// by calling the Error method of each element of errs, with a newline
// between each string.
//
// A non-nil error returned by Join implements the Unwrap() []error method.
func Join(errs ...error) error {
	n := 0
	for _, err := range errs {
		if err != nil {
			n++
		}
	}
	if n == 0 {
		return nil
	}
	e := &joinError{
		errs: make([]error, 0, n),
	}
	for _, err := range errs {
		if err != nil {
			e.errs = append(e.errs, err)
		}
	}
	return e
}

type joinError struct {
	errs []error
}

func (e *joinError) Error() string {
	// goSVM: Go's code, below, as errtext parts:
	//
	//	// Since Join returns nil if every value in errs is nil,
	//	// e.errs cannot be empty.
	//	if len(e.errs) == 1 {
	//		return e.errs[0].Error()
	//	}
	//
	//	b := []byte(e.errs[0].Error())
	//	for _, err := range e.errs[1:] {
	//		b = append(b, '\n')
	//		b = append(b, err.Error()...)
	//	}
	//	// At this point, b has at least one byte '\n'.
	//	return unsafe.String(&b[0], len(b))
	return errtext.Error(e)
}

// ErrorParts describes e's text to errtext: each joined error's text,
// separated by newlines.
//
// goSVM: not in Go (and not callable from outside the standard library).
func (e *joinError) ErrorParts(p *errtext.Parts) {
	for i, err := range e.errs {
		if i > 0 {
			p.Lit("\n")
		}
		p.Err(err)
	}
}

func (e *joinError) Unwrap() []error {
	return e.errs
}
