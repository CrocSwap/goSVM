// Package alpha imports log and zeta: a struct variable with methods,
// pointers to package storage, and init functions in two files.
package alpha

import (
	"gosvm/examples/global-values/log"
	"gosvm/examples/global-values/zeta"
)

// State is a struct held in a package-level variable.
type State struct {
	N    uint64
	Hist [4]uint8
	Last *uint64
	Tags []string
}

// Bump changes the state through a pointer receiver.
func (s *State) Bump(k uint64) {
	s.N = s.N + k
	s.Hist[k&3]++
	Counter++
}

// Total reads through a value receiver.
func (s State) Total() uint64 { return s.N + Counter }

var Counter uint64
var S State
var P = &S // a pointer to package storage
var Q = new(uint64)
var A1 = log.Mark('a') + byte(zeta.Hook(1)) // zeta is initialized already
var Bound = S.Bump                          // a method value bound to &S

func init() {
	log.Mark('A')
	S.Last = Q
	*Q = 40
	S.Tags = append(S.Tags, "init")
}
