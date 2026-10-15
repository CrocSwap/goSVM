// Package zeta imports only log. Go 1.21 initializes packages by import
// path once their imports are done, so beta runs before zeta, and zeta
// before alpha (which imports it).
package zeta

import "gosvm/examples/global-values/log"

// Z is initialized by a call; Base is a never-written constant (read-only
// data).
var Z = log.Mark('z')
var Base uint64 = 7

// Hook is set by init to a closure over the package's variables.
var Hook func(uint64) uint64

// Calls counts Hook calls (written by the closure).
var Calls uint32

func init() {
	log.Mark('Z')
	Hook = func(x uint64) uint64 {
		Calls++
		return x*Base + uint64(Z)
	}
}
