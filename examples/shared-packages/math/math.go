// Package math contains bounded arithmetic, with no runtime or SDK dependencies.
package math

func MulDiv(x, y, denominator uint64) uint64 {
	if denominator == 0 {
		return 0
	}
	return x * y / denominator
}
