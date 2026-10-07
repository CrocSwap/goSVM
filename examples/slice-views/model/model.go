// Package model exposes reusable views that borrow from caller storage.
package model

type Key [32]byte
type Box struct{ Key Key }

func Tail(s []byte, low uint64) []byte                 { return s[low:] }
func Forward(s []byte, low uint64) ([]byte, uint64)    { return Tail(s, low), uint64(cap(s)) }
func (b *Box) Window(low, high, maximum uint64) []byte { return b.Key[low:high:maximum] }
func FromKey(k *Key, low uint64) []byte                { return k[low:] }
