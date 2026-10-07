// Package model contains reusable value types and caller-borrowing methods.
package model

type Key [4]byte
type Small struct{ N uint64 }
type Box struct {
	Key   Key
	N     uint64
	Child Small
}
type Counter uint64

func (b Box) Value(x uint64) uint64 { b.N = b.N + x; return b.N + uint64(b.Key[0]) + b.Child.N }
func (b *Box) Add(x uint64) *uint64 {
	if b == nil {
		return nil
	}
	b.N = b.N + x
	b.N++
	b.N--
	return &b.N
}
func (b *Box) Self() *Box { return b }
func (b *Box) Nullable(x uint64) uint64 {
	if b == nil {
		return x + 1
	}
	return b.N + x
}
func (k *Key) Set(index uint64, value byte) { k[index] = value }
func (c *Counter) Increase(n uint64) Counter {
	*c = Counter(uint64(*c) + n)
	*c++
	*c--
	return *c
}
func borrow(p *uint64, n uint64) (*uint64, uint64)  { return p, n + 1 }
func Forward(p *uint64, n uint64) (*uint64, uint64) { return borrow(p, n) }
