// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// goSVM on-chain overlay of Go 1.22.0 errors/wrap.go. Changes (marked
// "goSVM:"): Is asks the compiler whether target's dynamic type is
// comparable instead of internal/reflectlite; is and as walk an
// Unwrap() []error tree with an explicit stack instead of recursion (same
// depth-first order); As is generic over the target's element type instead
// of using reflection (docs/GO_STDLIB.md#errors).

package errors

// Unwrap returns the result of calling the Unwrap method on err, if err's
// type contains an Unwrap method returning error.
// Otherwise, Unwrap returns nil.
//
// Unwrap only calls a method of the form "Unwrap() error".
// In particular Unwrap does not unwrap errors returned by [Join].
func Unwrap(err error) error {
	u, ok := err.(interface {
		Unwrap() error
	})
	if !ok {
		return nil
	}
	return u.Unwrap()
}

// Is reports whether any error in err's tree matches target.
//
// The tree consists of err itself, followed by the errors obtained by repeatedly
// calling its Unwrap() error or Unwrap() []error method. When err wraps multiple
// errors, Is examines err followed by a depth-first traversal of its children.
//
// An error is considered to match a target if it is equal to that target or if
// it implements a method Is(error) bool such that Is(target) returns true.
//
// An error type might provide an Is method so it can be treated as equivalent
// to an existing error. For example, if MyError defines
//
//	func (m MyError) Is(target error) bool { return target == fs.ErrExist }
//
// then Is(MyError{}, fs.ErrExist) returns true. See [syscall.Errno.Is] for
// an example in the standard library. An Is method should only shallowly
// compare err and the target and not call [Unwrap] on either.
func Is(err, target error) bool {
	if target == nil {
		return err == target
	}

	isComparable := comparableType(target) // goSVM: reflectlite.TypeOf(target).Comparable()
	return is(err, target, isComparable)
}

func is(err, target error, targetComparable bool) bool {
	// goSVM: Go calls is recursively for each error an Unwrap() []error
	// method returns. pending holds, for each such level being visited, the
	// errors not yet visited, so the same tree is visited in the same order.
	var pending [][]error
	for {
		if targetComparable && err == target {
			return true
		}
		if x, ok := err.(interface{ Is(error) bool }); ok && x.Is(target) {
			return true
		}
		switch x := err.(type) {
		case interface{ Unwrap() error }:
			err = x.Unwrap()
			if err != nil {
				continue
			}
		case interface{ Unwrap() []error }:
			pending = append(pending, x.Unwrap())
		}
		// goSVM: this error's subtree has no match (Go's "return false");
		// continue with the next error of the innermost pending level.
		for {
			if len(pending) == 0 {
				return false
			}
			level := pending[len(pending)-1]
			if len(level) > 0 {
				err = level[0]
				pending[len(pending)-1] = level[1:]
				break
			}
			pending = pending[:len(pending)-1]
		}
	}
}

// As finds the first error in err's tree that matches target, and if one is found, sets
// target to that error value and returns true. Otherwise, it returns false.
//
// The tree consists of err itself, followed by the errors obtained by repeatedly
// calling its Unwrap() error or Unwrap() []error method. When err wraps multiple
// errors, As examines err followed by a depth-first traversal of its children.
//
// An error matches target if the error's concrete value is assignable to the value
// pointed to by target, or if the error has a method As(interface{}) bool such that
// As(target) returns true. In the latter case, the As method is responsible for
// setting target.
//
// An error type might provide an As method so it can be treated as if it were a
// different error type.
//
// As panics if target is not a non-nil pointer to either a type that implements
// error, or to any interface type.
//
// goSVM: Go declares As(err error, target any) and finds the type of
// *target with reflection. goSVM has no run-time type information, so As is
// generic over the element type T of the pointer the caller passes: the
// usual errors.As(err, &target) compiles unchanged, while a target whose
// static type is not a pointer (an any variable, nil) is a check error.
func As[T any](err error, target *T) bool {
	if err == nil {
		return false
	}
	if target == nil {
		trap("errors: target must be a non-nil pointer")
	}
	// goSVM: T is an interface type exactly when its zero value converts
	// to a nil any; otherwise T must implement error.
	var zero T
	boxed := any(zero)
	if _, isError := boxed.(error); boxed != nil && !isError {
		trap("errors: *target must be interface or implement error")
	}
	// goSVM: the remaining errors of each Unwrap() []error level (as in is).
	var pending [][]error
	for {
		// goSVM: reflectlite.TypeOf(err).AssignableTo(targetType). For an
		// interface T that is err's dynamic type implementing T; otherwise
		// T implements error, so it is assignable only when identical.
		if x, ok := err.(T); ok {
			*target = x
			return true
		}
		if x, ok := err.(interface{ As(any) bool }); ok && x.As(target) {
			return true
		}
		switch x := err.(type) {
		case interface{ Unwrap() error }:
			err = x.Unwrap()
			if err != nil {
				continue
			}
		case interface{ Unwrap() []error }:
			pending = append(pending, x.Unwrap())
		}
		// goSVM: no match in this error's subtree; continue with the next
		// non-nil error of the innermost pending level (Go skips nil ones).
		for {
			if len(pending) == 0 {
				return false
			}
			level := pending[len(pending)-1]
			if len(level) > 0 {
				err = level[0]
				pending[len(pending)-1] = level[1:]
				if err != nil {
					break
				}
				continue
			}
			pending = pending[:len(pending)-1]
		}
	}
}
